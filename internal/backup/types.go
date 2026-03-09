package backup

import "time"

const (
	ManifestVersionEncrypted = 2

	SourceCreated  = "created"
	SourceImported = "imported"

	RestoreModeReplace = "replace"

	EncryptionZipAES256 = "zip-aes-256"
)

const (
	ImportJobStatusRunning   = "running"
	ImportJobStatusCompleted = "completed"
	ImportJobStatusFailed    = "failed"
)

const (
	ImportStageArchiveValidation = "archive_validation"
	ImportStageLibraryWrite      = "library_write"
	ImportStageCompleted         = "completed"
	ImportStageFailed            = "failed"
)

const (
	ImportSubstageManifestRead      = "manifest_read"
	ImportSubstagePayloadDecrypting = "payload_decrypting"
	ImportSubstagePayloadValidating = "payload_validating"
	ImportSubstageManifestRewriting = "manifest_rewriting"
	ImportSubstagePayloadRepacking  = "payload_repacking"
	ImportSubstageTempArchiveWrite  = "temp_archive_writing"
	ImportSubstageArchiveRenaming   = "archive_renaming"
)

const (
	ComponentConfig   = "config"
	ComponentAuth     = "auth"
	ComponentSessions = "sessions"
	ComponentMemory   = "memory"
	ComponentMCP      = "mcp"
	ComponentPersonas = "personas"
	ComponentBindings = "bindings"
)

type ComponentSummary struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	ItemCount int    `json:"item_count"`
}

type Manifest struct {
	Version         int                `json:"version"`
	BackupID        string             `json:"backup_id"`
	CreatedAt       time.Time          `json:"created_at"`
	Source          string             `json:"source"`
	Encryption      string             `json:"encryption,omitempty"`
	ContainsSecrets bool               `json:"contains_secrets"`
	RestoreMode     string             `json:"restore_mode"`
	RestartRequired bool               `json:"restart_required"`
	Components      []ComponentSummary `json:"components"`
	SizeBytes       int64              `json:"size_bytes"`
}

type Entry struct {
	Manifest
	FileName string `json:"file_name"`
}

type RestoreResult struct {
	RestartRequired bool `json:"restart_required"`
}

type ImportJobEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type ImportJobSnapshot struct {
	JobID           string           `json:"job_id"`
	Status          string           `json:"status"`
	Stage           string           `json:"stage"`
	Substage        string           `json:"substage,omitempty"`
	ProgressPercent int              `json:"progress_percent"`
	Message         string           `json:"message,omitempty"`
	ErrorCode       string           `json:"error_code,omitempty"`
	ErrorMessage    string           `json:"error_message,omitempty"`
	Events          []ImportJobEvent `json:"events,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	ExpiresAt       time.Time        `json:"expires_at"`
	FileName        string           `json:"file_name"`
	FileSizeBytes   int64            `json:"file_size_bytes"`
	Backup          *Entry           `json:"backup,omitempty"`
}
