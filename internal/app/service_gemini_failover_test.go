package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type scriptedGenerateResult struct {
	text string
	err  error
}

type scriptedGenerateProvider struct {
	id string

	mu      sync.Mutex
	results map[string]scriptedGenerateResult
	calls   []provider.GenerateRequest
	titleCh chan provider.GenerateRequest
}

func (p *scriptedGenerateProvider) ID() string {
	return p.id
}

func (p *scriptedGenerateProvider) ContextWindow(string) int {
	return 200_000
}

func (p *scriptedGenerateProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	p.mu.Lock()
	p.calls = append(p.calls, cloneGenerateRequest(req))
	result := p.results[req.Account.ID]
	titleCh := p.titleCh
	p.mu.Unlock()

	if titleCh != nil && isTitleGenerationRequest(req) {
		select {
		case titleCh <- cloneGenerateRequest(req):
		default:
		}
	}
	if result.err != nil {
		return provider.GenerateResponse{}, result.err
	}
	text := result.text
	if text == "" {
		text = "ok:" + req.Account.ID
	}
	return provider.GenerateResponse{
		Text:     text,
		Endpoint: "https://example.invalid/" + req.Account.ID,
	}, nil
}

func (p *scriptedGenerateProvider) callCount(accountID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, req := range p.calls {
		if req.Account.ID == accountID {
			count++
		}
	}
	return count
}

func (p *scriptedGenerateProvider) totalCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func TestHandleChatRotatesGeminiModelNotFoundBeforeProviderFallback(t *testing.T) {
	svc := NewService(ServiceOptions{})
	geminiProv := &scriptedGenerateProvider{
		id: "google-gemini-cli",
		results: map[string]scriptedGenerateResult{
			"g1": {
				err: &provider.FailureError{
					Reason:   core.FailureModelNotFound,
					Message:  "model not found",
					Endpoint: "https://example.invalid/g1",
					Status:   404,
				},
			},
			"g2": {text: "ok:g2"},
		},
	}
	fallbackProv := &scriptedGenerateProvider{
		id: "google-ai-studio",
		results: map[string]scriptedGenerateResult{
			"fb-1": {text: "fallback"},
		},
	}

	svc.RegisterProvider(geminiProv)
	svc.RegisterProvider(fallbackProv)
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", []core.Account{
		{ID: "g1", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-g1", Metadata: core.Metadata{"project_id": "proj-1"}},
		{ID: "g2", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-g2", Metadata: core.Metadata{"project_id": "proj-2"}},
	}, []string{"g1", "g2"}, core.DefaultCooldownConfig()))
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "fb-1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-fb"},
	}, []string{"fb-1"}, core.DefaultCooldownConfig()))
	svc.SetFallbacks([]core.FallbackEntry{{
		Provider: "google-ai-studio",
		Model:    "gemini-3.1-flash-lite-preview",
	}})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-rotate-before-fallback",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-gemini-cli",
		Model:          "gemini-3.1-pro-preview",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Provider != "google-gemini-cli" {
		t.Fatalf("Provider = %q, want google-gemini-cli", resp.Provider)
	}
	if resp.AccountID != "g2" {
		t.Fatalf("AccountID = %q, want g2", resp.AccountID)
	}
	if geminiProv.callCount("g1") != 1 {
		t.Fatalf("expected g1 to be tried once, got %d", geminiProv.callCount("g1"))
	}
	if geminiProv.callCount("g2") != 1 {
		t.Fatalf("expected g2 to be tried once, got %d", geminiProv.callCount("g2"))
	}
	if fallbackProv.totalCalls() != 0 {
		t.Fatalf("expected no fallback calls, got %d", fallbackProv.totalCalls())
	}
}

func TestHandleChatFallsBackAfterAllGeminiAccountsExhausted(t *testing.T) {
	svc := NewService(ServiceOptions{})
	geminiProv := &scriptedGenerateProvider{
		id: "google-gemini-cli",
		results: map[string]scriptedGenerateResult{
			"g1": {
				err: &provider.FailureError{
					Reason:   core.FailureModelNotFound,
					Message:  "model not found",
					Endpoint: "https://example.invalid/g1",
					Status:   404,
				},
			},
			"g2": {
				err: &provider.FailureError{
					Reason:     core.FailureRateLimit,
					Message:    "rate limit",
					Endpoint:   "https://example.invalid/g2",
					Status:     429,
					RetryAfter: 40 * time.Second,
				},
			},
		},
	}
	fallbackProv := &scriptedGenerateProvider{
		id: "google-ai-studio",
		results: map[string]scriptedGenerateResult{
			"fb-1": {text: "fallback"},
		},
	}

	svc.RegisterProvider(geminiProv)
	svc.RegisterProvider(fallbackProv)
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", []core.Account{
		{ID: "g1", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-g1", Metadata: core.Metadata{"project_id": "proj-1"}},
		{ID: "g2", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-g2", Metadata: core.Metadata{"project_id": "proj-2"}},
	}, []string{"g1", "g2"}, core.DefaultCooldownConfig()))
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "fb-1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-fb"},
	}, []string{"fb-1"}, core.DefaultCooldownConfig()))
	svc.SetFallbacks([]core.FallbackEntry{{
		Provider: "google-ai-studio",
		Model:    "gemini-3.1-flash-lite-preview",
	}})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-fallback-after-exhaustion",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-gemini-cli",
		Model:          "gemini-3.1-pro-preview",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Provider != "google-ai-studio" {
		t.Fatalf("Provider = %q, want google-ai-studio", resp.Provider)
	}
	if geminiProv.callCount("g1") != 1 {
		t.Fatalf("expected g1 to be tried once, got %d", geminiProv.callCount("g1"))
	}
	if geminiProv.callCount("g2") != 1 {
		t.Fatalf("expected g2 to be tried once, got %d", geminiProv.callCount("g2"))
	}
	if fallbackProv.callCount("fb-1") != 1 {
		t.Fatalf("expected one fallback call, got %d", fallbackProv.callCount("fb-1"))
	}
}

func TestGenerateSessionTitleAsyncUsesGeminiPrimaryByDefault(t *testing.T) {
	svc := NewService(ServiceOptions{})
	geminiProv := &scriptedGenerateProvider{
		id:      "google-gemini-cli",
		titleCh: make(chan provider.GenerateRequest, 1),
		results: map[string]scriptedGenerateResult{
			"g1": {text: "title"},
		},
	}
	fallbackProv := &scriptedGenerateProvider{
		id:      "google-ai-studio",
		titleCh: make(chan provider.GenerateRequest, 1),
		results: map[string]scriptedGenerateResult{
			"fb-1": {text: "title"},
		},
	}

	svc.RegisterProvider(geminiProv)
	svc.RegisterProvider(fallbackProv)
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", []core.Account{
		{ID: "g1", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-g1", Metadata: core.Metadata{"project_id": "proj-1"}},
	}, []string{"g1"}, core.DefaultCooldownConfig()))
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "fb-1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-fb"},
	}, []string{"fb-1"}, core.DefaultCooldownConfig()))
	svc.SetFallbacks([]core.FallbackEntry{{
		Provider: "google-ai-studio",
		Model:    "gemini-3.1-flash-lite-preview",
	}})

	_, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: "gemini-title-fallback",
		Surface:   core.SurfaceWeb,
		Provider:  "google-gemini-cli",
		Model:     "gemini-3.1-pro-preview",
		Message:   "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}

	select {
	case req := <-geminiProv.titleCh:
		if req.Account.ID != "g1" {
			t.Fatalf("title generation account = %q, want g1", req.Account.ID)
		}
		if req.Model != "gemini-3.1-pro-preview" {
			t.Fatalf("title generation model = %q, want gemini-3.1-pro-preview", req.Model)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Gemini title generation")
	}

	if fallbackProv.totalCalls() != 0 {
		t.Fatalf("expected fallback provider to remain unused, got %d calls", fallbackProv.totalCalls())
	}
}
