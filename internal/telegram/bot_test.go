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
