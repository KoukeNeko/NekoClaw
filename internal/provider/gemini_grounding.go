package provider

import (
	"encoding/json"
	"strings"

	"github.com/doeshing/nekoclaw/internal/core"
)

// Aliases so the rest of the provider package can refer to the canonical
// types defined in core without the import dance.
type (
	GroundingMetadata = core.GroundingMetadata
	GroundingSource   = core.GroundingSource
	GroundingSupport  = core.GroundingSupport
)

// extractGroundingMetadata parses a Gemini candidate's groundingMetadata
// block into a typed GroundingMetadata. Returns nil when the candidate
// has no grounding data.
//
// Schema reference (Gemini API):
//
//	groundingMetadata: {
//	  webSearchQueries: ["..."],
//	  groundingChunks: [{ web: { uri: "...", title: "..." } }],
//	  groundingSupports: [{
//	    segment:           { text: "..." },
//	    groundingChunkIndices: [0, 2],
//	  }]
//	}
func extractGroundingMetadata(candidate map[string]any) *GroundingMetadata {
	if candidate == nil {
		return nil
	}
	rawMeta, ok := candidate["groundingMetadata"].(map[string]any)
	if !ok || rawMeta == nil {
		return nil
	}

	queries := extractStringList(rawMeta["webSearchQueries"])
	sources := extractGroundingSources(rawMeta["groundingChunks"])
	supports := extractGroundingSupports(rawMeta["groundingSupports"])

	if len(queries) == 0 && len(sources) == 0 && len(supports) == 0 {
		return nil
	}

	return &GroundingMetadata{
		SearchQueries: queries,
		Sources:       sources,
		Supports:      supports,
	}
}

func extractStringList(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractGroundingSources(raw any) []GroundingSource {
	chunks, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]GroundingSource, 0, len(chunks))
	for _, rawChunk := range chunks {
		chunk, ok := rawChunk.(map[string]any)
		if !ok {
			continue
		}
		web, ok := chunk["web"].(map[string]any)
		if !ok {
			continue
		}
		uri, _ := web["uri"].(string)
		title, _ := web["title"].(string)
		uri = strings.TrimSpace(uri)
		title = strings.TrimSpace(title)
		if uri == "" && title == "" {
			continue
		}
		out = append(out, GroundingSource{URI: uri, Title: title})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractGroundingSupports(raw any) []GroundingSupport {
	supports, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]GroundingSupport, 0, len(supports))
	for _, rawSupport := range supports {
		support, ok := rawSupport.(map[string]any)
		if !ok {
			continue
		}
		var text string
		if seg, ok := support["segment"].(map[string]any); ok {
			if t, ok := seg["text"].(string); ok {
				text = strings.TrimSpace(t)
			}
		}
		indices := extractIntList(support["groundingChunkIndices"])
		if text == "" && len(indices) == 0 {
			continue
		}
		out = append(out, GroundingSupport{Text: text, SourceIndices: indices})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractIntList(raw any) []int {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case float64:
			out = append(out, int(v))
		case int:
			out = append(out, v)
		case int64:
			out = append(out, int(v))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// googleSearchToolKeyV2 is the tool key for Gemini 2.x and 3.x.
const googleSearchToolKeyV2 = "google_search"

// googleSearchToolKeyV15 is the tool key used by Gemini 1.5 (legacy).
const googleSearchToolKeyV15 = "googleSearchRetrieval"

// geminiModelSupportsGoogleSearch reports whether a Gemini model accepts
// the built-in Google Search grounding tool, and returns the correct
// tool key for the model family.
//
// Returns:
//   - toolKey: "google_search" for 2.x/3.x, "googleSearchRetrieval" for 1.5,
//     or "" if the model does not support Google Search grounding.
//   - supported: true if the tool key can be injected.
//
// Gemma, Llama, Mistral, Qwen and other open-source models served via
// Google AI Studio do NOT support this built-in tool — only Gemini does.
func geminiModelSupportsGoogleSearch(model string) (toolKey string, supported bool) {
	normalized := normalizeGeminiModelID(model)
	if normalized == "" {
		return "", false
	}

	// Only the gemini-* families accept the built-in Google Search tool.
	if !strings.HasPrefix(normalized, "gemini-") {
		return "", false
	}

	// Gemini 1.5 uses the legacy googleSearchRetrieval key.
	if strings.HasPrefix(normalized, "gemini-1.5") {
		return googleSearchToolKeyV15, true
	}

	// Gemini 2.x and 3.x use the unified google_search key.
	if strings.HasPrefix(normalized, "gemini-2.") ||
		strings.HasPrefix(normalized, "gemini-3.") ||
		strings.HasPrefix(normalized, "gemini-2-") ||
		strings.HasPrefix(normalized, "gemini-3-") {
		return googleSearchToolKeyV2, true
	}

	return "", false
}

// appendGoogleSearchTool appends the built-in Google Search tool entry to a
// Gemini tools array. It chooses the correct tool key based on the model and
// is a no-op when the model does not support search grounding.
//
// Returns the (possibly extended) tools slice. Callers should pass the slice
// produced by toGeminiFunctionDeclarations so that custom function
// declarations and the built-in tool coexist for Gemini 2.x+.
func appendGoogleSearchTool(tools []map[string]any, modelID string) []map[string]any {
	toolKey, supported := geminiModelSupportsGoogleSearch(modelID)
	if !supported {
		return tools
	}
	return append(tools, map[string]any{toolKey: map[string]any{}})
}

// extractGroundingFromBody parses a raw JSON Gemini response body and
// returns the merged grounding metadata across all candidates. Returns nil
// when no grounding is present. Tolerates the {"response": {...}} wrapper
// used by Gemini Internal as well as the bare AI Studio shape.
func extractGroundingFromBody(body []byte) *GroundingMetadata {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil
	}
	actual := root
	if response, ok := root["response"].(map[string]any); ok {
		actual = response
	}
	candidates, _ := actual["candidates"].([]any)
	if len(candidates) == 0 {
		return nil
	}
	var merged *GroundingMetadata
	for _, rawCandidate := range candidates {
		candidate, _ := rawCandidate.(map[string]any)
		merged = mergeGrounding(merged, extractGroundingMetadata(candidate))
	}
	return merged
}

// hasFunctionDeclarations reports whether a Gemini tools slice already
// contains custom function_declarations. Used to detect the Gemini 1.5
// constraint where googleSearchRetrieval cannot coexist with functions.
func hasFunctionDeclarations(tools []map[string]any) bool {
	for _, entry := range tools {
		if _, ok := entry["function_declarations"]; ok {
			return true
		}
	}
	return false
}

// maybeInjectGoogleSearch returns the tools slice with the built-in Google
// Search tool appended when:
//   - The caller requested it (enabled = true)
//   - The model supports it (Gemini 2.x / 3.x / 1.5)
//   - The XML fallback path is not active (search is incompatible with that)
//   - For Gemini 1.5: there are no custom function_declarations
//     (the API rejects mixing googleSearchRetrieval + functions)
//
// The function logs a warn line when the request is silently dropped so
// operators can diagnose missing grounding without it being silent.
func maybeInjectGoogleSearch(
	tools []map[string]any,
	enabled bool,
	useXMLTools bool,
	providerID string,
	modelID string,
) []map[string]any {
	if !enabled {
		return tools
	}
	if useXMLTools {
		logProvider.Warnf(
			"google_search ignored: provider=%s model=%s reason=xml_fallback_active",
			providerID, modelID,
		)
		return tools
	}
	toolKey, supported := geminiModelSupportsGoogleSearch(modelID)
	if !supported {
		logProvider.Warnf(
			"google_search ignored: provider=%s model=%s reason=model_not_supported",
			providerID, modelID,
		)
		return tools
	}
	if toolKey == googleSearchToolKeyV15 && hasFunctionDeclarations(tools) {
		logProvider.Warnf(
			"google_search ignored: provider=%s model=%s reason=conflicts_with_functions_on_gemini_1_5",
			providerID, modelID,
		)
		return tools
	}
	return append(tools, map[string]any{toolKey: map[string]any{}})
}
