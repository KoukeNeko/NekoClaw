package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/backup"
)

func TestBackupsEndpointsCreateListDownloadRestoreAndDelete(t *testing.T) {
	svc, root := newBackupTestService(t)
	seedBackupAPIState(t, root, svc)
	handler := NewServer(svc).Handler()

	createResp := performJSONRequest(t, handler, http.MethodPost, "/v1/backups/create", `{}`)
	if createResp.Code != http.StatusOK {
		t.Fatalf("unexpected create status: %d body=%s", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Backup backup.Entry `json:"backup"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.Backup.BackupID == "" {
		t.Fatal("expected backup id")
	}

	listResp := performJSONRequest(t, handler, http.MethodGet, "/v1/backups", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("unexpected list status: %d body=%s", listResp.Code, listResp.Body.String())
	}
	var listPayload struct {
		Backups []backup.Entry `json:"backups"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listPayload.Backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(listPayload.Backups))
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/v1/backups/download?id="+createPayload.Backup.BackupID, nil)
	downloadResp := httptest.NewRecorder()
	handler.ServeHTTP(downloadResp, downloadReq)
	if downloadResp.Code != http.StatusOK {
		t.Fatalf("unexpected download status: %d body=%s", downloadResp.Code, downloadResp.Body.String())
	}
	if got := downloadResp.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("content-type = %q, want application/zip", got)
	}
	if got := downloadResp.Header().Get("Content-Disposition"); !strings.Contains(got, createPayload.Backup.FileName) {
		t.Fatalf("content-disposition %q missing file name", got)
	}

	writeTestFile(t, filepath.Join(root, "config.json"), `{"general":{"timezone":"Mutated/Timezone"}}`)
	restoreResp := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/backups/restore",
		`{"id":"`+createPayload.Backup.BackupID+`"}`,
	)
	if restoreResp.Code != http.StatusOK {
		t.Fatalf("unexpected restore status: %d body=%s", restoreResp.Code, restoreResp.Body.String())
	}
	var restorePayload struct {
		RestartRequired bool `json:"restart_required"`
	}
	if err := json.Unmarshal(restoreResp.Body.Bytes(), &restorePayload); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	if !restorePayload.RestartRequired {
		t.Fatal("expected restart_required=true")
	}

	content, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if !strings.Contains(string(content), "Asia/Taipei") {
		t.Fatalf("expected restored config.json, got %s", string(content))
	}

	deleteResp := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/backups/delete",
		`{"id":"`+createPayload.Backup.BackupID+`"}`,
	)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	listResp = performJSONRequest(t, handler, http.MethodGet, "/v1/backups", "")
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response after delete: %v", err)
	}
	if len(listPayload.Backups) != 0 {
		t.Fatalf("expected 0 backups after delete, got %d", len(listPayload.Backups))
	}
}

func TestBackupsImportAndValidation(t *testing.T) {
	svc, root := newBackupTestService(t)
	seedBackupAPIState(t, root, svc)
	handler := NewServer(svc).Handler()

	createResp := performJSONRequest(t, handler, http.MethodPost, "/v1/backups/create", `{}`)
	if createResp.Code != http.StatusOK {
		t.Fatalf("unexpected create status: %d body=%s", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Backup backup.Entry `json:"backup"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	rawArchive, err := os.ReadFile(filepath.Join(root, "backups", createPayload.Backup.FileName))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}

	invalidImport := performMultipartRequest(t, handler, "/v1/backups/import", "file", "broken.zip", []byte("not-a-zip"))
	if invalidImport.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid import to return 400, got %d body=%s", invalidImport.Code, invalidImport.Body.String())
	}

	importResp := performMultipartRequest(t, handler, "/v1/backups/import", "file", "backup.zip", rawArchive)
	if importResp.Code != http.StatusOK {
		t.Fatalf("unexpected import status: %d body=%s", importResp.Code, importResp.Body.String())
	}
	var importPayload struct {
		Backup backup.Entry `json:"backup"`
	}
	if err := json.Unmarshal(importResp.Body.Bytes(), &importPayload); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if importPayload.Backup.Source != backup.SourceImported {
		t.Fatalf("source = %q, want %q", importPayload.Backup.Source, backup.SourceImported)
	}

	missingIDRestore := performJSONRequest(t, handler, http.MethodPost, "/v1/backups/restore", `{}`)
	if missingIDRestore.Code != http.StatusBadRequest {
		t.Fatalf("expected restore missing id to return 400, got %d body=%s", missingIDRestore.Code, missingIDRestore.Body.String())
	}
}

func newBackupTestService(t *testing.T) (*app.Service, string) {
	t.Helper()
	root := t.TempDir()
	store, err := auth.NewStore(auth.StoreOptions{
		BaseDir: filepath.Join(root, "auth"),
		Keyring: newMemoryKeyring(),
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	svc := app.NewService(app.ServiceOptions{})
	svc.SetConfigDir(root)
	svc.SetBackupManager(backup.NewManager(backup.ManagerOptions{
		ConfigRoot:  root,
		SessionsDir: filepath.Join(root, "sessions"),
		MemoryDir:   filepath.Join(root, "memory"),
		MCPDir:      filepath.Join(root, "mcp"),
		PersonasDir: filepath.Join(root, "personas"),
		StateDir:    filepath.Join(root, "state"),
		AuthStore:   store,
	}))
	return svc, root
}

func seedBackupAPIState(t *testing.T, root string, svc *app.Service) {
	t.Helper()
	mkdirAll := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", path, err)
		}
	}
	mkdirAll(filepath.Join(root, "sessions"))
	mkdirAll(filepath.Join(root, "memory"))
	mkdirAll(filepath.Join(root, "mcp"))
	mkdirAll(filepath.Join(root, "personas", "cat"))
	mkdirAll(filepath.Join(root, "state"))
	mkdirAll(filepath.Join(root, "auth"))
	writeTestFile(t, filepath.Join(root, "config.json"), `{"general":{"timezone":"Asia/Taipei"}}`)
	writeTestFile(t, filepath.Join(root, "sessions", "metadata.json"), `{"sessions":{"chat-1":{"session_id":"chat-1","message_count":1,"created_at":"2026-03-09T10:00:00Z","updated_at":"2026-03-09T10:00:00Z"}}}`)
	writeTestFile(t, filepath.Join(root, "memory", "MEMORY.md"), "api memory")
	writeTestFile(t, filepath.Join(root, "mcp", "alpha.json"), `{"name":"alpha","transport":"stdio","command":"alpha","trust":"trusted"}`)
	writeTestFile(t, filepath.Join(root, "mcp-builtin.json"), `{"playwright":true}`)
	writeTestFile(t, filepath.Join(root, "personas", "cat", "config.yaml"), "meta:\n  id: cat\n")
	writeTestFile(t, filepath.Join(root, "persona-state.json"), `{"active":"cat"}`)
	writeTestFile(t, filepath.Join(root, "state", "discord-bindings.json"), `{"bindings":{"chan-1":{"session_id":"discord:alpha"}}}`)
	writeTestFile(t, filepath.Join(root, "auth", "security-state.json"), `{"password_hash":"hash"}`)
	_ = svc
}

func performMultipartRequest(
	t *testing.T,
	handler http.Handler,
	path string,
	fieldName string,
	fileName string,
	data []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("Write multipart file failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}
