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
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/backup"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/security"
)

const apiBackupPassword = "backup-password-123"

func TestBackupsEndpointsCreateListDownloadRestoreAndDelete(t *testing.T) {
	svc, root := newBackupTestService(t)
	seedBackupAPIState(t, root, svc)
	handler := NewServer(svc).Handler()

	createResp := performJSONRequest(t, handler, http.MethodPost, "/v1/backups/create", `{"password":"`+apiBackupPassword+`"}`)
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
	if createPayload.Backup.Encryption != backup.EncryptionZipAES256 {
		t.Fatalf("encryption = %q, want %q", createPayload.Backup.Encryption, backup.EncryptionZipAES256)
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
		`{"id":"`+createPayload.Backup.BackupID+`","password":"`+apiBackupPassword+`"}`,
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

	createResp := performJSONRequest(t, handler, http.MethodPost, "/v1/backups/create", `{"password":"`+apiBackupPassword+`"}`)
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

	invalidImport := performMultipartRequest(
		t,
		handler,
		"/v1/backups/import",
		"file",
		"broken.zip",
		[]byte("not-a-zip"),
		map[string]string{"password": apiBackupPassword},
	)
	if invalidImport.Code != http.StatusAccepted {
		t.Fatalf("expected invalid import to return 202, got %d body=%s", invalidImport.Code, invalidImport.Body.String())
	}
	invalidJob := decodeBackupImportJob(t, invalidImport)
	invalidFinal := waitForBackupImportJob(t, handler, invalidJob.JobID, nil)
	if invalidFinal.Status != backup.ImportJobStatusFailed {
		t.Fatalf("invalid import job status = %q, want failed", invalidFinal.Status)
	}
	if invalidFinal.ErrorCode != "invalid_backup_manifest" {
		t.Fatalf("invalid import error code = %q, want invalid_backup_manifest", invalidFinal.ErrorCode)
	}

	importResp := performMultipartRequest(
		t,
		handler,
		"/v1/backups/import",
		"file",
		"backup.zip",
		rawArchive,
		map[string]string{"password": apiBackupPassword},
	)
	if importResp.Code != http.StatusAccepted {
		t.Fatalf("unexpected import status: %d body=%s", importResp.Code, importResp.Body.String())
	}
	startedJob := decodeBackupImportJob(t, importResp)
	importedJob := waitForBackupImportJob(t, handler, startedJob.JobID, nil)
	if importedJob.Status != backup.ImportJobStatusCompleted {
		t.Fatalf("import job status = %q, want completed", importedJob.Status)
	}
	if importedJob.Backup == nil {
		t.Fatal("expected completed import job to include backup entry")
	}
	if importedJob.Backup.Source != backup.SourceImported {
		t.Fatalf("source = %q, want %q", importedJob.Backup.Source, backup.SourceImported)
	}

	wrongPassword := performMultipartRequest(
		t,
		handler,
		"/v1/backups/import",
		"file",
		"backup.zip",
		rawArchive,
		map[string]string{"password": "wrong-password-456"},
	)
	if wrongPassword.Code != http.StatusAccepted {
		t.Fatalf("wrong password import status = %d body=%s", wrongPassword.Code, wrongPassword.Body.String())
	}
	wrongPasswordJob := decodeBackupImportJob(t, wrongPassword)
	wrongPasswordFinal := waitForBackupImportJob(t, handler, wrongPasswordJob.JobID, nil)
	if wrongPasswordFinal.Status != backup.ImportJobStatusFailed {
		t.Fatalf("wrong password job status = %q, want failed", wrongPasswordFinal.Status)
	}
	if wrongPasswordFinal.ErrorCode != "invalid_backup_password" {
		t.Fatalf("wrong password import error code = %q, want invalid_backup_password", wrongPasswordFinal.ErrorCode)
	}

	missingPasswordCreate := performJSONRequest(t, handler, http.MethodPost, "/v1/backups/create", `{}`)
	if missingPasswordCreate.Code != http.StatusBadRequest {
		t.Fatalf("expected missing password create to return 400, got %d body=%s", missingPasswordCreate.Code, missingPasswordCreate.Body.String())
	}
	if code := errorCodeFromBody(t, missingPasswordCreate); code != "password_required" {
		t.Fatalf("missing password create error code = %q, want password_required", code)
	}

	missingPasswordImport := performMultipartRequest(t, handler, "/v1/backups/import", "file", "backup.zip", rawArchive, nil)
	if missingPasswordImport.Code != http.StatusBadRequest {
		t.Fatalf("expected missing password import to return 400, got %d body=%s", missingPasswordImport.Code, missingPasswordImport.Body.String())
	}
	if code := errorCodeFromBody(t, missingPasswordImport); code != "password_required" {
		t.Fatalf("missing password import error code = %q, want password_required", code)
	}

	missingIDRestore := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/backups/restore",
		`{"password":"`+apiBackupPassword+`"}`,
	)
	if missingIDRestore.Code != http.StatusBadRequest {
		t.Fatalf("expected restore missing id to return 400, got %d body=%s", missingIDRestore.Code, missingIDRestore.Body.String())
	}

	missingPasswordRestore := performJSONRequest(
		t,
		handler,
		http.MethodPost,
		"/v1/backups/restore",
		`{"id":"`+createPayload.Backup.BackupID+`"}`,
	)
	if missingPasswordRestore.Code != http.StatusBadRequest {
		t.Fatalf("expected missing password restore to return 400, got %d body=%s", missingPasswordRestore.Code, missingPasswordRestore.Body.String())
	}
	if code := errorCodeFromBody(t, missingPasswordRestore); code != "password_required" {
		t.Fatalf("missing password restore error code = %q, want password_required", code)
	}
}

func TestBackupsImportRequiresOriginOrTrustedUIHeaderWhenSecurityEnabled(t *testing.T) {
	svc, root := newBackupTestService(t)
	seedBackupAPIState(t, root, svc)
	svc.SetSecurityConfig(core.DefaultSecurityConfig())

	entry, err := svc.CreateBackup(apiBackupPassword)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}
	rawArchive, err := os.ReadFile(filepath.Join(root, "backups", entry.FileName))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}

	runtime, err := security.NewRuntime(security.RuntimeOptions{
		BaseDir: t.TempDir(),
		Config:  svc.GetSecurityConfig(),
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	setupToken, err := runtime.EnsureSetupToken()
	if err != nil {
		t.Fatalf("EnsureSetupToken failed: %v", err)
	}

	server := NewServer(svc)
	server.SetSecurityRuntime(runtime)
	handler := server.Handler()

	setup := performSecurityRequest(t, handler, http.MethodPost, "/v1/security/setup", `{
		"token":"`+setupToken+`",
		"password":"correct horse battery"
	}`, nil, nil)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", setup.Code, setup.Body.String())
	}
	sessionCookie := requireSessionCookie(t, setup)

	blocked := performMultipartRequestWithOptions(
		t,
		handler,
		"/v1/backups/import",
		"file",
		"backup.zip",
		rawArchive,
		map[string]string{"password": apiBackupPassword},
		nil,
		[]*http.Cookie{sessionCookie},
	)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("expected blocked import to return 403, got %d body=%s", blocked.Code, blocked.Body.String())
	}
	if code := errorCodeFromBody(t, blocked); code != "origin_mismatch" {
		t.Fatalf("blocked import error code = %q, want origin_mismatch", code)
	}

	allowed := performMultipartRequestWithOptions(
		t,
		handler,
		"/v1/backups/import",
		"file",
		"backup.zip",
		rawArchive,
		map[string]string{"password": apiBackupPassword},
		map[string]string{trustedBrowserUIHeader: trustedBrowserUIHeaderValue},
		[]*http.Cookie{sessionCookie},
	)
	if allowed.Code != http.StatusAccepted {
		t.Fatalf("expected trusted UI import to return 202, got %d body=%s", allowed.Code, allowed.Body.String())
	}
	allowedJob := decodeBackupImportJob(t, allowed)
	finalJob := waitForBackupImportJob(t, handler, allowedJob.JobID, []*http.Cookie{sessionCookie})
	if finalJob.Status != backup.ImportJobStatusCompleted {
		t.Fatalf("trusted UI import job status = %q, want completed", finalJob.Status)
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
	formFields map[string]string,
) *httptest.ResponseRecorder {
	return performMultipartRequestWithOptions(t, handler, path, fieldName, fileName, data, formFields, nil, nil)
}

func performMultipartRequestWithOptions(
	t *testing.T,
	handler http.Handler,
	path string,
	fieldName string,
	fileName string,
	data []byte,
	formFields map[string]string,
	headers map[string]string,
	cookies []*http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range formFields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("WriteField(%s) failed: %v", key, err)
		}
	}
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
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeBackupImportJob(t *testing.T, rr *httptest.ResponseRecorder) backup.ImportJobSnapshot {
	t.Helper()
	var payload struct {
		Job backup.ImportJobSnapshot `json:"job"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode backup import job response: %v", err)
	}
	if strings.TrimSpace(payload.Job.JobID) == "" {
		t.Fatal("expected import job id")
	}
	return payload.Job
}

func waitForBackupImportJob(
	t *testing.T,
	handler http.Handler,
	jobID string,
	cookies []*http.Cookie,
) backup.ImportJobSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/v1/backups/import/jobs/"+jobID, nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected import job status: %d body=%s", rr.Code, rr.Body.String())
		}
		job := decodeBackupImportJob(t, rr)
		if job.Status == backup.ImportJobStatusCompleted || job.Status == backup.ImportJobStatusFailed {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("backup import job %s did not reach a terminal state", jobID)
	return backup.ImportJobSnapshot{}
}
