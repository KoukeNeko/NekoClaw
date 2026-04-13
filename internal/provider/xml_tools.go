package provider

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// geminiModelSupportsNativeTools returns true if the model supports
// Gemini-native function calling via the tools[] payload. Models served
// through Google AI Studio that lack native function calling support
// (e.g. Llama, Mistral, Qwen) need the XML fallback path instead.
func geminiModelSupportsNativeTools(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	normalized = strings.TrimPrefix(normalized, "models/")
	if normalized == "" {
		return true
	}

	// Gemini and Gemma families support native function calling.
	if strings.HasPrefix(normalized, "gemini-") {
		return true
	}
	if strings.HasPrefix(normalized, "gemma-") {
		return true
	}

	// Known open-source model families that do NOT support native
	// function calling through the Gemini API.
	noNativePrefixes := []string{
		"llama-", "llama3", "llama4",
		"mistral-", "mixtral-",
		"qwen-", "qwen2", "qwen3",
		"deepseek-",
		"phi-",
		"yi-",
		"codestral-",
	}
	for _, prefix := range noNativePrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return false
		}
	}

	// Unknown models default to native (will error at API level if wrong).
	return true
}

// formatToolsXMLBlock formats tool definitions as an XML block for injection
// into the system instruction when native function calling is unavailable.
func formatToolsXMLBlock(tools []ToolDefinition) string {
	if len(tools) == 0 {
		return ""
	}

	type toolEntry struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}

	entries := make([]toolEntry, 0, len(tools))
	for _, t := range tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		entries = append(entries, toolEntry{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  schema,
		})
	}

	toolsJSON, _ := json.MarshalIndent(entries, "", "  ")

	return fmt.Sprintf(`<tools>
%s
</tools>

You have access to the tools listed above. When you want to use a tool, output your call in exactly this format:
<tool_call>{"name": "tool_name", "arguments": {"param": "value"}}</tool_call>

Rules:
- You may make multiple tool calls by outputting multiple <tool_call> lines.
- Arguments must be valid JSON matching the tool's parameter schema.
- After receiving tool results, continue your response based on the results.
- Do NOT wrap tool calls in code blocks or add any extra formatting.`, string(toolsJSON))
}

// toolCallRegex matches <tool_call>...</tool_call> blocks in model output.
// Uses non-greedy matching to handle multiple calls correctly.
var toolCallRegex = regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`)

// extractToolCallsFromXML scans the model's text output for <tool_call> tags
// and extracts structured ToolCall values. Returns the cleaned text (with
// tool_call tags removed), extracted calls, and whether any were found.
func extractToolCallsFromXML(text string) (cleanText string, calls []ToolCall, found bool) {
	matches := toolCallRegex.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil, false
	}

	var extractedCalls []ToolCall
	for _, loc := range matches {
		if len(loc) < 4 {
			continue
		}
		inner := text[loc[2]:loc[3]]
		call, ok := parseXMLToolCall(inner)
		if ok {
			extractedCalls = append(extractedCalls, call)
		}
	}

	if len(extractedCalls) == 0 {
		return text, nil, false
	}

	// Remove all <tool_call>...</tool_call> blocks from the text.
	cleaned := toolCallRegex.ReplaceAllString(text, "")
	cleaned = strings.TrimSpace(cleaned)

	return cleaned, extractedCalls, true
}

// parseXMLToolCall parses the JSON content inside a <tool_call> tag.
// Accepts both standard JSON and Python-style single-quoted dicts.
func parseXMLToolCall(raw string) (ToolCall, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ToolCall{}, false
	}

	// Try standard JSON first.
	var parsed struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		// Try fixing Python-style single quotes → double quotes.
		fixed := strings.ReplaceAll(raw, "'", `"`)
		if err2 := json.Unmarshal([]byte(fixed), &parsed); err2 != nil {
			return ToolCall{}, false
		}
	}

	name := strings.TrimSpace(parsed.Name)
	if name == "" {
		return ToolCall{}, false
	}

	args := parsed.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}

	return ToolCall{
		ID:        generateXMLToolCallID(),
		Name:      name,
		Arguments: args,
	}, true
}

func generateXMLToolCallID() string {
	return fmt.Sprintf("xml-tc-%d", time.Now().UnixNano())
}
