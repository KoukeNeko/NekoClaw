package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type captureToolTurnProvider struct {
	id string

	mu       sync.Mutex
	requests []provider.ToolTurnRequest
}

func (p *captureToolTurnProvider) ID() string { return p.id }

func (p *captureToolTurnProvider) ContextWindow(string) int { return 200_000 }

func (p *captureToolTurnProvider) Generate(_ context.Context, _ provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{Text: "unused"}, nil
}

func (p *captureToolTurnProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{SupportsTools: true}
}

func (p *captureToolTurnProvider) GenerateToolTurn(_ context.Context, req provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	return provider.ToolTurnResponse{
		Text: "## Goal\nplan\n## Context\nctx\n## Files to Modify\n- a\n## New Files\n- none\n## Implementation Steps\n- step\n## Verification\n- test\n## Risks\n- risk",
	}, nil
}

func (p *captureToolTurnProvider) lastRequest() provider.ToolTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return provider.ToolTurnRequest{}
	}
	return p.requests[len(p.requests)-1]
}

func TestResolveModelRoleFallsBackToDefaultActionConfig(t *testing.T) {
	svc := NewService(ServiceOptions{})
	svc.SetDefaultProvider("google-ai-studio")
	svc.SetDefaultModel("gemini-2.5-pro")
	svc.SetDefaultThinkingMode(core.ThinkingModeHigh)
	svc.SetModelRolesConfig(core.ModelRolesConfig{
		Planner: core.ModelRoleConfig{
			ThinkingMode: core.ThinkingModeMedium,
		},
	})

	got := svc.ResolveModelRole(core.ModelRolePlanner)
	if got.Provider != "google-ai-studio" {
		t.Fatalf("planner provider = %q, want google-ai-studio", got.Provider)
	}
	if got.Model != "gemini-2.5-pro" {
		t.Fatalf("planner model = %q, want gemini-2.5-pro", got.Model)
	}
	if got.ThinkingMode != core.ThinkingModeMedium {
		t.Fatalf("planner thinking = %q, want %q", got.ThinkingMode, core.ThinkingModeMedium)
	}
}

func TestHandleChatUsesActionRoleWhenProviderMissing(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &captureGenerationProvider{id: "openai"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("openai", []core.Account{
		{ID: "o1", Provider: "openai", Type: core.AccountAPIKey, Token: "token-openai"},
	}, []string{"o1"}, core.DefaultCooldownConfig()))
	svc.SetModelRolesConfig(core.ModelRolesConfig{
		Action: core.ModelRoleConfig{
			Provider:     "openai",
			Model:        "gpt-5",
			ThinkingMode: core.ThinkingModeHigh,
		},
	})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "action-default",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", resp.Provider)
	}
	if got := prov.lastRequest(); got.Model != "gpt-5" {
		t.Fatalf("request model = %q, want gpt-5", got.Model)
	}
}

func TestCreatePlanUsesPlannerRoleAndThinkingMode(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &captureToolTurnProvider{id: "google-ai-studio"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "g1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-g1"},
	}, []string{"g1"}, core.DefaultCooldownConfig()))
	svc.SetModelRolesConfig(core.ModelRolesConfig{
		Planner: core.ModelRoleConfig{
			Provider:     "google-ai-studio",
			Model:        "gemini-2.5-pro",
			ThinkingMode: core.ThinkingModeMedium,
		},
	})

	plan, err := svc.CreatePlan(context.Background(), core.CreatePlanRequest{
		SessionID: "plan-role",
		Surface:   core.SurfaceWeb,
		Provider:  "openai",
		Model:     "gpt-5",
		Prompt:    "implement it",
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if plan.Provider != "google-ai-studio" {
		t.Fatalf("plan provider = %q, want google-ai-studio", plan.Provider)
	}
	if plan.Model != "gemini-2.5-pro" {
		t.Fatalf("plan model = %q, want gemini-2.5-pro", plan.Model)
	}

	got := prov.lastRequest()
	if got.Model != "gemini-2.5-pro" {
		t.Fatalf("planner request model = %q, want gemini-2.5-pro", got.Model)
	}
	if got.Generation == nil || got.Generation.ThinkingMode == nil {
		t.Fatalf("expected planner thinking mode to be forwarded")
	}
	if *got.Generation.ThinkingMode != core.ThinkingModeMedium {
		t.Fatalf("planner thinking = %q, want %q", *got.Generation.ThinkingMode, core.ThinkingModeMedium)
	}
}

func TestResolveCompactionRuntimeUsesCompactionRole(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &captureGenerationProvider{id: "google-ai-studio"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "g1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-g1"},
	}, []string{"g1"}, core.DefaultCooldownConfig()))
	svc.SetDefaultProvider("openai")
	svc.SetDefaultModel("gpt-5")
	svc.SetModelRolesConfig(core.ModelRolesConfig{
		Compaction: core.ModelRoleConfig{
			Provider:     "google-ai-studio",
			Model:        "gemini-2.5-flash",
			ThinkingMode: core.ThinkingModeLow,
		},
	})

	got := resolveCompactionRuntime(svc, nil, core.DefaultCompactionConfig())
	if got.Provider == nil || got.Provider.ID() != "google-ai-studio" {
		t.Fatalf("compaction provider = %#v, want google-ai-studio", got.Provider)
	}
	if got.ModelID != "gemini-2.5-flash" {
		t.Fatalf("compaction model = %q, want gemini-2.5-flash", got.ModelID)
	}
	if !got.AllowLLMSummary {
		t.Fatalf("expected llm summary to be enabled")
	}
	if got.Generation == nil || got.Generation.ThinkingMode == nil {
		t.Fatalf("expected compaction thinking mode to be set")
	}
	if *got.Generation.ThinkingMode != core.ThinkingModeLow {
		t.Fatalf("compaction thinking = %q, want %q", *got.Generation.ThinkingMode, core.ThinkingModeLow)
	}
}

func TestGenerateSessionTitleAsyncUsesTitleRoleTarget(t *testing.T) {
	svc := NewService(ServiceOptions{})
	geminiProv := &scriptedGenerateProvider{
		id: "google-gemini-cli",
		results: map[string]scriptedGenerateResult{
			"g1": {text: "primary"},
		},
	}
	titleProv := &scriptedGenerateProvider{
		id:      "openai",
		titleCh: make(chan provider.GenerateRequest, 1),
		results: map[string]scriptedGenerateResult{
			"o1": {text: "title"},
		},
	}

	svc.RegisterProvider(geminiProv)
	svc.RegisterProvider(titleProv)
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", []core.Account{
		{ID: "g1", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-g1", Metadata: core.Metadata{"project_id": "proj-1"}},
	}, []string{"g1"}, core.DefaultCooldownConfig()))
	svc.RegisterPool(core.NewAccountPool("openai", []core.Account{
		{ID: "o1", Provider: "openai", Type: core.AccountAPIKey, Token: "token-o1"},
	}, []string{"o1"}, core.DefaultCooldownConfig()))
	svc.SetModelRolesConfig(core.ModelRolesConfig{
		Title: core.ModelRoleConfig{
			Provider:     "openai",
			Model:        "gpt-5-mini",
			ThinkingMode: core.ThinkingModeHigh,
		},
	})

	_, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: "title-role",
		Surface:   core.SurfaceWeb,
		Provider:  "google-gemini-cli",
		Model:     "gemini-3.1-pro-preview",
		Message:   "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}

	select {
	case req := <-titleProv.titleCh:
		if req.Account.ID != "o1" {
			t.Fatalf("title account = %q, want o1", req.Account.ID)
		}
		if req.Model != "gpt-5-mini" {
			t.Fatalf("title model = %q, want gpt-5-mini", req.Model)
		}
		if req.Generation != nil && req.Generation.ThinkingMode != nil {
			t.Fatalf("expected unsupported provider thinking mode to be ignored")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for title generation request")
	}
}

func TestSaveDefaultProviderConfigSyncsActionRole(t *testing.T) {
	svc := NewService(ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	if err := svc.SaveDefaultProviderConfig("google-ai-studio", "gemini-2.5-pro", core.ThinkingModeHigh); err != nil {
		t.Fatalf("SaveDefaultProviderConfig() error = %v", err)
	}

	got := svc.GetModelRolesConfig().Action
	if got.Provider != "google-ai-studio" {
		t.Fatalf("action provider = %q, want google-ai-studio", got.Provider)
	}
	if got.Model != "gemini-2.5-pro" {
		t.Fatalf("action model = %q, want gemini-2.5-pro", got.Model)
	}
	if got.ThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("action thinking = %q, want %q", got.ThinkingMode, core.ThinkingModeHigh)
	}

	cfg, err := core.LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.ModelRoles.Action.Provider != "google-ai-studio" {
		t.Fatalf("persisted action provider = %q, want google-ai-studio", cfg.ModelRoles.Action.Provider)
	}
}
