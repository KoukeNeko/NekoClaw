package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
	"github.com/doeshing/nekoclaw/internal/security"
)

func TestBrowserSecuritySetupProtectsManagementAPI(t *testing.T) {
	handler, _, _ := newBrowserSecurityTestHandler(t)

	health := performSecurityRequest(t, handler, http.MethodGet, "/healthz", "", nil, nil)
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", health.Code, health.Body.String())
	}

	callback := performSecurityRequest(t, handler, http.MethodGet, "/oauth2callback", "", nil, nil)
	if callback.Code == http.StatusUnauthorized {
		t.Fatalf("oauth2callback should not be auth-blocked: %s", callback.Body.String())
	}

	discord := performSecurityRequest(t, handler, http.MethodPost, "/v1/integrations/discord/events", `{
		"channel_id":"chan-1",
		"user_id":"user-1",
		"provider":"mock",
		"model":"default",
		"message":"hello"
	}`, nil, nil)
	if discord.Code != http.StatusOK {
		t.Fatalf("discord event status = %d body=%s", discord.Code, discord.Body.String())
	}

	sessions := performSecurityRequest(t, handler, http.MethodGet, "/v1/sessions", "", nil, nil)
	if sessions.Code != http.StatusUnauthorized {
		t.Fatalf("sessions status = %d body=%s", sessions.Code, sessions.Body.String())
	}
	if code := errorCodeFromBody(t, sessions); code != "setup_required" {
		t.Fatalf("sessions error code = %q, want setup_required", code)
	}

	chat := performSecurityRequest(t, handler, http.MethodPost, "/v1/chat", `{
		"session_id":"main",
		"surface":"web",
		"provider":"mock",
		"model":"default",
		"message":"hello"
	}`, nil, nil)
	if chat.Code != http.StatusUnauthorized {
		t.Fatalf("chat status = %d body=%s", chat.Code, chat.Body.String())
	}

	general := performSecurityRequest(t, handler, http.MethodGet, "/v1/general/config", "", nil, nil)
	if general.Code != http.StatusUnauthorized {
		t.Fatalf("general config status = %d body=%s", general.Code, general.Body.String())
	}
}

func TestBrowserSecuritySetupLoginAndLogoutFlow(t *testing.T) {
	handler, _, setupToken := newBrowserSecurityTestHandler(t)

	invalidSetup := performSecurityRequest(t, handler, http.MethodPost, "/v1/security/setup", `{
		"token":"bad-token",
		"password":"correct horse battery"
	}`, nil, nil)
	if invalidSetup.Code != http.StatusBadRequest {
		t.Fatalf("invalid setup status = %d body=%s", invalidSetup.Code, invalidSetup.Body.String())
	}

	setup := performSecurityRequest(t, handler, http.MethodPost, "/v1/security/setup", `{
		"token":"`+setupToken+`",
		"password":"correct horse battery"
	}`, nil, nil)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", setup.Code, setup.Body.String())
	}
	sessionCookie := requireSessionCookie(t, setup)

	authorizedSessions := performSecurityRequest(t, handler, http.MethodGet, "/v1/sessions", "", nil, []*http.Cookie{sessionCookie})
	if authorizedSessions.Code != http.StatusOK {
		t.Fatalf("authorized sessions status = %d body=%s", authorizedSessions.Code, authorizedSessions.Body.String())
	}

	logout := performSecurityRequest(t, handler, http.MethodPost, "/v1/security/logout", `{}`, map[string]string{
		"Origin": "http://example.com",
	}, []*http.Cookie{sessionCookie})
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", logout.Code, logout.Body.String())
	}

	stale := performSecurityRequest(t, handler, http.MethodGet, "/v1/sessions", "", nil, []*http.Cookie{sessionCookie})
	if stale.Code != http.StatusUnauthorized {
		t.Fatalf("stale session status = %d body=%s", stale.Code, stale.Body.String())
	}

	badLogin := performSecurityRequest(t, handler, http.MethodPost, "/v1/security/login", `{"password":"wrong password"}`, nil, nil)
	if badLogin.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d body=%s", badLogin.Code, badLogin.Body.String())
	}

	login := performSecurityRequest(t, handler, http.MethodPost, "/v1/security/login", `{"password":"correct horse battery"}`, nil, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	if requireSessionCookie(t, login).Value == "" {
		t.Fatalf("expected login response to set session cookie")
	}
}

func TestBrowserSecurityRejectsCrossSiteMutations(t *testing.T) {
	handler, _, setupToken := newBrowserSecurityTestHandler(t)

	setup := performSecurityRequest(t, handler, http.MethodPost, "/v1/security/setup", `{
		"token":"`+setupToken+`",
		"password":"correct horse battery"
	}`, nil, nil)
	sessionCookie := requireSessionCookie(t, setup)

	crossSite := performSecurityRequest(t, handler, http.MethodPut, "/v1/general/config", `{"timezone":"Asia/Taipei"}`, nil, []*http.Cookie{sessionCookie})
	if crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site status = %d body=%s", crossSite.Code, crossSite.Body.String())
	}
	if code := errorCodeFromBody(t, crossSite); code != "origin_mismatch" {
		t.Fatalf("cross-site error code = %q, want origin_mismatch", code)
	}

	sameOrigin := performSecurityRequest(t, handler, http.MethodPut, "/v1/general/config", `{"timezone":"Asia/Taipei"}`, map[string]string{
		"Origin": "http://example.com",
	}, []*http.Cookie{sessionCookie})
	if sameOrigin.Code != http.StatusOK {
		t.Fatalf("same-origin status = %d body=%s", sameOrigin.Code, sameOrigin.Body.String())
	}
}

func TestBrowserSecurityDisableAndReEnableFlow(t *testing.T) {
	handler, _, setupToken := newBrowserSecurityTestHandler(t)

	setup := performSecurityRequest(t, handler, http.MethodPost, "/v1/security/setup", `{
		"token":"`+setupToken+`",
		"password":"correct horse battery"
	}`, nil, nil)
	sessionCookie := requireSessionCookie(t, setup)

	disable := performSecurityRequest(t, handler, http.MethodPut, "/v1/security/config", `{
		"auth_enabled":false,
		"session_idle_minutes":480,
		"session_absolute_hours":168,
		"login_max_attempts":5,
		"login_block_minutes":15
	}`, map[string]string{
		"Origin": "http://example.com",
	}, []*http.Cookie{sessionCookie})
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", disable.Code, disable.Body.String())
	}

	anonymousSessions := performSecurityRequest(t, handler, http.MethodGet, "/v1/sessions", "", nil, nil)
	if anonymousSessions.Code != http.StatusOK {
		t.Fatalf("anonymous sessions after disable = %d body=%s", anonymousSessions.Code, anonymousSessions.Body.String())
	}

	enable := performSecurityRequest(t, handler, http.MethodPut, "/v1/security/config", `{
		"auth_enabled":true,
		"session_idle_minutes":480,
		"session_absolute_hours":168,
		"login_max_attempts":5,
		"login_block_minutes":15
	}`, nil, nil)
	if enable.Code != http.StatusOK {
		t.Fatalf("enable status = %d body=%s", enable.Code, enable.Body.String())
	}

	protectedAgain := performSecurityRequest(t, handler, http.MethodGet, "/v1/sessions", "", nil, nil)
	if protectedAgain.Code != http.StatusUnauthorized {
		t.Fatalf("protectedAgain status = %d body=%s", protectedAgain.Code, protectedAgain.Body.String())
	}
	if code := errorCodeFromBody(t, protectedAgain); code != "auth_required" {
		t.Fatalf("protectedAgain error code = %q, want auth_required", code)
	}
}

func newBrowserSecurityTestHandler(t *testing.T) (http.Handler, *security.Runtime, string) {
	t.Helper()

	svc := app.NewService(app.ServiceOptions{})
	svc.SetConfigDir(t.TempDir())
	svc.SetSecurityConfig(core.DefaultSecurityConfig())

	mockProvider := provider.NewMockProvider()
	svc.RegisterProvider(mockProvider)
	svc.RegisterPool(core.NewAccountPool("mock", []core.Account{
		{ID: "mock-default", Provider: "mock", Type: core.AccountAPIKey, Token: "mock"},
	}, nil, core.DefaultCooldownConfig()))

	now := time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)
	rt, err := security.NewRuntime(security.RuntimeOptions{
		BaseDir: t.TempDir(),
		Config:  svc.GetSecurityConfig(),
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	token, err := rt.EnsureSetupToken()
	if err != nil {
		t.Fatalf("EnsureSetupToken failed: %v", err)
	}

	server := NewServer(svc)
	server.SetSecurityRuntime(rt)
	return server.Handler(), rt, token
}

func performSecurityRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
	headers map[string]string,
	cookies []*http.Cookie,
) *httptest.ResponseRecorder {
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
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func requireSessionCookie(t *testing.T, rr *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range rr.Result().Cookies() {
		if cookie.Name == security.CookieName {
			return cookie
		}
	}
	t.Fatalf("session cookie not found in response")
	return nil
}

func errorCodeFromBody(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body: %v body=%s", err, rr.Body.String())
	}
	errorPayload, _ := payload["error"].(map[string]any)
	code, _ := errorPayload["code"].(string)
	return code
}
