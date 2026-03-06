package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/logger"
	"github.com/doeshing/nekoclaw/internal/provider"
)

var logSession = logger.New("session-save", logger.Yellow)

const (
	maxRecentMessages = 15
	slugNoReplyMarker = "NO_SLUG"
)

// SessionSaver saves session transcripts as dated memory files on rotate/reset.
type SessionSaver struct {
	memoryDir string
	prov      provider.Provider
	model     string
}

// NewSessionSaver creates a SessionSaver.
func NewSessionSaver(memoryDir string, prov provider.Provider, model string) *SessionSaver {
	return &SessionSaver{
		memoryDir: memoryDir,
		prov:      prov,
		model:     model,
	}
}

// SaveSessionMemory extracts recent user+assistant messages from the session
// entries and saves them as a dated memory file in {memoryDir}/memory/.
func (s *SessionSaver) SaveSessionMemory(ctx context.Context, account core.Account, sessionID string, entries []core.SessionEntry) error {
	if s.memoryDir == "" || len(entries) == 0 {
		return nil
	}

	// Extract recent user+assistant messages.
	recent := extractRecentMessages(entries, maxRecentMessages)
	if len(recent) == 0 {
		logSession.Logf("skip: session_id=%s reason=no_messages", sessionID)
		return nil
	}

	// Build conversation text for slug generation and file content.
	conversationText := formatConversation(recent)

	// Generate slug via LLM.
	slug := s.generateSlug(ctx, account, conversationText)

	// Build file content.
	now := time.Now()
	content := buildSessionMemoryContent(now, sessionID, conversationText)

	// Ensure memory/ subdirectory exists.
	memSubdir := filepath.Join(s.memoryDir, "memory")
	if err := os.MkdirAll(memSubdir, 0o755); err != nil {
		return fmt.Errorf("create memory subdir: %w", err)
	}

	// Write file.
	filename := fmt.Sprintf("%s-%s.md", now.Format("2006-01-02"), slug)
	path := filepath.Join(memSubdir, filename)

	// Avoid overwriting: append counter if file exists.
	if _, err := os.Stat(path); err == nil {
		for i := 2; i <= 99; i++ {
			filename = fmt.Sprintf("%s-%s-%d.md", now.Format("2006-01-02"), slug, i)
			path = filepath.Join(memSubdir, filename)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				break
			}
		}
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write session memory: %w", err)
	}

	logSession.Logf("saved: path=%s messages=%d", filename, len(recent))
	return nil
}

type messageSnippet struct {
	role    core.MessageRole
	content string
}

func extractRecentMessages(entries []core.SessionEntry, limit int) []messageSnippet {
	var messages []messageSnippet
	for _, e := range entries {
		if e.Type != core.EntryMessage {
			continue
		}
		if e.Role != core.RoleUser && e.Role != core.RoleAssistant {
			continue
		}
		text := strings.TrimSpace(e.Content)
		if text == "" {
			continue
		}
		messages = append(messages, messageSnippet{role: e.Role, content: text})
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages
}

func formatConversation(messages []messageSnippet) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(string(m.role))
		sb.WriteString(": ")
		sb.WriteString(truncateRunes(m.content, 500))
		sb.WriteString("\n")
	}
	return sb.String()
}

func buildSessionMemoryContent(now time.Time, sessionID, conversation string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Session: %s\n\n", now.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("- **Session ID**: %s\n\n", sessionID))
	sb.WriteString("## Conversation\n\n")
	sb.WriteString(conversation)
	return sb.String()
}

func (s *SessionSaver) generateSlug(ctx context.Context, account core.Account, conversation string) string {
	if s.prov == nil {
		return fallbackSlug()
	}

	prompt := fmt.Sprintf("根據以下對話，生成一個簡短的英文 slug（2-4 個單字，用連字號連接）。\n只回覆 slug，不要加其他文字。\n\n%s", truncateRunes(conversation, 2000))

	resp, err := s.prov.Generate(ctx, provider.GenerateRequest{
		Model: s.model,
		Messages: []core.Message{
			{Role: core.RoleUser, Content: prompt},
		},
		Account: account,
	})
	if err != nil {
		logSession.Warnf("slug generation failed: %v", err)
		return fallbackSlug()
	}

	slug := sanitizeSlug(strings.TrimSpace(resp.Text))
	if slug == "" || slug == slugNoReplyMarker {
		return fallbackSlug()
	}
	return slug
}

func fallbackSlug() string {
	return time.Now().Format("1504") // HHMM
}

func sanitizeSlug(raw string) string {
	// Take first line only.
	if idx := strings.IndexAny(raw, "\n\r"); idx >= 0 {
		raw = raw[:idx]
	}
	// Remove quotes, backticks, and extra whitespace.
	raw = strings.Trim(raw, "\"'`")
	raw = strings.TrimSpace(raw)

	// Convert spaces to hyphens, lowercase, keep only alphanumeric and hyphens.
	raw = strings.ToLower(raw)
	var sb strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			sb.WriteRune(r)
		} else if r == ' ' || r == '_' {
			sb.WriteRune('-')
		}
	}
	slug := sb.String()

	// Collapse multiple hyphens and trim.
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")

	// Cap length.
	if len(slug) > 50 {
		slug = slug[:50]
		slug = strings.TrimRight(slug, "-")
	}
	return slug
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes-1]) + "…"
}

// estimateTokens is a simple rune-based token estimator.
func estimateTokens(s string) int {
	runes := utf8.RuneCountInString(strings.TrimSpace(s))
	t := (runes + charsPerToken - 1) / charsPerToken
	if t < 1 {
		return 1
	}
	return t
}
