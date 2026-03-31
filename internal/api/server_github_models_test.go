package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

func TestGitHubModelsTokenCRUDAndChatFlow(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeGitHubModelsProvider{})
	svc.RegisterPool(core.NewAccountPool("github-models", nil, nil, core.DefaultCooldownConfig()))

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

	providersResp := performJSONRequest(t, handler, http.MethodGet, "/v1/providers", "")
	if providersResp.Code != http.StatusOK {
		t.Fatalf("unexpected providers status: %d body=%s", providersResp.Code, providersResp.Body.String())
	}
	if !strings.Contains(providersResp.Body.String(), "github-models") {
		t.Fatalf("providers response missing github-models: %s", providersResp.Body.String())
	}

	fallbackModelsResp := performJSONRequest(t, handler, http.MethodGet, "/v1/models?provider=github-models", "")
	if fallbackModelsResp.Code != http.StatusOK {
		t.Fatalf("unexpected fallback models status: %d body=%s", fallbackModelsResp.Code, fallbackModelsResp.Body.String())
	}
	if !strings.Contains(fallbackModelsResp.Body.String(), "openai/gpt-5-chat") {
		t.Fatalf("fallback models missing expected default: %s", fallbackModelsResp.Body.String())
	}

	addResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/github-models/add-token", `{"token":"ghp_valid_token","display_name":"main","set_preferred":true}`)
	if addResp.Code != http.StatusOK {
		t.Fatalf("unexpected add status: %d body=%s", addResp.Code, addResp.Body.String())
	}
	var added app.GitHubModelsAddTokenResult
	if err := json.Unmarshal(addResp.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if strings.TrimSpace(added.ProfileID) == "" {
		t.Fatalf("missing profile_id in add response")
	}
	if !added.Preferred {
		t.Fatalf("expected preferred=true from add response")
	}

	profilesResp := performJSONRequest(t, handler, http.MethodGet, "/v1/auth/github-models/profiles", "")
	if profilesResp.Code != http.StatusOK {
		t.Fatalf("unexpected profiles status: %d body=%s", profilesResp.Code, profilesResp.Body.String())
	}
	if !strings.Contains(profilesResp.Body.String(), added.ProfileID) {
		t.Fatalf("profiles response missing added profile: %s", profilesResp.Body.String())
	}

	useResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/github-models/use", `{"profile_id":"`+added.ProfileID+`"}`)
	if useResp.Code != http.StatusOK {
		t.Fatalf("unexpected use status: %d body=%s", useResp.Code, useResp.Body.String())
	}

	modelsResp := performJSONRequest(t, handler, http.MethodGet, "/v1/models?provider=github-models&profile_id="+added.ProfileID, "")
	if modelsResp.Code != http.StatusOK {
		t.Fatalf("unexpected models status: %d body=%s", modelsResp.Code, modelsResp.Body.String())
	}
	if !strings.Contains(modelsResp.Body.String(), "openai/gpt-5-chat") {
		t.Fatalf("models response missing expected model: %s", modelsResp.Body.String())
	}

	chatReq := `{"session_id":"gh1","surface":"web","provider":"github-models","model":"default","message":"hello"}`
	chatResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("unexpected chat status: %d body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"model":"openai/gpt-5-chat"`) {
		t.Fatalf("chat should resolve github-models default model: %s", chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"account_id":"`+added.ProfileID+`"`) {
		t.Fatalf("chat missing account_id: %s", chatResp.Body.String())
	}

	deleteResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/github-models/delete", `{"profile_id":"`+added.ProfileID+`"}`)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	chatResp = performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 after token deletion, got %d body=%s", chatResp.Code, chatResp.Body.String())
	}
}

func TestGitHubModelsAddTokenValidationFailure(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeGitHubModelsProvider{})
	svc.RegisterPool(core.NewAccountPool("github-models", nil, nil, core.DefaultCooldownConfig()))

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

	resp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/github-models/add-token", `{"token":"bad-token"}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected add status: %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid_github_token") {
		t.Fatalf("expected invalid_github_token error code: %s", resp.Body.String())
	}
}

type fakeGitHubModelsProvider struct{}

func (p *fakeGitHubModelsProvider) ID() string {
	return "github-models"
}

func (p *fakeGitHubModelsProvider) ContextWindow(string) int {
	return 200_000
}

func (p *fakeGitHubModelsProvider) ListModels(_ context.Context, _ core.Account) ([]string, error) {
	return []string{"openai/gpt-5-chat", "openai/gpt-5-mini", "openai/gpt-4.1"}, nil
}

func (p *fakeGitHubModelsProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	if strings.TrimSpace(req.Account.Token) == "bad-token" {
		return provider.GenerateResponse{}, &provider.FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "bad GitHub token / PAT",
			Endpoint: "https://models.github.ai",
			Status:   http.StatusUnauthorized,
		}
	}
	return provider.GenerateResponse{
		Text:     "echo:" + req.Messages[len(req.Messages)-1].Content,
		Endpoint: "https://models.github.ai",
	}, nil
}
