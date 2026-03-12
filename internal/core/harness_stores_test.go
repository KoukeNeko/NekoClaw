package core

import "testing"

func TestTaskStoreGetMissingSessionReturnsEmptyItems(t *testing.T) {
	store, err := NewTaskStore("")
	if err != nil {
		t.Fatalf("NewTaskStore() error = %v", err)
	}

	list := store.Get("session-a")
	if list.SessionID != "session-a" {
		t.Fatalf("SessionID = %q, want session-a", list.SessionID)
	}
	if list.Items == nil {
		t.Fatalf("Items should be an empty slice, got nil")
	}
	if len(list.Items) != 0 {
		t.Fatalf("len(Items) = %d, want 0", len(list.Items))
	}
}
