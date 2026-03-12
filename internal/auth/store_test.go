package auth

import "testing"

type testMemoryKeyring struct {
	values map[string]string
}

func newTestMemoryKeyring() *testMemoryKeyring {
	return &testMemoryKeyring{values: map[string]string{}}
}

func (k *testMemoryKeyring) Available() bool {
	return true
}

func (k *testMemoryKeyring) Set(key, value string) error {
	k.values[key] = value
	return nil
}

func (k *testMemoryKeyring) Get(key string) (string, error) {
	value, ok := k.values[key]
	if !ok {
		return "", ErrCredentialNotFound
	}
	return value, nil
}

func (k *testMemoryKeyring) Delete(key string) error {
	delete(k.values, key)
	return nil
}

func TestStorePersistsGeminiExecutionMode(t *testing.T) {
	baseDir := t.TempDir()
	keyring := newTestMemoryKeyring()

	store, err := NewStore(StoreOptions{
		BaseDir: baseDir,
		Keyring: keyring,
	})
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	meta := ProfileMetadata{
		ProfileID:     "gemini-main",
		Provider:      "google-gemini-cli",
		Type:          "oauth",
		Email:         "gemini@example.com",
		ProjectID:     "proj-1",
		ExecutionMode: "cli_headless",
	}
	if err := store.UpsertProfile(meta); err != nil {
		t.Fatalf("UpsertProfile() error = %v", err)
	}

	reloaded, err := NewStore(StoreOptions{
		BaseDir: baseDir,
		Keyring: keyring,
	})
	if err != nil {
		t.Fatalf("NewStore(reload) error = %v", err)
	}

	profile, err := reloaded.GetProfile(meta.Provider, meta.ProfileID)
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	if profile.ExecutionMode != meta.ExecutionMode {
		t.Fatalf("ExecutionMode = %q, want %q", profile.ExecutionMode, meta.ExecutionMode)
	}

	profiles, err := reloaded.ListProfiles(meta.Provider)
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(ListProfiles()) = %d, want 1", len(profiles))
	}
	if profiles[0].ExecutionMode != meta.ExecutionMode {
		t.Fatalf("ListProfiles()[0].ExecutionMode = %q, want %q", profiles[0].ExecutionMode, meta.ExecutionMode)
	}
}
