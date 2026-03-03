package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestAnthropicGenerateUsesBearerForSetupToken(t *testing.T) {
	setupToken := AnthropicSetupTokenPrefix + strings.Repeat("a", AnthropicSetupTokenMinLength)

	var gotAuth string
	var gotAPIKey string
	var gotBeta string

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotAuth = req.Header.Get("Authorization")
			gotAPIKey = req.Header.Get("x-api-key")
			gotBeta = req.Header.Get("anthropic-beta")
			return newHTTPResponse(http.StatusOK, `{
				"content": [
					{"type":"text","text":"hello"},
					{"type":"text","text":"world"}
				],
				"usage": {"input_tokens": 7, "output_tokens": 5}
			}`), nil
		}),
	}

	p := NewAnthropicProvider(AnthropicOptions{HTTPClient: client})
	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "default",
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: "system rule"},
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{Provider: "anthropic", Type: core.AccountToken, Token: setupToken},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if gotAuth != "Bearer "+setupToken {
		t.Fatalf("unexpected authorization header: %q", gotAuth)
	}
	if gotAPIKey != "" {
		t.Fatalf("did not expect x-api-key for token auth: %q", gotAPIKey)
	}
	if !strings.Contains(gotBeta, "oauth-2025-04-20") {
		t.Fatalf("expected oauth beta header, got: %q", gotBeta)
	}
	if resp.Text != "hello\nworld" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 5 || resp.Usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestAnthropicGenerateUsesAPIKeyHeaderForAPIKey(t *testing.T) {
	var gotAuth string
	var gotAPIKey string
	var gotBeta string

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotAuth = req.Header.Get("Authorization")
			gotAPIKey = req.Header.Get("x-api-key")
			gotBeta = req.Header.Get("anthropic-beta")
			return newHTTPResponse(http.StatusOK, `{
				"content":[{"type":"text","text":"ok"}],
				"usage": {"input_tokens": 1, "output_tokens": 2}
			}`), nil
		}),
	}

	p := NewAnthropicProvider(AnthropicOptions{HTTPClient: client})
	_, err := p.Generate(context.Background(), GenerateRequest{
		Model: "claude-sonnet-4-6",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{Provider: "anthropic", Type: core.AccountAPIKey, Token: "sk-ant-api-1"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("did not expect bearer auth for api-key mode: %q", gotAuth)
	}
	if gotAPIKey != "sk-ant-api-1" {
		t.Fatalf("unexpected x-api-key: %q", gotAPIKey)
	}
	if strings.Contains(gotBeta, "oauth-2025-04-20") {
		t.Fatalf("did not expect oauth-only beta header for api-key mode: %q", gotBeta)
	}
}

func TestAnthropicStatusClassification(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   core.FailureReason
	}{
		{status: http.StatusUnauthorized, body: "unauthorized", want: core.FailureAuthPermanent},
		{status: http.StatusForbidden, body: "quota exceeded", want: core.FailureBilling},
		{status: http.StatusForbidden, body: "permission denied", want: core.FailureAuthPermanent},
		{status: http.StatusTooManyRequests, body: "rate limit", want: core.FailureRateLimit},
		{status: http.StatusBadRequest, body: "invalid request", want: core.FailureFormat},
		{status: http.StatusNotFound, body: "not found", want: core.FailureModelNotFound},
		{status: http.StatusInternalServerError, body: "internal", want: core.FailureUnknown},
	}

	for _, tc := range cases {
		got := classifyAnthropicStatus(tc.status, tc.body)
		if got != tc.want {
			t.Fatalf("classifyAnthropicStatus(%d, %q)=%s want=%s", tc.status, tc.body, got, tc.want)
		}
	}
}

func TestValidateAnthropicSetupToken(t *testing.T) {
	if err := ValidateAnthropicSetupToken("bad-token"); err == nil {
		t.Fatalf("expected validation failure")
	}
	good := AnthropicSetupTokenPrefix + strings.Repeat("a", AnthropicSetupTokenMinLength)
	if err := ValidateAnthropicSetupToken(good); err != nil {
		t.Fatalf("expected valid setup token: %v", err)
	}
}
