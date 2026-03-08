package tokenutil

import (
	"strings"
	"testing"
)

func TestEstimateStringEnglish(t *testing.T) {
	// 400 English characters should yield ~100 tokens (400/4).
	text := strings.Repeat("abcd", 100)
	got := EstimateString(text)
	if got < 90 || got > 110 {
		t.Fatalf("English 400 chars: expected ~100 tokens, got %d", got)
	}
}

func TestEstimateStringCJK(t *testing.T) {
	// 100 CJK characters at 1.5 tokens each → ~150 tokens.
	text := strings.Repeat("你好世界測試", 17) // 102 CJK chars
	got := EstimateString(text)
	if got < 130 || got > 170 {
		t.Fatalf("CJK 102 chars: expected ~153 tokens, got %d", got)
	}
}

func TestEstimateStringMixed(t *testing.T) {
	// 50 CJK + 200 English chars → 50*1.5 + 200/4 = 75 + 50 = ~125 tokens.
	cjk := strings.Repeat("中", 50)
	eng := strings.Repeat("a", 200)
	text := cjk + eng
	got := EstimateString(text)
	if got < 110 || got > 140 {
		t.Fatalf("Mixed 50 CJK + 200 eng: expected ~125 tokens, got %d", got)
	}
}

func TestEstimateStringCJKHigherThanOldEstimate(t *testing.T) {
	// The old estimator would give 100 CJK chars → 25 tokens (100/4).
	// The new CJK-aware estimator should give ~150 tokens (100*1.5).
	text := strings.Repeat("啊", 100)
	got := EstimateString(text)
	oldEstimate := 100 / latinCharsPerToken // 25

	if got <= oldEstimate*2 {
		t.Fatalf("CJK estimation should be significantly higher than old: old=%d new=%d", oldEstimate, got)
	}
}

func TestEstimateStringWithOverheadAddsEnvelope(t *testing.T) {
	text := "hello"
	bare := EstimateString(text)
	withOverhead := EstimateStringWithOverhead(text)
	if withOverhead <= bare {
		t.Fatalf("WithOverhead (%d) should exceed bare (%d)", withOverhead, bare)
	}
}

func TestEstimateStringEmpty(t *testing.T) {
	got := EstimateString("")
	if got != 1 {
		t.Fatalf("empty string: expected 1, got %d", got)
	}
}

func TestIsCJKCoversMainRanges(t *testing.T) {
	cases := []struct {
		r    rune
		want bool
		name string
	}{
		{'a', false, "ASCII letter"},
		{'1', false, "ASCII digit"},
		{' ', false, "space"},
		{'中', true, "CJK Unified Ideograph"},
		{'あ', true, "Hiragana"},
		{'カ', true, "Katakana"},
		{'한', true, "Hangul"},
		{'Ａ', true, "Fullwidth A"},
	}
	for _, tc := range cases {
		got := isCJK(tc.r)
		if got != tc.want {
			t.Errorf("isCJK(%q) [%s]: got %v, want %v", tc.r, tc.name, got, tc.want)
		}
	}
}

func TestEstimateStringJapanese(t *testing.T) {
	// Japanese text mixing kanji and hiragana.
	text := "これはテストです。日本語のトークン数を確認します。"
	got := EstimateString(text)
	// 23 CJK characters → ~35 tokens.
	if got < 25 || got > 45 {
		t.Fatalf("Japanese text: expected ~35 tokens, got %d", got)
	}
}
