package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestOpenAIProviderGenerateParsesResponsesOutput(t *testing.T) {
	var gotPath string
	var gotModel string
	var gotAuth string
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotAuth = req.Header.Get("Authorization")

			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			gotModel, _ = payload["model"].(string)

			return newHTTPResponse(http.StatusOK, `{
				"output":[
					{
						"type":"message",
						"role":"assistant",
						"content":[
							{"type":"output_text","text":"hello"},
							{"type":"output_text","text":"world"}
						]
					}
				],
				"usage":{"input_tokens":12,"output_tokens":4,"total_tokens":16}
			}`), nil
		}),
	}
	p := NewOpenAIProvider(OpenAIOptions{
		ProviderID:   "openai",
		BaseURL:      "https://api.openai.com/v1",
		DefaultModel: "gpt-5.1-codex",
		HTTPClient:   client,
	})

	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "default",
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: "system"},
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{
			ID:       "openai-main",
			Provider: "openai",
			Type:     core.AccountAPIKey,
			Token:    "sk-openai-key",
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotModel != "gpt-5.1-codex" {
		t.Fatalf("unexpected model: %q", gotModel)
	}
	if gotAuth != "Bearer sk-openai-key" {
		t.Fatalf("unexpected authorization header: %q", gotAuth)
	}
	if resp.Text != "hello\nworld" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
	if resp.Usage.TotalTokens != 16 {
		t.Fatalf("unexpected usage total: %d", resp.Usage.TotalTokens)
	}
}

func TestOpenAIProviderDefaultModelForCodex(t *testing.T) {
	p := NewOpenAIProvider(OpenAIOptions{
		ProviderID: "openai-codex",
	})
	model, source, err := p.DiscoverPreferredModel(context.Background(), core.Account{})
	if err != nil {
		t.Fatalf("DiscoverPreferredModel error: %v", err)
	}
	if model != "gpt-5.3-codex" || source != "fallback" {
		t.Fatalf("unexpected discovery result: model=%q source=%q", model, source)
	}
}

func TestClassifyOpenAIStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   core.FailureReason
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: "unauthorized", want: core.FailureAuthPermanent},
		{name: "forbidden billing", status: http.StatusForbidden, body: "insufficient_quota", want: core.FailureBilling},
		{name: "forbidden auth", status: http.StatusForbidden, body: "permission denied", want: core.FailureAuthPermanent},
		{name: "rate limit", status: http.StatusTooManyRequests, body: "rate limit", want: core.FailureRateLimit},
		{name: "bad request", status: http.StatusBadRequest, body: "invalid_request_error", want: core.FailureFormat},
		{name: "model missing", status: http.StatusNotFound, body: "model_not_found", want: core.FailureModelNotFound},
		{name: "server error", status: http.StatusInternalServerError, body: "internal", want: core.FailureUnknown},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOpenAIStatus(tc.status, tc.body)
			if got != tc.want {
				t.Fatalf("classifyOpenAIStatus(%d, %q)=%s want=%s", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestSummarizeOpenAIError(t *testing.T) {
	got := summarizeOpenAIError([]byte(`{"error":{"message":"bad key","code":"invalid_api_key"}}`))
	if !strings.Contains(got, "bad key") || !strings.Contains(got, "invalid_api_key") {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestOpenAIGenerateToolTurnParsesFunctionCall(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(http.StatusOK, `{
				"output":[
					{"type":"function_call","call_id":"call_123","name":"git_status","arguments":"{}"}
				],
				"usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}
			}`), nil
		}),
	}
	p := NewOpenAIProvider(OpenAIOptions{
		ProviderID: "openai",
		HTTPClient: client,
	})
	resp, err := p.GenerateToolTurn(context.Background(), ToolTurnRequest{
		Model: "gpt-5.1-codex",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "show status"},
		},
		Account: core.Account{
			Provider: "openai",
			Type:     core.AccountAPIKey,
			Token:    "sk-openai-test",
		},
		Tools: []ToolDefinition{
			{Name: "git_status", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("GenerateToolTurn failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_123" || resp.ToolCalls[0].Name != "git_status" {
		t.Fatalf("unexpected tool call: %+v", resp.ToolCalls[0])
	}
	if resp.StopReason != "tool_calls" {
		t.Fatalf("unexpected stop reason: %q", resp.StopReason)
	}
}
