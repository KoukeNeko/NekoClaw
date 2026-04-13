package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeminiModelSupportsNativeTools(t *testing.T) {
	tests := []struct {
		model   string
		support bool
	}{
		{"gemini-2.5-pro", true},
		{"gemini-3.1-flash-lite", true},
		{"gemma-4-31b-it", true},
		{"gemma-3-27b", true},
		{"llama-3.1-70b", false},
		{"llama3-8b", false},
		{"mistral-large", false},
		{"qwen2-72b", false},
		{"qwen3-coder", false},
		{"deepseek-v3", false},
		{"phi-3-mini", false},
		{"mixtral-8x7b", false},
		{"models/gemini-2.5-flash", true},
		{"models/llama-3.1-8b", false},
		{"", true},
		{"unknown-model", true},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := geminiModelSupportsNativeTools(tt.model)
			if got != tt.support {
				t.Errorf("geminiModelSupportsNativeTools(%q) = %v, want %v", tt.model, got, tt.support)
			}
		})
	}
}

func TestFormatToolsXMLBlock(t *testing.T) {
	tools := []ToolDefinition{
		{
			Name:        "file_read",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		},
		{
			Name:        "web_search",
			Description: "Search the web",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		},
	}

	result := formatToolsXMLBlock(tools)

	if !strings.Contains(result, "<tools>") {
		t.Error("missing <tools> tag")
	}
	if !strings.Contains(result, "</tools>") {
		t.Error("missing </tools> tag")
	}
	if !strings.Contains(result, "file_read") {
		t.Error("missing file_read tool")
	}
	if !strings.Contains(result, "web_search") {
		t.Error("missing web_search tool")
	}
	if !strings.Contains(result, "<tool_call>") {
		t.Error("missing tool_call instruction")
	}
}

func TestFormatToolsXMLBlock_Empty(t *testing.T) {
	result := formatToolsXMLBlock(nil)
	if result != "" {
		t.Errorf("expected empty string for nil tools, got %q", result)
	}
}

func TestExtractToolCallsFromXML_Single(t *testing.T) {
	text := `I'll read the file for you.
<tool_call>{"name": "file_read", "arguments": {"path": "/etc/hosts"}}</tool_call>
Let me check the results.`

	cleanText, calls, found := extractToolCallsFromXML(text)

	if !found {
		t.Fatal("expected to find tool calls")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "file_read" {
		t.Errorf("expected file_read, got %s", calls[0].Name)
	}
	if !strings.HasPrefix(calls[0].ID, "xml-tc-") {
		t.Errorf("expected xml-tc- prefix in ID, got %s", calls[0].ID)
	}

	var args map[string]string
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatal(err)
	}
	if args["path"] != "/etc/hosts" {
		t.Errorf("expected path=/etc/hosts, got %s", args["path"])
	}

	if strings.Contains(cleanText, "<tool_call>") {
		t.Error("cleaned text still contains <tool_call>")
	}
	if !strings.Contains(cleanText, "read the file") {
		t.Error("cleaned text should preserve non-tool content")
	}
}

func TestExtractToolCallsFromXML_Multiple(t *testing.T) {
	text := `Let me search and read.
<tool_call>{"name": "web_search", "arguments": {"query": "golang"}}</tool_call>
<tool_call>{"name": "file_read", "arguments": {"path": "/tmp/test"}}</tool_call>`

	_, calls, found := extractToolCallsFromXML(text)

	if !found {
		t.Fatal("expected to find tool calls")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Errorf("first call expected web_search, got %s", calls[0].Name)
	}
	if calls[1].Name != "file_read" {
		t.Errorf("second call expected file_read, got %s", calls[1].Name)
	}
}

func TestExtractToolCallsFromXML_None(t *testing.T) {
	text := "Just a normal response without any tool calls."

	cleanText, calls, found := extractToolCallsFromXML(text)

	if found {
		t.Error("expected no tool calls")
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
	if cleanText != text {
		t.Errorf("expected unchanged text")
	}
}

func TestExtractToolCallsFromXML_MalformedJSON(t *testing.T) {
	text := `<tool_call>not valid json at all</tool_call>`

	cleanText, calls, found := extractToolCallsFromXML(text)

	if found {
		t.Error("expected no valid tool calls")
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
	if cleanText != text {
		t.Error("expected original text returned")
	}
}

func TestExtractToolCallsFromXML_PythonStyle(t *testing.T) {
	text := `<tool_call>{'name': 'file_read', 'arguments': {'path': '/tmp/x'}}</tool_call>`

	_, calls, found := extractToolCallsFromXML(text)

	if !found {
		t.Fatal("expected to find tool call from Python-style dict")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "file_read" {
		t.Errorf("expected file_read, got %s", calls[0].Name)
	}
}

func TestExtractToolCallsFromXML_EmptyArguments(t *testing.T) {
	text := `<tool_call>{"name": "datetime"}</tool_call>`

	_, calls, found := extractToolCallsFromXML(text)

	if !found {
		t.Fatal("expected to find tool call")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if string(calls[0].Arguments) != `{}` {
		t.Errorf("expected empty args {}, got %s", calls[0].Arguments)
	}
}

func TestExtractToolCallsFromXML_MixedValidInvalid(t *testing.T) {
	text := `
<tool_call>invalid json</tool_call>
<tool_call>{"name": "datetime"}</tool_call>
<tool_call>{"name": ""}</tool_call>`

	_, calls, found := extractToolCallsFromXML(text)

	if !found {
		t.Fatal("expected to find at least one valid call")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 valid call (datetime), got %d", len(calls))
	}
	if calls[0].Name != "datetime" {
		t.Errorf("expected datetime, got %s", calls[0].Name)
	}
}
