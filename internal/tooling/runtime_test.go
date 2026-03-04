package tooling

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type fakeToolProvider struct {
	id      string
	turns   []provider.ToolTurnResponse
	calls   int
	support bool
}

func (f *fakeToolProvider) ID() string { return f.id }

func (f *fakeToolProvider) ContextWindow(_ string) int { return 100000 }

func (f *fakeToolProvider) Generate(_ context.Context, _ provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{Text: "unused"}, nil
}

func (f *fakeToolProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{SupportsTools: f.support}
}

func (f *fakeToolProvider) GenerateToolTurn(_ context.Context, _ provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	if f.calls >= len(f.turns) {
		return provider.ToolTurnResponse{Text: "done"}, nil
	}
	resp := f.turns[f.calls]
	f.calls++
	return resp, nil
}

type fakeExecutor struct {
	mutating map[string]bool
}

func (e *fakeExecutor) Run(_ context.Context, call provider.ToolCall) (string, error) {
	return "ok:" + call.Name, nil
}

func (e *fakeExecutor) IsMutating(toolName string) bool {
	return e.mutating[toolName]
}

func (e *fakeExecutor) IsCallMutating(call provider.ToolCall) bool {
	return e.mutating[call.Name]
}

func (e *fakeExecutor) Definitions() []provider.ToolDefinition {
	return []provider.ToolDefinition{
		{Name: "providers_list", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "file_write", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
}

func (e *fakeExecutor) HasTool(toolName string) bool {
	_, ok := e.mutating[toolName]
	return ok
}

func (e *fakeExecutor) ArgumentPreview(call provider.ToolCall) string {
	return string(call.Arguments)
}

func TestRuntimeReadOnlyToolAutoExecutes(t *testing.T) {
	executor := &fakeExecutor{mutating: map[string]bool{
		"providers_list": false,
	}}
	store := NewApprovalStore(0)
	rt := NewRuntime(executor, store)
	fakeProv := &fakeToolProvider{
		id:      "anthropic",
		support: true,
		turns: []provider.ToolTurnResponse{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "c1", Name: "providers_list", Arguments: json.RawMessage(`{}`)},
				},
			},
			{
				Text: "done",
			},
		},
	}
	result, err := rt.Run(context.Background(), RunRequest{
		SessionID:    "s1",
		Surface:      core.SurfaceTUI,
		ProviderID:   "anthropic",
		ModelID:      "claude-sonnet-4-5",
		Account:      core.Account{ID: "a1"},
		ToolProvider: fakeProv,
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hello"},
		},
		UserMessage: core.Message{Role: core.RoleUser, Content: "hello"},
		EnableTools: true,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.Pending {
		t.Fatalf("expected completed run, got pending")
	}
	if result.Response.Status != core.ChatStatusCompleted {
		t.Fatalf("unexpected status: %s", result.Response.Status)
	}
	if result.Response.Reply != "done" {
		t.Fatalf("unexpected reply: %q", result.Response.Reply)
	}
}

func TestRuntimeMutatingToolRequiresApprovalAndResume(t *testing.T) {
	executor := &fakeExecutor{mutating: map[string]bool{
		"file_write": true,
	}}
	store := NewApprovalStore(0)
	rt := NewRuntime(executor, store)
	fakeProv := &fakeToolProvider{
		id:      "anthropic",
		support: true,
		turns: []provider.ToolTurnResponse{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "file_write", Arguments: json.RawMessage(`{"path":"a.txt","content":"x"}`)},
				},
			},
			{
				Text: "write complete",
			},
		},
	}
	first, err := rt.Run(context.Background(), RunRequest{
		SessionID:    "s2",
		Surface:      core.SurfaceTUI,
		ProviderID:   "anthropic",
		ModelID:      "claude-sonnet-4-5",
		Account:      core.Account{ID: "a1"},
		ToolProvider: fakeProv,
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "write"},
		},
		UserMessage: core.Message{Role: core.RoleUser, Content: "write"},
		EnableTools: true,
	})
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if !first.Pending {
		t.Fatalf("expected pending approval")
	}
	if first.Response.Status != core.ChatStatusApprovalRequired {
		t.Fatalf("unexpected status: %s", first.Response.Status)
	}
	if first.Response.RunID == "" {
		t.Fatalf("missing run id")
	}
	if len(first.Response.PendingApprovals) != 1 {
		t.Fatalf("expected one pending approval, got %d", len(first.Response.PendingApprovals))
	}

	second, err := rt.Run(context.Background(), RunRequest{
		SessionID:    "s2",
		Surface:      core.SurfaceTUI,
		ProviderID:   "anthropic",
		ModelID:      "claude-sonnet-4-5",
		Account:      core.Account{ID: "a1"},
		ToolProvider: fakeProv,
		EnableTools:  true,
		RunID:        first.Response.RunID,
		Approvals: []core.ToolApprovalDecision{
			{ApprovalID: "call-1", Decision: "allow"},
		},
	})
	if err != nil {
		t.Fatalf("resume run failed: %v", err)
	}
	if second.Pending {
		t.Fatalf("expected completed run on resume")
	}
	if second.Response.Status != core.ChatStatusCompleted {
		t.Fatalf("unexpected status: %s", second.Response.Status)
	}
	if second.Response.Reply != "write complete" {
		t.Fatalf("unexpected reply: %q", second.Response.Reply)
	}
}

func TestRuntimeReturnsToolsNotSupported(t *testing.T) {
	executor := &fakeExecutor{mutating: map[string]bool{}}
	store := NewApprovalStore(0)
	rt := NewRuntime(executor, store)
	fakeProv := &fakeToolProvider{id: "mock", support: false}
	_, err := rt.Run(context.Background(), RunRequest{
		SessionID:    "s3",
		Surface:      core.SurfaceTUI,
		ProviderID:   "mock",
		ModelID:      "default",
		Account:      core.Account{ID: "a1"},
		ToolProvider: fakeProv,
		EnableTools:  true,
	})
	if err == nil {
		t.Fatalf("expected tools_not_supported error")
	}
}
