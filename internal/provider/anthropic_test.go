package provider

import (
	"context"
	"encoding/json"
	"io"
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

func TestExtractTextAndUsageFromAnthropicParsesPromptCaching(t *testing.T) {
	text, usage, ok := extractTextAndUsageFromAnthropic([]byte(`{
		"content":[{"type":"text","text":"cached reply"}],
		"usage":{
			"input_tokens":80,
			"output_tokens":12,
			"cache_read_input_tokens":40,
			"cache_creation_input_tokens":10
		}
	}`))
	if !ok {
		t.Fatalf("expected usage parse to succeed")
	}
	if text != "cached reply" {
		t.Fatalf("unexpected text: %q", text)
	}
	if usage.CachedTokens == nil || *usage.CachedTokens != 40 {
		t.Fatalf("unexpected cached tokens: %+v", usage)
	}
	if usage.PromptTokensTotal == nil || *usage.PromptTokensTotal != 130 {
		t.Fatalf("unexpected prompt total: %+v", usage)
	}
	if usage.PromptTokensUncached == nil || *usage.PromptTokensUncached != 90 {
		t.Fatalf("unexpected prompt uncached: %+v", usage)
	}
}

func TestReadAnthropicSSEParsesPromptCaching(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		"event: message_start",
		"data: {\"message\":{\"usage\":{\"input_tokens\":60,\"cache_read_input_tokens\":25,\"cache_creation_input_tokens\":5}}}",
		"",
		"event: content_block_delta",
		"data: {\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}",
		"",
		"event: message_delta",
		"data: {\"usage\":{\"output_tokens\":15}}",
		"",
		"event: message_stop",
		"data: {}",
		"",
	}, "\n"))

	p := NewAnthropicProvider(AnthropicOptions{})
	ch := make(chan GenerateStreamChunk, 8)
	go p.readAnthropicSSE(context.Background(), &http.Response{Body: io.NopCloser(body)}, ch)

	var done GenerateStreamChunk
	for chunk := range ch {
		if chunk.Done {
			done = chunk
		}
	}

	if done.Usage.CachedTokens == nil || *done.Usage.CachedTokens != 25 {
		t.Fatalf("unexpected cached tokens: %+v", done.Usage)
	}
	if done.Usage.PromptTokensTotal == nil || *done.Usage.PromptTokensTotal != 90 {
		t.Fatalf("unexpected prompt total: %+v", done.Usage)
	}
	if done.Usage.PromptTokensUncached == nil || *done.Usage.PromptTokensUncached != 65 {
		t.Fatalf("unexpected prompt uncached: %+v", done.Usage)
	}
	if done.Usage.OutputTokens != 15 || done.Usage.TotalTokens != 75 {
		t.Fatalf("unexpected output usage: %+v", done.Usage)
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

func TestAnthropicGenerateToolTurnParsesToolUse(t *testing.T) {
	setupToken := AnthropicSetupTokenPrefix + strings.Repeat("a", AnthropicSetupTokenMinLength)
	var gotTools int
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if tools, ok := payload["tools"].([]any); ok {
				gotTools = len(tools)
			}
			return newHTTPResponse(http.StatusOK, `{
				"content":[
					{"type":"tool_use","id":"toolu_123","name":"providers_list","input":{}}
				],
				"stop_reason":"tool_use",
				"usage":{"input_tokens":10,"output_tokens":4}
			}`), nil
		}),
	}
	p := NewAnthropicProvider(AnthropicOptions{HTTPClient: client})
	resp, err := p.GenerateToolTurn(context.Background(), ToolTurnRequest{
		Model: "claude-sonnet-4-5",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "list providers"},
		},
		Account: core.Account{Provider: "anthropic", Type: core.AccountToken, Token: setupToken},
		Tools: []ToolDefinition{
			{Name: "providers_list", Description: "list providers", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("GenerateToolTurn failed: %v", err)
	}
	if gotTools != 1 {
		t.Fatalf("expected 1 tool in request, got %d", gotTools)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("unexpected stop reason: %q", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "toolu_123" || resp.ToolCalls[0].Name != "providers_list" {
		t.Fatalf("unexpected tool call: %+v", resp.ToolCalls[0])
	}
}

func TestSplitAnthropicToolMessages_DropsOrphanedToolMessages(t *testing.T) {
	system, turns := splitAnthropicToolMessages([]core.Message{
		{Role: core.RoleSystem, Content: "system"},
		{Role: core.RoleUser, Content: "question"},
		{Role: core.RoleAssistant, ToolName: "search", ToolCallID: "tc-1", Content: `{"q":"a"}`},
		{Role: core.RoleAssistant, Content: "reply after broken tool turn"},
		{Role: core.RoleTool, ToolName: "search", ToolCallID: "tc-1", Content: `{"result":"late"}`},
		{Role: core.RoleAssistant, ToolName: "fetch", ToolCallID: "tc-2", Content: `{"url":"https://example.com"}`},
		{Role: core.RoleTool, ToolName: "fetch", ToolCallID: "tc-2", Content: `{"ok":true}`},
	})

	if system != "system" {
		t.Fatalf("unexpected system prompt: %q", system)
	}
	if len(turns) != 4 {
		t.Fatalf("expected 4 turns after sanitization, got %d", len(turns))
	}
	if turns[0].Role != "user" {
		t.Fatalf("expected user turn first, got %+v", turns[0])
	}
	if turns[1].Role != "assistant" {
		t.Fatalf("expected assistant text turn second, got %+v", turns[1])
	}
	if turns[2].Role != "assistant" {
		t.Fatalf("expected assistant tool_use turn third, got %+v", turns[2])
	}
	block, ok := turns[2].Content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected tool_use content block, got %+v", turns[2].Content[0])
	}
	if block["type"] != "tool_use" || block["id"] != "tc-2" {
		t.Fatalf("expected surviving tool_use tc-2, got %+v", block)
	}
	if turns[3].Role != "user" {
		t.Fatalf("expected user tool_result turn fourth, got %+v", turns[3])
	}
	resultBlock, ok := turns[3].Content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected tool_result content block, got %+v", turns[3].Content[0])
	}
	if resultBlock["type"] != "tool_result" || resultBlock["tool_use_id"] != "tc-2" {
		t.Fatalf("expected surviving tool_result tc-2, got %+v", resultBlock)
	}
}
