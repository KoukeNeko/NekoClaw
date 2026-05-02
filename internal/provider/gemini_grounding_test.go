package provider

import (
	"encoding/json"
	"testing"
)

func TestGeminiModelSupportsGoogleSearch(t *testing.T) {
	tests := []struct {
		model   string
		wantKey string
		wantOK  bool
	}{
		// Gemini 2.x / 3.x → google_search
		{"gemini-2.5-pro", googleSearchToolKeyV2, true},
		{"gemini-2.5-flash", googleSearchToolKeyV2, true},
		{"gemini-2.0-flash", googleSearchToolKeyV2, true},
		{"gemini-3.1-pro-preview", googleSearchToolKeyV2, true},
		{"gemini-3.1-flash", googleSearchToolKeyV2, true},
		{"gemini-3-pro", googleSearchToolKeyV2, true},
		{"models/gemini-2.5-flash", googleSearchToolKeyV2, true},

		// Gemini 1.5 → googleSearchRetrieval (legacy)
		{"gemini-1.5-pro", googleSearchToolKeyV15, true},
		{"gemini-1.5-flash", googleSearchToolKeyV15, true},
		{"models/gemini-1.5-pro-latest", googleSearchToolKeyV15, true},

		// Gemma, Llama, Mistral, Qwen, etc. → unsupported
		{"gemma-3-27b", "", false},
		{"gemma-4-31b-it", "", false},
		{"llama-3.1-70b", "", false},
		{"mistral-large", "", false},
		{"qwen2-72b", "", false},
		{"deepseek-v3", "", false},
		{"phi-3-mini", "", false},

		// Edge cases
		{"", "", false},
		{"unknown-model", "", false},
		{"gemini", "", false}, // no version suffix
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			gotKey, gotOK := geminiModelSupportsGoogleSearch(tt.model)
			if gotOK != tt.wantOK {
				t.Errorf("geminiModelSupportsGoogleSearch(%q) supported = %v, want %v", tt.model, gotOK, tt.wantOK)
			}
			if gotKey != tt.wantKey {
				t.Errorf("geminiModelSupportsGoogleSearch(%q) toolKey = %q, want %q", tt.model, gotKey, tt.wantKey)
			}
		})
	}
}

func TestAppendGoogleSearchTool(t *testing.T) {
	t.Run("supported model appends google_search", func(t *testing.T) {
		tools := []map[string]any{
			{"function_declarations": []any{map[string]any{"name": "file_read"}}},
		}
		got := appendGoogleSearchTool(tools, "gemini-2.5-flash")
		if len(got) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(got))
		}
		entry, ok := got[1][googleSearchToolKeyV2]
		if !ok {
			t.Fatalf("expected %q key in appended entry, got %v", googleSearchToolKeyV2, got[1])
		}
		if cfg, ok := entry.(map[string]any); !ok || len(cfg) != 0 {
			t.Errorf("expected empty config map for google_search, got %v", entry)
		}
	})

	t.Run("legacy model appends googleSearchRetrieval", func(t *testing.T) {
		got := appendGoogleSearchTool(nil, "gemini-1.5-pro")
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(got))
		}
		if _, ok := got[0][googleSearchToolKeyV15]; !ok {
			t.Errorf("expected %q key, got %v", googleSearchToolKeyV15, got[0])
		}
	})

	t.Run("unsupported model returns input unchanged", func(t *testing.T) {
		tools := []map[string]any{
			{"function_declarations": []any{map[string]any{"name": "file_read"}}},
		}
		got := appendGoogleSearchTool(tools, "gemma-3-27b")
		if len(got) != 1 {
			t.Errorf("expected unchanged length 1, got %d", len(got))
		}
	})

	t.Run("unsupported model with nil input returns nil", func(t *testing.T) {
		got := appendGoogleSearchTool(nil, "llama-3.1-70b")
		if got != nil {
			t.Errorf("expected nil for unsupported model with nil input, got %v", got)
		}
	})
}

// realGeminiResponseWithGrounding is a representative AI Studio response
// fixture for a query that triggered google_search grounding.
const realGeminiResponseWithGrounding = `{
  "candidates": [
    {
      "content": {
        "parts": [
          { "text": "The latest iPhone is iPhone 17, announced in September 2025." }
        ],
        "role": "model"
      },
      "finishReason": "STOP",
      "groundingMetadata": {
        "webSearchQueries": ["latest iPhone model 2025", "iPhone 17 release date"],
        "groundingChunks": [
          { "web": { "uri": "https://apple.com/iphone", "title": "Apple iPhone" } },
          { "web": { "uri": "https://example.com/news/iphone17", "title": "iPhone 17 News" } }
        ],
        "groundingSupports": [
          {
            "segment": { "text": "The latest iPhone is iPhone 17" },
            "groundingChunkIndices": [0, 1]
          }
        ]
      }
    }
  ],
  "usageMetadata": {
    "promptTokenCount": 12,
    "candidatesTokenCount": 18,
    "totalTokenCount": 30
  }
}`

func TestExtractGroundingMetadata_RealResponse(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal([]byte(realGeminiResponseWithGrounding), &root); err != nil {
		t.Fatalf("fixture unmarshal: %v", err)
	}
	candidates := root["candidates"].([]any)
	candidate := candidates[0].(map[string]any)

	got := extractGroundingMetadata(candidate)
	if got == nil {
		t.Fatal("expected grounding metadata, got nil")
	}

	if want := []string{"latest iPhone model 2025", "iPhone 17 release date"}; !equalStrings(got.SearchQueries, want) {
		t.Errorf("SearchQueries = %v, want %v", got.SearchQueries, want)
	}

	if len(got.Sources) != 2 {
		t.Fatalf("Sources len = %d, want 2", len(got.Sources))
	}
	if got.Sources[0].URI != "https://apple.com/iphone" || got.Sources[0].Title != "Apple iPhone" {
		t.Errorf("Sources[0] = %+v", got.Sources[0])
	}
	if got.Sources[1].URI != "https://example.com/news/iphone17" {
		t.Errorf("Sources[1].URI = %q", got.Sources[1].URI)
	}

	if len(got.Supports) != 1 {
		t.Fatalf("Supports len = %d, want 1", len(got.Supports))
	}
	if got.Supports[0].Text != "The latest iPhone is iPhone 17" {
		t.Errorf("Supports[0].Text = %q", got.Supports[0].Text)
	}
	if !equalInts(got.Supports[0].SourceIndices, []int{0, 1}) {
		t.Errorf("Supports[0].SourceIndices = %v", got.Supports[0].SourceIndices)
	}
}

func TestExtractGroundingMetadata_NoMetadata(t *testing.T) {
	candidate := map[string]any{
		"content": map[string]any{
			"parts": []any{map[string]any{"text": "plain answer"}},
		},
	}
	if got := extractGroundingMetadata(candidate); got != nil {
		t.Errorf("expected nil for candidate without grounding, got %+v", got)
	}
}

func TestExtractGroundingMetadata_EmptyMetadata(t *testing.T) {
	candidate := map[string]any{
		"groundingMetadata": map[string]any{},
	}
	if got := extractGroundingMetadata(candidate); got != nil {
		t.Errorf("expected nil for empty groundingMetadata, got %+v", got)
	}
}

func TestExtractGroundingMetadata_PartialFields(t *testing.T) {
	candidate := map[string]any{
		"groundingMetadata": map[string]any{
			"webSearchQueries": []any{"only queries"},
		},
	}
	got := extractGroundingMetadata(candidate)
	if got == nil {
		t.Fatal("expected non-nil for partial metadata")
	}
	if len(got.SearchQueries) != 1 || got.SearchQueries[0] != "only queries" {
		t.Errorf("SearchQueries = %v", got.SearchQueries)
	}
	if got.Sources != nil || got.Supports != nil {
		t.Errorf("expected nil sources/supports, got sources=%v supports=%v", got.Sources, got.Supports)
	}
}

func TestExtractToolCallsFromGeminiJSON_PropagatesGrounding(t *testing.T) {
	r := extractToolCallsFromGeminiJSON([]byte(realGeminiResponseWithGrounding))
	if !r.OK {
		t.Fatal("extract OK = false")
	}
	if r.Grounding == nil {
		t.Fatal("expected grounding to be propagated to extract result")
	}
	if len(r.Grounding.Sources) != 2 {
		t.Errorf("Grounding.Sources len = %d, want 2", len(r.Grounding.Sources))
	}
}

func TestMergeGrounding(t *testing.T) {
	a := &GroundingMetadata{
		SearchQueries: []string{"q1"},
		Sources:       []GroundingSource{{URI: "https://a.example", Title: "A"}},
	}
	b := &GroundingMetadata{
		SearchQueries: []string{"q2"},
		Sources:       []GroundingSource{{URI: "https://b.example", Title: "B"}},
	}
	merged := mergeGrounding(a, b)
	if !equalStrings(merged.SearchQueries, []string{"q1", "q2"}) {
		t.Errorf("merged queries = %v", merged.SearchQueries)
	}
	if len(merged.Sources) != 2 {
		t.Errorf("merged sources len = %d", len(merged.Sources))
	}

	// Empty incoming returns existing.
	if got := mergeGrounding(a, nil); got != a {
		t.Errorf("merge with nil incoming should return existing")
	}
	// Empty existing returns incoming.
	if got := mergeGrounding(nil, a); got != a {
		t.Errorf("merge with nil existing should return incoming")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
