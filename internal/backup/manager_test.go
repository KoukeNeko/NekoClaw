package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/auth"
	yzip "github.com/yeka/zip"
)

const testBackupPassword = "backup-password-123"

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

	entry, err := env.manager.Create(testBackupPassword)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if entry.Source != SourceCreated {
		t.Fatalf("source = %q, want %q", entry.Source, SourceCreated)
	}
	if entry.Encryption != EncryptionZipAES256 {
		t.Fatalf("encryption = %q, want %q", entry.Encryption, EncryptionZipAES256)
	}

	assertArchiveSecurity(t, filepath.Join(env.root, "backups", entry.FileName))

	backups, err := env.manager.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}
	if backups[0].Encryption != EncryptionZipAES256 {
		t.Fatalf("listed encryption = %q, want %q", backups[0].Encryption, EncryptionZipAES256)
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
	writeFile(t, filepath.Join(env.root, "auth", "security-state.json"), `{"password_hash":"current-hash"}`)
	writeFile(t, filepath.Join(env.root, "memory", "orphan.md"), "orphan")

	restoreResult, err := env.manager.Restore(entry.BackupID, testBackupPassword)
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
	if got := strings.TrimSpace(readFile(t, filepath.Join(env.root, "auth", "security-state.json"))); !strings.Contains(got, "current-hash") {
		t.Fatalf("security-state.json should have been kept, got %s", got)
	}
}

func TestManagerImportDeleteAndRejectInvalidPassword(t *testing.T) {
	env := newTestEnv(t)
	seedState(t, env, "backup-secret", "Asia/Taipei")

	created, err := env.manager.Create(testBackupPassword)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	raw := readBytes(t, filepath.Join(env.root, "backups", created.FileName))

	imported, err := env.manager.Import(bytes.NewReader(raw), testBackupPassword)
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

	if _, err := env.manager.Import(bytes.NewReader(raw), "wrong-password-456"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected invalid password error, got %v", err)
	}
	if _, err := env.manager.Import(bytes.NewBufferString("not-a-zip"), testBackupPassword); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected invalid manifest error, got %v", err)
	}
	if _, err := env.manager.Import(bytes.NewReader(raw), ""); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("expected password required error, got %v", err)
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
}

func TestManagerStartImportJobCompletesAndCleansTempUpload(t *testing.T) {
	env := newTestEnv(t)
	seedState(t, env, "backup-secret", "Asia/Taipei")

	created, err := env.manager.Create(testBackupPassword)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	raw := readBytes(t, filepath.Join(env.root, "backups", created.FileName))

	started, err := env.manager.StartImport(bytes.NewReader(raw), "backup.zip", int64(len(raw)), testBackupPassword)
	if err != nil {
		t.Fatalf("StartImport failed: %v", err)
	}
	if started.Status != ImportJobStatusRunning {
		t.Fatalf("start status = %q, want running", started.Status)
	}

	final := waitForManagerImportJob(t, env.manager, started.JobID)
	if final.Status != ImportJobStatusCompleted {
		t.Fatalf("final status = %q, want completed", final.Status)
	}
	if final.Backup == nil || final.Backup.Source != SourceImported {
		t.Fatalf("expected imported backup entry, got %#v", final.Backup)
	}

	tempUploads, err := filepath.Glob(filepath.Join(env.root, "backup-upload-*.zip"))
	if err != nil {
		t.Fatalf("glob temp uploads failed: %v", err)
	}
	if len(tempUploads) != 0 {
		t.Fatalf("expected temp uploads to be cleaned, found %v", tempUploads)
	}
}

func TestManagerImportJobTTLExpiresCompletedJob(t *testing.T) {
	current := time.Date(2026, 3, 9, 7, 0, 0, 0, time.UTC)
	root := t.TempDir()
	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: filepath.Join(root, "auth"),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	manager := NewManager(ManagerOptions{
		ConfigRoot:   root,
		SessionsDir:  filepath.Join(root, "sessions"),
		MemoryDir:    filepath.Join(root, "memory"),
		MCPDir:       filepath.Join(root, "mcp"),
		PersonasDir:  filepath.Join(root, "personas"),
		StateDir:     filepath.Join(root, "state"),
		AuthStore:    store,
		Now:          func() time.Time { return current },
		JobTTL:       10 * time.Minute,
		MaxJobEvents: 8,
	})
	env := testEnv{root: root, authStore: store, manager: manager}
	seedState(t, env, "backup-secret", "Asia/Taipei")

	created, err := env.manager.Create(testBackupPassword)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	raw := readBytes(t, filepath.Join(env.root, "backups", created.FileName))

	started, err := env.manager.StartImport(bytes.NewReader(raw), "backup.zip", int64(len(raw)), testBackupPassword)
	if err != nil {
		t.Fatalf("StartImport failed: %v", err)
	}
	final := waitForManagerImportJob(t, env.manager, started.JobID)
	if final.Status != ImportJobStatusCompleted {
		t.Fatalf("final status = %q, want completed", final.Status)
	}

	current = current.Add(11 * time.Minute)
	if _, err := env.manager.GetImportJob(started.JobID); !errors.Is(err, ErrImportJobNotFound) {
		t.Fatalf("expected import job to expire, got %v", err)
	}
}

func TestManagerRestoreRollsBackOnFailure(t *testing.T) {
	env := newTestEnv(t)
	profile := seedState(t, env, "backup-secret", "Asia/Taipei")

	entry, err := env.manager.Create(testBackupPassword)
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

	if _, err := badManager.Restore(entry.BackupID, testBackupPassword); err == nil {
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

func assertArchiveSecurity(t *testing.T, archivePath string) {
	t.Helper()
	reader, err := yzip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("OpenReader failed: %v", err)
	}
	defer reader.Close()

	foundManifest := false
	foundEncryptedPayload := false
	for _, file := range reader.File {
		if strings.Contains(file.Name, "search.db") {
			t.Fatalf("archive unexpectedly included search.db: %q", file.Name)
		}
		if strings.Contains(file.Name, "security-state.json") {
			t.Fatalf("archive unexpectedly included security-state.json: %q", file.Name)
		}
		if file.Name == "manifest.json" {
			foundManifest = true
			if file.IsEncrypted() {
				t.Fatal("manifest.json should remain plaintext")
			}
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("Open manifest.json failed: %v", err)
			}
			var manifest Manifest
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
				rc.Close()
				t.Fatalf("decode manifest.json failed: %v", err)
			}
			if err := rc.Close(); err != nil {
				t.Fatalf("close manifest.json failed: %v", err)
			}
			if manifest.Encryption != EncryptionZipAES256 {
				t.Fatalf("manifest encryption = %q, want %q", manifest.Encryption, EncryptionZipAES256)
			}
		}
		if strings.HasPrefix(file.Name, "payload/") && !file.FileInfo().IsDir() {
			if !file.IsEncrypted() {
				t.Fatalf("payload entry should be encrypted: %q", file.Name)
			}
			foundEncryptedPayload = true
		}
	}
	if !foundManifest {
		t.Fatal("manifest.json missing from archive")
	}
	if !foundEncryptedPayload {
		t.Fatal("expected at least one encrypted payload entry")
	}
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

func waitForManagerImportJob(t *testing.T, manager *Manager, jobID string) ImportJobSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.GetImportJob(jobID)
		if err != nil {
			t.Fatalf("GetImportJob failed: %v", err)
		}
		if snapshot.Status == ImportJobStatusCompleted || snapshot.Status == ImportJobStatusFailed {
			return snapshot
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("import job %s did not reach a terminal state", jobID)
	return ImportJobSnapshot{}
}
