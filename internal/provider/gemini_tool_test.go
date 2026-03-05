package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

// ---------------------------------------------------------------------------
// toGeminiFunctionDeclarations
// ---------------------------------------------------------------------------

func TestToGeminiFunctionDeclarations_Basic(t *testing.T) {
	tools := []ToolDefinition{
		{
			Name:        "get_weather",
			Description: "Get weather for a city",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
		},
		{
			Name:        "search",
			Description: "Search the web",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		},
	}
	result := toGeminiFunctionDeclarations(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool group, got %d", len(result))
	}
	decls, ok := result[0]["function_declarations"].([]map[string]any)
	if !ok {
		t.Fatal("function_declarations missing or wrong type")
	}
	if len(decls) != 2 {
		t.Fatalf("expected 2 declarations, got %d", len(decls))
	}
	if decls[0]["name"] != "get_weather" {
		t.Errorf("expected name get_weather, got %v", decls[0]["name"])
	}
	if decls[1]["name"] != "search" {
		t.Errorf("expected name search, got %v", decls[1]["name"])
	}
}

func TestToGeminiFunctionDeclarations_Empty(t *testing.T) {
	result := toGeminiFunctionDeclarations(nil)
	if result != nil {
		t.Fatalf("expected nil for empty tools, got %v", result)
	}
}

func TestToGeminiFunctionDeclarations_NoSchema(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "simple_tool", Description: "A tool"},
	}
	result := toGeminiFunctionDeclarations(tools)
	decls := result[0]["function_declarations"].([]map[string]any)
	if _, hasParams := decls[0]["parameters"]; hasParams {
		t.Error("expected no parameters field when InputSchema is empty")
	}
}

// ---------------------------------------------------------------------------
// toGeminiToolContents
// ---------------------------------------------------------------------------

func TestToGeminiToolContents_SystemMessage(t *testing.T) {
	messages := []core.Message{
		{Role: core.RoleSystem, Content: "You are a helpful assistant."},
		{Role: core.RoleUser, Content: "Hello"},
	}
	system, contents := toGeminiToolContents(messages)
	if system != "You are a helpful assistant." {
		t.Errorf("expected system instruction, got %q", system)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content entry (user only), got %d", len(contents))
	}
	if contents[0]["role"] != "user" {
		t.Errorf("expected user role, got %v", contents[0]["role"])
	}
}

func TestToGeminiToolContents_ToolCallAndResult(t *testing.T) {
	messages := []core.Message{
		{Role: core.RoleUser, Content: "What's the weather?"},
		{
			Role:       core.RoleAssistant,
			Content:    `{"city":"Tokyo"}`,
			ToolCallID: "tc-1",
			ToolName:   "get_weather",
		},
		{
			Role:       core.RoleTool,
			Content:    `{"temp": 22}`,
			ToolCallID: "tc-1",
			ToolName:   "get_weather",
		},
		{Role: core.RoleAssistant, Content: "The temperature in Tokyo is 22 degrees."},
	}
	system, contents := toGeminiToolContents(messages)
	if system != "" {
		t.Errorf("expected empty system, got %q", system)
	}
	// Grouped: user, model(functionCall), user(functionResponse), model(text) = 4
	if len(contents) != 4 {
		t.Fatalf("expected 4 content entries, got %d", len(contents))
	}

	// [0] user message
	if contents[0]["role"] != "user" {
		t.Errorf("entry 0: expected user, got %v", contents[0]["role"])
	}

	// [1] assistant tool call → model with functionCall
	if contents[1]["role"] != "model" {
		t.Errorf("entry 1: expected model, got %v", contents[1]["role"])
	}
	parts1, _ := contents[1]["parts"].([]map[string]any)
	if len(parts1) != 1 {
		t.Fatalf("entry 1: expected 1 part, got %d", len(parts1))
	}
	fc, ok := parts1[0]["functionCall"].(map[string]any)
	if !ok {
		t.Fatal("entry 1: missing functionCall")
	}
	if fc["name"] != "get_weather" {
		t.Errorf("entry 1: expected name get_weather, got %v", fc["name"])
	}

	// [2] tool result → user with functionResponse
	if contents[2]["role"] != "user" {
		t.Errorf("entry 2: expected user, got %v", contents[2]["role"])
	}
	parts2, _ := contents[2]["parts"].([]map[string]any)
	fr, ok := parts2[0]["functionResponse"].(map[string]any)
	if !ok {
		t.Fatal("entry 2: missing functionResponse")
	}
	if fr["name"] != "get_weather" {
		t.Errorf("entry 2: expected name get_weather, got %v", fr["name"])
	}

	// [3] assistant text → model with text
	if contents[3]["role"] != "model" {
		t.Errorf("entry 3: expected model, got %v", contents[3]["role"])
	}
}

func TestToGeminiToolContents_GroupsInterleavedToolCalls(t *testing.T) {
	// Runtime creates interleaved assistant+tool pairs for parallel tool calls.
	messages := []core.Message{
		{Role: core.RoleUser, Content: "Search and navigate"},
		{Role: core.RoleAssistant, Content: `{"q":"test"}`, ToolCallID: "tc-1", ToolName: "search"},
		{Role: core.RoleTool, Content: `{"result":"found"}`, ToolCallID: "tc-1", ToolName: "search"},
		{Role: core.RoleAssistant, Content: `{"url":"http://x"}`, ToolCallID: "tc-2", ToolName: "navigate"},
		{Role: core.RoleTool, Content: `{"ok":true}`, ToolCallID: "tc-2", ToolName: "navigate"},
	}
	_, contents := toGeminiToolContents(messages)
	// Should be: user, model(2 functionCalls), user(2 functionResponses) = 3
	if len(contents) != 3 {
		t.Fatalf("expected 3 content entries (grouped), got %d", len(contents))
	}
	// Model block should have 2 functionCall parts
	modelParts, _ := contents[1]["parts"].([]map[string]any)
	if len(modelParts) != 2 {
		t.Fatalf("expected 2 functionCall parts, got %d", len(modelParts))
	}
	// User block should have 2 functionResponse parts
	userParts, _ := contents[2]["parts"].([]map[string]any)
	if len(userParts) != 2 {
		t.Fatalf("expected 2 functionResponse parts, got %d", len(userParts))
	}
}

func TestToGeminiToolContents_RawModelContent(t *testing.T) {
	rawContent := json.RawMessage(`{"role":"model","parts":[{"functionCall":{"name":"search","args":{"q":"test"}}}],"thought_signature":"abc123"}`)
	messages := []core.Message{
		{Role: core.RoleUser, Content: "Search for something"},
		{Role: core.RoleAssistant, Content: `{"q":"test"}`, ToolCallID: "tc-1", ToolName: "search", ProviderMeta: rawContent},
		{Role: core.RoleTool, Content: `{"result":"found"}`, ToolCallID: "tc-1", ToolName: "search"},
	}
	_, contents := toGeminiToolContents(messages)
	// Should be: user, raw model content, user(functionResponse) = 3
	if len(contents) != 3 {
		t.Fatalf("expected 3 content entries, got %d", len(contents))
	}
	// The model block should have thought_signature preserved.
	if _, ok := contents[1]["thought_signature"]; !ok {
		t.Error("expected thought_signature in raw model content block")
	}
}

func TestToGeminiToolContents_EmptyContentSkipped(t *testing.T) {
	messages := []core.Message{
		{Role: core.RoleUser, Content: ""},
		{Role: core.RoleAssistant, Content: ""},
		{Role: core.RoleUser, Content: "Hello"},
	}
	_, contents := toGeminiToolContents(messages)
	if len(contents) != 1 {
		t.Fatalf("expected 1 content entry, got %d", len(contents))
	}
}

// ---------------------------------------------------------------------------
// extractToolCallsFromGeminiResponse
// ---------------------------------------------------------------------------

func TestExtractToolCallsFromGeminiResponse_TextOnly(t *testing.T) {
	body := `{
		"candidates": [{
			"content": {
				"parts": [{"text": "Hello world"}]
			}
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 5,
			"totalTokenCount": 15
		}
	}`
	r := extractToolCallsFromGeminiResponse([]byte(body))
	if !r.OK {
		t.Fatal("expected OK=true")
	}
	if r.Text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", r.Text)
	}
	if len(r.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(r.Calls))
	}
	if r.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", r.Usage.TotalTokens)
	}
	if len(r.RawModelContent) != 0 {
		t.Error("expected no RawModelContent for text-only response")
	}
}

func TestExtractToolCallsFromGeminiResponse_FunctionCall(t *testing.T) {
	body := `{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "Let me check the weather."},
					{"functionCall": {"name": "get_weather", "args": {"city": "Tokyo"}}}
				],
				"thought_signature": "sig123"
			}
		}],
		"usageMetadata": {
			"promptTokenCount": 20,
			"candidatesTokenCount": 10,
			"totalTokenCount": 30
		}
	}`
	r := extractToolCallsFromGeminiResponse([]byte(body))
	if !r.OK {
		t.Fatal("expected OK=true")
	}
	if r.Text != "Let me check the weather." {
		t.Errorf("expected text, got %q", r.Text)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(r.Calls))
	}
	if r.Calls[0].Name != "get_weather" {
		t.Errorf("expected get_weather, got %s", r.Calls[0].Name)
	}
	if !strings.HasPrefix(r.Calls[0].ID, "gemini-tc-") {
		t.Errorf("expected generated ID, got %s", r.Calls[0].ID)
	}
	var args map[string]any
	if err := json.Unmarshal(r.Calls[0].Arguments, &args); err != nil {
		t.Fatalf("failed to parse arguments: %v", err)
	}
	if args["city"] != "Tokyo" {
		t.Errorf("expected city=Tokyo, got %v", args["city"])
	}
	if r.Usage.InputTokens != 20 {
		t.Errorf("expected 20 input tokens, got %d", r.Usage.InputTokens)
	}
	// Should have RawModelContent with thought_signature preserved.
	if len(r.RawModelContent) == 0 {
		t.Fatal("expected RawModelContent for function call response")
	}
	var rawContent map[string]any
	if err := json.Unmarshal(r.RawModelContent, &rawContent); err != nil {
		t.Fatalf("failed to parse RawModelContent: %v", err)
	}
	if rawContent["thought_signature"] != "sig123" {
		t.Errorf("expected thought_signature=sig123, got %v", rawContent["thought_signature"])
	}
}

func TestExtractToolCallsFromGeminiResponse_ResponseWrapper(t *testing.T) {
	// Gemini Internal wraps the response in a "response" envelope.
	body := `{
		"response": {
			"candidates": [{
				"content": {
					"parts": [{"functionCall": {"name": "search", "args": {"q": "test"}}}]
				}
			}],
			"usageMetadata": {"totalTokenCount": 8}
		}
	}`
	r := extractToolCallsFromGeminiResponse([]byte(body))
	if !r.OK {
		t.Fatal("expected OK=true")
	}
	if len(r.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(r.Calls))
	}
	if r.Calls[0].Name != "search" {
		t.Errorf("expected search, got %s", r.Calls[0].Name)
	}
	if r.Usage.TotalTokens != 8 {
		t.Errorf("expected 8 total tokens, got %d", r.Usage.TotalTokens)
	}
}

func TestExtractToolCallsFromGeminiResponse_MultipleFunctionCalls(t *testing.T) {
	body := `{
		"candidates": [{
			"content": {
				"parts": [
					{"functionCall": {"name": "tool_a", "args": {"x": 1}}},
					{"functionCall": {"name": "tool_b", "args": {"y": 2}}}
				]
			}
		}]
	}`
	r := extractToolCallsFromGeminiResponse([]byte(body))
	if !r.OK {
		t.Fatal("expected OK=true")
	}
	if len(r.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(r.Calls))
	}
	if r.Calls[0].Name != "tool_a" {
		t.Errorf("expected tool_a, got %s", r.Calls[0].Name)
	}
	if r.Calls[1].Name != "tool_b" {
		t.Errorf("expected tool_b, got %s", r.Calls[1].Name)
	}
}

func TestExtractToolCallsFromGeminiResponse_Empty(t *testing.T) {
	body := `{"candidates": []}`
	r := extractToolCallsFromGeminiResponse([]byte(body))
	if r.OK {
		t.Error("expected OK=false for empty candidates")
	}
}

func TestExtractToolCallsFromGeminiResponse_InvalidJSON(t *testing.T) {
	r := extractToolCallsFromGeminiResponse([]byte("not json"))
	if r.OK {
		t.Error("expected OK=false for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// buildGeminiGenerationConfig
// ---------------------------------------------------------------------------

func TestBuildGeminiGenerationConfig_Nil(t *testing.T) {
	result := buildGeminiGenerationConfig(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestBuildGeminiGenerationConfig_WithValues(t *testing.T) {
	temp := 0.7
	topP := 0.9
	gen := &GenerationParams{Temperature: &temp, TopP: &topP}
	result := buildGeminiGenerationConfig(gen)
	if result == nil {
		t.Fatal("expected non-nil config")
	}
	if result["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", result["temperature"])
	}
	if result["topP"] != 0.9 {
		t.Errorf("expected topP 0.9, got %v", result["topP"])
	}
}

func TestBuildGeminiGenerationConfig_EmptyParams(t *testing.T) {
	gen := &GenerationParams{}
	result := buildGeminiGenerationConfig(gen)
	if result != nil {
		t.Errorf("expected nil for empty params, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// parseJSONOrWrap
// ---------------------------------------------------------------------------

func TestParseJSONOrWrap_ValidJSON(t *testing.T) {
	result := parseJSONOrWrap(`{"key": "value"}`)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["key"] != "value" {
		t.Errorf("expected key=value, got %v", m["key"])
	}
}

func TestParseJSONOrWrap_PlainText(t *testing.T) {
	result := parseJSONOrWrap("just some text")
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["result"] != "just some text" {
		t.Errorf("expected result=just some text, got %v", m["result"])
	}
}

func TestParseJSONOrWrap_Empty(t *testing.T) {
	result := parseJSONOrWrap("")
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}
