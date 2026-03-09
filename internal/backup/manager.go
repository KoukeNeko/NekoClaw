package backup

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/auth"
	yzip "github.com/yeka/zip"
)

var (
	ErrNotConfigured     = errors.New("backup manager not configured")
	ErrBackupNotFound    = errors.New("backup not found")
	ErrImportJobNotFound = errors.New("backup import job not found")
	ErrInvalidArchive    = errors.New("invalid backup archive")
	ErrInvalidManifest   = errors.New("invalid backup manifest")
	ErrPasswordRequired  = errors.New("backup password is required")
	ErrInvalidPassword   = errors.New("invalid backup password")
)

const (
	minBackupPasswordLength = 12
	maxBackupPasswordLength = 128
	defaultImportJobTTL     = 10 * time.Minute
	defaultMaxImportEvents  = 24
)

type ManagerOptions struct {
	ConfigRoot   string
	SessionsDir  string
	MemoryDir    string
	MCPDir       string
	PersonasDir  string
	StateDir     string
	AuthStore    *auth.Store
	JobTTL       time.Duration
	Now          func() time.Time
	MaxJobEvents int
}

type Manager struct {
	mu                  sync.Mutex
	configRoot          string
	backupsDir          string
	configPath          string
	authStore           *auth.Store
	sessionsDir         string
	memoryDir           string
	mcpDir              string
	mcpBuiltinStatePath string
	personasDir         string
	personaStatePath    string
	stateDir            string
	importJobs          map[string]*importJob
	jobTTL              time.Duration
	now                 func() time.Time
	maxJobEvents        int
}

type importJob struct {
	snapshot   ImportJobSnapshot
	password   string
	uploadPath string
}

type importProgressReporter func(stage, substage string, progress int, message string)

type authExport struct {
	Profiles []authExportProfile `json:"profiles"`
}

type authExportProfile struct {
	Metadata   auth.ProfileMetadata `json:"metadata"`
	Credential *auth.Credential     `json:"credential,omitempty"`
}

func NewManager(opts ManagerOptions) *Manager {
	configRoot := strings.TrimSpace(opts.ConfigRoot)
	mcpDir := strings.TrimSpace(opts.MCPDir)
	personasDir := strings.TrimSpace(opts.PersonasDir)
	jobTTL := opts.JobTTL
	if jobTTL <= 0 {
		jobTTL = defaultImportJobTTL
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	maxJobEvents := opts.MaxJobEvents
	if maxJobEvents <= 0 {
		maxJobEvents = defaultMaxImportEvents
	}

	manager := &Manager{
		configRoot:   configRoot,
		backupsDir:   filepath.Join(configRoot, "backups"),
		configPath:   filepath.Join(configRoot, "config.json"),
		authStore:    opts.AuthStore,
		sessionsDir:  strings.TrimSpace(opts.SessionsDir),
		memoryDir:    strings.TrimSpace(opts.MemoryDir),
		mcpDir:       mcpDir,
		personasDir:  personasDir,
		stateDir:     strings.TrimSpace(opts.StateDir),
		importJobs:   map[string]*importJob{},
		jobTTL:       jobTTL,
		now:          now,
		maxJobEvents: maxJobEvents,
	}
	if mcpDir != "" {
		manager.mcpBuiltinStatePath = filepath.Join(filepath.Dir(mcpDir), "mcp-builtin.json")
	}
	if personasDir != "" {
		manager.personaStatePath = filepath.Join(filepath.Dir(personasDir), "persona-state.json")
	}
	return manager
}

func (m *Manager) Create(password string) (Entry, error) {
	if err := m.ensureConfigured(); err != nil {
		return Entry{}, err
	}
	if err := validateBackupPassword(password); err != nil {
		return Entry{}, err
	}

	tempRoot, err := os.MkdirTemp(m.configRoot, "backup-build-*")
	if err != nil {
		return Entry{}, err
	}
	defer os.RemoveAll(tempRoot)

	manifest := newManifest(SourceCreated, time.Now().UTC())
	manifest, err = m.snapshotCurrentState(tempRoot, manifest)
	if err != nil {
		return Entry{}, err
	}
	return m.writeSnapshotToLibrary(tempRoot, manifest, password)
}

func (m *Manager) StartImport(reader io.Reader, fileName string, fileSize int64, password string) (ImportJobSnapshot, error) {
	if err := m.ensureConfigured(); err != nil {
		return ImportJobSnapshot{}, err
	}
	if err := validateBackupPassword(password); err != nil {
		return ImportJobSnapshot{}, err
	}

	uploadPath, written, err := m.writeUploadToTempFile(reader)
	if err != nil {
		return ImportJobSnapshot{}, err
	}
	if fileSize <= 0 {
		fileSize = written
	}

	now := m.now().UTC()
	job := &importJob{
		snapshot: ImportJobSnapshot{
			JobID:           generateImportJobID(),
			Status:          ImportJobStatusRunning,
			Stage:           ImportStageArchiveValidation,
			Substage:        ImportSubstageManifestRead,
			ProgressPercent: 18,
			Message:         "已收到 archive，正在讀取 manifest.json。",
			Events: []ImportJobEvent{{
				At:      now,
				Message: "已收到 archive，準備開始驗證備份內容。",
			}},
			CreatedAt:     now,
			UpdatedAt:     now,
			ExpiresAt:     now.Add(m.jobTTL),
			FileName:      sanitizeImportFileName(fileName),
			FileSizeBytes: fileSize,
		},
		password:   password,
		uploadPath: uploadPath,
	}
	if job.snapshot.FileName == "" {
		job.snapshot.FileName = filepath.Base(uploadPath)
	}

	m.mu.Lock()
	m.clearExpiredImportJobsLocked(now)
	m.importJobs[job.snapshot.JobID] = job
	m.mu.Unlock()

	go m.runImportJob(job.snapshot.JobID)
	return m.snapshotImportJob(job), nil
}

func (m *Manager) Import(reader io.Reader, password string) (Entry, error) {
	if err := m.ensureConfigured(); err != nil {
		return Entry{}, err
	}
	if err := validateBackupPassword(password); err != nil {
		return Entry{}, err
	}

	uploadPath, _, err := m.writeUploadToTempFile(reader)
	if err != nil {
		return Entry{}, err
	}
	defer os.Remove(uploadPath)
	return m.importUploadPath(uploadPath, password, nil)
}

func (m *Manager) GetImportJob(id string) (ImportJobSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearExpiredImportJobsLocked(m.now().UTC())
	job, ok := m.importJobs[strings.TrimSpace(id)]
	if !ok {
		return ImportJobSnapshot{}, ErrImportJobNotFound
	}
	return m.snapshotImportJob(job), nil
}

func (m *Manager) List() ([]Entry, error) {
	if err := m.ensureConfigured(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.backupsDir, 0o700); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(m.backupsDir)
	if err != nil {
		return nil, err
	}

	backups := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
			continue
		}
		path := filepath.Join(m.backupsDir, entry.Name())
		manifest, err := readManifestFromArchive(path)
		if err != nil {
			continue
		}
		if err := validateManifest(manifest); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		manifest.SizeBytes = info.Size()
		backups = append(backups, Entry{
			Manifest: manifest,
			FileName: entry.Name(),
		})
	}

	sort.SliceStable(backups, func(i, j int) bool {
		if backups[i].CreatedAt.Equal(backups[j].CreatedAt) {
			return backups[i].FileName > backups[j].FileName
		}
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}

func (m *Manager) Delete(id string) error {
	path, _, err := m.archivePath(id)
	if err != nil {
		return err
	}
	if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return nil
}

func (m *Manager) ArchivePath(id string) (string, string, error) {
	return m.archivePath(id)
}

func (m *Manager) Restore(id string, password string) (RestoreResult, error) {
	if err := m.ensureConfigured(); err != nil {
		return RestoreResult{}, err
	}
	if err := validateBackupPassword(password); err != nil {
		return RestoreResult{}, err
	}

	archivePath, _, err := m.archivePath(id)
	if err != nil {
		return RestoreResult{}, err
	}

	tempRoot, err := os.MkdirTemp(m.configRoot, "backup-restore-*")
	if err != nil {
		return RestoreResult{}, err
	}
	defer os.RemoveAll(tempRoot)

	manifest, err := readManifestFromArchive(archivePath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := validateManifest(manifest); err != nil {
		return RestoreResult{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}

	extractedRoot := filepath.Join(tempRoot, "archive")
	if err := extractZipToDir(archivePath, extractedRoot, password); err != nil {
		return RestoreResult{}, err
	}

	rollbackRoot := filepath.Join(tempRoot, "rollback")
	rollbackManifest := newManifest(SourceCreated, time.Now().UTC())
	rollbackManifest, err = m.snapshotCurrentState(rollbackRoot, rollbackManifest)
	if err != nil {
		return RestoreResult{}, err
	}

	if err := m.applySnapshot(extractedRoot, manifest); err != nil {
		if rollbackErr := m.applySnapshot(rollbackRoot, rollbackManifest); rollbackErr != nil {
			return RestoreResult{}, fmt.Errorf("restore failed: %w (rollback failed: %v)", err, rollbackErr)
		}
		return RestoreResult{}, err
	}

	return RestoreResult{RestartRequired: true}, nil
}

func (m *Manager) ensureConfigured() error {
	if strings.TrimSpace(m.configRoot) == "" {
		return ErrNotConfigured
	}
	return nil
}

func (m *Manager) runImportJob(jobID string) {
	m.updateImportJob(jobID, ImportStageArchiveValidation, ImportSubstageManifestRead, 18, "正在讀取 manifest.json。")

	password, uploadPath, ok := m.importJobSecrets(jobID)
	if !ok {
		return
	}

	entry, err := m.importUploadPath(uploadPath, password, m.updateImportJobCallback(jobID))
	if err != nil {
		m.failImportJob(jobID, err)
		return
	}
	m.completeImportJob(jobID, entry)
}

func (m *Manager) importJobSecrets(jobID string) (string, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.importJobs[jobID]
	if !ok {
		return "", "", false
	}
	return job.password, job.uploadPath, true
}

func (m *Manager) updateImportJobCallback(jobID string) importProgressReporter {
	return func(stage, substage string, progress int, message string) {
		m.updateImportJob(jobID, stage, substage, progress, message)
	}
}

func (m *Manager) updateImportJob(jobID string, stage string, substage string, progress int, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.importJobs[jobID]
	if !ok {
		return
	}
	now := m.now().UTC()
	job.snapshot.Status = ImportJobStatusRunning
	job.snapshot.Stage = stage
	job.snapshot.Substage = substage
	job.snapshot.ProgressPercent = progress
	job.snapshot.Message = strings.TrimSpace(message)
	job.snapshot.UpdatedAt = now
	job.snapshot.ExpiresAt = now.Add(m.jobTTL)
	m.appendImportEventLocked(job, message)
}

func (m *Manager) completeImportJob(jobID string, entry Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.importJobs[jobID]
	if !ok {
		return
	}
	now := m.now().UTC()
	job.snapshot.Status = ImportJobStatusCompleted
	job.snapshot.Stage = ImportStageCompleted
	job.snapshot.Substage = ""
	job.snapshot.ProgressPercent = 100
	job.snapshot.Message = "備份檔已寫入備份庫。"
	job.snapshot.ErrorCode = ""
	job.snapshot.ErrorMessage = ""
	copyEntry := entry
	job.snapshot.Backup = &copyEntry
	job.snapshot.UpdatedAt = now
	job.snapshot.ExpiresAt = now.Add(m.jobTTL)
	m.appendImportEventLocked(job, "備份檔已寫入備份庫。")
	m.releaseImportJobResourcesLocked(job)
}

func (m *Manager) failImportJob(jobID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.importJobs[jobID]
	if !ok {
		return
	}
	now := m.now().UTC()
	job.snapshot.Status = ImportJobStatusFailed
	job.snapshot.Stage = ImportStageFailed
	if job.snapshot.ProgressPercent < 1 {
		job.snapshot.ProgressPercent = 1
	}
	job.snapshot.Message = importFailureMessage(err)
	job.snapshot.ErrorCode = importErrorCode(err)
	job.snapshot.ErrorMessage = err.Error()
	job.snapshot.UpdatedAt = now
	job.snapshot.ExpiresAt = now.Add(m.jobTTL)
	m.appendImportEventLocked(job, importFailureMessage(err))
	m.releaseImportJobResourcesLocked(job)
}

func (m *Manager) snapshotImportJob(job *importJob) ImportJobSnapshot {
	if job == nil {
		return ImportJobSnapshot{}
	}
	snapshot := job.snapshot
	if len(job.snapshot.Events) > 0 {
		snapshot.Events = make([]ImportJobEvent, len(job.snapshot.Events))
		copy(snapshot.Events, job.snapshot.Events)
	}
	if job.snapshot.Backup != nil {
		entry := *job.snapshot.Backup
		snapshot.Backup = &entry
	}
	return snapshot
}

func (m *Manager) appendImportEventLocked(job *importJob, message string) {
	if job == nil {
		return
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}
	if n := len(job.snapshot.Events); n > 0 && job.snapshot.Events[n-1].Message == msg {
		return
	}
	job.snapshot.Events = append(job.snapshot.Events, ImportJobEvent{
		At:      m.now().UTC(),
		Message: msg,
	})
	if len(job.snapshot.Events) > m.maxJobEvents {
		job.snapshot.Events = job.snapshot.Events[len(job.snapshot.Events)-m.maxJobEvents:]
	}
}

func (m *Manager) clearExpiredImportJobsLocked(now time.Time) {
	for id, job := range m.importJobs {
		if job == nil {
			delete(m.importJobs, id)
			continue
		}
		if now.After(job.snapshot.ExpiresAt) {
			m.releaseImportJobResourcesLocked(job)
			delete(m.importJobs, id)
		}
	}
}

func (m *Manager) releaseImportJobResourcesLocked(job *importJob) {
	if job == nil {
		return
	}
	job.password = ""
	if job.uploadPath != "" {
		_ = os.Remove(job.uploadPath)
		job.uploadPath = ""
	}
}

func (m *Manager) writeUploadToTempFile(reader io.Reader) (string, int64, error) {
	uploadFile, err := os.CreateTemp(m.configRoot, "backup-upload-*.zip")
	if err != nil {
		return "", 0, err
	}
	uploadPath := uploadFile.Name()
	written, err := io.Copy(uploadFile, reader)
	if err != nil {
		_ = uploadFile.Close()
		_ = os.Remove(uploadPath)
		return "", 0, err
	}
	if err := uploadFile.Close(); err != nil {
		_ = os.Remove(uploadPath)
		return "", 0, err
	}
	return uploadPath, written, nil
}

func (m *Manager) importUploadPath(uploadPath string, password string, report importProgressReporter) (Entry, error) {
	tempRoot, err := os.MkdirTemp(m.configRoot, "backup-import-*")
	if err != nil {
		return Entry{}, err
	}
	defer os.RemoveAll(tempRoot)

	if report != nil {
		report(ImportStageArchiveValidation, ImportSubstageManifestRead, 18, "正在讀取 manifest.json。")
	}
	uploadedManifest, err := readManifestFromArchive(uploadPath)
	if err != nil {
		return Entry{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := validateManifest(uploadedManifest); err != nil {
		return Entry{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}

	extractedRoot := filepath.Join(tempRoot, "extracted")
	if report != nil {
		report(ImportStageArchiveValidation, ImportSubstagePayloadDecrypting, 36, "正在用提供的密碼解密 payload。")
	}
	if err := extractZipToDir(uploadPath, extractedRoot, password); err != nil {
		return Entry{}, err
	}

	payloadSrc := filepath.Join(extractedRoot, "payload")
	if !dirExists(payloadSrc) {
		return Entry{}, fmt.Errorf("%w: payload directory missing", ErrInvalidArchive)
	}

	if report != nil {
		report(ImportStageArchiveValidation, ImportSubstagePayloadValidating, 54, "正在驗證 payload 與 component 摘要。")
	}
	createdAt := uploadedManifest.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = m.now().UTC()
	}
	manifest := newManifest(SourceImported, createdAt)
	if _, err := copyDirectory(payloadSrc, filepath.Join(tempRoot, "payload"), nil); err != nil {
		return Entry{}, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	manifest.Components, err = buildComponentSummaries(filepath.Join(tempRoot, "payload"))
	if err != nil {
		return Entry{}, err
	}
	if err := writeManifest(filepath.Join(tempRoot, "manifest.json"), manifest); err != nil {
		return Entry{}, err
	}
	return m.writeSnapshotToLibraryWithProgress(tempRoot, manifest, password, report)
}

func importErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrPasswordRequired):
		return "password_required"
	case errors.Is(err, ErrInvalidPassword):
		return "invalid_backup_password"
	case errors.Is(err, ErrInvalidManifest):
		return "invalid_backup_manifest"
	case errors.Is(err, ErrInvalidArchive):
		return "invalid_backup_archive"
	case errors.Is(err, ErrNotConfigured):
		return "backup_not_configured"
	default:
		return "backup_import_failed"
	}
}

func importFailureMessage(err error) string {
	switch {
	case errors.Is(err, ErrInvalidPassword):
		return "備份密碼無法解密這份 archive。"
	case errors.Is(err, ErrInvalidManifest):
		return "manifest.json 無效，無法匯入這份備份。"
	case errors.Is(err, ErrInvalidArchive):
		return "archive 內容驗證失敗，匯入已中止。"
	default:
		return "匯入備份檔失敗。"
	}
}

func (m *Manager) snapshotCurrentState(snapshotRoot string, manifest Manifest) (Manifest, error) {
	payloadRoot := filepath.Join(snapshotRoot, "payload")
	if err := os.MkdirAll(payloadRoot, 0o700); err != nil {
		return Manifest{}, err
	}

	if _, err := copyFileIfExists(m.configPath, filepath.Join(payloadRoot, "config", "config.json")); err != nil {
		return Manifest{}, err
	}
	if _, err := m.exportAuth(filepath.Join(payloadRoot, "auth", "store.json")); err != nil {
		return Manifest{}, err
	}
	if _, err := copyDirectory(m.sessionsDir, filepath.Join(payloadRoot, "sessions"), nil); err != nil {
		return Manifest{}, err
	}
	if _, err := copyDirectory(m.memoryDir, filepath.Join(payloadRoot, "memory"), func(path string) bool {
		base := filepath.Base(path)
		if base == "search.db" || base == "search.db-shm" || base == "search.db-wal" {
			return false
		}
		return !strings.HasSuffix(base, ".tmp")
	}); err != nil {
		return Manifest{}, err
	}
	if _, err := copyDirectory(m.mcpDir, filepath.Join(payloadRoot, "mcp", "configs"), nil); err != nil {
		return Manifest{}, err
	}
	if _, err := copyFileIfExists(m.mcpBuiltinStatePath, filepath.Join(payloadRoot, "mcp", "mcp-builtin.json")); err != nil {
		return Manifest{}, err
	}
	if _, err := copyDirectory(m.personasDir, filepath.Join(payloadRoot, "personas", "definitions"), nil); err != nil {
		return Manifest{}, err
	}
	if _, err := copyFileIfExists(m.personaStatePath, filepath.Join(payloadRoot, "personas", "persona-state.json")); err != nil {
		return Manifest{}, err
	}
	if _, err := copyFileIfExists(m.bindingPath("discord-bindings.json"), filepath.Join(payloadRoot, "bindings", "discord-bindings.json")); err != nil {
		return Manifest{}, err
	}
	if _, err := copyFileIfExists(m.bindingPath("telegram-bindings.json"), filepath.Join(payloadRoot, "bindings", "telegram-bindings.json")); err != nil {
		return Manifest{}, err
	}

	components, err := buildComponentSummaries(payloadRoot)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Components = components
	if err := writeManifest(filepath.Join(snapshotRoot, "manifest.json"), manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m *Manager) writeSnapshotToLibrary(snapshotRoot string, manifest Manifest, password string) (Entry, error) {
	return m.writeSnapshotToLibraryWithProgress(snapshotRoot, manifest, password, nil)
}

func (m *Manager) writeSnapshotToLibraryWithProgress(snapshotRoot string, manifest Manifest, password string, report importProgressReporter) (Entry, error) {
	if err := os.MkdirAll(m.backupsDir, 0o700); err != nil {
		return Entry{}, err
	}

	finalPath := filepath.Join(m.backupsDir, backupFileName(manifest.BackupID))
	tempPath := finalPath + ".tmp"
	defer os.Remove(tempPath)

	var finalSize int64
	for attempt := 0; attempt < 5; attempt++ {
		if report != nil {
			report(ImportStageLibraryWrite, ImportSubstageManifestRewriting, 68, "正在重寫 manifest.json 與 size metadata。")
		}
		if err := writeManifest(filepath.Join(snapshotRoot, "manifest.json"), manifest); err != nil {
			return Entry{}, err
		}
		if report != nil {
			report(ImportStageLibraryWrite, ImportSubstagePayloadRepacking, 78, "正在用這次提供的密碼重新封裝 payload。")
			report(ImportStageLibraryWrite, ImportSubstageTempArchiveWrite, 90, "正在寫入暫存 archive。")
		}
		if err := zipSnapshot(snapshotRoot, tempPath, password); err != nil {
			return Entry{}, err
		}
		info, err := os.Stat(tempPath)
		if err != nil {
			return Entry{}, err
		}
		finalSize = info.Size()
		if info.Size() == manifest.SizeBytes {
			break
		}
		manifest.SizeBytes = info.Size()
	}
	manifest.SizeBytes = finalSize

	if report != nil {
		report(ImportStageLibraryWrite, ImportSubstageArchiveRenaming, 97, "暫存 archive 已完成，正在切換正式檔案。")
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return Entry{}, err
	}
	return Entry{
		Manifest: manifest,
		FileName: filepath.Base(finalPath),
	}, nil
}

func (m *Manager) applySnapshot(snapshotRoot string, manifest Manifest) error {
	if err := validateManifest(manifest); err != nil {
		return err
	}

	payloadRoot := filepath.Join(snapshotRoot, "payload")
	if err := replaceFile(filepath.Join(payloadRoot, "config", "config.json"), m.configPath); err != nil {
		return err
	}
	if err := m.importAuth(filepath.Join(payloadRoot, "auth", "store.json")); err != nil {
		return err
	}
	if err := replaceDirectory(filepath.Join(payloadRoot, "sessions"), m.sessionsDir); err != nil {
		return err
	}
	if err := replaceDirectory(filepath.Join(payloadRoot, "memory"), m.memoryDir); err != nil {
		return err
	}
	if err := replaceDirectory(filepath.Join(payloadRoot, "mcp", "configs"), m.mcpDir); err != nil {
		return err
	}
	if err := replaceFile(filepath.Join(payloadRoot, "mcp", "mcp-builtin.json"), m.mcpBuiltinStatePath); err != nil {
		return err
	}
	if err := replaceDirectory(filepath.Join(payloadRoot, "personas", "definitions"), m.personasDir); err != nil {
		return err
	}
	if err := replaceFile(filepath.Join(payloadRoot, "personas", "persona-state.json"), m.personaStatePath); err != nil {
		return err
	}
	if err := replaceFile(filepath.Join(payloadRoot, "bindings", "discord-bindings.json"), m.bindingPath("discord-bindings.json")); err != nil {
		return err
	}
	if err := replaceFile(filepath.Join(payloadRoot, "bindings", "telegram-bindings.json"), m.bindingPath("telegram-bindings.json")); err != nil {
		return err
	}
	return nil
}

func (m *Manager) exportAuth(destPath string) (int, error) {
	if m.authStore == nil {
		return 0, nil
	}
	profiles, err := m.authStore.ListProfiles("")
	if err != nil {
		return 0, err
	}
	if len(profiles) == 0 {
		return 0, nil
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Provider != profiles[j].Provider {
			return profiles[i].Provider < profiles[j].Provider
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})

	payload := authExport{Profiles: make([]authExportProfile, 0, len(profiles))}
	for _, profile := range profiles {
		item := authExportProfile{Metadata: profile}
		credential, err := m.authStore.LoadCredential(profile.Provider, profile.ProfileID)
		switch {
		case err == nil:
			item.Credential = &credential
		case errors.Is(err, auth.ErrCredentialNotFound):
			// Keep metadata-only profiles.
		default:
			return 0, err
		}
		payload.Profiles = append(payload.Profiles, item)
	}
	if err := writeJSONFile(destPath, payload, 0o600); err != nil {
		return 0, err
	}
	return len(payload.Profiles), nil
}

func (m *Manager) importAuth(srcPath string) error {
	if m.authStore == nil {
		if fileExists(srcPath) {
			return ErrNotConfigured
		}
		return nil
	}

	if err := clearAuthStore(m.authStore); err != nil {
		return err
	}
	if !fileExists(srcPath) {
		return nil
	}

	var payload authExport
	if err := readJSONFile(srcPath, &payload); err != nil {
		return err
	}
	for _, profile := range payload.Profiles {
		if err := m.authStore.UpsertProfile(profile.Metadata); err != nil {
			return err
		}
		if profile.Credential != nil {
			if err := m.authStore.SaveCredential(profile.Metadata.Provider, profile.Metadata.ProfileID, *profile.Credential); err != nil {
				return err
			}
		}
	}
	return nil
}

func clearAuthStore(store *auth.Store) error {
	profiles, err := store.ListProfiles("")
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if err := store.DeleteCredential(profile.Provider, profile.ProfileID); err != nil && !errors.Is(err, auth.ErrCredentialNotFound) {
			return err
		}
		if err := store.DeleteProfile(profile.Provider, profile.ProfileID); err != nil && !errors.Is(err, auth.ErrProfileNotFound) {
			return err
		}
	}
	return nil
}

func buildComponentSummaries(payloadRoot string) ([]ComponentSummary, error) {
	sessionsCount, err := countFiles(filepath.Join(payloadRoot, "sessions"))
	if err != nil {
		return nil, err
	}
	memoryCount, err := countFiles(filepath.Join(payloadRoot, "memory"))
	if err != nil {
		return nil, err
	}
	mcpConfigsCount, err := countFiles(filepath.Join(payloadRoot, "mcp", "configs"))
	if err != nil {
		return nil, err
	}
	personaDefsCount, err := countFiles(filepath.Join(payloadRoot, "personas", "definitions"))
	if err != nil {
		return nil, err
	}
	bindingsCount, err := countFiles(filepath.Join(payloadRoot, "bindings"))
	if err != nil {
		return nil, err
	}

	components := []ComponentSummary{
		{Key: ComponentConfig, Label: "Config", ItemCount: boolCount(fileExists(filepath.Join(payloadRoot, "config", "config.json")))},
		{Key: ComponentAuth, Label: "Auth", ItemCount: boolCount(fileExists(filepath.Join(payloadRoot, "auth", "store.json")))},
		{Key: ComponentSessions, Label: "Sessions", ItemCount: sessionsCount},
		{Key: ComponentMemory, Label: "Memory", ItemCount: memoryCount},
		{Key: ComponentMCP, Label: "MCP", ItemCount: mcpConfigsCount + boolCount(fileExists(filepath.Join(payloadRoot, "mcp", "mcp-builtin.json")))},
		{Key: ComponentPersonas, Label: "Personas", ItemCount: personaDefsCount + boolCount(fileExists(filepath.Join(payloadRoot, "personas", "persona-state.json")))},
		{Key: ComponentBindings, Label: "Bindings", ItemCount: bindingsCount},
	}
	return components, nil
}

func validateBackupPassword(password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return ErrPasswordRequired
	}
	if len(password) < minBackupPasswordLength || len(password) > maxBackupPasswordLength {
		return ErrInvalidPassword
	}
	return nil
}

func mapZipError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, yzip.ErrPassword), errors.Is(err, yzip.ErrDecryption), errors.Is(err, yzip.ErrAuthentication):
		return ErrInvalidPassword
	default:
		return fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
}

func newManifest(source string, createdAt time.Time) Manifest {
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return Manifest{
		Version:         ManifestVersionEncrypted,
		BackupID:        generateBackupID(),
		CreatedAt:       createdAt.UTC(),
		Source:          source,
		Encryption:      EncryptionZipAES256,
		ContainsSecrets: true,
		RestoreMode:     RestoreModeReplace,
		RestartRequired: true,
		Components: []ComponentSummary{
			{Key: ComponentConfig, Label: "Config"},
			{Key: ComponentAuth, Label: "Auth"},
			{Key: ComponentSessions, Label: "Sessions"},
			{Key: ComponentMemory, Label: "Memory"},
			{Key: ComponentMCP, Label: "MCP"},
			{Key: ComponentPersonas, Label: "Personas"},
			{Key: ComponentBindings, Label: "Bindings"},
		},
	}
}

func validateManifest(manifest Manifest) error {
	if strings.TrimSpace(manifest.BackupID) == "" {
		return ErrInvalidManifest
	}
	switch manifest.Source {
	case SourceCreated, SourceImported:
	default:
		return ErrInvalidManifest
	}
	if !manifest.ContainsSecrets || manifest.RestoreMode != RestoreModeReplace || !manifest.RestartRequired {
		return ErrInvalidManifest
	}
	if manifest.Version != ManifestVersionEncrypted {
		return ErrInvalidManifest
	}
	if manifest.Encryption != EncryptionZipAES256 {
		return ErrInvalidManifest
	}
	if len(manifest.Components) != 7 {
		return ErrInvalidManifest
	}
	expected := map[string]struct{}{
		ComponentConfig:   {},
		ComponentAuth:     {},
		ComponentSessions: {},
		ComponentMemory:   {},
		ComponentMCP:      {},
		ComponentPersonas: {},
		ComponentBindings: {},
	}
	for _, component := range manifest.Components {
		if _, ok := expected[component.Key]; !ok {
			return ErrInvalidManifest
		}
		delete(expected, component.Key)
	}
	if len(expected) != 0 {
		return ErrInvalidManifest
	}
	return nil
}

func (m *Manager) archivePath(id string) (string, string, error) {
	if err := m.ensureConfigured(); err != nil {
		return "", "", err
	}
	cleanID := sanitizeBackupID(id)
	if cleanID == "" {
		return "", "", ErrBackupNotFound
	}
	fileName := backupFileName(cleanID)
	path := filepath.Join(m.backupsDir, fileName)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", ErrBackupNotFound
		}
		return "", "", err
	}
	return path, fileName, nil
}

func backupFileName(id string) string {
	return "backup-" + sanitizeBackupID(id) + ".zip"
}

func sanitizeBackupID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-':
			builder.WriteRune(r)
		}
	}
	return strings.Trim(builder.String(), "-")
}

func generateBackupID() string {
	random := make([]byte, 4)
	_, _ = rand.Read(random)
	return strings.ToLower(time.Now().UTC().Format("20060102t150405z") + "-" + hex.EncodeToString(random))
}

func generateImportJobID() string {
	random := make([]byte, 12)
	_, _ = rand.Read(random)
	return strings.ToLower(base64.RawURLEncoding.EncodeToString(random))
}

func sanitizeImportFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return filepath.Base(name)
}

func writeManifest(path string, manifest Manifest) error {
	return writeJSONFile(path, manifest, 0o600)
}

func readManifest(path string) (Manifest, error) {
	var manifest Manifest
	if err := readJSONFile(path, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func readManifestFromArchive(path string) (Manifest, error) {
	reader, err := yzip.OpenReader(path)
	if err != nil {
		return Manifest{}, err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name != "manifest.json" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return Manifest{}, err
		}
		defer rc.Close()
		var manifest Manifest
		if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
			return Manifest{}, err
		}
		return manifest, nil
	}
	return Manifest{}, fs.ErrNotExist
}

func writeJSONFile(path string, payload any, perm fs.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

func readJSONFile(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func zipSnapshot(srcRoot string, destZip string, password string) error {
	if err := os.MkdirAll(filepath.Dir(destZip), 0o700); err != nil {
		return err
	}
	file, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := yzip.NewWriter(file)

	var paths []string
	if err := filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(paths)

	for _, path := range paths {
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "" || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("invalid zip path %q", rel)
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		header, err := yzip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = rel
		header.Method = yzip.Deflate
		if strings.HasPrefix(rel, "payload/") && strings.TrimSpace(password) != "" {
			header.SetPassword(password)
			header.SetEncryptionMethod(yzip.AES256Encryption)
		}
		target, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(target, source); err != nil {
			source.Close()
			return err
		}
		if err := source.Close(); err != nil {
			return err
		}
	}
	return writer.Close()
}

func extractZipToDir(zipPath, dst string, password string) error {
	reader, err := yzip.OpenReader(zipPath)
	if err != nil {
		return mapZipError(err)
	}
	defer reader.Close()

	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	root := filepath.Clean(dst)
	for _, file := range reader.File {
		name := filepath.Clean(filepath.FromSlash(file.Name))
		if name == "." || strings.HasPrefix(name, "..") {
			return fmt.Errorf("unsafe archive path %q", file.Name)
		}
		targetPath := filepath.Join(root, name)
		if targetPath != root && !strings.HasPrefix(targetPath, root+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe archive path %q", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			return err
		}
		if file.IsEncrypted() {
			if strings.TrimSpace(password) == "" {
				return ErrPasswordRequired
			}
			file.SetPassword(password)
		}
		reader, err := file.Open()
		if err != nil {
			return mapZipError(err)
		}
		target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			reader.Close()
			return err
		}
		if _, err := io.Copy(target, reader); err != nil {
			target.Close()
			reader.Close()
			return mapZipError(err)
		}
		if err := target.Close(); err != nil {
			reader.Close()
			return err
		}
		if err := reader.Close(); err != nil {
			return err
		}
	}
	return nil
}

func countFiles(root string) (int, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return 0, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 1, nil
	}

	count := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		count++
		return nil
	})
	return count, err
}

func copyFileIfExists(src, dst string) (bool, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return false, nil
	}
	info, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("%s is a directory", src)
	}
	if err := copyFile(src, dst); err != nil {
		return false, err
	}
	return true, nil
}

func copyFile(src, dst string) error {
	if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	target, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer target.Close()

	_, err = io.Copy(target, source)
	return err
}

func copyDirectory(src, dst string, filter func(path string) bool) (int, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return 0, nil
	}
	info, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", src)
	}

	count := 0
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if filter != nil && !filter(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		targetPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o700)
		}
		if err := copyFile(path, targetPath); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func replaceFile(src, dst string) error {
	if strings.TrimSpace(dst) == "" {
		if fileExists(src) {
			return ErrNotConfigured
		}
		return nil
	}
	if err := removePath(dst); err != nil {
		return err
	}
	if !fileExists(src) {
		return nil
	}
	return copyFile(src, dst)
}

func replaceDirectory(src, dst string) error {
	if strings.TrimSpace(dst) == "" {
		if dirExists(src) {
			return ErrNotConfigured
		}
		return nil
	}
	if err := removePath(dst); err != nil {
		return err
	}
	if !dirExists(src) {
		return nil
	}
	_, err := copyDirectory(src, dst, nil)
	return err
}

func removePath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	return info.IsDir()
}

func boolCount(ok bool) int {
	if ok {
		return 1
	}
	return 0
}

func (m *Manager) bindingPath(fileName string) string {
	if strings.TrimSpace(m.stateDir) == "" {
		return ""
	}
	return filepath.Join(m.stateDir, fileName)
}
