package binding

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadNonExistentReturnsEmptyStore(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")

	s, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load non-existent: %v", err)
	}
	if got := len(s.All()); got != 0 {
		t.Fatalf("expected empty store, got %d entries", got)
	}
}

func TestSetGetDelete(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")

	s, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Set a binding.
	s.Set("channel-1", Entry{SessionID: "session-abc"})

	// Get it back.
	entry, ok := s.Get("channel-1")
	if !ok {
		t.Fatal("expected to find channel-1 binding")
	}
	if entry.SessionID != "session-abc" {
		t.Fatalf("expected session-abc, got %s", entry.SessionID)
	}
	if entry.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}

	// Delete it.
	s.Delete("channel-1")
	_, ok = s.Get("channel-1")
	if ok {
		t.Fatal("expected channel-1 to be deleted")
	}
}

func TestPersistenceAcrossLoads(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")

	// First load: write some bindings.
	s1, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load 1: %v", err)
	}

	cutoff := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	s1.Set("ch-a", Entry{SessionID: "sid-a", HistoryCutoff: &cutoff})
	s1.Set("ch-b", Entry{SessionID: "sid-b"})

	// Second load from the same file: verify data persisted.
	s2, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load 2: %v", err)
	}

	all := s2.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}

	entryA, ok := s2.Get("ch-a")
	if !ok {
		t.Fatal("expected ch-a binding")
	}
	if entryA.SessionID != "sid-a" {
		t.Fatalf("expected sid-a, got %s", entryA.SessionID)
	}
	if entryA.HistoryCutoff == nil {
		t.Fatal("expected HistoryCutoff to be set for ch-a")
	}
	if !entryA.HistoryCutoff.Equal(cutoff) {
		t.Fatalf("expected cutoff %v, got %v", cutoff, *entryA.HistoryCutoff)
	}

	entryB, ok := s2.Get("ch-b")
	if !ok {
		t.Fatal("expected ch-b binding")
	}
	if entryB.SessionID != "sid-b" {
		t.Fatalf("expected sid-b, got %s", entryB.SessionID)
	}
	if entryB.HistoryCutoff != nil {
		t.Fatal("expected nil HistoryCutoff for ch-b")
	}
}

func TestDeletePersists(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")

	s1, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load 1: %v", err)
	}
	s1.Set("x", Entry{SessionID: "s1"})
	s1.Set("y", Entry{SessionID: "s2"})
	s1.Delete("x")

	// Reload and verify only "y" remains.
	s2, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load 2: %v", err)
	}
	if _, ok := s2.Get("x"); ok {
		t.Fatal("expected x to be deleted after reload")
	}
	if _, ok := s2.Get("y"); !ok {
		t.Fatal("expected y to remain after reload")
	}
}

func TestCorruptFileStartsFresh(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")

	// Write garbage to the file.
	if err := os.WriteFile(storePath, []byte("{invalid json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	s, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load corrupt: %v", err)
	}
	if got := len(s.All()); got != 0 {
		t.Fatalf("expected empty store from corrupt file, got %d entries", got)
	}

	// Should be able to write new entries.
	s.Set("k", Entry{SessionID: "v"})
	e, ok := s.Get("k")
	if !ok || e.SessionID != "v" {
		t.Fatal("expected to set entry after corrupt load")
	}
}

func TestAllReturnsSnapshot(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")

	s, err := Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s.Set("a", Entry{SessionID: "1"})
	snap := s.All()

	// Mutating the snapshot should not affect the store.
	snap["a"] = Entry{SessionID: "mutated"}
	entry, _ := s.Get("a")
	if entry.SessionID != "1" {
		t.Fatal("All() did not return a snapshot; store was mutated")
	}
}

func TestCreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "deep", "nested", "bindings.json")

	s, err := Load(nested)
	if err != nil {
		t.Fatalf("Load nested: %v", err)
	}

	s.Set("k", Entry{SessionID: "v"})

	// Verify the file was actually created.
	if _, err := os.Stat(nested); os.IsNotExist(err) {
		t.Fatal("expected binding file to be created in nested directory")
	}
}
