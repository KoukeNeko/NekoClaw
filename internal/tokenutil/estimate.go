package tokenutil

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// latinCharsPerToken is the average characters-per-token ratio for Latin
	// scripts (English, European languages). Most BPE tokenizers produce
	// roughly 1 token per 4 characters of English text.
	latinCharsPerToken = 4

	// cjkTokensPerChar is the average tokens-per-character ratio for CJK
	// scripts (Chinese, Japanese kanji, Korean). CJK characters typically
	// consume 1-2 tokens each in modern tokenizers; 1.5 is the midpoint.
	cjkTokensPerChar = 1.5

	// envelopeOverhead accounts for message framing (role tag, JSON
	// envelope, separator tokens) that every message incurs regardless
	// of content length.
	envelopeOverhead = 24
)

// EstimateString returns an estimated token count for a raw string using
// CJK-aware character counting. CJK characters are weighted at ~1.5
// tokens each; all other characters use the standard 4-chars-per-token
// heuristic.
func EstimateString(s string) int {
	s = strings.TrimSpace(s)
	cjk, other := classifyRunes(s)
	t := cjkTokens(cjk) + latinTokens(other)
	if t < 1 {
		return 1
	}
	return t
}

// EstimateStringWithOverhead is like EstimateString but adds the standard
// message envelope overhead, suitable for estimating a single message or
// session entry.
func EstimateStringWithOverhead(s string) int {
	s = strings.TrimSpace(s)
	runes := utf8.RuneCountInString(s)
	if runes == 0 {
		return 1
	}
	cjk, other := classifyRunes(s)
	t := cjkTokens(cjk) + latinTokens(other+envelopeOverhead)
	if t < 1 {
		return 1
	}
	return t
}

// classifyRunes counts CJK runes and non-CJK runes in s.
func classifyRunes(s string) (cjk int, other int) {
	for _, r := range s {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	return
}

// cjkTokens converts CJK character count to estimated tokens.
func cjkTokens(n int) int {
	return int(float64(n)*cjkTokensPerChar + 0.5)
}

// latinTokens converts non-CJK character count to estimated tokens
// using the standard chars-per-token ratio with ceiling division.
func latinTokens(n int) int {
	return (n + latinCharsPerToken - 1) / latinCharsPerToken
}

// isCJK reports whether r is a CJK Unified Ideograph or related symbol
// that typically consumes more than one BPE token. This covers:
//   - CJK Radicals Supplement      (U+2E80–U+2EFF)
//   - Kangxi Radicals              (U+2F00–U+2FDF)
//   - CJK Unified Ideographs Ext A (U+3400–U+4DBF)
//   - CJK Unified Ideographs       (U+4E00–U+9FFF)
//   - CJK Compatibility Ideographs (U+F900–U+FAFF)
//   - CJK Compatibility Forms       (U+FE30–U+FE4F)
//   - CJK Unified Ideographs Ext B+ (U+20000–U+2FA1F)
//   - Hiragana, Katakana            (U+3040–U+30FF)
//   - Hangul Syllables              (U+AC00–U+D7AF)
//   - Fullwidth Forms               (U+FF00–U+FF60, U+FFE0–U+FFE6)
func isCJK(r rune) bool {
	if unicode.Is(unicode.Han, r) {
		return true
	}
	if unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
		return true
	}
	if unicode.Is(unicode.Hangul, r) {
		return true
	}
	// Fullwidth ASCII variants and fullwidth punctuation.
	if r >= 0xFF00 && r <= 0xFF60 || r >= 0xFFE0 && r <= 0xFFE6 {
		return true
	}
	return false
}
