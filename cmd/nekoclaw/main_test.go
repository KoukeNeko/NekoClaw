package main

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
)

func TestResolveDefaultLogFilePath_UsesHomeDefaultWhenAuthDirEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := resolveDefaultLogFilePath("")
	want := filepath.Join(home, ".nekoclaw", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(empty) = %q, want %q", got, want)
	}
}

func TestLoadAIStudioAccountsFromEnv_MergesAndDedupes(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "key-a")
	t.Setenv("GOOGLE_API_KEY", "key-a")
	t.Setenv("GEMINI_API_KEYS", "key-b,key-c")
	t.Setenv("GOOGLE_API_KEYS", "key-c,key-d")
	t.Setenv("GEMINI_API_KEY_TEAM", "key-e")
	t.Setenv("GOOGLE_API_KEY_X", "key-f")

	accounts := loadAIStudioAccountsFromEnv()
	if len(accounts) != 6 {
		t.Fatalf("unexpected accounts length: %d", len(accounts))
	}
	seenTokens := map[string]struct{}{}
	for _, account := range accounts {
		if account.Provider != "google-ai-studio" {
			t.Fatalf("unexpected provider: %s", account.Provider)
		}
		if account.Type != core.AccountAPIKey {
			t.Fatalf("unexpected account type: %s", account.Type)
		}
		if strings.TrimSpace(account.Token) == "" {
			t.Fatalf("expected token")
		}
		seenTokens[account.Token] = struct{}{}
	}
	keys := make([]string, 0, len(seenTokens))
	for key := range seenTokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"key-a", "key-b", "key-c", "key-d", "key-e", "key-f"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected key set: got=%v want=%v", keys, want)
	}
}

func TestHydrateAIStudioProfilesLoadsCredentials(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("google-ai-studio", "ai-1", auth.Credential{
		AccessToken: "api-key-1",
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID:   "ai-1",
		Provider:    "google-ai-studio",
		Type:        string(core.AccountAPIKey),
		DisplayName: "main",
		KeyHint:     "****-1",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	if err := hydrateAIStudioProfiles(svc, store); err != nil {
		t.Fatalf("hydrate ai studio profiles: %v", err)
	}

	pool := svc.Pool("google-ai-studio")
	if pool == nil {
		t.Fatalf("missing pool")
	}
	account, ok := pool.GetAccount("ai-1")
	if !ok {
		t.Fatalf("expected ai-1 loaded")
	}
	if account.Token != "api-key-1" {
		t.Fatalf("unexpected token: %q", account.Token)
	}
	if account.Metadata["display_name"] != "main" {
		t.Fatalf("unexpected display name: %q", account.Metadata["display_name"])
	}
}

func TestResolveDefaultLogFilePath_AuthDirParentRoot(t *testing.T) {
	got := resolveDefaultLogFilePath("/tmp/custom/auth")
	want := filepath.Join("/tmp/custom", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(auth) = %q, want %q", got, want)
	}
}

func TestResolveDefaultLogFilePath_NonAuthDirRoot(t *testing.T) {
	got := resolveDefaultLogFilePath("/tmp/custom-state")
	want := filepath.Join("/tmp/custom-state", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(non-auth) = %q, want %q", got, want)
	}
}

func TestHydrateGeminiProfilesSkipsMissingProjectWithoutEnv(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("google-gemini-cli", "p1", auth.Credential{
		AccessToken:  "token-p1",
		RefreshToken: "refresh-p1",
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID: "p1",
		Provider:  "google-gemini-cli",
		Type:      string(core.AccountOAuth),
		Email:     "p1@example.com",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	if err := hydrateGeminiProfiles(svc, store); err != nil {
		t.Fatalf("hydrate profiles: %v", err)
	}

	pool := svc.Pool("google-gemini-cli")
	if pool == nil {
		t.Fatalf("missing pool")
	}
	if _, ok := pool.GetAccount("p1"); ok {
		t.Fatalf("expected profile p1 not loaded without project")
	}
}

func TestHydrateGeminiProfilesUsesEnvFallbackProject(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project-1")

	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", nil, nil, core.DefaultCooldownConfig()))
	store := mustNewAuthStore(t)

	if err := store.SaveCredential("google-gemini-cli", "p2", auth.Credential{
		AccessToken:  "token-p2",
		RefreshToken: "refresh-p2",
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID: "p2",
		Provider:  "google-gemini-cli",
		Type:      string(core.AccountOAuth),
		Email:     "p2@example.com",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	if err := hydrateGeminiProfiles(svc, store); err != nil {
		t.Fatalf("hydrate profiles: %v", err)
	}

	pool := svc.Pool("google-gemini-cli")
	if pool == nil {
		t.Fatalf("missing pool")
	}
	account, ok := pool.GetAccount("p2")
	if !ok {
		t.Fatalf("expected profile p2 loaded with env fallback")
	}
	if account.Metadata["project_id"] != "env-project-1" {
		t.Fatalf("unexpected project_id: %q", account.Metadata["project_id"])
	}
	if account.Metadata["project_source"] != "env_fallback" {
		t.Fatalf("unexpected project_source: %q", account.Metadata["project_source"])
	}
}

func mustNewAuthStore(t *testing.T) *auth.Store {
	t.Helper()
	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: t.TempDir(),
		Keyring: newMainTestKeyring(),
	})
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	return store
}

type mainTestKeyring struct {
	mu   sync.Mutex
	data map[string]string
}

func newMainTestKeyring() *mainTestKeyring {
	return &mainTestKeyring{data: map[string]string{}}
}

func (k *mainTestKeyring) Available() bool {
	return true
}

func (k *mainTestKeyring) Set(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	return nil
}

func (k *mainTestKeyring) Get(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.data[key]
	if !ok {
		return "", auth.ErrCredentialNotFound
	}
	return value, nil
}

func (k *mainTestKeyring) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	return nil
}
