package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

func newTestSearchIndex(t *testing.T) *SearchIndex {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	t.Cleanup(func() {
		if err := idx.Close(); err != nil {
			t.Fatalf("close index: %v", err)
		}
	})
	return idx
}

func insertRawChunk(t *testing.T, idx *SearchIndex, sessionID, entryID, content, source, timestamp string) {
	t.Helper()
	if _, err := idx.db.Exec(
		`INSERT INTO chunks (session_id, entry_id, path, start_line, end_line, content, role, source, timestamp) VALUES (?, ?, '', 0, 0, ?, 'user', ?, ?)`,
		sessionID,
		entryID,
		content,
		source,
		timestamp,
	); err != nil {
		t.Fatalf("insert raw chunk: %v", err)
	}
}

func TestSearchIndex_CreateAndQuery(t *testing.T) {
	idx := newTestSearchIndex(t)

	entries := []core.SessionEntry{
		core.NewMessageEntry(core.RoleUser, "How do I implement a binary search tree in Go?"),
		core.NewMessageEntry(core.RoleAssistant, "Here is an implementation of a binary search tree using struct pointers."),
		core.NewMessageEntry(core.RoleUser, "Can you add a delete method?"),
	}

	if err := idx.Index("test-session", entries); err != nil {
		t.Fatalf("failed to index: %v", err)
	}

	results, err := idx.Search("binary search tree", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results for 'binary search tree'")
	}

	found := false
	for _, r := range results {
		if strings.Contains(r.Content, "binary search tree") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected result containing 'binary search tree'")
	}
}

func TestSearchIndex_EmptyQuery(t *testing.T) {
	idx := newTestSearchIndex(t)

	results, err := idx.Search("", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results for empty query")
	}
}

func TestSearchIndex_DuplicateIndex(t *testing.T) {
	idx := newTestSearchIndex(t)

	entries := []core.SessionEntry{
		core.NewMessageEntry(core.RoleUser, "unique content for testing"),
	}

	// Index twice — should not duplicate.
	if err := idx.Index("s1", entries); err != nil {
		t.Fatalf("first index failed: %v", err)
	}
	if err := idx.Index("s1", entries); err != nil {
		t.Fatalf("second index failed: %v", err)
	}

	results, err := idx.Search("unique content", 100)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (no duplicates), got %d", len(results))
	}
}

func TestSearchIndex_DeleteSession(t *testing.T) {
	idx := newTestSearchIndex(t)

	entries := []core.SessionEntry{
		core.NewMessageEntry(core.RoleUser, "deletable content here"),
	}
	if err := idx.Index("to-delete", entries); err != nil {
		t.Fatalf("index failed: %v", err)
	}

	// Verify it's searchable.
	results, err := idx.Search("deletable", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected results before delete")
	}

	// Delete the session.
	if err := idx.DeleteSession("to-delete"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Verify it's gone.
	results, err = idx.Search("deletable", 10)
	if err != nil {
		t.Fatalf("search after delete failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results after delete, got %d", len(results))
	}
}

func TestSearchIndex_FTS5Ranking(t *testing.T) {
	idx := newTestSearchIndex(t)

	entries := []core.SessionEntry{
		core.NewMessageEntry(core.RoleUser, "Go programming language features"),
		core.NewMessageEntry(core.RoleAssistant, "Go Go Go - the Go programming language is great for Go developers who love Go"),
	}
	if err := idx.Index("rank-test", entries); err != nil {
		t.Fatalf("index failed: %v", err)
	}

	results, err := idx.Search("Go programming", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// Results should be ranked (higher score = better match).
	if results[0].Score < results[1].Score {
		t.Fatalf("first result should have higher score: %f < %f", results[0].Score, results[1].Score)
	}
}

func TestSearchIndex_LegacyTimestampFormats(t *testing.T) {
	testCases := []struct {
		name      string
		timestamp string
		want      string
	}{
		{
			name:      "with_monotonic_suffix",
			timestamp: "2026-03-05 17:20:16.276647 +0800 CST m=+32.558701501",
			want:      "2026-03-05T17:20:16.276647+08:00",
		},
		{
			name:      "without_monotonic_suffix",
			timestamp: "2026-03-04 23:50:56.141936472 +0800 CST",
			want:      "2026-03-04T23:50:56.141936472+08:00",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			idx := newTestSearchIndex(t)
			insertRawChunk(t, idx, "legacy-session", tc.name, "legacytimestamp token", "sessions", tc.timestamp)

			results, err := idx.Search("legacytimestamp", 10)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			if got := results[0].Timestamp.Format(time.RFC3339Nano); got != tc.want {
				t.Fatalf("timestamp = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSearchIndex_SearchSkipsInvalidTimestampRows(t *testing.T) {
	idx := newTestSearchIndex(t)
	insertRawChunk(t, idx, "bad-session", "bad-entry", "resilience token", "sessions", "not-a-real-timestamp")
	insertRawChunk(t, idx, "good-session", "good-entry", "resilience token", "sessions", "2026-03-05 17:20:16.276647 +0800 CST m=+32.558701501")

	results, err := idx.Search("resilience", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 valid result, got %d", len(results))
	}
	if results[0].EntryID != "good-entry" {
		t.Fatalf("expected good-entry to survive, got %q", results[0].EntryID)
	}
}

func TestSearchIndex_WritesRFC3339Timestamps(t *testing.T) {
	idx := newTestSearchIndex(t)

	sessionTS := time.Date(2026, 3, 9, 10, 11, 12, 345678901, time.FixedZone("UTC+8", 8*60*60))
	entry := core.SessionEntry{
		Type:      core.EntryMessage,
		ID:        "format-entry",
		Timestamp: sessionTS,
		Role:      core.RoleUser,
		Content:   "format verification",
	}
	if err := idx.Index("format-session", []core.SessionEntry{entry}); err != nil {
		t.Fatalf("index session entry: %v", err)
	}

	var storedSessionTS string
	if err := idx.db.QueryRow(`SELECT timestamp FROM chunks WHERE entry_id = ? LIMIT 1`, entry.ID).Scan(&storedSessionTS); err != nil {
		t.Fatalf("query stored session timestamp: %v", err)
	}
	if storedSessionTS != formatSearchTimestamp(sessionTS) {
		t.Fatalf("stored session timestamp = %q, want %q", storedSessionTS, formatSearchTimestamp(sessionTS))
	}
	if strings.Contains(storedSessionTS, "m=+") {
		t.Fatalf("stored session timestamp should not contain monotonic suffix: %q", storedSessionTS)
	}

	memoryDir := t.TempDir()
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")
	if err := os.WriteFile(memoryFile, []byte("memory format verification"), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}
	fileTS := time.Date(2026, 3, 8, 7, 8, 9, 987654321, time.FixedZone("UTC+8", 8*60*60))
	if err := os.Chtimes(memoryFile, fileTS, fileTS); err != nil {
		t.Fatalf("chtimes memory file: %v", err)
	}
	if err := idx.IndexMemoryFiles(memoryDir); err != nil {
		t.Fatalf("index memory files: %v", err)
	}

	var storedFileTS string
	if err := idx.db.QueryRow(`SELECT timestamp FROM chunks WHERE path = 'MEMORY.md' LIMIT 1`).Scan(&storedFileTS); err != nil {
		t.Fatalf("query stored file timestamp: %v", err)
	}
	if storedFileTS != formatSearchTimestamp(fileTS) {
		t.Fatalf("stored file timestamp = %q, want %q", storedFileTS, formatSearchTimestamp(fileTS))
	}
	if strings.Contains(storedFileTS, "m=+") {
		t.Fatalf("stored file timestamp should not contain monotonic suffix: %q", storedFileTS)
	}

	var indexedAt string
	if err := idx.db.QueryRow(`SELECT indexed_at FROM indexed_files WHERE path = 'MEMORY.md'`).Scan(&indexedAt); err != nil {
		t.Fatalf("query indexed_at: %v", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, indexedAt); err != nil {
		t.Fatalf("indexed_at should be RFC3339Nano, got %q: %v", indexedAt, err)
	}
	if strings.Contains(indexedAt, "m=+") {
		t.Fatalf("indexed_at should not contain monotonic suffix: %q", indexedAt)
	}
}

func TestChunkText(t *testing.T) {
	// Short text should produce 1 chunk.
	short := "Hello world"
	chunks := chunkText(short, 400, 80)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short text, got %d", len(chunks))
	}
	if chunks[0] != short {
		t.Fatalf("expected %q, got %q", short, chunks[0])
	}

	// Long text should produce multiple overlapping chunks.
	long := strings.Repeat("word ", 1000) // ~5000 chars
	chunks = chunkText(long, 100, 20)     // 400 chars target, 80 overlap
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestChunkText_Empty(t *testing.T) {
	chunks := chunkText("", 400, 80)
	if len(chunks) != 1 || chunks[0] != "" {
		t.Fatalf("expected 1 empty chunk, got %v", chunks)
	}
}
