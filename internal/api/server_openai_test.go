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

func TestOpenAIChatDefaultModel(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeOpenAIProvider{id: "openai"})
	svc.RegisterPool(core.NewAccountPool("openai", []core.Account{
		{
			ID:       "openai-main",
			Provider: "openai",
			Type:     core.AccountAPIKey,
			Token:    "sk-openai-main",
		},
	}, nil, core.DefaultCooldownConfig()))

	server := NewServer(svc)
	handler := server.Handler()

	chatReq := `{"session_id":"o1","surface":"tui","provider":"openai","model":"default","message":"hello"}`
	chatResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("unexpected chat status: %d body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"model":"gpt-5.1-codex"`) {
		t.Fatalf("chat should resolve openai default model: %s", chatResp.Body.String())
	}
}

func TestOpenAIRequiresAPIKeyWhenOnlyCodexOAuthExists(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeOpenAIProvider{id: "openai"})
	svc.RegisterProvider(&fakeOpenAIProvider{id: "openai-codex"})
	svc.RegisterPool(core.NewAccountPool("openai", nil, nil, core.DefaultCooldownConfig()))
	svc.RegisterPool(core.NewAccountPool("openai-codex", []core.Account{
		{
			ID:       "openai-codex:user_example_com",
			Provider: "openai-codex",
			Type:     core.AccountOAuth,
			Token:    "oauth-token-1",
		},
	}, nil, core.DefaultCooldownConfig()))

	server := NewServer(svc)
	handler := server.Handler()

	chatReq := `{"session_id":"o2","surface":"tui","provider":"openai","model":"default","message":"hello"}`
	chatResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when openai key missing, got %d body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `No API key found for provider \"openai\"`) {
		t.Fatalf("expected openai/codex guard message: %s", chatResp.Body.String())
	}
}

func TestChatEnableToolsReturnsToolsNotSupportedForNonToolProvider(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeOpenAIProvider{id: "openai"})
	svc.RegisterPool(core.NewAccountPool("openai", []core.Account{
		{
			ID:       "openai-main",
			Provider: "openai",
			Type:     core.AccountAPIKey,
			Token:    "sk-openai-main",
		},
	}, nil, core.DefaultCooldownConfig()))

	server := NewServer(svc)
	handler := server.Handler()

	chatReq := `{"session_id":"o-tools","surface":"tui","provider":"openai","model":"default","message":"hello","enable_tools":true}`
	chatResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when tools are unsupported, got %d body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"tools_not_supported"`) {
		t.Fatalf("expected tools_not_supported code, got %s", chatResp.Body.String())
	}
}

func TestOpenAICodexBrowserLoginManualCompleteFlow(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeOpenAIProvider{id: "openai-codex"})
	svc.RegisterPool(core.NewAccountPool("openai-codex", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	svc.SetAuthIntegration(nil, store)
	svc.SetOpenAICodexLoginManager(auth.NewOpenAICodexLoginManager(auth.OpenAICodexLoginManagerOptions{
		IsRemote: func() bool { return true },
		Runner:   openAICodexBrowserTestRunner{},
		JobTTL:   2 * time.Minute,
	}))

	server := NewServer(svc)
	handler := server.Handler()

	startResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/openai-codex/browser/start", `{"mode":"auto"}`)
	if startResp.Code != http.StatusOK {
		t.Fatalf("unexpected start status: %d body=%s", startResp.Code, startResp.Body.String())
	}
	var started app.OpenAICodexBrowserStartResult
	if err := json.Unmarshal(startResp.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started.Status != "manual_required" || started.Mode != "manual" {
		t.Fatalf("unexpected manual start response: %+v", started)
	}

	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2NvdW50IjoiYWJjMTIzIn0.signaturepayload"
	manualBody := `{"job_id":"` + started.JobID + `","token":"` + token + `","display_name":"codex-main"}`
	completeResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/openai-codex/browser/manual/complete", manualBody)
	if completeResp.Code != http.StatusOK {
		t.Fatalf("unexpected manual complete status: %d body=%s", completeResp.Code, completeResp.Body.String())
	}
	if !strings.Contains(completeResp.Body.String(), `"provider":"openai-codex"`) {
		t.Fatalf("unexpected complete response: %s", completeResp.Body.String())
	}
}

func TestOpenAICodexBrowserLoginCLIBridgeCompletes(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeOpenAIProvider{id: "openai-codex"})
	svc.RegisterPool(core.NewAccountPool("openai-codex", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	svc.SetAuthIntegration(nil, store)
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2NvdW50Ijoiam9iMTIzIn0.signaturepayload"
	svc.SetOpenAICodexLoginManager(auth.NewOpenAICodexLoginManager(auth.OpenAICodexLoginManagerOptions{
		IsRemote: func() bool { return false },
		Runner: openAICodexBrowserTestRunner{
			token: token,
		},
		JobTTL: 2 * time.Minute,
	}))

	server := NewServer(svc)
	handler := server.Handler()

	startResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/openai-codex/browser/start", `{"mode":"auto"}`)
	if startResp.Code != http.StatusOK {
		t.Fatalf("unexpected start status: %d body=%s", startResp.Code, startResp.Body.String())
	}
	var started app.OpenAICodexBrowserStartResult
	if err := json.Unmarshal(startResp.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if strings.TrimSpace(started.JobID) == "" {
		t.Fatalf("missing job id: %+v", started)
	}

	var final app.OpenAICodexBrowserJobResult
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobResp := performJSONRequest(t, handler, http.MethodGet, "/v1/auth/openai-codex/browser/jobs/"+started.JobID, "")
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

type openAICodexBrowserTestRunner struct {
	token string
}

func (openAICodexBrowserTestRunner) Available(context.Context) error {
	return nil
}

func (r openAICodexBrowserTestRunner) RunLogin(_ context.Context, emit func(message string)) (string, error) {
	if emit != nil {
		emit("Launching codex login --device-auth flow")
	}
	return strings.TrimSpace(r.token), nil
}

type fakeOpenAIProvider struct {
	id string
}

func (p *fakeOpenAIProvider) ID() string {
	return strings.TrimSpace(p.id)
}

func (p *fakeOpenAIProvider) ContextWindow(string) int {
	return 200_000
}

func (p *fakeOpenAIProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{
		Text:     "echo:" + req.Messages[len(req.Messages)-1].Content,
		Endpoint: "https://api.openai.com/v1",
	}, nil
}
