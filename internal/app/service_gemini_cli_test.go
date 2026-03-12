package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type recordingGeminiCLIRunner struct {
	generateCalls int
	streamCalls   int
	lastReq       geminiCLIHeadlessRequest
	resp          geminiCLIHeadlessResponse
	err           error
}

func (r *recordingGeminiCLIRunner) Generate(_ context.Context, req geminiCLIHeadlessRequest) (geminiCLIHeadlessResponse, error) {
	r.generateCalls++
	r.lastReq = req
	return r.resp, r.err
}

func (r *recordingGeminiCLIRunner) GenerateStream(
	_ context.Context,
	req geminiCLIHeadlessRequest,
	onText func(string),
) (geminiCLIHeadlessResponse, error) {
	r.streamCalls++
	r.lastReq = req
	if onText != nil && strings.TrimSpace(r.resp.Text) != "" {
		onText(r.resp.Text)
	}
	return r.resp, r.err
}

type countingGeminiProvider struct {
	generateCalls int
	toolCalls     int
}

func (p *countingGeminiProvider) ID() string {
	return geminiCLIProviderID
}

func (p *countingGeminiProvider) ContextWindow(string) int {
	return 200_000
}

func (p *countingGeminiProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	p.generateCalls++
	return provider.GenerateResponse{
		Text:     "provider:" + req.Account.ID,
		Endpoint: "https://example.invalid/" + req.Account.ID,
	}, nil
}

func (p *countingGeminiProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{SupportsTools: true}
}

func (p *countingGeminiProvider) GenerateToolTurn(_ context.Context, req provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	p.toolCalls++
	return provider.ToolTurnResponse{
		Text:     "tool:" + req.Account.ID,
		Endpoint: "https://example.invalid/" + req.Account.ID,
	}, nil
}

type rateLimitPrimaryProvider struct {
	id    string
	calls int
}

func (p *rateLimitPrimaryProvider) ID() string {
	return p.id
}

func (p *rateLimitPrimaryProvider) ContextWindow(string) int {
	return 200_000
}

func (p *rateLimitPrimaryProvider) Generate(_ context.Context, _ provider.GenerateRequest) (provider.GenerateResponse, error) {
	p.calls++
	return provider.GenerateResponse{}, &provider.FailureError{
		Reason:     core.FailureRateLimit,
		Message:    "rate limit",
		Endpoint:   "https://example.invalid/rate-limit",
		Status:     429,
		RetryAfter: 45 * time.Second,
	}
}

func newGeminiCLIAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store
}

func seedGeminiCLIProfile(t *testing.T, store *auth.Store, executionMode string) {
	t.Helper()
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID:     "g1",
		Provider:      geminiCLIProviderID,
		Type:          string(core.AccountOAuth),
		Email:         "g1@example.com",
		ProjectID:     "proj-1",
		Endpoint:      "https://cloudcode-pa.googleapis.com",
		ExecutionMode: executionMode,
	}); err != nil {
		t.Fatalf("UpsertProfile() error = %v", err)
	}
	if err := store.SaveCredential(geminiCLIProviderID, "g1", auth.Credential{
		AccessToken:  "stored-access-token",
		RefreshToken: "stored-refresh-token",
		ExpiresAt:    time.Unix(1_700_000_000, 0).UTC(),
	}); err != nil {
		t.Fatalf("SaveCredential() error = %v", err)
	}
}

func newGeminiCLIAccount() core.Account {
	return core.Account{
		ID:       "g1",
		Provider: geminiCLIProviderID,
		Type:     core.AccountOAuth,
		Token:    "pool-access-token",
		Metadata: core.Metadata{
			"project_id":     "proj-1",
			"endpoint":       "https://cloudcode-pa.googleapis.com",
			"execution_mode": geminiExecutionModeCLIHeadless,
		},
	}
}

func TestHandleChatUsesGeminiCLIHeadlessForPrimaryNonToolChat(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &countingGeminiProvider{}
	cliRunner := &recordingGeminiCLIRunner{
		resp: geminiCLIHeadlessResponse{
			Text: "cli:g1",
			Usage: core.UsageInfo{
				InputTokens:  11,
				OutputTokens: 7,
				TotalTokens:  18,
			},
		},
	}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool(geminiCLIProviderID, []core.Account{
		newGeminiCLIAccount(),
	}, nil, core.DefaultCooldownConfig()))
	svc.geminiCLIRunner = cliRunner

	store := newGeminiCLIAuthStore(t)
	seedGeminiCLIProfile(t, store, geminiExecutionModeCLIHeadless)
	svc.SetAuthIntegration(nil, store)

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-cli-primary",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       geminiCLIProviderID,
		Model:          "gemini-2.5-pro",
		Message:        "hello from cli",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if cliRunner.generateCalls != 1 {
		t.Fatalf("cli generate calls = %d, want 1", cliRunner.generateCalls)
	}
	if prov.generateCalls != 0 {
		t.Fatalf("provider generate calls = %d, want 0", prov.generateCalls)
	}
	if resp.Reply != "cli:g1" {
		t.Fatalf("Reply = %q, want cli:g1", resp.Reply)
	}
	if resp.AccountID != "g1" {
		t.Fatalf("AccountID = %q, want g1", resp.AccountID)
	}
	if cliRunner.lastReq.Model != "gemini-2.5-pro" {
		t.Fatalf("Model = %q, want gemini-2.5-pro", cliRunner.lastReq.Model)
	}
	if !strings.Contains(cliRunner.lastReq.Prompt, "hello from cli") {
		t.Fatalf("Prompt missing user message: %q", cliRunner.lastReq.Prompt)
	}
	if cliRunner.lastReq.Credential.AccessToken != "stored-access-token" {
		t.Fatalf("AccessToken = %q, want stored-access-token", cliRunner.lastReq.Credential.AccessToken)
	}
}

func TestHandleChatWithToolsBypassesGeminiCLIHeadless(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &countingGeminiProvider{}
	cliRunner := &recordingGeminiCLIRunner{
		resp: geminiCLIHeadlessResponse{Text: "cli:g1"},
	}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool(geminiCLIProviderID, []core.Account{
		newGeminiCLIAccount(),
	}, nil, core.DefaultCooldownConfig()))
	svc.geminiCLIRunner = cliRunner

	store := newGeminiCLIAuthStore(t)
	seedGeminiCLIProfile(t, store, geminiExecutionModeCLIHeadless)
	svc.SetAuthIntegration(nil, store)

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-cli-tools",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       geminiCLIProviderID,
		Model:          "gemini-2.5-pro",
		Message:        "hello with tools",
		EnableTools:    true,
		ToolMode:       core.ToolModeDefault,
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if cliRunner.generateCalls != 0 || cliRunner.streamCalls != 0 {
		t.Fatalf("cli runner should not be called, got generate=%d stream=%d", cliRunner.generateCalls, cliRunner.streamCalls)
	}
	if prov.toolCalls != 1 {
		t.Fatalf("tool calls = %d, want 1", prov.toolCalls)
	}
	if prov.generateCalls != 0 {
		t.Fatalf("generate calls = %d, want 0", prov.generateCalls)
	}
	if resp.Reply != "tool:g1" {
		t.Fatalf("Reply = %q, want tool:g1", resp.Reply)
	}
}

func TestHandleChatFallbackGeminiDoesNotUseCLIHeadless(t *testing.T) {
	svc := NewService(ServiceOptions{})
	primary := &rateLimitPrimaryProvider{id: "primary-provider"}
	gemini := &countingGeminiProvider{}
	cliRunner := &recordingGeminiCLIRunner{
		resp: geminiCLIHeadlessResponse{Text: "cli:g1"},
	}
	svc.RegisterProvider(primary)
	svc.RegisterProvider(gemini)
	svc.RegisterPool(core.NewAccountPool("primary-provider", []core.Account{
		{ID: "p1", Provider: "primary-provider", Type: core.AccountAPIKey, Token: "primary-token"},
	}, nil, core.DefaultCooldownConfig()))
	svc.RegisterPool(core.NewAccountPool(geminiCLIProviderID, []core.Account{
		newGeminiCLIAccount(),
	}, nil, core.DefaultCooldownConfig()))
	svc.SetFallbacks([]core.FallbackEntry{{
		Provider: geminiCLIProviderID,
		Model:    "gemini-2.5-pro",
	}})
	svc.geminiCLIRunner = cliRunner

	store := newGeminiCLIAuthStore(t)
	seedGeminiCLIProfile(t, store, geminiExecutionModeCLIHeadless)
	svc.SetAuthIntegration(nil, store)

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-cli-fallback",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "primary-provider",
		Model:          "primary-model",
		Message:        "hello fallback",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if primary.calls != 1 {
		t.Fatalf("primary calls = %d, want 1", primary.calls)
	}
	if cliRunner.generateCalls != 0 || cliRunner.streamCalls != 0 {
		t.Fatalf("cli runner should not be called for fallback, got generate=%d stream=%d", cliRunner.generateCalls, cliRunner.streamCalls)
	}
	if gemini.generateCalls != 1 {
		t.Fatalf("gemini provider generate calls = %d, want 1", gemini.generateCalls)
	}
	if resp.Provider != geminiCLIProviderID {
		t.Fatalf("Provider = %q, want %q", resp.Provider, geminiCLIProviderID)
	}
	if resp.Reply != "provider:g1" {
		t.Fatalf("Reply = %q, want provider:g1", resp.Reply)
	}
}
