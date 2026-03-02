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

func TestAIStudioKeyCRUDAndChatFlow(t *testing.T) {
	svc := app.NewService()
	svc.RegisterProvider(&fakeAIStudioProvider{})
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", nil, nil, core.DefaultCooldownConfig()))

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

	addResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/ai-studio/add-key", `{"api_key":"key-1","display_name":"main","set_preferred":true}`)
	if addResp.Code != http.StatusOK {
		t.Fatalf("unexpected add status: %d body=%s", addResp.Code, addResp.Body.String())
	}
	var added app.AIStudioAddKeyResult
	if err := json.Unmarshal(addResp.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if strings.TrimSpace(added.ProfileID) == "" {
		t.Fatalf("missing profile_id in add response")
	}
	if !added.Preferred {
		t.Fatalf("expected preferred=true from add response")
	}

	profilesResp := performJSONRequest(t, handler, http.MethodGet, "/v1/auth/ai-studio/profiles", "")
	if profilesResp.Code != http.StatusOK {
		t.Fatalf("unexpected profiles status: %d body=%s", profilesResp.Code, profilesResp.Body.String())
	}
	if !strings.Contains(profilesResp.Body.String(), added.ProfileID) {
		t.Fatalf("profiles response missing added profile: %s", profilesResp.Body.String())
	}

	useResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/ai-studio/use", `{"profile_id":"`+added.ProfileID+`"}`)
	if useResp.Code != http.StatusOK {
		t.Fatalf("unexpected use status: %d body=%s", useResp.Code, useResp.Body.String())
	}

	modelsResp := performJSONRequest(t, handler, http.MethodGet, "/v1/ai-studio/models?profile_id="+added.ProfileID, "")
	if modelsResp.Code != http.StatusOK {
		t.Fatalf("unexpected models status: %d body=%s", modelsResp.Code, modelsResp.Body.String())
	}
	if !strings.Contains(modelsResp.Body.String(), "gemini-2.5-pro") {
		t.Fatalf("models response missing expected model: %s", modelsResp.Body.String())
	}

	chatReq := `{"session_id":"s1","surface":"tui","provider":"google-ai-studio","model":"default","message":"hello"}`
	chatResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("unexpected chat status: %d body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"model":"gemini-2.5-pro"`) {
		t.Fatalf("chat should resolve default model: %s", chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"account_id":"`+added.ProfileID+`"`) {
		t.Fatalf("chat missing account_id: %s", chatResp.Body.String())
	}

	deleteResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/ai-studio/delete", `{"profile_id":"`+added.ProfileID+`"}`)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	chatResp = performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 after key deletion, got %d body=%s", chatResp.Code, chatResp.Body.String())
	}
}

func TestAIStudioAddKeyValidationFailure(t *testing.T) {
	svc := app.NewService()
	svc.RegisterProvider(&fakeAIStudioProvider{})
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", nil, nil, core.DefaultCooldownConfig()))

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

	resp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/ai-studio/add-key", `{"api_key":"bad-key"}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected add status: %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "key_validation_failed") {
		t.Fatalf("expected key_validation_failed error code: %s", resp.Body.String())
	}
}

type fakeAIStudioProvider struct{}

func (p *fakeAIStudioProvider) ID() string {
	return "google-ai-studio"
}

func (p *fakeAIStudioProvider) ContextWindow(string) int {
	return 1_000_000
}

func (p *fakeAIStudioProvider) DiscoverPreferredModel(_ context.Context, account core.Account) (string, string, error) {
	if strings.TrimSpace(account.Token) == "bad-key" {
		return "", "", &provider.FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "API key not valid",
			Endpoint: "https://generativelanguage.googleapis.com/v1beta",
			Status:   http.StatusBadRequest,
		}
	}
	return "gemini-2.5-pro", "models.list", nil
}

func (p *fakeAIStudioProvider) ListModels(_ context.Context, account core.Account) ([]string, error) {
	if strings.TrimSpace(account.Token) == "bad-key" {
		return nil, &provider.FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "API key not valid",
			Endpoint: "https://generativelanguage.googleapis.com/v1beta",
			Status:   http.StatusBadRequest,
		}
	}
	return []string{"gemini-2.5-pro", "gemini-2.5-flash"}, nil
}

func (p *fakeAIStudioProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{
		Text:     "echo:" + req.Messages[len(req.Messages)-1].Content,
		Endpoint: "https://generativelanguage.googleapis.com/v1beta",
	}, nil
}
