package discord

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/doeshing/nekoclaw/internal/core"
)

func makeTestMessage(
	id string,
	authorID string,
	authorName string,
	content string,
	ts time.Time,
	imageCount int,
) *discordgo.Message {
	attachments := make([]*discordgo.MessageAttachment, 0, imageCount)
	for i := 0; i < imageCount; i++ {
		attachments = append(attachments, &discordgo.MessageAttachment{
			URL:         fmt.Sprintf("https://example.com/%s-%d.png", id, i),
			Filename:    fmt.Sprintf("%s-%d.png", id, i),
			ContentType: "image/png",
		})
	}
	return &discordgo.Message{
		ID:          id,
		Content:     content,
		Timestamp:   ts,
		Author:      &discordgo.User{ID: authorID, Username: authorName},
		Attachments: attachments,
	}
}

func TestBuildChannelHistoryEntriesFromMessages_BudgetAndCaps(t *testing.T) {
	originalExtractor := historyImageExtractor
	historyImageExtractor = func(attachments []*discordgo.MessageAttachment, limit int) []core.ImageData {
		if limit <= 0 || len(attachments) == 0 {
			return nil
		}
		n := len(attachments)
		if n > limit {
			n = limit
		}
		out := make([]core.ImageData, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, core.ImageData{
				MimeType: "image/png",
				Data:     "ZmFrZQ==",
				FileName: attachments[i].Filename,
			})
		}
		return out
	}
	defer func() { historyImageExtractor = originalExtractor }()

	now := time.Now().UTC()
	messages := make([]*discordgo.Message, 0, 50)
	for i := 0; i < 50; i++ {
		content := fmt.Sprintf("msg-%02d %s", i, strings.Repeat("x", 40))
		messages = append(messages, makeTestMessage(
			fmt.Sprintf("id-%02d", i),
			fmt.Sprintf("user-%02d", i),
			fmt.Sprintf("User%02d", i),
			content,
			now.Add(-time.Duration(i)*time.Minute),
			1,
		))
	}

	entries := buildChannelHistoryEntriesFromMessages(messages, "bot-id", nil, now)

	if len(entries) != discordHistoryTargetEntries {
		t.Fatalf("expected %d entries, got %d", discordHistoryTargetEntries, len(entries))
	}

	if !strings.Contains(entries[0].FormattedText, "msg-39") {
		t.Fatalf("expected oldest retained entry to be msg-39, got %q", entries[0].FormattedText)
	}
	if !strings.Contains(entries[len(entries)-1].FormattedText, "msg-00") {
		t.Fatalf("expected newest retained entry to be msg-00, got %q", entries[len(entries)-1].FormattedText)
	}

	totalChars := 0
	totalImages := 0
	for _, e := range entries {
		totalChars += runeLen(e.FormattedText)
		totalImages += len(e.Images)
	}
	if totalChars > discordHistoryCharBudget {
		t.Fatalf("history exceeded char budget: %d > %d", totalChars, discordHistoryCharBudget)
	}
	if totalImages > discordHistoryImageTotalLimit {
		t.Fatalf("history exceeded image cap: %d > %d", totalImages, discordHistoryImageTotalLimit)
	}
	if totalImages != discordHistoryImageTotalLimit {
		t.Fatalf("expected image cap to be reached (%d), got %d", discordHistoryImageTotalLimit, totalImages)
	}
}

func TestBuildChannelHistoryEntriesFromMessages_RespectsCutoff(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-5 * time.Minute)

	messages := make([]*discordgo.Message, 0, 10)
	for i := 0; i < 10; i++ {
		messages = append(messages, makeTestMessage(
			fmt.Sprintf("cut-%d", i),
			"user",
			"Alice",
			fmt.Sprintf("cut-%d", i),
			now.Add(-time.Duration(i)*time.Minute),
			0,
		))
	}

	entries := buildChannelHistoryEntriesFromMessages(messages, "bot-id", &cutoff, now)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries newer than cutoff, got %d", len(entries))
	}

	for _, e := range entries {
		if strings.Contains(e.FormattedText, "cut-5") || strings.Contains(e.FormattedText, "cut-6") ||
			strings.Contains(e.FormattedText, "cut-7") || strings.Contains(e.FormattedText, "cut-8") ||
			strings.Contains(e.FormattedText, "cut-9") {
			t.Fatalf("found message older than cutoff: %q", e.FormattedText)
		}
	}
}

func TestBuildEphemeralMessages_RoleMapping_BotAsAssistant(t *testing.T) {
	now := time.Now().UTC()
	messages := []*discordgo.Message{
		makeTestMessage("u1", "user-1", "Alice", "hello from user", now.Add(-1*time.Minute), 0),
		makeTestMessage("b1", "bot-1", "Neko", "hello from bot", now.Add(-2*time.Minute), 0),
	}

	entries := buildChannelHistoryEntriesFromMessages(messages, "bot-1", nil, now)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	ephemeral := buildEphemeralMessages(entries)
	if len(ephemeral) != 2 {
		t.Fatalf("expected 2 ephemeral messages, got %d", len(ephemeral))
	}

	if ephemeral[0].Role != core.RoleAssistant {
		t.Fatalf("expected bot history role assistant, got %s", ephemeral[0].Role)
	}
	if ephemeral[0].Content != "hello from bot" {
		t.Fatalf("expected raw bot content without user prefix, got %q", ephemeral[0].Content)
	}

	if ephemeral[1].Role != core.RoleUser {
		t.Fatalf("expected non-bot history role user, got %s", ephemeral[1].Role)
	}
	if !strings.Contains(ephemeral[1].Content, "hello from user") {
		t.Fatalf("expected user content in formatted history, got %q", ephemeral[1].Content)
	}
	if !strings.HasPrefix(ephemeral[1].Content, "[Alice · ") {
		t.Fatalf("expected user history prefix, got %q", ephemeral[1].Content)
	}
}

func TestResponseElapsedPrefersServerValue(t *testing.T) {
	resp := core.ChatResponse{ElapsedMs: 27_500}
	fallback := 31 * time.Second

	got := responseElapsed(resp, fallback)
	if got != 27_500*time.Millisecond {
		t.Fatalf("responseElapsed = %s, want 27.5s", got)
	}

	stats := formatUsageStats(core.UsageInfo{OutputTokens: 550}, got, "google-gemini-cli", "gemini-3-pro-preview")
	if !strings.Contains(stats, "⏱ 27.5s") {
		t.Fatalf("usage stats should use server elapsed, got %q", stats)
	}
	if strings.Contains(stats, "31.0s") {
		t.Fatalf("usage stats should not use fallback elapsed, got %q", stats)
	}
}

func TestResponseElapsedFallsBackWhenMissing(t *testing.T) {
	fallback := 31 * time.Second

	got := responseElapsed(core.ChatResponse{}, fallback)
	if got != fallback {
		t.Fatalf("responseElapsed = %s, want %s", got, fallback)
	}
}
