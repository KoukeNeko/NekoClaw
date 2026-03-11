package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/botcmd"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/persona"
)

func TestResponseElapsedPrefersServerValue(t *testing.T) {
	resp := core.ChatResponse{ElapsedMs: 28_800}
	fallback := 35 * time.Second

	got := responseElapsed(resp, fallback)
	if got != 28_800*time.Millisecond {
		t.Fatalf("responseElapsed = %s, want 28.8s", got)
	}

	stats := formatUsageStats(core.UsageInfo{OutputTokens: 576}, got, "google-gemini-cli", "gemini-3.1-pro-preview")
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
		"⏱ 37.5s · ↑26.5K ↓334 (26.9K) · 9 tok/s · google-gemini-cli/gemini-3.1-pro-preview",
		"🔧 使用的工具：\n1. memory_search\n2. bash",
	})

	want := strings.Join([]string{
		"> ⏱ 37\\.5s · ↑26\\.5K ↓334 \\(26\\.9K\\) · 9 tok/s · google\\-gemini\\-cli/gemini\\-3\\.1\\-pro\\-preview",
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

func TestTelegramBotCommandsComeFromSharedSpecs(t *testing.T) {
	commands := telegramBotCommands()
	if len(commands) != 3 {
		t.Fatalf("expected 3 telegram commands, got %d", len(commands))
	}
	if commands[0].Command != botcmd.CommandReset || commands[1].Command != botcmd.CommandPersona || commands[2].Command != botcmd.CommandPlan {
		t.Fatalf("unexpected command list: %#v", commands)
	}
}

func TestParseTelegramCallbackInvocation(t *testing.T) {
	inv, ok := parseTelegramCallbackInvocation(encodeTelegramCallbackData(botcmd.ActionPersonaSelect, "hero"))
	if !ok {
		t.Fatal("expected callback data to parse")
	}
	if inv.Name != botcmd.CommandPersona || inv.ComponentAction != botcmd.ActionPersonaSelect || inv.ComponentValue != "hero" {
		t.Fatalf("unexpected invocation: %#v", inv)
	}
}

func TestBuildTelegramPersonaKeyboardUsesSharedSelector(t *testing.T) {
	selector := &botcmd.PersonaSelector{
		Title:    "📋 可用角色：",
		OffLabel: "❌ 停用角色",
		Options: []botcmd.PersonaOption{
			{DirName: "hero", Name: "Hero", Active: true},
			{DirName: "mage", Name: "Mage"},
		},
	}

	rows := buildTelegramPersonaKeyboard(selector)
	if len(rows) != 3 {
		t.Fatalf("expected 3 keyboard rows, got %d", len(rows))
	}
	if rows[0][0].Text != "▶ Hero" {
		t.Fatalf("unexpected active persona label: %q", rows[0][0].Text)
	}
	if rows[2][0].Text != "❌ 停用角色" {
		t.Fatalf("unexpected off label: %q", rows[2][0].Text)
	}
}

func TestRenderPersonaSelectorTextMarksActivePersona(t *testing.T) {
	selector := botcmd.BuildPersonaSelector([]persona.PersonaInfo{
		{DirName: "hero", Name: "Hero", Description: "Brave cat"},
		{DirName: "mage", Name: "Mage"},
	}, &persona.PersonaInfo{DirName: "hero"})

	got := botcmd.RenderPersonaSelectorText(selector, "✅ 已切換為角色「Hero」。")
	if !strings.Contains(got, "✅ 已切換為角色「Hero」。") {
		t.Fatalf("expected notice in selector text, got %q", got)
	}
	if !strings.Contains(got, "▶ Hero") {
		t.Fatalf("expected active persona marker, got %q", got)
	}
}
