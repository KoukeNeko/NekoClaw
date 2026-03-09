package app

import (
	"errors"
	"testing"

	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
)

type memoryKeyring struct {
	values map[string]string
}

func newMemoryKeyring() *memoryKeyring {
	return &memoryKeyring{values: map[string]string{}}
}

func (k *memoryKeyring) Available() bool {
	return true
}

func (k *memoryKeyring) Set(key, value string) error {
	k.values[key] = value
	return nil
}

func (k *memoryKeyring) Get(key string) (string, error) {
	value, ok := k.values[key]
	if !ok {
		return "", auth.ErrCredentialNotFound
	}
	return value, nil
}

func (k *memoryKeyring) Delete(key string) error {
	delete(k.values, key)
	return nil
}

func TestUseGeminiProfileRequiresPersistedProjectEvenWhenEnvIsSet(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "still-ignored")

	svc := NewService(ServiceOptions{})
	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	svc.SetAuthIntegration(nil, store)

	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID: "missing-project",
		Provider:  "google-gemini-cli",
		Type:      string(core.AccountOAuth),
		Email:     "tester@example.com",
		ProjectID: "",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	err = svc.UseGeminiProfile("missing-project")
	if !errors.Is(err, ErrGeminiMissingProject) {
		t.Fatalf("expected ErrGeminiMissingProject, got %v", err)
	}
}
