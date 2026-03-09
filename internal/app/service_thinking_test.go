package app

import (
	"context"
	"sync"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type captureGenerationProvider struct {
	id       string
	mu       sync.Mutex
	requests []provider.GenerateRequest
	err      error
}

func (p *captureGenerationProvider) ID() string {
	return p.id
}

func (p *captureGenerationProvider) ContextWindow(string) int {
	return 200_000
}

func (p *captureGenerationProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	if p.err != nil {
		return provider.GenerateResponse{}, p.err
	}

	return provider.GenerateResponse{
		Text:     "ok",
		Endpoint: "https://example.invalid/" + p.id,
	}, nil
}

func (p *captureGenerationProvider) lastRequest() provider.GenerateRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return provider.GenerateRequest{}
	}
	return p.requests[len(p.requests)-1]
}

func TestHandleChatAppliesDefaultThinkingModeToGeminiPrimary(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &captureGenerationProvider{id: "google-ai-studio"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "a1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-a1"},
	}, []string{"a1"}, core.DefaultCooldownConfig()))
	svc.SetDefaultThinkingMode(core.ThinkingModeHigh)

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "thinking-primary",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-ai-studio",
		Model:          "gemini-2.5-pro",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Reply != "ok" {
		t.Fatalf("unexpected reply %q", resp.Reply)
	}

	got := prov.lastRequest()
	if got.Generation == nil || got.Generation.ThinkingMode == nil {
		t.Fatalf("expected ThinkingMode to be forwarded")
	}
	if *got.Generation.ThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("ThinkingMode = %q, want %q", *got.Generation.ThinkingMode, core.ThinkingModeHigh)
	}
}

func TestHandleChatUsesFallbackThinkingModePerEntry(t *testing.T) {
	svc := NewService(ServiceOptions{})
	primary := &captureGenerationProvider{
		id: "openai",
		err: &provider.FailureError{
			Reason:   core.FailureRateLimit,
			Message:  "rate limit",
			Endpoint: "https://example.invalid/openai",
			Status:   429,
		},
	}
	fallback := &captureGenerationProvider{id: "google-ai-studio"}
	svc.RegisterProvider(primary)
	svc.RegisterProvider(fallback)
	svc.RegisterPool(core.NewAccountPool("openai", []core.Account{
		{ID: "o1", Provider: "openai", Type: core.AccountAPIKey, Token: "token-openai"},
	}, []string{"o1"}, core.DefaultCooldownConfig()))
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "g1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-gemini"},
	}, []string{"g1"}, core.DefaultCooldownConfig()))
	svc.SetFallbacks([]core.FallbackEntry{
		{
			Provider:     "google-ai-studio",
			Model:        "gemini-2.5-flash",
			ThinkingMode: core.ThinkingModeMedium,
		},
	})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "thinking-fallback",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "openai",
		Model:          "gpt-5",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Provider != "google-ai-studio" {
		t.Fatalf("expected fallback provider, got %q", resp.Provider)
	}

	got := fallback.lastRequest()
	if got.Generation == nil || got.Generation.ThinkingMode == nil {
		t.Fatalf("expected fallback ThinkingMode to be forwarded")
	}
	if *got.Generation.ThinkingMode != core.ThinkingModeMedium {
		t.Fatalf("ThinkingMode = %q, want %q", *got.Generation.ThinkingMode, core.ThinkingModeMedium)
	}
}

func TestHandleChatIgnoresThinkingModeForNonGeminiProviders(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &captureGenerationProvider{id: "openai"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("openai", []core.Account{
		{ID: "o1", Provider: "openai", Type: core.AccountAPIKey, Token: "token-openai"},
	}, []string{"o1"}, core.DefaultCooldownConfig()))
	svc.SetDefaultThinkingMode(core.ThinkingModeHigh)

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "thinking-openai",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "openai",
		Model:          "gpt-5",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Reply != "ok" {
		t.Fatalf("unexpected reply %q", resp.Reply)
	}

	got := prov.lastRequest()
	if got.Generation != nil && got.Generation.ThinkingMode != nil {
		t.Fatalf("expected ThinkingMode to be ignored for non-Gemini providers, got %q", *got.Generation.ThinkingMode)
	}
}
