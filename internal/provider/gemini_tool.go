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
// Message role mapping:
//   - RoleSystem        → collected into systemInstruction
//   - RoleAssistant + ToolCallID + ToolName → model role, functionCall part
//   - RoleAssistant (plain)                 → model role, text part
//   - RoleTool                              → user role, functionResponse part
//   - default (user)                        → user role, text + inline_data parts
func toGeminiToolContents(messages []core.Message) (string, []map[string]any) {
	systemParts := make([]string, 0, 4)
	contents := make([]map[string]any, 0, len(messages))

	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)

		switch msg.Role {
		case core.RoleSystem:
			if text != "" {
				systemParts = append(systemParts, text)
			}

		case core.RoleAssistant:
			if strings.TrimSpace(msg.ToolCallID) != "" && strings.TrimSpace(msg.ToolName) != "" {
				// Assistant message that represents a tool call (functionCall).
				args := parseJSONOrWrap(text)
				contents = append(contents, map[string]any{
					"role": "model",
					"parts": []map[string]any{
						{
							"functionCall": map[string]any{
								"name": strings.TrimSpace(msg.ToolName),
								"args": args,
							},
						},
					},
				})
				continue
			}
			if text == "" {
				continue
			}
			contents = append(contents, map[string]any{
				"role": "model",
				"parts": []map[string]any{
					{"text": text},
				},
			})

		case core.RoleTool:
			toolName := strings.TrimSpace(msg.ToolName)
			if toolName == "" {
				toolName = "unknown_tool"
			}
			responseContent := parseJSONOrWrap(text)
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{
						"functionResponse": map[string]any{
							"name":     toolName,
							"response": responseContent,
						},
					},
				},
			})

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
				continue
			}
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": parts,
			})
		}
	}

	return strings.Join(systemParts, "\n\n"), contents
}

// extractToolCallsFromGeminiResponse parses a Gemini generateContent JSON
// response body and extracts text + functionCall parts from candidates.
// Works for both raw responses and those wrapped in a "response" envelope.
func extractToolCallsFromGeminiResponse(body []byte) (string, []ToolCall, core.UsageInfo, bool) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return "", nil, core.UsageInfo{}, false
	}

	// Gemini Internal wraps the actual response inside "response".
	actual := root
	if response, ok := root["response"].(map[string]any); ok {
		actual = response
	}

	candidates, _ := actual["candidates"].([]any)
	if len(candidates) == 0 {
		return "", nil, core.UsageInfo{}, false
	}

	var textParts []string
	var calls []ToolCall

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
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			if part == nil {
				continue
			}
			if txt, ok := part["text"].(string); ok && strings.TrimSpace(txt) != "" {
				textParts = append(textParts, txt)
			}
			if fc, ok := part["functionCall"].(map[string]any); ok {
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
	}

	if len(textParts) == 0 && len(calls) == 0 {
		return "", nil, core.UsageInfo{}, false
	}

	usage := parseUsageMetadata(actual)

	return strings.Join(textParts, ""), calls, usage, true
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

// generateGeminiToolCallID creates a unique ID for Gemini tool calls since
// the Gemini API does not return tool call IDs.
func generateGeminiToolCallID() string {
	return fmt.Sprintf("gemini-tc-%d", time.Now().UnixNano())
}
