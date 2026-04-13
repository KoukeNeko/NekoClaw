package provider

import "strings"

// getModelToolGuidance returns model-family-specific operational guidance
// for tool calling. The guidance is injected into the system instruction
// alongside tool definitions to improve tool usage quality for different
// model families. Returns empty string if no specific guidance applies.
func getModelToolGuidance(providerID, modelID string) string {
	normalized := strings.ToLower(strings.TrimSpace(modelID))
	normalized = strings.TrimPrefix(normalized, "models/")

	// Open-source models need the most detailed guidance since they have
	// less training on function calling conventions.
	if isOpenSourceModel(normalized) {
		return openSourceToolGuidance
	}

	switch {
	case strings.HasPrefix(normalized, "gemini-"):
		return geminiToolGuidance
	case isGPTFamily(providerID, normalized):
		return gptToolGuidance
	case strings.HasPrefix(normalized, "claude-"):
		return claudeToolGuidance
	}

	return ""
}

func isOpenSourceModel(model string) bool {
	prefixes := []string{
		"llama-", "llama3", "llama4",
		"mistral-",
		"qwen-", "qwen2", "qwen3",
		"deepseek-",
		"phi-",
		"yi-",
		"codestral-",
		"mixtral-",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

func isGPTFamily(providerID, model string) bool {
	if strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
		return true
	}
	if providerID == "openai" || providerID == "opencode" {
		return true
	}
	return false
}

const openSourceToolGuidance = `Tool Calling Guidelines:
- You MUST use the exact tool names and parameter names provided. Do not invent tool names.
- Arguments must be valid JSON. String values must be quoted. Numbers and booleans must NOT be quoted.
- Always provide ALL required parameters for each tool call.
- If a tool returns an error, read the error message carefully and adjust your approach.
- Do not repeat the same tool call with the same arguments if it already failed.
- Be concise in responses. Avoid unnecessary explanation before or after tool calls.`

const geminiToolGuidance = `Tool Calling Guidelines:
- Be concise. Avoid unnecessary explanation before or after tool calls.
- Use absolute file paths when calling file tools.
- Verify the current state before making edits (read before write).
- When multiple independent reads are needed, request them all in a single turn.`

const gptToolGuidance = `Tool Calling Guidelines:
- Tool results persist across turns. Do not re-call a tool if you already have its output.
- Check prerequisites before performing actions (verify file exists before reading).
- Verify outputs match expectations after each mutating tool call.
- Never fabricate tool output. If a tool fails, report the error accurately.`

const claudeToolGuidance = `Tool Calling Guidelines:
- When performing multi-step operations, plan the full sequence before starting.
- Verify the result after file modifications to confirm correctness.`
