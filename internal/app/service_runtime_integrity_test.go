package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type fakeCriticProvider struct{}

func (p *fakeCriticProvider) ID() string { return "critic-provider" }

func (p *fakeCriticProvider) ContextWindow(string) int { return 200_000 }

func (p *fakeCriticProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{Text: "unused:" + req.Messages[len(req.Messages)-1].Content}, nil
}

func (p *fakeCriticProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{SupportsTools: true}
}

func (p *fakeCriticProvider) GenerateToolTurn(_ context.Context, _ provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	return provider.ToolTurnResponse{
		Text: strings.TrimSpace(`
## Findings
- Add a regression test.

## Missing Verification
- go test ./...

## Risks
- edge cases
`),
	}, nil
}

func TestWorkflowToolCallUsesRuntimeAndApprovalGateIsRejected(t *testing.T) {
	workspace := t.TempDir()
	snapshotStore, err := core.NewSnapshotStore(filepath.Join(t.TempDir(), "snapshots"))
	if err != nil {
		t.Fatalf("snapshot store: %v", err)
	}
	workflowStore, err := core.NewWorkflowStore(filepath.Join(t.TempDir(), "workflows"))
	if err != nil {
		t.Fatalf("workflow store: %v", err)
	}
	workflowRunStore, err := core.NewWorkflowRunStore(filepath.Join(t.TempDir(), "workflow-runs"))
	if err != nil {
		t.Fatalf("workflow run store: %v", err)
	}
	permissionStore, err := core.NewPermissionStore(filepath.Join(t.TempDir(), "permissions"))
	if err != nil {
		t.Fatalf("permission store: %v", err)
	}
	svc := NewService(ServiceOptions{
		WorkspaceRoot:    workspace,
		SnapshotStore:    snapshotStore,
		WorkflowStore:    workflowStore,
		WorkflowRunStore: workflowRunStore,
		PermissionStore:  permissionStore,
	})

	fullPath := filepath.Join(workspace, "wf.txt")
	if err := svc.SavePermissions([]core.PermissionGrant{{
		WorkspaceRoot: workspaceScopeValue(workspace),
		ToolName:      "file_write",
		Selector:      fullPath,
		Decision:      "allow_for_workspace",
	}}); err != nil {
		t.Fatalf("SavePermissions() error = %v", err)
	}

	record, err := svc.SaveWorkflow(core.WorkflowRecord{
		Name:        "tool-step",
		SessionID:   "wf-session",
		Surface:     core.SurfaceWeb,
		Enabled:     true,
		TriggerKind: core.WorkflowTriggerWebhook,
		Steps: []core.WorkflowStep{{
			ID:       "step-1",
			Kind:     core.WorkflowStepToolCall,
			ToolName: "file_write",
			ToolArgs: json.RawMessage(`{"path":"wf.txt","content":"after"}`),
		}},
	})
	if err != nil {
		t.Fatalf("SaveWorkflow() error = %v", err)
	}

	run, err := svc.RunWorkflow(context.Background(), record.ID, nil)
	if err != nil {
		t.Fatalf("RunWorkflow() error = %v", err)
	}
	if run.Status != core.WorkflowRunCompleted {
		t.Fatalf("workflow status = %s, want completed", run.Status)
	}
	if got := string(mustReadAppFile(t, fullPath)); got != "after" {
		t.Fatalf("workflow file = %q, want after", got)
	}
	if got := len(svc.ListSnapshots("wf-session")); got != 1 {
		t.Fatalf("snapshots = %d, want 1", got)
	}

	if _, err := svc.SaveWorkflow(core.WorkflowRecord{
		Name:        "bad",
		Enabled:     true,
		TriggerKind: core.WorkflowTriggerWebhook,
		Steps: []core.WorkflowStep{{
			Kind: core.WorkflowStepApprovalGate,
		}},
	}); err == nil || !strings.Contains(err.Error(), "unsupported_step_kind") {
		t.Fatalf("SaveWorkflow() error = %v, want unsupported_step_kind", err)
	}
}

func TestFinalizePlanExecutionResponseRunsCriticAndAttachesMetadata(t *testing.T) {
	svc := NewService(ServiceOptions{})
	svc.RegisterProvider(&fakeCriticProvider{})
	svc.RegisterPool(core.NewAccountPool("critic-provider", []core.Account{{
		ID:       "critic-main",
		Provider: "critic-provider",
		Type:     core.AccountToken,
		Token:    "token",
	}}, nil, core.DefaultCooldownConfig()))
	svc.SetModelRolesConfig(core.ModelRolesConfig{
		Action: core.ModelRoleConfig{Provider: "critic-provider", Model: "default"},
		Critic: core.ModelRoleConfig{Provider: "critic-provider", Model: "default"},
	})

	plan, err := svc.plans.Create(core.PlanRecord{
		SessionID:     "plan-session",
		Surface:       core.SurfaceWeb,
		Provider:      "critic-provider",
		Model:         "default",
		Status:        core.PlanStatusExecuting,
		RequestPrompt: "ship the change",
		PlanMarkdown:  "## Goal\nShip the change",
		PlanSummary:   "Ship the change",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	svc.sessions.AppendMessage("plan-session", core.Message{
		Role:      core.RoleAssistant,
		Content:   "execution done",
		CreatedAt: nowForTest(),
	})

	record, resp, note, err := svc.finalizePlanExecutionResponse(plan.ID, core.ChatResponse{
		SessionID: "plan-session",
		Provider:  "critic-provider",
		Model:     "default",
		Reply:     "execution done",
		Status:    core.ChatStatusCompleted,
	})
	if err != nil {
		t.Fatalf("finalizePlanExecutionResponse() error = %v", err)
	}
	if record.Status != core.PlanStatusCompleted {
		t.Fatalf("plan status = %s, want completed", record.Status)
	}
	if len(resp.SubagentArtifacts) != 1 {
		t.Fatalf("subagent artifacts = %d, want 1", len(resp.SubagentArtifacts))
	}
	if !strings.Contains(note, "## Findings") {
		t.Fatalf("critic note missing findings: %q", note)
	}
	entries := svc.sessions.History("plan-session")
	last := entries[len(entries)-1]
	if len(last.MsgSubagentArtifacts) != 1 {
		t.Fatalf("assistant metadata artifacts = %d, want 1", len(last.MsgSubagentArtifacts))
	}
}

func mustReadAppFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func nowForTest() time.Time {
	return time.Now().UTC()
}
