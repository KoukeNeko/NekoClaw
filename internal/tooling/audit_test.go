package tooling

import (
	"strings"
	"testing"
)

func TestTruncateHeadTailShortInput(t *testing.T) {
	input := "hello world"
	got := truncateHeadTail(input, 100)
	if got != input {
		t.Fatalf("expected unchanged input, got %q", got)
	}
}

func TestTruncateHeadTailExactLimit(t *testing.T) {
	input := strings.Repeat("a", 500)
	got := truncateHeadTail(input, 500)
	if got != input {
		t.Fatalf("expected unchanged input at exact limit")
	}
}

func TestTruncateHeadTailLongInput(t *testing.T) {
	input := strings.Repeat("H", 500) + strings.Repeat("M", 9000) + strings.Repeat("T", 500)
	got := truncateHeadTail(input, 1000)

	// Output should be <= 1000 chars.
	if len(got) > 1000 {
		t.Fatalf("expected output <= 1000 chars, got %d", len(got))
	}
	// Should start with "H"s (head preserved).
	if !strings.HasPrefix(got, "HHH") {
		t.Fatal("head not preserved")
	}
	// Should end with "T"s (tail preserved).
	if !strings.HasSuffix(got, "TTT") {
		t.Fatal("tail not preserved")
	}
	// Should contain truncation marker.
	if !strings.Contains(got, "[...truncated") {
		t.Fatal("missing truncation marker")
	}
}

func TestTruncateHeadTailVerySmallLimit(t *testing.T) {
	input := strings.Repeat("x", 500)
	got := truncateHeadTail(input, 50)
	// Should fall back to head-only for very small limits.
	if len(got) > 50 {
		t.Fatalf("expected output <= 50 chars, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatal("expected ... suffix for small limit fallback")
	}
}

func TestTruncateHeadTailZeroLimit(t *testing.T) {
	input := "some text"
	got := truncateHeadTail(input, 0)
	if got != input {
		t.Fatalf("expected unchanged for zero limit, got %q", got)
	}
}

func TestTrimPreviewBasic(t *testing.T) {
	input := strings.Repeat("a", 300)
	got := trimPreview(input, 200)
	if len(got) != 203 { // 200 + "..."
		t.Fatalf("expected 203 chars, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatal("expected ... suffix")
	}
}
