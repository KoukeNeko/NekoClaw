package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

func TestGeminiOAuthManualFlowEndToEnd(t *testing.T) {
	svc := app.NewService()
	svc.RegisterProvider(fakeGeminiProvider{})
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	manager := auth.NewGeminiOAuthManager(auth.ManagerOptions{
		StateTTL: 5 * time.Minute,
		IsRemote: func() bool { return true },
	})
	svc.SetAuthIntegration(manager, store)

	server := NewServer(svc)
	handler := server.Handler()

	startBody := `{"profile_id":"p1"}`
	startResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/gemini/start", startBody)
	if startResp.Code != http.StatusOK {
		t.Fatalf("unexpected start status: %d body=%s", startResp.Code, startResp.Body.String())
	}
	var started map[string]any
	if err := json.Unmarshal(startResp.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	state, _ := started["state"].(string)
	mode, _ := started["mode"].(string)
	if strings.TrimSpace(state) == "" {
		t.Fatalf("missing oauth state in start response")
	}
	if mode != "manual" {
		t.Fatalf("expected manual mode in test, got %q", mode)
	}

	completeBody := `{"state":"` + state + `","callback_url_or_code":"http://localhost:8085/oauth2callback?code=code-1&state=` + state + `"}`
	completeResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/gemini/manual/complete", completeBody)
	if completeResp.Code != http.StatusOK {
		t.Fatalf("unexpected complete status: %d body=%s", completeResp.Code, completeResp.Body.String())
	}

	profilesResp := performJSONRequest(t, handler, http.MethodGet, "/v1/auth/gemini/profiles", "")
	if profilesResp.Code != http.StatusOK {
		t.Fatalf("unexpected profiles status: %d body=%s", profilesResp.Code, profilesResp.Body.String())
	}
	if !strings.Contains(profilesResp.Body.String(), `"profile_id":"p1"`) {
		t.Fatalf("profiles response missing p1: %s", profilesResp.Body.String())
	}

	chatReq := `{"session_id":"s1","surface":"tui","provider":"google-gemini-cli","model":"gemini-test","message":"hello"}`
	chatResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("unexpected chat status: %d body=%s", chatResp.Code, chatResp.Body.String())
	}
	if !strings.Contains(chatResp.Body.String(), `"account_id":"p1"`) {
		t.Fatalf("chat response missing account id p1: %s", chatResp.Body.String())
	}
}

func TestOAuthCallbackStateMismatch(t *testing.T) {
	svc := app.NewService()
	svc.RegisterProvider(fakeGeminiProvider{})
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	manager := auth.NewGeminiOAuthManager(auth.ManagerOptions{
		StateTTL: 5 * time.Minute,
		IsRemote: func() bool { return true },
	})
	svc.SetAuthIntegration(manager, store)
	server := NewServer(svc)
	handler := server.Handler()

	startResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/gemini/start", `{}`)
	if startResp.Code != http.StatusOK {
		t.Fatalf("unexpected start status: %d", startResp.Code)
	}
	var started map[string]any
	_ = json.Unmarshal(startResp.Body.Bytes(), &started)
	state, _ := started["state"].(string)

	url := "/oauth2callback?code=abc&state=wrong-" + state
	callbackResp := performJSONRequest(t, handler, http.MethodGet, url, "")
	if callbackResp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for state mismatch, got %d body=%s", callbackResp.Code, callbackResp.Body.String())
	}
}

func TestOAuthStartSupportsRemoteModeAndRedirectOverride(t *testing.T) {
	svc := app.NewService()
	svc.RegisterProvider(fakeGeminiProvider{})
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", nil, nil, core.DefaultCooldownConfig()))

	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	manager := auth.NewGeminiOAuthManager(auth.ManagerOptions{
		StateTTL: 5 * time.Minute,
		IsRemote: func() bool { return false },
	})
	svc.SetAuthIntegration(manager, store)
	server := NewServer(svc)
	handler := server.Handler()

	startBody := `{"mode":"remote","redirect_uri":"https://bot.example.com/oauth2callback"}`
	startResp := performJSONRequest(t, handler, http.MethodPost, "/v1/auth/gemini/start", startBody)
	if startResp.Code != http.StatusOK {
		t.Fatalf("unexpected start status: %d body=%s", startResp.Code, startResp.Body.String())
	}
	var started map[string]any
	if err := json.Unmarshal(startResp.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if mode, _ := started["mode"].(string); mode != "manual" {
		t.Fatalf("expected manual mode, got %q", mode)
	}
	if oauthMode, _ := started["oauth_mode"].(string); oauthMode != "remote" {
		t.Fatalf("expected oauth_mode remote, got %q", oauthMode)
	}
	if redirect, _ := started["redirect_uri"].(string); redirect != "https://bot.example.com/oauth2callback" {
		t.Fatalf("unexpected redirect_uri: %q", redirect)
	}
}

func performJSONRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

type fakeGeminiProvider struct{}

func (fakeGeminiProvider) ID() string {
	return "google-gemini-cli"
}

func (fakeGeminiProvider) ContextWindow(string) int {
	return 32000
}

func (fakeGeminiProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{
		Text: "echo:" + req.Messages[len(req.Messages)-1].Content,
	}, nil
}

func (fakeGeminiProvider) StartOAuth(_ context.Context, req provider.OAuthStartRequest) (provider.OAuthStartResponse, error) {
	return provider.OAuthStartResponse{
		AuthURL: "https://accounts.google.com/o/oauth2/v2/auth?state=" + req.State,
	}, nil
}

func (fakeGeminiProvider) CompleteOAuth(_ context.Context, req provider.OAuthCompleteRequest) (provider.OAuthCredential, error) {
	return provider.OAuthCredential{
		AccessToken:    "access-" + req.Code,
		RefreshToken:   "refresh-" + req.Code,
		ExpiresAt:      time.Now().Add(30 * time.Minute),
		Email:          "p1@example.com",
		ProjectID:      "proj-1",
		ActiveEndpoint: "https://cloudcode-pa.googleapis.com",
	}, nil
}

func (fakeGeminiProvider) RefreshOAuthIfNeeded(_ context.Context, credential provider.OAuthCredential) (provider.OAuthCredential, bool, error) {
	return credential, false, nil
}

type memoryKeyring struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemoryKeyring() *memoryKeyring {
	return &memoryKeyring{data: map[string]string{}}
}

func (k *memoryKeyring) Available() bool {
	return true
}

func (k *memoryKeyring) Set(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	return nil
}

func (k *memoryKeyring) Get(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.data[key]
	if !ok {
		return "", auth.ErrCredentialNotFound
	}
	return value, nil
}

func (k *memoryKeyring) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	return nil
}
