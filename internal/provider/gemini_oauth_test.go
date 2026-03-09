package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func resetOAuthClientConfigCache() {
	oauthClientMu.Lock()
	cachedOAuthClientConfig = nil
	oauthClientMu.Unlock()
}

func writeFakeGeminiCLI(t *testing.T, clientID, clientSecret string) string {
	t.Helper()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	oauthDir := filepath.Join(
		root,
		"lib",
		"node_modules",
		"@google",
		"gemini-cli",
		"node_modules",
		"@google",
		"gemini-cli-core",
		"dist",
		"src",
		"code_assist",
	)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	if err := os.MkdirAll(oauthDir, 0o755); err != nil {
		t.Fatalf("mkdir oauth dir: %v", err)
	}

	binPath := filepath.Join(binDir, "gemini")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake gemini binary: %v", err)
	}
	oauthPath := filepath.Join(oauthDir, "oauth2.js")
	content := []byte("client_id=" + clientID + "\nclient_secret=" + clientSecret + "\n")
	if err := os.WriteFile(oauthPath, content, 0o644); err != nil {
		t.Fatalf("write fake oauth config: %v", err)
	}
	return binDir
}

func TestResolveOAuthClientConfigLoadsGeminiCLI(t *testing.T) {
	binDir := writeFakeGeminiCLI(
		t,
		"222222222222-cli.apps.googleusercontent.com",
		"GOCSPX-cli-secret",
	)
	t.Setenv("PATH", binDir)

	resetOAuthClientConfigCache()
	t.Cleanup(resetOAuthClientConfigCache)

	cfg, err := resolveOAuthClientConfig()
	if err != nil {
		t.Fatalf("resolve oauth client config: %v", err)
	}
	if cfg.ClientID != "222222222222-cli.apps.googleusercontent.com" {
		t.Fatalf("expected cli client id, got %q", cfg.ClientID)
	}
	if cfg.ClientSecret != "GOCSPX-cli-secret" {
		t.Fatalf("expected cli client secret, got %q", cfg.ClientSecret)
	}
}

func TestCompleteOAuthFailsWhenLoadCodeAssistUnavailable(t *testing.T) {
	binDir := writeFakeGeminiCLI(
		t,
		"123456789012-cli.apps.googleusercontent.com",
		"GOCSPX-cli-secret",
	)
	t.Setenv("PATH", binDir)

	resetOAuthClientConfigCache()
	t.Cleanup(resetOAuthClientConfigCache)

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.URL.Host == "oauth2.googleapis.com" && req.URL.Path == "/token":
				return newHTTPResponse(http.StatusOK, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`), nil
			case req.URL.Host == "www.googleapis.com" && req.URL.Path == "/oauth2/v1/userinfo":
				return newHTTPResponse(http.StatusOK, `{"email":"tester@example.com"}`), nil
			case strings.HasSuffix(req.URL.Host, "endpoint-a.test") || strings.HasSuffix(req.URL.Host, "endpoint-b.test"):
				return newHTTPResponse(http.StatusInternalServerError, `{"error":"unavailable"}`), nil
			default:
				return newHTTPResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
		}),
	}

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints:  []string{"https://endpoint-a.test", "https://endpoint-b.test"},
		HTTPClient: client,
	})

	_, err := p.CompleteOAuth(context.Background(), OAuthCompleteRequest{
		Code:               "code-1",
		Verifier:           "verifier-1",
		RedirectURI:        "http://localhost:8085/oauth2callback",
		EndpointPreference: "",
		ProjectID:          "",
	})
	if err == nil {
		t.Fatalf("expected oauth completion failure")
	}
	if !errors.Is(err, ErrProjectDiscoveryFailed) {
		t.Fatalf("expected project discovery failure, got: %v", err)
	}
}

func TestCompleteOAuthFailsWhenSecurityPolicyNeedsProvisionedProject(t *testing.T) {
	binDir := writeFakeGeminiCLI(
		t,
		"123456789012-cli.apps.googleusercontent.com",
		"GOCSPX-cli-secret",
	)
	t.Setenv("PATH", binDir)

	resetOAuthClientConfigCache()
	t.Cleanup(resetOAuthClientConfigCache)

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.URL.Host == "oauth2.googleapis.com" && req.URL.Path == "/token":
				return newHTTPResponse(http.StatusOK, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`), nil
			case req.URL.Host == "www.googleapis.com" && req.URL.Path == "/oauth2/v1/userinfo":
				return newHTTPResponse(http.StatusOK, `{"email":"tester@example.com"}`), nil
			case strings.HasSuffix(req.URL.Host, "endpoint-a.test") && req.URL.Path == "/v1internal:loadCodeAssist":
				return newHTTPResponse(http.StatusForbidden, `{"error":{"details":[{"reason":"SECURITY_POLICY_VIOLATED"}]}}`), nil
			default:
				return newHTTPResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
		}),
	}

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints:  []string{"https://endpoint-a.test"},
		HTTPClient: client,
	})

	_, err := p.CompleteOAuth(context.Background(), OAuthCompleteRequest{
		Code:               "code-1",
		Verifier:           "verifier-1",
		RedirectURI:        "http://localhost:8085/oauth2callback",
		EndpointPreference: "",
		ProjectID:          "",
	})
	if err == nil {
		t.Fatalf("expected project discovery failure")
	}
	if !errors.Is(err, ErrProjectDiscoveryFailed) {
		t.Fatalf("expected project discovery failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "provision") {
		t.Fatalf("unexpected error: %v", err)
	}
}
