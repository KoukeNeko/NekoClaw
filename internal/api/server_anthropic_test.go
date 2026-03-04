package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

func TestAnthropicCredentialCRUDAndChatFlow(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeAnthropicProvider{})
	svc.RegisterPool(core.NewAccountPool("anthropic", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	svc.SetAuthIntegration(nil, store)

	server := NewServer(svc)
	handler := server.Handler()

	setupToken := provider.AnthropicSetupTokenPrefix + strings.Repeat("a", provider.AnthropicSetupTokenMinLength)
	addResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/anthropic/add-token", `{"setup_token":"`+setupToken+`","display_name":"sub-main","set_preferred":true}`)
	if addResp.Code != http.StatusOK {
		t.Fatalf("unexpected add status: %d body=%s", addResp.Code, addResp.Body.String())
	}
	if !strings.Contains(addResp.Body.String(), `"provider":"anthropic"`) {
		t.Fatalf("unexpected add response: %s", addResp.Body.String())
	}

	profilesResp := performJSONRequest(t, handler, http.MethodGet, "/v1/auth/anthropic/profiles", "")
	if profilesResp.Code != http.StatusOK {
		t.Fatalf("unexpected profiles status: %d body=%s", profilesResp.Code, profilesResp.Body.String())
	}
	if !strings.Contains(profilesResp.Body.String(), "sub-main") {
		t.Fatalf("profiles response missing display name: %s", profilesResp.Body.String())
	}

	chatReq := `{"session_id":"a1","surface":"tui","provider":"anthropic","model":"default","message":"hello"}`
	chatResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("unexpected chat status: %d body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"model":"claude-sonnet-4-5"`) {
		t.Fatalf("chat should resolve anthropic default model: %s", chatResp.Body.String())
	}

	deleteResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/anthropic/delete", `{"profile_id":"anthropic:sub_main_aaaaaa"}`)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	chatResp = performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 after delete, got %d body=%s", chatResp.Code, chatResp.Body.String())
	}
}

func TestAnthropicAddTokenValidationError(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeAnthropicProvider{})
	svc.RegisterPool(core.NewAccountPool("anthropic", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	svc.SetAuthIntegration(nil, store)

	server := NewServer(svc)
	handler := server.Handler()

	resp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/anthropic/add-token", `{"setup_token":"bad-token"}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid_setup_token") {
		t.Fatalf("expected invalid_setup_token: %s", resp.Body.String())
	}
}

func TestAnthropicBrowserLoginManualCompleteFlow(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeAnthropicProvider{})
	svc.RegisterPool(core.NewAccountPool("anthropic", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	svc.SetAuthIntegration(nil, store)
	svc.SetAnthropicLoginManager(auth.NewAnthropicLoginManager(auth.AnthropicLoginManagerOptions{
		IsRemote: func() bool { return true },
		Runner:   anthropicBrowserTestRunner{},
		JobTTL:   2 * time.Minute,
	}))

	server := NewServer(svc)
	handler := server.Handler()

	startResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/anthropic/browser/start", `{"mode":"auto"}`)
	if startResp.Code != http.StatusOK {
		t.Fatalf("unexpected start status: %d body=%s", startResp.Code, startResp.Body.String())
	}
	var started app.AnthropicBrowserStartResult
	if err := json.Unmarshal(startResp.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started.Status != "manual_required" || started.Mode != "manual" {
		t.Fatalf("unexpected manual start response: %+v", started)
	}

	setupToken := provider.AnthropicSetupTokenPrefix + strings.Repeat("b", provider.AnthropicSetupTokenMinLength)
	manualBody := `{"job_id":"` + started.JobID + `","setup_token":"` + setupToken + `","display_name":"manual-main"}`
	completeResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/anthropic/browser/manual/complete", manualBody)
	if completeResp.Code != http.StatusOK {
		t.Fatalf("unexpected manual complete status: %d body=%s", completeResp.Code, completeResp.Body.String())
	}
	if !strings.Contains(completeResp.Body.String(), `"provider":"anthropic"`) {
		t.Fatalf("unexpected complete response: %s", completeResp.Body.String())
	}
}

func TestAnthropicBrowserLoginCLIBridgeCompletes(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeAnthropicProvider{})
	svc.RegisterPool(core.NewAccountPool("anthropic", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	svc.SetAuthIntegration(nil, store)
	setupToken := provider.AnthropicSetupTokenPrefix + strings.Repeat("c", provider.AnthropicSetupTokenMinLength)
	svc.SetAnthropicLoginManager(auth.NewAnthropicLoginManager(auth.AnthropicLoginManagerOptions{
		IsRemote: func() bool { return false },
		Runner: anthropicBrowserTestRunner{
			token: setupToken,
		},
		JobTTL: 2 * time.Minute,
	}))

	server := NewServer(svc)
	handler := server.Handler()

	startResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/anthropic/browser/start", `{"mode":"auto"}`)
	if startResp.Code != http.StatusOK {
		t.Fatalf("unexpected start status: %d body=%s", startResp.Code, startResp.Body.String())
	}
	var started app.AnthropicBrowserStartResult
	if err := json.Unmarshal(startResp.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if strings.TrimSpace(started.JobID) == "" {
		t.Fatalf("missing job id: %+v", started)
	}

	var final app.AnthropicBrowserJobResult
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobResp := performJSONRequest(t, handler, http.MethodGet, "/v1/auth/anthropic/browser/jobs/"+started.JobID, "")
		if jobResp.Code != http.StatusOK {
			t.Fatalf("unexpected job status: %d body=%s", jobResp.Code, jobResp.Body.String())
		}
		if err := json.Unmarshal(jobResp.Body.Bytes(), &final); err != nil {
			t.Fatalf("decode job response: %v", err)
		}
		if final.Status == "completed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final.Status != "completed" {
		t.Fatalf("expected completed status, got %+v", final)
	}
	if strings.TrimSpace(final.ProfileID) == "" {
		t.Fatalf("expected completed profile id: %+v", final)
	}
}

type anthropicBrowserTestRunner struct {
	token string
}

func (anthropicBrowserTestRunner) Available(context.Context) error {
	return nil
}

func (r anthropicBrowserTestRunner) RunSetupToken(_ context.Context, emit func(message string)) (string, error) {
	if emit != nil {
		emit("Launching claude setup-token flow")
	}
	return strings.TrimSpace(r.token), nil
}

type fakeAnthropicProvider struct{}

func (p *fakeAnthropicProvider) ID() string {
	return "anthropic"
}

func (p *fakeAnthropicProvider) ContextWindow(string) int {
	return 200_000
}

func (p *fakeAnthropicProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{
		Text:     "echo:" + req.Messages[len(req.Messages)-1].Content,
		Endpoint: "https://api.anthropic.com",
	}, nil
}
