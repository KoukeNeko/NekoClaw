package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistentStore_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "sessions")

	_, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	transcriptDir := filepath.Join(dataDir, "transcripts")
	info, err := os.Stat(transcriptDir)
	if err != nil {
		t.Fatalf("transcripts directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected directory, got file")
	}
}

func TestAppendAndHistory_InMemory(t *testing.T) {
	store := NewSessionStore()

	store.AppendMessage("s1", Message{Role: RoleUser, Content: "hello"})
	store.AppendMessage("s1", Message{Role: RoleAssistant, Content: "hi"})

	msgs := store.HistoryAsMessages("s1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Fatalf("expected 'hello', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "hi" {
		t.Fatalf("expected 'hi', got %q", msgs[1].Content)
	}

	empty := store.HistoryAsMessages("nonexistent")
	if len(empty) != 0 {
		t.Fatalf("expected empty history, got %d messages", len(empty))
	}
}

func TestAppendAndHistory_Persistent(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	now := time.Now()
	store.AppendMessage("test",
		Message{Role: RoleUser, Content: "ping", CreatedAt: now},
		Message{Role: RoleAssistant, Content: "pong", CreatedAt: now},
	)

	msgs := store.HistoryAsMessages("test")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Verify JSONL file exists and has correct lines
	// (session header + 2 message entries = 3 lines)
	jsonlPath := filepath.Join(dataDir, "transcripts", "test.jsonl")
	content, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("failed to read JSONL: %v", err)
	}
	lines := nonEmptyLines(string(content))
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSONL lines (1 header + 2 messages), got %d", len(lines))
	}

	// First line should be the session header.
	var header SessionEntry
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("failed to unmarshal header: %v", err)
	}
	if header.Type != EntrySession {
		t.Fatalf("expected session header, got type=%q", header.Type)
	}

	// Second line should be the first message.
	var entry SessionEntry
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("failed to unmarshal first message: %v", err)
	}
	if entry.Type != EntryMessage {
		t.Fatalf("expected message entry, got type=%q", entry.Type)
	}
	if entry.Content != "ping" {
		t.Fatalf("expected 'ping', got %q", entry.Content)
	}
}

func TestLazyLoading(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "sessions")
	store1, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	store1.AppendMessage("persist",
		Message{Role: RoleUser, Content: "remember me"},
		Message{Role: RoleAssistant, Content: "noted"},
	)

	// Create a second store pointing at the same directory.
	store2, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error creating second store: %v", err)
	}

	msgs := store2.HistoryAsMessages("persist")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after lazy load, got %d", len(msgs))
	}
	if msgs[0].Content != "remember me" {
		t.Fatalf("expected 'remember me', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "noted" {
		t.Fatalf("expected 'noted', got %q", msgs[1].Content)
	}
}

func TestMetadataUpdatedOnAppend(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	before := time.Now()
	store.AppendMessage("meta-test",
		Message{Role: RoleUser, Content: "q1"},
		Message{Role: RoleAssistant, Content: "a1"},
	)
	after := time.Now()

	sessions := store.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	meta := sessions[0]
	if meta.SessionID != "meta-test" {
		t.Fatalf("expected 'meta-test', got %q", meta.SessionID)
	}
	if meta.MessageCount != 2 {
		t.Fatalf("expected message_count=2, got %d", meta.MessageCount)
	}
	if meta.CreatedAt.Before(before) || meta.CreatedAt.After(after) {
		t.Fatalf("created_at %v outside expected window [%v, %v]", meta.CreatedAt, before, after)
	}
	if meta.UpdatedAt.Before(before) || meta.UpdatedAt.After(after) {
		t.Fatalf("updated_at %v outside expected window [%v, %v]", meta.UpdatedAt, before, after)
	}

	// Verify metadata.json on disk
	metaPath := filepath.Join(dataDir, "metadata.json")
	content, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read metadata.json: %v", err)
	}
	var file metadataFile
	if err := json.Unmarshal(content, &file); err != nil {
		t.Fatalf("failed to unmarshal metadata: %v", err)
	}
	diskMeta, ok := file.Sessions["meta-test"]
	if !ok {
		t.Fatalf("session 'meta-test' not found in metadata.json")
	}
	if diskMeta.MessageCount != 2 {
		t.Fatalf("disk metadata message_count=%d, expected 2", diskMeta.MessageCount)
	}
}

func TestListSessions_SortedByUpdatedAt(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	store.AppendMessage("older", Message{Role: RoleUser, Content: "a"})
	time.Sleep(10 * time.Millisecond)
	store.AppendMessage("newer", Message{Role: RoleUser, Content: "b"})

	sessions := store.ListSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].SessionID != "newer" {
		t.Fatalf("expected 'newer' first, got %q", sessions[0].SessionID)
	}
	if sessions[1].SessionID != "older" {
		t.Fatalf("expected 'older' second, got %q", sessions[1].SessionID)
	}
}

func TestDeleteSession(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	store.AppendMessage("to-delete", Message{Role: RoleUser, Content: "bye"})

	jsonlPath := filepath.Join(dataDir, "transcripts", "to-delete.jsonl")
	if _, err := os.Stat(jsonlPath); err != nil {
		t.Fatalf("JSONL file should exist before delete: %v", err)
	}

	if err := store.DeleteSession("to-delete"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Fatalf("JSONL file should be removed after delete")
	}

	msgs := store.HistoryAsMessages("to-delete")
	if len(msgs) != 0 {
		t.Fatalf("expected empty history after delete, got %d", len(msgs))
	}

	sessions := store.ListSessions()
	for _, s := range sessions {
		if s.SessionID == "to-delete" {
			t.Fatalf("deleted session still in ListSessions")
		}
	}
}

func TestSanitizeSessionID(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"main", "main"},
		{"discord:chan:user", "discord_chan_user"},
		{"path/to/session", "path_to_session"},
		{"a<b>c|d", "a_b_c_d"},
		{"  spaces  ", "spaces"},
	}
	for _, tc := range cases {
		got := sanitizeSessionID(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeSessionID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestCorruptJSONLLine(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "sessions")
	_, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write a JSONL file with a corrupt line in the middle.
	// Use typed entry format.
	jsonlPath := filepath.Join(dataDir, "transcripts", "corrupt.jsonl")
	line1, _ := json.Marshal(NewMessageEntry(RoleUser, "good1"))
	line3, _ := json.Marshal(NewMessageEntry(RoleAssistant, "good2"))
	content := string(line1) + "\n" + "{{CORRUPT LINE}}\n" + string(line3) + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write corrupt JSONL: %v", err)
	}

	store2, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := store2.HistoryAsMessages("corrupt")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 valid messages (skipping corrupt), got %d", len(msgs))
	}
	if msgs[0].Content != "good1" {
		t.Fatalf("expected 'good1', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "good2" {
		t.Fatalf("expected 'good2', got %q", msgs[1].Content)
	}
}

func TestBackwardCompatibleLoad(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "sessions")
	_, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write a JSONL file in the old plain-Message format (no "type" field).
	jsonlPath := filepath.Join(dataDir, "transcripts", "legacy.jsonl")
	line1, _ := json.Marshal(Message{Role: RoleUser, Content: "old format", CreatedAt: time.Now()})
	line2, _ := json.Marshal(Message{Role: RoleAssistant, Content: "still works", CreatedAt: time.Now()})
	content := string(line1) + "\n" + string(line2) + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write legacy JSONL: %v", err)
	}

	store2, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := store2.HistoryAsMessages("legacy")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages from legacy format, got %d", len(msgs))
	}
	if msgs[0].Content != "old format" {
		t.Fatalf("expected 'old format', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "still works" {
		t.Fatalf("expected 'still works', got %q", msgs[1].Content)
	}

	// Verify the raw entries have been migrated to typed format.
	entries := store2.History("legacy")
	for _, e := range entries {
		if e.Type != EntryMessage {
			t.Fatalf("expected all legacy entries migrated to EntryMessage, got %q", e.Type)
		}
		if e.ID == "" {
			t.Fatalf("migrated entry should have an ID")
		}
	}
}

func TestSessionHeaderEntry(t *testing.T) {
	store := NewSessionStore()

	store.AppendMessage("test", Message{Role: RoleUser, Content: "hello"})

	entries := store.History("test")
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries (header + message), got %d", len(entries))
	}
	if entries[0].Type != EntrySession {
		t.Fatalf("first entry should be session header, got %q", entries[0].Type)
	}
	if entries[0].Version != 3 {
		t.Fatalf("session header version should be 3, got %d", entries[0].Version)
	}
	if entries[1].Type != EntryMessage {
		t.Fatalf("second entry should be message, got %q", entries[1].Type)
	}
	if entries[1].Content != "hello" {
		t.Fatalf("expected 'hello', got %q", entries[1].Content)
	}
}

func TestEntryID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewEntryID()
		if len(id) != 8 {
			t.Fatalf("expected 8-char hex ID, got %q (len=%d)", id, len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate ID generated: %q", id)
		}
		seen[id] = true
	}
}

func TestCompactionEntryInHistory(t *testing.T) {
	store := NewSessionStore()

	store.AppendMessage("s1", Message{Role: RoleUser, Content: "hello"})
	store.Append("s1", NewCompactionEntry("summary of dropped", 5, 1000, "abc12345"))
	store.AppendMessage("s1", Message{Role: RoleUser, Content: "world"})

	// Raw entries should include all types.
	entries := store.History("s1")
	compactionCount := 0
	for _, e := range entries {
		if e.Type == EntryCompaction {
			compactionCount++
			if e.Summary != "summary of dropped" {
				t.Fatalf("expected compaction summary, got %q", e.Summary)
			}
		}
	}
	if compactionCount != 1 {
		t.Fatalf("expected 1 compaction entry, got %d", compactionCount)
	}

	// HistoryAsMessages should inject compaction as system message.
	msgs := store.HistoryAsMessages("s1")
	foundSystem := false
	for _, m := range msgs {
		if m.Role == RoleSystem && m.Content == "summary of dropped" {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Fatalf("compaction summary should appear as system message in HistoryAsMessages")
	}
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
