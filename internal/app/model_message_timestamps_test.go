package app

import (
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/contextwindow"
	"github.com/doeshing/nekoclaw/internal/core"
)

func TestDecorateModelMessagesWithSentAt_PrefixesOnlyUserAndPlainAssistant(t *testing.T) {
	fallbackNow := time.Date(2026, 3, 8, 19, 13, 45, 0, time.FixedZone("UTC+8", 8*60*60))
	createdAt := time.Date(2026, 3, 8, 19, 9, 1, 0, time.FixedZone("UTC+8", 8*60*60))
	location, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	original := []core.Message{
		{Role: core.RoleSystem, Content: "system"},
		{Role: core.RoleUser, Content: "hello", CreatedAt: createdAt},
		{Role: core.RoleAssistant, Content: "world", CreatedAt: createdAt},
		{Role: core.RoleAssistant, Content: `{"q":"test"}`, ToolName: "search", ToolCallID: "tc-1", CreatedAt: createdAt},
		{Role: core.RoleTool, Content: `{"result":"found"}`, ToolName: "search", ToolCallID: "tc-1", CreatedAt: createdAt},
		{Role: core.RoleUser, Images: []core.ImageData{{MimeType: "image/png", Data: "abc"}}},
	}

	decorated := decorateModelMessagesWithSentAt(original, fallbackNow, location)

	if len(decorated) != len(original) {
		t.Fatalf("expected %d messages, got %d", len(original), len(decorated))
	}
	if decorated[0].Content != "system" {
		t.Fatalf("system message should remain unchanged, got %q", decorated[0].Content)
	}
	if decorated[1].Content != "[sent_at: 2026-03-08T19:09+08:00]\nhello" {
		t.Fatalf("unexpected user decoration: %q", decorated[1].Content)
	}
	if decorated[2].Content != "[sent_at: 2026-03-08T19:09+08:00]\nworld" {
		t.Fatalf("unexpected assistant decoration: %q", decorated[2].Content)
	}
	if decorated[3].Content != `{"q":"test"}` {
		t.Fatalf("assistant tool call should remain unchanged, got %q", decorated[3].Content)
	}
	if decorated[4].Content != `{"result":"found"}` {
		t.Fatalf("tool message should remain unchanged, got %q", decorated[4].Content)
	}
	if decorated[5].Content != "[sent_at: 2026-03-08T19:13+08:00]" {
		t.Fatalf("image-only user message should become timestamp-only text, got %q", decorated[5].Content)
	}
	if len(decorated[5].Images) != 1 || decorated[5].Images[0].MimeType != "image/png" {
		t.Fatalf("image-only user message should preserve images, got %+v", decorated[5].Images)
	}
	if original[1].Content != "hello" || original[2].Content != "world" || original[5].Content != "" {
		t.Fatalf("original messages should not be mutated: %+v", original)
	}
}

func TestResolveMessageLocation_PrefersRequestThenGeneralThenUTC(t *testing.T) {
	tests := []struct {
		name               string
		requestTimezone    string
		configuredTimezone string
		wantName           string
	}{
		{
			name:               "request timezone wins",
			requestTimezone:    "Asia/Taipei",
			configuredTimezone: "America/Los_Angeles",
			wantName:           "Asia/Taipei",
		},
		{
			name:               "configured timezone fallback",
			requestTimezone:    "Invalid/Timezone",
			configuredTimezone: "Asia/Tokyo",
			wantName:           "Asia/Tokyo",
		},
		{
			name:               "utc fallback",
			requestTimezone:    "Invalid/Timezone",
			configuredTimezone: "Another/Invalid",
			wantName:           "UTC",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			location := resolveMessageLocation(tc.requestTimezone, tc.configuredTimezone)
			if location.String() != tc.wantName {
				t.Fatalf("location = %q, want %q", location.String(), tc.wantName)
			}
		})
	}
}

func TestResolveCurrentUserMessageTime_FallsBackOnInvalidInput(t *testing.T) {
	fallback := time.Date(2026, 3, 8, 11, 13, 0, 0, time.UTC)
	got := resolveCurrentUserMessageTime("2026-03-08T19:13:27.123+08:00", fallback)
	if got.Format(time.RFC3339Nano) != "2026-03-08T19:13:27.123+08:00" {
		t.Fatalf("unexpected parsed timestamp: %s", got.Format(time.RFC3339Nano))
	}

	got = resolveCurrentUserMessageTime("not-a-timestamp", fallback)
	if !got.Equal(fallback) {
		t.Fatalf("expected fallback time, got %s", got.Format(time.RFC3339Nano))
	}
}

func TestTrimMessagesPreservingSystem_DropsOnlyNonSystem(t *testing.T) {
	messages := []core.Message{
		{Role: core.RoleSystem, Content: "system-a"},
		{Role: core.RoleUser, Content: strings.Repeat("x", 5000)},
		{Role: core.RoleSystem, Content: "system-b"},
		{Role: core.RoleAssistant, Content: "latest reply"},
	}

	budget := contextwindow.EstimateMessageTokens(messages[0]) +
		contextwindow.EstimateMessageTokens(messages[2]) +
		contextwindow.EstimateMessageTokens(messages[3]) + 16

	trimmed, dropped := trimMessagesPreservingSystem(messages, budget)

	if len(trimmed) != 3 {
		t.Fatalf("expected 3 kept messages, got %d", len(trimmed))
	}
	if len(dropped) != 1 {
		t.Fatalf("expected 1 dropped message, got %d", len(dropped))
	}
	if trimmed[0].Role != core.RoleSystem || trimmed[0].Content != "system-a" {
		t.Fatalf("first system message should be preserved, got %+v", trimmed[0])
	}
	if trimmed[1].Role != core.RoleSystem || trimmed[1].Content != "system-b" {
		t.Fatalf("second system message should be preserved, got %+v", trimmed[1])
	}
	if trimmed[2].Role != core.RoleAssistant || trimmed[2].Content != "latest reply" {
		t.Fatalf("latest assistant message should be preserved, got %+v", trimmed[2])
	}
	if dropped[0].Role != core.RoleUser {
		t.Fatalf("only the non-system user message should be dropped, got %+v", dropped[0])
	}
}
