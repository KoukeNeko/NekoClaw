package provider

// modelContextWindows maps known model name prefixes to their context window
// sizes. When a provider's ContextWindow(model) is called, it checks this map
// before falling back to the provider-level default.
//
// Models are matched by longest-prefix-first, so "gemini-2.5-pro" matches
// before "gemini-2.5". This allows specific overrides for variants.
var modelContextWindows = map[string]int{
	// Gemini models
	"gemini-3.1-pro":       2_000_000,
	"gemini-3-pro":         1_000_000,
	"gemini-3-flash":       1_000_000,
	"gemini-2.5-pro":       1_000_000,
	"gemini-2.5-flash":     1_000_000,
	"gemini-2.0-flash":     1_000_000,
	"gemini-1.5-pro":       2_000_000,
	"gemini-1.5-flash":     1_000_000,

	// Anthropic models
	"claude-opus-4":        200_000,
	"claude-sonnet-4":      200_000,
	"claude-haiku-3":       200_000,

	// OpenAI models
	"gpt-5":                200_000,
	"gpt-5.1":              200_000,
	"gpt-5.3":              200_000,
	"gpt-4.1":              1_000_000,
	"gpt-4o":               128_000,
	"gpt-4-turbo":          128_000,
	"o3":                   200_000,
	"o4-mini":              200_000,
}

// lookupModelContextWindow returns the context window for a known model,
// or 0 if no match is found. Matches by longest prefix first.
func lookupModelContextWindow(model string) int {
	if model == "" {
		return 0
	}
	// Try exact match first.
	if cw, ok := modelContextWindows[model]; ok {
		return cw
	}
	// Try longest prefix match.
	bestLen := 0
	bestCW := 0
	for prefix, cw := range modelContextWindows {
		if len(prefix) > bestLen && len(model) >= len(prefix) && model[:len(prefix)] == prefix {
			bestLen = len(prefix)
			bestCW = cw
		}
	}
	return bestCW
}
