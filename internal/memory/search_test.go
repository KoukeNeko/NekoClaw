package memory

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestSearchIndex_CreateAndQuery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

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
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	results, err := idx.Search("", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results for empty query")
	}
}

func TestSearchIndex_DuplicateIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

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
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

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
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

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
