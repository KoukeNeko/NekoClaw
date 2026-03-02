package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
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

func TestCompleteOAuthFailsWhenLoadCodeAssistUnavailable(t *testing.T) {
	t.Setenv("OPENCLAW_GEMINI_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("OPENCLAW_GEMINI_OAUTH_CLIENT_SECRET", "test-client-secret")

	oauthClientMu.Lock()
	cachedOAuthClientConfig = nil
	oauthClientMu.Unlock()
	t.Cleanup(func() {
		oauthClientMu.Lock()
		cachedOAuthClientConfig = nil
		oauthClientMu.Unlock()
	})

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

func TestCompleteOAuthAllowsSecurityPolicyPathWithEnvProject(t *testing.T) {
	t.Setenv("OPENCLAW_GEMINI_OAUTH_CLIENT_ID", "test-client-id")
	t.Setenv("OPENCLAW_GEMINI_OAUTH_CLIENT_SECRET", "test-client-secret")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project-1")

	oauthClientMu.Lock()
	cachedOAuthClientConfig = nil
	oauthClientMu.Unlock()
	t.Cleanup(func() {
		oauthClientMu.Lock()
		cachedOAuthClientConfig = nil
		oauthClientMu.Unlock()
	})

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

	credential, err := p.CompleteOAuth(context.Background(), OAuthCompleteRequest{
		Code:               "code-1",
		Verifier:           "verifier-1",
		RedirectURI:        "http://localhost:8085/oauth2callback",
		EndpointPreference: "",
		ProjectID:          "",
	})
	if err != nil {
		t.Fatalf("complete oauth failed: %v", err)
	}
	if credential.ProjectID != "env-project-1" {
		t.Fatalf("unexpected project id: %q", credential.ProjectID)
	}
	if credential.ActiveEndpoint != "https://endpoint-a.test" {
		t.Fatalf("unexpected active endpoint: %q", credential.ActiveEndpoint)
	}
}
