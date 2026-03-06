package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestShouldReset_IdleTimeout(t *testing.T) {
	store := NewSessionStore()
	config := DefaultLifecycleConfig()
	config.IdleTimeout = 1 * time.Millisecond

	lifecycle := NewSessionLifecycle(store, config)

	store.AppendMessage("idle-session", Message{Role: RoleUser, Content: "old"})

	// Force metadata to look idle.
	store.mu.Lock()
	meta := store.metadata["idle-session"]
	meta.UpdatedAt = time.Now().Add(-1 * time.Hour)
	store.metadata["idle-session"] = meta
	store.mu.Unlock()

	if !lifecycle.ShouldReset("idle-session") {
		t.Fatalf("expected session to need reset (idle timeout)")
	}
}

func TestShouldReset_BotSessionSkipsIdleTimeout(t *testing.T) {
	store := NewSessionStore()
	config := DefaultLifecycleConfig()
	config.IdleTimeout = 1 * time.Millisecond

	lifecycle := NewSessionLifecycle(store, config)

	store.AppendMessage("discord:123", Message{Role: RoleUser, Content: "old"})

	// Force metadata to look idle.
	store.mu.Lock()
	meta := store.metadata["discord:123"]
	meta.UpdatedAt = time.Now().Add(-1 * time.Hour)
	store.metadata["discord:123"] = meta
	store.mu.Unlock()

	// Bot sessions should NOT be reset by idle timeout.
	if lifecycle.ShouldReset("discord:123") {
		t.Fatalf("bot sessions should not be reset by idle timeout")
	}
}

func TestShouldReset_NoReset(t *testing.T) {
	store := NewSessionStore()
	config := DefaultLifecycleConfig()
	config.IdleTimeout = 24 * time.Hour

	lifecycle := NewSessionLifecycle(store, config)

	store.AppendMessage("active-session", Message{Role: RoleUser, Content: "recent"})

	if lifecycle.ShouldReset("active-session") {
		t.Fatalf("expected no reset for recently active session")
	}
}

func TestShouldReset_NonexistentSession(t *testing.T) {
	store := NewSessionStore()
	lifecycle := NewSessionLifecycle(store, DefaultLifecycleConfig())

	if lifecycle.ShouldReset("nonexistent") {
		t.Fatalf("expected no reset for nonexistent session")
	}
}

func TestRotateSession(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewPersistentSessionStore(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lifecycle := NewSessionLifecycle(store, DefaultLifecycleConfig())

	store.AppendMessage("rotate-me",
		Message{Role: RoleUser, Content: "hello"},
		Message{Role: RoleAssistant, Content: "hi"},
	)

	if err := lifecycle.RotateSession("rotate-me"); err != nil {
		t.Fatalf("rotate failed: %v", err)
	}

	// Original session should be empty.
	msgs := store.HistoryAsMessages("rotate-me")
	if len(msgs) != 0 {
		t.Fatalf("expected empty history after rotation, got %d", len(msgs))
	}

	// There should be an archived session.
	sessions := store.ListSessions()
	foundArchived := false
	for _, s := range sessions {
		if s.SessionID != "rotate-me" && s.MessageCount > 0 {
			foundArchived = true
			break
		}
	}
	if !foundArchived {
		t.Fatalf("expected archived session to exist")
	}
}

func TestHousekeeping_Retention(t *testing.T) {
	store := NewSessionStore()
	config := DefaultLifecycleConfig()
	config.RetentionDays = 1

	lifecycle := NewSessionLifecycle(store, config)

	store.AppendMessage("old-session", Message{Role: RoleUser, Content: "ancient"})

	// Force metadata to be old.
	store.mu.Lock()
	meta := store.metadata["old-session"]
	meta.UpdatedAt = time.Now().Add(-48 * time.Hour)
	store.metadata["old-session"] = meta
	store.mu.Unlock()

	if err := lifecycle.RunHousekeeping(); err != nil {
		t.Fatalf("housekeeping failed: %v", err)
	}

	sessions := store.ListSessions()
	for _, s := range sessions {
		if s.SessionID == "old-session" {
			t.Fatalf("old session should have been deleted by housekeeping")
		}
	}
}

func TestHousekeeping_MaxEntries(t *testing.T) {
	store := NewSessionStore()
	config := DefaultLifecycleConfig()
	config.MaxEntries = 5

	lifecycle := NewSessionLifecycle(store, config)

	// Add more messages than the cap.
	for i := 0; i < 10; i++ {
		store.AppendMessage("big-session", Message{Role: RoleUser, Content: "msg"})
	}

	if err := lifecycle.RunHousekeeping(); err != nil {
		t.Fatalf("housekeeping failed: %v", err)
	}

	// The oversized session should have been rotated (original cleared).
	msgs := store.HistoryAsMessages("big-session")
	if len(msgs) > config.MaxEntries {
		t.Fatalf("expected big-session to be rotated, still has %d messages", len(msgs))
	}
}
