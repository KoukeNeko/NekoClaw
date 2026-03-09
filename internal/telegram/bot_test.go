package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestResponseElapsedPrefersServerValue(t *testing.T) {
	resp := core.ChatResponse{ElapsedMs: 28_800}
	fallback := 35 * time.Second

	got := responseElapsed(resp, fallback)
	if got != 28_800*time.Millisecond {
		t.Fatalf("responseElapsed = %s, want 28.8s", got)
	}

	stats := formatUsageStats(core.UsageInfo{OutputTokens: 576}, got, "google-gemini-cli", "gemini-3-pro-preview")
	if !strings.Contains(stats, "⏱ 28.8s") {
		t.Fatalf("usage stats should use server elapsed, got %q", stats)
	}
	if strings.Contains(stats, "35.0s") {
		t.Fatalf("usage stats should not use fallback elapsed, got %q", stats)
	}
}

func TestResponseElapsedFallsBackWhenMissing(t *testing.T) {
	fallback := 35 * time.Second

	got := responseElapsed(core.ChatResponse{}, fallback)
	if got != fallback {
		t.Fatalf("responseElapsed = %s, want %s", got, fallback)
	}
}

func TestFormatFooterBlockquote(t *testing.T) {
	got := formatFooterBlockquote([]string{
		"⏱ 37.5s · ↑26.5K ↓334 (26.9K) · 9 tok/s · google-gemini-cli/gemini-3-pro-preview",
		"🔧 使用的工具：\n1. memory_search\n2. bash",
	})

	want := strings.Join([]string{
		"> ⏱ 37\\.5s · ↑26\\.5K ↓334 \\(26\\.9K\\) · 9 tok/s · google\\-gemini\\-cli/gemini\\-3\\-pro\\-preview",
		"> 🔧 使用的工具：",
		"> 1\\. memory\\_search",
		"> 2\\. bash",
	}, "\n")

	if got != want {
		t.Fatalf("formatFooterBlockquote() = %q, want %q", got, want)
	}
}

func TestFormatReplyWithFooterUsesQuotedBlock(t *testing.T) {
	got := formatReplyWithFooter("主回覆", []string{"⏱ 1.2s", "🔧 使用的工具：\n1. bash"})

	want := strings.Join([]string{
		"主回覆",
		"",
		"> ⏱ 1\\.2s",
		"> 🔧 使用的工具：",
		"> 1\\. bash",
	}, "\n")

	if got != want {
		t.Fatalf("formatReplyWithFooter() = %q, want %q", got, want)
	}
}
