package backup

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/doeshing/nekoclaw/internal/auth"
)

type memoryKeyring struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemoryKeyring() *memoryKeyring {
	return &memoryKeyring{data: map[string]string{}}
}

func (k *memoryKeyring) Available() bool {
	return true
}

func (k *memoryKeyring) Set(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	return nil
}

func (k *memoryKeyring) Get(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.data[key]
	if !ok {
		return "", auth.ErrCredentialNotFound
	}
	return value, nil
}

func (k *memoryKeyring) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	return nil
}

type testEnv struct {
	root      string
	authStore *auth.Store
	manager   *Manager
}

func TestManagerCreateListAndRestoreRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	profile := seedState(t, env, "backup-secret", "Asia/Taipei")

	entry, err := env.manager.Create()
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if entry.Source != SourceCreated {
		t.Fatalf("source = %q, want %q", entry.Source, SourceCreated)
	}

	archiveEntries := zipEntryNames(t, filepath.Join(env.root, "backups", entry.FileName))
	for _, name := range archiveEntries {
		if strings.Contains(name, "search.db") {
			t.Fatalf("archive unexpectedly included search.db: %q", name)
		}
	}

	backups, err := env.manager.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}

	writeFile(t, filepath.Join(env.root, "config.json"), `{"general":{"timezone":"Mutated/Timezone"}}`)
	if err := clearAuthStore(env.authStore); err != nil {
		t.Fatalf("clear auth store: %v", err)
	}
	removePathForTest(t, filepath.Join(env.root, "memory"))
	removePathForTest(t, filepath.Join(env.root, "sessions"))
	removePathForTest(t, filepath.Join(env.root, "mcp"))
	removePathForTest(t, filepath.Join(env.root, "personas"))
	removePathForTest(t, filepath.Join(env.root, "state"))
	removePathForTest(t, filepath.Join(env.root, "security-state.json"))
	writeFile(t, filepath.Join(env.root, "memory", "orphan.md"), "orphan")

	restoreResult, err := env.manager.Restore(entry.BackupID)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if !restoreResult.RestartRequired {
		t.Fatalf("expected restart_required=true")
	}

	if got := strings.TrimSpace(readFile(t, filepath.Join(env.root, "config.json"))); !strings.Contains(got, "Asia/Taipei") {
		t.Fatalf("config.json not restored: %s", got)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(env.root, "memory", "MEMORY.md"))); got != "Long-term memory" {
		t.Fatalf("MEMORY.md = %q", got)
	}
	if _, err := os.Stat(filepath.Join(env.root, "memory", "orphan.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected orphan memory file to be removed, got err=%v", err)
	}
	credential, err := env.authStore.LoadCredential(profile.Provider, profile.ProfileID)
	if err != nil {
		t.Fatalf("LoadCredential failed: %v", err)
	}
	if credential.AccessToken != "backup-secret" {
		t.Fatalf("credential.AccessToken = %q, want %q", credential.AccessToken, "backup-secret")
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(env.root, "state", "discord-bindings.json"))); !strings.Contains(got, "discord:alpha") {
		t.Fatalf("discord binding not restored: %s", got)
	}
}

func TestManagerImportDeleteAndRejectInvalidArchive(t *testing.T) {
	env := newTestEnv(t)
	seedState(t, env, "backup-secret", "Asia/Taipei")

	created, err := env.manager.Create()
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	raw := readBytes(t, filepath.Join(env.root, "backups", created.FileName))

	imported, err := env.manager.Import(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if imported.Source != SourceImported {
		t.Fatalf("source = %q, want %q", imported.Source, SourceImported)
	}

	backups, err := env.manager.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups after import, got %d", len(backups))
	}

	if err := env.manager.Delete(imported.BackupID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	backups, err = env.manager.List()
	if err != nil {
		t.Fatalf("List after delete failed: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup after delete, got %d", len(backups))
	}

	if _, err := env.manager.Import(bytes.NewBufferString("not-a-zip")); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("expected invalid archive error, got %v", err)
	}
}

func TestManagerRestoreRollsBackOnFailure(t *testing.T) {
	env := newTestEnv(t)
	profile := seedState(t, env, "backup-secret", "Asia/Taipei")

	entry, err := env.manager.Create()
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	writeFile(t, filepath.Join(env.root, "config.json"), `{"general":{"timezone":"Rollback/Keep"}}`)
	if err := env.authStore.SaveCredential(profile.Provider, profile.ProfileID, auth.Credential{AccessToken: "current-secret"}); err != nil {
		t.Fatalf("SaveCredential failed: %v", err)
	}

	brokenStatePath := filepath.Join(env.root, "broken-state")
	writeFile(t, brokenStatePath, "not a directory")
	badManager := NewManager(ManagerOptions{
		ConfigRoot:  env.root,
		SessionsDir: filepath.Join(env.root, "sessions"),
		MemoryDir:   filepath.Join(env.root, "memory"),
		MCPDir:      filepath.Join(env.root, "mcp"),
		PersonasDir: filepath.Join(env.root, "personas"),
		StateDir:    brokenStatePath,
		AuthStore:   env.authStore,
	})

	if _, err := badManager.Restore(entry.BackupID); err == nil {
		t.Fatal("expected restore to fail")
	}

	if got := strings.TrimSpace(readFile(t, filepath.Join(env.root, "config.json"))); !strings.Contains(got, "Rollback/Keep") {
		t.Fatalf("config.json should have rolled back, got %s", got)
	}
	credential, err := env.authStore.LoadCredential(profile.Provider, profile.ProfileID)
	if err != nil {
		t.Fatalf("LoadCredential failed: %v", err)
	}
	if credential.AccessToken != "current-secret" {
		t.Fatalf("credential.AccessToken = %q, want %q", credential.AccessToken, "current-secret")
	}
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()
	root := t.TempDir()
	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: filepath.Join(root, "auth"),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	manager := NewManager(ManagerOptions{
		ConfigRoot:  root,
		SessionsDir: filepath.Join(root, "sessions"),
		MemoryDir:   filepath.Join(root, "memory"),
		MCPDir:      filepath.Join(root, "mcp"),
		PersonasDir: filepath.Join(root, "personas"),
		StateDir:    filepath.Join(root, "state"),
		AuthStore:   store,
	})
	return testEnv{
		root:      root,
		authStore: store,
		manager:   manager,
	}
}

func seedState(t *testing.T, env testEnv, token string, timezone string) auth.ProfileMetadata {
	t.Helper()
	writeFile(t, filepath.Join(env.root, "config.json"), `{"general":{"timezone":"`+timezone+`"}}`)

	profile := auth.ProfileMetadata{
		ProfileID:   "openai:main",
		Provider:    "openai",
		Type:        "api_key",
		DisplayName: "Main",
		KeyHint:     "****main",
	}
	if err := env.authStore.UpsertProfile(profile); err != nil {
		t.Fatalf("UpsertProfile failed: %v", err)
	}
	if err := env.authStore.SaveCredential(profile.Provider, profile.ProfileID, auth.Credential{AccessToken: token}); err != nil {
		t.Fatalf("SaveCredential failed: %v", err)
	}

	writeFile(t, filepath.Join(env.root, "auth", "security-state.json"), `{"password_hash":"hash"}`)
	writeFile(t, filepath.Join(env.root, "sessions", "metadata.json"), `{"sessions":{"chat-1":{"session_id":"chat-1","message_count":2,"created_at":"2026-03-09T10:00:00Z","updated_at":"2026-03-09T10:05:00Z"}}}`)
	writeFile(t, filepath.Join(env.root, "sessions", "transcripts", "chat-1.jsonl"), `{"type":"session","id":"header","timestamp":"2026-03-09T10:00:00Z","version":3}`+"\n")
	writeFile(t, filepath.Join(env.root, "memory", "MEMORY.md"), "Long-term memory\n")
	writeFile(t, filepath.Join(env.root, "memory", "2026-03-09.md"), "Daily log\n")
	writeFile(t, filepath.Join(env.root, "memory", "search.db"), "sqlite-cache")
	writeFile(t, filepath.Join(env.root, "mcp", "alpha.json"), `{"name":"alpha","transport":"stdio","command":"alpha","trust":"trusted"}`)
	writeFile(t, filepath.Join(env.root, "mcp-builtin.json"), `{"playwright":true}`)
	writeFile(t, filepath.Join(env.root, "personas", "cat", "config.yaml"), "meta:\n  id: cat\n")
	writeFile(t, filepath.Join(env.root, "persona-state.json"), `{"active":"cat"}`)
	writeFile(t, filepath.Join(env.root, "state", "discord-bindings.json"), `{"bindings":{"chan-1":{"session_id":"discord:alpha"}}}`)
	writeFile(t, filepath.Join(env.root, "state", "telegram-bindings.json"), `{"bindings":{"chat-1":{"session_id":"telegram:alpha"}}}`)
	return profile
}

func zipEntryNames(t *testing.T, archivePath string) []string {
	t.Helper()
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("OpenReader failed: %v", err)
	}
	defer reader.Close()

	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return data
}

func removePathForTest(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll(%s): %v", path, err)
	}
}
