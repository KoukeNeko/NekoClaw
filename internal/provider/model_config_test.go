package provider

import "testing"

func TestLookupModelContextWindowExact(t *testing.T) {
	cw := lookupModelContextWindow("gemini-2.5-pro")
	if cw != 1_000_000 {
		t.Fatalf("expected 1000000 for gemini-2.5-pro, got %d", cw)
	}
}

func TestLookupModelContextWindowPrefix(t *testing.T) {
	// "claude-sonnet-4-6" should match prefix "claude-sonnet-4".
	cw := lookupModelContextWindow("claude-sonnet-4-6")
	if cw != 200_000 {
		t.Fatalf("expected 200000 for claude-sonnet-4-6, got %d", cw)
	}
}

func TestLookupModelContextWindowLongestPrefix(t *testing.T) {
	// "gpt-5.1-codex" should match "gpt-5.1" (len=5) over "gpt-5" (len=5).
	cw := lookupModelContextWindow("gpt-5.1-codex")
	if cw != 200_000 {
		t.Fatalf("expected 200000 for gpt-5.1-codex, got %d", cw)
	}
}

func TestLookupModelContextWindowUnknown(t *testing.T) {
	cw := lookupModelContextWindow("unknown-model-xyz")
	if cw != 0 {
		t.Fatalf("expected 0 for unknown model, got %d", cw)
	}
}

func TestLookupModelContextWindowEmpty(t *testing.T) {
	cw := lookupModelContextWindow("")
	if cw != 0 {
		t.Fatalf("expected 0 for empty model, got %d", cw)
	}
}
