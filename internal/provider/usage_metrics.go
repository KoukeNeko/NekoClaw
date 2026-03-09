package provider

import "github.com/doeshing/nekoclaw/internal/core"

func intPtr(v int) *int {
	value := v
	return &value
}

func buildGeminiUsage(inputTokens, outputTokens, totalTokens int, cachedTokens *int) core.UsageInfo {
	usage := core.UsageInfo{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
	}
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if cachedTokens != nil {
		usage.CachedTokens = intPtr(*cachedTokens)
		usage.PromptTokensTotal = intPtr(usage.InputTokens)
		promptUncached := usage.InputTokens - *cachedTokens
		if promptUncached < 0 {
			promptUncached = 0
		}
		usage.PromptTokensUncached = intPtr(promptUncached)
	}
	return usage
}
