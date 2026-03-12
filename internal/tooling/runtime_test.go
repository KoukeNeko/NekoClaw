package tooling

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type fakeToolProvider struct {
	id        string
	turns     []provider.ToolTurnResponse
	calls     int
	support   bool
	lastTools []provider.ToolDefinition
}

func (f *fakeToolProvider) ID() string { return f.id }

func (f *fakeToolProvider) ContextWindow(_ string) int { return 100000 }

func (f *fakeToolProvider) Generate(_ context.Context, _ provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{Text: "unused"}, nil
}

func (f *fakeToolProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{SupportsTools: f.support}
}

func (f *fakeToolProvider) GenerateToolTurn(_ context.Context, req provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	f.lastTools = append([]provider.ToolDefinition(nil), req.Tools...)
	if f.calls >= len(f.turns) {
		return provider.ToolTurnResponse{Text: "done"}, nil
	}
	resp := f.turns[f.calls]
	f.calls++
	return resp, nil
}

type fakeExecutor struct {
	mutating         map[string]bool
	readOnlySafe     map[string]bool
	requiresApproval map[string]bool
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

func (e *fakeExecutor) IsReadOnlySafe(toolName string) bool {
	return e.readOnlySafe[toolName]
}

func (e *fakeExecutor) RequiresApproval(call provider.ToolCall) bool {
	if e.requiresApproval == nil {
		return e.mutating[call.Name]
	}
	return e.requiresApproval[call.Name]
}

func (e *fakeExecutor) Definitions() []provider.ToolDefinition {
	names := []string{"providers_list", "file_write", "memory_search", "memory_get", "memory_save", "task_update"}
	out := make([]provider.ToolDefinition, 0, len(names))
	for _, name := range names {
		out = append(out, provider.ToolDefinition{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)})
	}
	return out
}

func (e *fakeExecutor) HasTool(toolName string) bool {
	if _, ok := e.mutating[toolName]; ok {
		return true
	}
	if _, ok := e.readOnlySafe[toolName]; ok {
		return true
	}
	_, ok := e.requiresApproval[toolName]
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
		Surface:      core.SurfaceWeb,
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
		Surface:      core.SurfaceWeb,
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
		Surface:      core.SurfaceWeb,
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
		Surface:      core.SurfaceWeb,
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

func TestRuntimeAllowedToolsFiltersMutatingSchemas(t *testing.T) {
	executor := &fakeExecutor{mutating: map[string]bool{
		"providers_list": false,
		"file_write":     true,
	}, readOnlySafe: map[string]bool{"providers_list": true}, requiresApproval: map[string]bool{"file_write": true}}
	store := NewApprovalStore(0)
	rt := NewRuntime(executor, store)
	fakeProv := &fakeToolProvider{id: "anthropic", support: true}

	_, err := rt.Run(context.Background(), RunRequest{
		SessionID:    "planner-1",
		Surface:      core.SurfaceWeb,
		ProviderID:   "anthropic",
		ModelID:      "claude-sonnet-4-5",
		Account:      core.Account{ID: "a1"},
		ToolProvider: fakeProv,
		Messages:     []core.Message{{Role: core.RoleUser, Content: "plan"}},
		UserMessage:  core.Message{Role: core.RoleUser, Content: "plan"},
		EnableTools:  true,
		AllowedTools: []string{"providers_list"},
	})
	if err != nil {
		t.Fatalf("planner run failed: %v", err)
	}
	if len(fakeProv.lastTools) != 1 || fakeProv.lastTools[0].Name != "providers_list" {
		t.Fatalf("unexpected filtered tool definitions: %#v", fakeProv.lastTools)
	}
}

func TestRuntimeDefaultActionToolNamesExposeMemoryTools(t *testing.T) {
	rt := NewRuntime(NewRuntimeExecutor(stubToolBackend{}, DefaultPolicy(t.TempDir()), ExecutorConfig{}), NewApprovalStore(0))
	got := rt.DefaultActionToolNames()
	required := []string{"memory_search", "memory_get", "memory_save"}
	for _, name := range required {
		found := false
		for _, item := range got {
			if item == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("default action tools missing %q: %v", name, got)
		}
	}
}

func TestRuntimeReadOnlyToolNamesIncludeMemoryReadButExcludeMemorySave(t *testing.T) {
	rt := NewRuntime(NewRuntimeExecutor(stubToolBackend{}, DefaultPolicy(t.TempDir()), ExecutorConfig{}), NewApprovalStore(0))
	got := rt.ReadOnlyToolNames()
	wantPresent := []string{"memory_search", "memory_get"}
	wantMissing := []string{"memory_save", "task_update", "exec_command"}
	for _, name := range wantPresent {
		found := false
		for _, item := range got {
			if item == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("read-only tools missing %q: %v", name, got)
		}
	}
	for _, name := range wantMissing {
		for _, item := range got {
			if item == name {
				t.Fatalf("read-only tools unexpectedly contain %q: %v", name, got)
			}
		}
	}
}

func TestRuntimeMemorySaveCompletesWithoutApproval(t *testing.T) {
	executor := &fakeExecutor{
		mutating:         map[string]bool{"memory_save": true},
		readOnlySafe:     map[string]bool{"memory_search": true, "memory_get": true},
		requiresApproval: map[string]bool{"memory_save": false},
	}
	rt := NewRuntime(executor, NewApprovalStore(0))
	fakeProv := &fakeToolProvider{
		id:      "anthropic",
		support: true,
		turns: []provider.ToolTurnResponse{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "memory_save", Arguments: json.RawMessage(`{"content":"remember this"}`)},
				},
			},
			{Text: "saved"},
		},
	}

	prepareCalled := false
	result, err := rt.Run(context.Background(), RunRequest{
		SessionID:    "memory-save",
		Surface:      core.SurfaceWeb,
		ProviderID:   "anthropic",
		ModelID:      "default",
		Account:      core.Account{ID: "a1"},
		ToolProvider: fakeProv,
		Messages:     []core.Message{{Role: core.RoleUser, Content: "remember this"}},
		UserMessage:  core.Message{Role: core.RoleUser, Content: "remember this"},
		EnableTools:  true,
		PrepareMutation: func(sessionID string, call provider.ToolCall, snapshotID string) (*core.SnapshotRecord, error) {
			prepareCalled = true
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.Pending {
		t.Fatalf("memory_save should not require approval")
	}
	if result.Response.Status != core.ChatStatusCompleted {
		t.Fatalf("status = %s, want completed", result.Response.Status)
	}
	if result.Response.Reply != "saved" {
		t.Fatalf("reply = %q, want saved", result.Response.Reply)
	}
	if prepareCalled {
		t.Fatalf("memory_save should not trigger snapshot preparation")
	}
}

func TestRuntimeMutatingToolRequiresApprovalOnDiscord(t *testing.T) {
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
		},
	}
	result, err := rt.Run(context.Background(), RunRequest{
		SessionID:    "discord-1",
		Surface:      core.SurfaceDiscord,
		ProviderID:   "anthropic",
		ModelID:      "claude-sonnet-4-5",
		Account:      core.Account{ID: "a1"},
		ToolProvider: fakeProv,
		Messages:     []core.Message{{Role: core.RoleUser, Content: "write"}},
		UserMessage:  core.Message{Role: core.RoleUser, Content: "write"},
		EnableTools:  true,
	})
	if err != nil {
		t.Fatalf("discord run failed: %v", err)
	}
	if !result.Pending || result.Response.Status != core.ChatStatusApprovalRequired {
		t.Fatalf("expected discord approval_required, got pending=%v status=%s", result.Pending, result.Response.Status)
	}
}

func TestRuntimeLookupGrantAutoApprovesMutatingTool(t *testing.T) {
	executor := &fakeExecutor{mutating: map[string]bool{
		"file_write": true,
	}}
	rt := NewRuntime(executor, NewApprovalStore(0))
	fakeProv := &fakeToolProvider{
		id:      "anthropic",
		support: true,
		turns: []provider.ToolTurnResponse{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "file_write", Arguments: json.RawMessage(`{"path":"a.txt","content":"x"}`)},
				},
			},
			{Text: "auto-approved"},
		},
	}

	result, err := rt.Run(context.Background(), RunRequest{
		SessionID:    "s-grant",
		Surface:      core.SurfaceWeb,
		ProviderID:   "anthropic",
		ModelID:      "default",
		Account:      core.Account{ID: "a1"},
		ToolProvider: fakeProv,
		Messages:     []core.Message{{Role: core.RoleUser, Content: "write"}},
		UserMessage:  core.Message{Role: core.RoleUser, Content: "write"},
		EnableTools:  true,
		SelectorForCall: func(call provider.ToolCall) string {
			return "/tmp/a.txt"
		},
		LookupGrant: func(sessionID, toolName, selector string) (string, bool) {
			return "allow_for_session", true
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.Pending {
		t.Fatalf("expected auto-approved run, got pending")
	}
	if result.Response.Reply != "auto-approved" {
		t.Fatalf("unexpected reply: %q", result.Response.Reply)
	}
}

func TestValidateStaleReadDetectsExternalChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	hash, err := hashFileState(path)
	if err != nil {
		t.Fatalf("hashFileState(before): %v", err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}
	err = validateStaleRead(
		provider.ToolCall{Name: "file_write"},
		path,
		map[string]string{path: hash},
		map[string]struct{}{},
	)
	if err == nil {
		t.Fatalf("expected stale-read error")
	}
}
