package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

func TestGeminiQuotaEndpoint_UsesDefaultProviderAndProfileID(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:retrieveUserQuota" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"buckets":[{"modelId":"gemini-3.1-pro-preview","remainingFraction":0.75}]}`))
	}))
	defer srv.Close()

	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(provider.NewGeminiInternalProvider(provider.GeminiInternalOptions{
		Endpoints:  []string{srv.URL},
		HTTPClient: srv.Client(),
	}))
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", []core.Account{
		{ID: "gemini-profile-1", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-profile-1"},
		{ID: "gemini-profile-2", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-profile-2"},
	}, []string{"gemini-profile-1", "gemini-profile-2"}, core.DefaultCooldownConfig()))

	resp := performJSONRequest(t, NewServer(svc).Handler(), http.MethodGet, "/v1/gemini/quota?profile_id=gemini-profile-2", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Provider string                       `json:"provider"`
		Account  string                       `json:"account"`
		Quota    provider.GeminiQuotaResponse `json:"quota"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Provider != "google-gemini-cli" {
		t.Fatalf("provider = %q, want %q", payload.Provider, "google-gemini-cli")
	}
	if payload.Account != "gemini-profile-2" {
		t.Fatalf("account = %q, want %q", payload.Account, "gemini-profile-2")
	}
	if gotAuth != "Bearer token-profile-2" {
		t.Fatalf("authorization header = %q, want %q", gotAuth, "Bearer token-profile-2")
	}
	if len(payload.Quota.Buckets) != 1 {
		t.Fatalf("quota buckets len = %d, want 1", len(payload.Quota.Buckets))
	}
	if payload.Quota.Buckets[0].ModelID != "gemini-3.1-pro-preview" {
		t.Fatalf("quota bucket model = %q, want %q", payload.Quota.Buckets[0].ModelID, "gemini-3.1-pro-preview")
	}
	if payload.Quota.Buckets[0].RemainingFraction != 0.75 {
		t.Fatalf("remainingFraction = %v, want 0.75", payload.Quota.Buckets[0].RemainingFraction)
	}
}

func TestGeminiQuotaEndpoint_UsesExplicitAccountID(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"buckets":[{"modelId":"gemini-2.5-flash","remainingFraction":0.5}]}`))
	}))
	defer srv.Close()

	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(provider.NewGeminiInternalProvider(provider.GeminiInternalOptions{
		Endpoints:  []string{srv.URL},
		HTTPClient: srv.Client(),
	}))
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", []core.Account{
		{ID: "first", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-first"},
		{ID: "second", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-second"},
	}, []string{"first", "second"}, core.DefaultCooldownConfig()))

	resp := performJSONRequest(t, NewServer(svc).Handler(), http.MethodGet, "/v1/gemini/quota?provider=google-gemini-cli&account_id=second", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Account string                       `json:"account"`
		Quota   provider.GeminiQuotaResponse `json:"quota"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Account != "second" {
		t.Fatalf("account = %q, want %q", payload.Account, "second")
	}
	if gotAuth != "Bearer token-second" {
		t.Fatalf("authorization header = %q, want %q", gotAuth, "Bearer token-second")
	}
	if len(payload.Quota.Buckets) != 1 || payload.Quota.Buckets[0].ModelID != "gemini-2.5-flash" {
		t.Fatalf("unexpected quota payload: %+v", payload.Quota)
	}
}
