package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type rateLimitThenRotateProvider struct {
	id    string
	mu    sync.Mutex
	calls map[string]int
}

func (p *rateLimitThenRotateProvider) ID() string {
	return p.id
}

func (p *rateLimitThenRotateProvider) ContextWindow(string) int {
	return 200_000
}

func (p *rateLimitThenRotateProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	p.mu.Lock()
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	p.calls[req.Account.ID]++
	p.mu.Unlock()

	if req.Account.ID == "a1" {
		return provider.GenerateResponse{}, &provider.FailureError{
			Reason:     core.FailureRateLimit,
			Message:    "rate limit",
			Endpoint:   "https://example.invalid/a1",
			Status:     429,
			RetryAfter: 10 * time.Second,
		}
	}

	return provider.GenerateResponse{
		Text:     "ok:" + req.Account.ID,
		Endpoint: "https://example.invalid/a2",
	}, nil
}

func (p *rateLimitThenRotateProvider) callCount(accountID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[accountID]
}

func TestHandleChatRotatesInsteadOfInlineRetryWhenAlternativeAccountIsAvailable(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &rateLimitThenRotateProvider{id: "test-provider"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("test-provider", []core.Account{
		{ID: "a1", Provider: "test-provider", Type: core.AccountAPIKey, Token: "token-a1"},
		{ID: "a2", Provider: "test-provider", Type: core.AccountAPIKey, Token: "token-a2"},
	}, []string{"a1", "a2"}, core.DefaultCooldownConfig()))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	resp, err := svc.HandleChat(ctx, core.ChatRequest{
		SessionID:      "rotate-on-rate-limit",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "test-provider",
		Model:          "test-model",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.AccountID != "a2" {
		t.Fatalf("expected fallback to a2, got %q", resp.AccountID)
	}
	if resp.Reply != "ok:a2" {
		t.Fatalf("unexpected reply %q", resp.Reply)
	}
	if got := prov.callCount("a1"); got != 1 {
		t.Fatalf("expected a1 to be tried once before rotation, got %d", got)
	}
}
