package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

// ---------------------------------------------------------------------------
// Gemini function calling — shared helpers for both Google AI Studio and
// Gemini Internal providers.
// ---------------------------------------------------------------------------

// toGeminiFunctionDeclarations converts provider.ToolDefinition slice into
// the Gemini API "tools" array format:
//
//	[{"function_declarations": [{name, description, parameters}, ...]}]
func toGeminiFunctionDeclarations(tools []ToolDefinition) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	declarations := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		decl := map[string]any{
			"name": strings.TrimSpace(tool.Name),
		}
		if desc := strings.TrimSpace(tool.Description); desc != "" {
			decl["description"] = desc
		}
		if len(tool.InputSchema) > 0 {
			var schema any
			if err := json.Unmarshal(tool.InputSchema, &schema); err == nil {
				stripUnsupportedSchemaFields(schema)
				decl["parameters"] = schema
			}
		}
		declarations = append(declarations, decl)
	}
	return []map[string]any{
		{"function_declarations": declarations},
	}
}

// toGeminiToolContents converts core.Message slice (including tool-related
// messages) into Gemini contents and an optional systemInstruction string.
//
// Handles proper grouping:
//   - Consecutive assistant tool-call messages → one model content block
//   - Consecutive tool result messages → one user content block
//   - ProviderMeta on assistant messages → raw model content (preserves thought_signature)
func toGeminiToolContents(messages []core.Message) (string, []map[string]any) {
	systemParts := make([]string, 0, 4)
	contents := make([]map[string]any, 0, len(messages))

	i := 0
	for i < len(messages) {
		msg := messages[i]
		text := strings.TrimSpace(msg.Content)

		switch msg.Role {
		case core.RoleSystem:
			if text != "" {
				systemParts = append(systemParts, text)
			}
			i++

		case core.RoleAssistant:
			if strings.TrimSpace(msg.ToolCallID) != "" && strings.TrimSpace(msg.ToolName) != "" {
				// Tool call assistant message(s). Check if the first one carries
				// the raw model content block (with thought_signature etc.).
				if len(msg.ProviderMeta) > 0 {
					rawContent, frParts, next := collectToolTurnWithRaw(messages, i)
					i = next
					if rawContent != nil {
						contents = append(contents, rawContent)
					}
					if len(frParts) > 0 {
						contents = append(contents, map[string]any{
							"role":  "user",
							"parts": frParts,
						})
					}
					continue
				}
				// No raw content — reconstruct. Group consecutive assistant+tool
				// pairs into batched model/user content blocks.
				fcParts, frParts, next := collectToolTurnPair(messages, i)
				i = next
				if len(fcParts) > 0 {
					contents = append(contents, map[string]any{
						"role":  "model",
						"parts": fcParts,
					})
				}
				if len(frParts) > 0 {
					contents = append(contents, map[string]any{
						"role":  "user",
						"parts": frParts,
					})
				}
				continue
			}
			// Plain text assistant message.
			if text == "" {
				i++
				continue
			}
			contents = append(contents, map[string]any{
				"role": "model",
				"parts": []map[string]any{
					{"text": text},
				},
			})
			i++

		case core.RoleTool:
			// Orphan tool messages (shouldn't happen, but handle gracefully).
			_, frParts, next := collectToolTurnPair(messages, i)
			i = next
			if len(frParts) > 0 {
				contents = append(contents, map[string]any{
					"role":  "user",
					"parts": frParts,
				})
			}

		default:
			// User message — may include images.
			parts := make([]map[string]any, 0, len(msg.Images)+1)
			for _, img := range msg.Images {
				parts = append(parts, map[string]any{
					"inline_data": map[string]any{
						"mime_type": img.MimeType,
						"data":      img.Data,
					},
				})
			}
			if text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
			if len(parts) == 0 {
				i++
				continue
			}
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": parts,
			})
			i++
		}
	}

	return strings.Join(systemParts, "\n\n"), contents
}

// collectToolTurnPair scans messages starting at idx, collecting interleaved
// assistant-with-ToolCallID and tool messages into batched functionCall and
// functionResponse part arrays. Returns (fcParts, frParts, nextIndex).
func collectToolTurnPair(messages []core.Message, idx int) ([]map[string]any, []map[string]any, int) {
	var fcParts []map[string]any
	var frParts []map[string]any
	i := idx
	for i < len(messages) {
		msg := messages[i]
		if msg.Role == core.RoleAssistant && strings.TrimSpace(msg.ToolCallID) != "" {
			args := parseJSONOrWrap(strings.TrimSpace(msg.Content))
			fcParts = append(fcParts, map[string]any{
				"functionCall": map[string]any{
					"name": strings.TrimSpace(msg.ToolName),
					"args": args,
				},
			})
			i++
			continue
		}
		if msg.Role == core.RoleTool {
			toolName := strings.TrimSpace(msg.ToolName)
			if toolName == "" {
				toolName = "unknown_tool"
			}
			frParts = append(frParts, map[string]any{
				"functionResponse": map[string]any{
					"name":     toolName,
					"response": parseJSONOrWrap(strings.TrimSpace(msg.Content)),
				},
			})
			i++
			continue
		}
		break
	}
	return fcParts, frParts, i
}

// collectToolTurnWithRaw handles a model turn where the first assistant message
// carries ProviderMeta (raw model content block). It outputs the raw block
// directly and collects tool results into a functionResponse user block.
// Returns (modelContent, frParts, nextIndex).
func collectToolTurnWithRaw(messages []core.Message, idx int) (map[string]any, []map[string]any, int) {
	var rawContent map[string]any
	_ = json.Unmarshal(messages[idx].ProviderMeta, &rawContent)

	var frParts []map[string]any
	i := idx
	for i < len(messages) {
		msg := messages[i]
		if msg.Role == core.RoleAssistant && strings.TrimSpace(msg.ToolCallID) != "" {
			// Skip — already represented in the raw content block.
			i++
			continue
		}
		if msg.Role == core.RoleTool {
			toolName := strings.TrimSpace(msg.ToolName)
			if toolName == "" {
				toolName = "unknown_tool"
			}
			frParts = append(frParts, map[string]any{
				"functionResponse": map[string]any{
					"name":     toolName,
					"response": parseJSONOrWrap(strings.TrimSpace(msg.Content)),
				},
			})
			i++
			continue
		}
		break
	}
	return rawContent, frParts, i
}

// ---------------------------------------------------------------------------
// Response extraction
// ---------------------------------------------------------------------------

// geminiExtractResult bundles the return values of extraction functions.
type geminiExtractResult struct {
	Text            string
	Calls           []ToolCall
	Usage           core.UsageInfo
	RawModelContent json.RawMessage // raw candidate content (preserves thought_signature)
	OK              bool
}

// extractToolCallsFromGeminiResponse parses a Gemini generateContent response
// body and extracts text + functionCall parts from candidates.
// Handles both plain JSON and SSE (data: ...) formats, as well as responses
// wrapped in a "response" envelope.
func extractToolCallsFromGeminiResponse(body []byte) geminiExtractResult {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return geminiExtractResult{}
	}

	// SSE mode: parse each "data:" event and accumulate results.
	if strings.Contains(trimmed, "data:") {
		return extractToolCallsFromGeminiSSE(trimmed)
	}

	return extractToolCallsFromGeminiJSON(body)
}

// extractToolCallsFromGeminiSSE parses SSE events and accumulates text + functionCall
// parts across all events.
func extractToolCallsFromGeminiSSE(raw string) geminiExtractResult {
	lines := strings.Split(raw, "\n")
	var textParts []string
	var calls []ToolCall
	var usage core.UsageInfo
	var rawModelContent json.RawMessage
	var eventData []string

	flush := func() {
		if len(eventData) == 0 {
			return
		}
		chunk := strings.TrimSpace(strings.Join(eventData, "\n"))
		eventData = eventData[:0]
		if chunk == "" || chunk == "[DONE]" {
			return
		}
		r := extractToolCallsFromGeminiJSON([]byte(chunk))
		if !r.OK {
			return
		}
		if r.Text != "" {
			textParts = append(textParts, r.Text)
		}
		calls = append(calls, r.Calls...)
		if r.Usage.InputTokens > 0 || r.Usage.OutputTokens > 0 {
			usage = r.Usage
		}
		// Keep raw content from any event that contains function calls.
		if len(r.Calls) > 0 && len(r.RawModelContent) > 0 {
			rawModelContent = r.RawModelContent
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			continue
		}
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		eventData = append(eventData, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
	}
	flush()

	if len(textParts) == 0 && len(calls) == 0 {
		return geminiExtractResult{}
	}
	return geminiExtractResult{
		Text:            strings.Join(textParts, ""),
		Calls:           calls,
		Usage:           usage,
		RawModelContent: rawModelContent,
		OK:              true,
	}
}

// extractToolCallsFromGeminiJSON parses a single JSON response body.
func extractToolCallsFromGeminiJSON(body []byte) geminiExtractResult {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return geminiExtractResult{}
	}

	// Gemini Internal wraps the actual response inside "response".
	actual := root
	if response, ok := root["response"].(map[string]any); ok {
		actual = response
	}

	candidates, _ := actual["candidates"].([]any)
	if len(candidates) == 0 {
		return geminiExtractResult{}
	}

	var textParts []string
	var calls []ToolCall
	var rawModelContent json.RawMessage

	for _, rawCandidate := range candidates {
		candidate, _ := rawCandidate.(map[string]any)
		if candidate == nil {
			continue
		}
		content, _ := candidate["content"].(map[string]any)
		if content == nil {
			continue
		}
		parts, _ := content["parts"].([]any)
		hasFunctionCall := false
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			if part == nil {
				continue
			}
			if txt, ok := part["text"].(string); ok && strings.TrimSpace(txt) != "" {
				textParts = append(textParts, txt)
			}
			if fc, ok := part["functionCall"].(map[string]any); ok {
				hasFunctionCall = true
				name, _ := fc["name"].(string)
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				var args json.RawMessage
				if argsRaw, ok := fc["args"]; ok {
					if encoded, err := json.Marshal(argsRaw); err == nil {
						args = encoded
					}
				}
				if len(args) == 0 {
					args = json.RawMessage(`{}`)
				}
				calls = append(calls, ToolCall{
					ID:        generateGeminiToolCallID(),
					Name:      name,
					Arguments: args,
				})
			}
		}
		// Preserve the raw candidate content when it has function calls
		// (includes thought_signature and any other provider-specific fields).
		if hasFunctionCall {
			if encoded, err := json.Marshal(content); err == nil {
				rawModelContent = encoded
			}
		}
	}

	if len(textParts) == 0 && len(calls) == 0 {
		return geminiExtractResult{}
	}

	usage := parseUsageMetadata(actual)

	return geminiExtractResult{
		Text:            strings.Join(textParts, ""),
		Calls:           calls,
		Usage:           usage,
		RawModelContent: rawModelContent,
		OK:              true,
	}
}

// buildGeminiGenerationConfig creates a generationConfig map from GenerationParams.
func buildGeminiGenerationConfig(gen *GenerationParams) map[string]any {
	if gen == nil {
		return nil
	}
	config := map[string]any{}
	if gen.Temperature != nil {
		config["temperature"] = *gen.Temperature
	}
	if gen.TopP != nil {
		config["topP"] = *gen.TopP
	}
	if gen.FrequencyPenalty != nil {
		config["frequencyPenalty"] = *gen.FrequencyPenalty
	}
	if gen.PresencePenalty != nil {
		config["presencePenalty"] = *gen.PresencePenalty
	}
	if len(config) == 0 {
		return nil
	}
	return config
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// parseJSONOrWrap tries to parse s as JSON. If it succeeds, returns the parsed
// value. Otherwise wraps it in {"result": s}.
func parseJSONOrWrap(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(s), &parsed); err == nil {
		return parsed
	}
	return map[string]any{"result": s}
}

// geminiUnsupportedSchemaKeys lists JSON Schema fields that the Gemini API
// does not accept in function declaration parameters.
var geminiUnsupportedSchemaKeys = map[string]bool{
	"additionalProperties": true,
	"$schema":              true,
}

// stripUnsupportedSchemaFields recursively removes JSON Schema fields that
// the Gemini API rejects (e.g. additionalProperties, $schema).
func stripUnsupportedSchemaFields(v any) {
	switch node := v.(type) {
	case map[string]any:
		for key := range geminiUnsupportedSchemaKeys {
			delete(node, key)
		}
		for _, child := range node {
			stripUnsupportedSchemaFields(child)
		}
	case []any:
		for _, item := range node {
			stripUnsupportedSchemaFields(item)
		}
	}
}

// generateGeminiToolCallID creates a unique ID for Gemini tool calls since
// the Gemini API does not return tool call IDs.
func generateGeminiToolCallID() string {
	return fmt.Sprintf("gemini-tc-%d", time.Now().UnixNano())
}
