package backup

import "time"

const (
	ManifestVersionLegacy    = 1
	ManifestVersionEncrypted = 2

	SourceCreated  = "created"
	SourceImported = "imported"

	RestoreModeReplace = "replace"

	EncryptionNone      = "none"
	EncryptionZipAES256 = "zip-aes-256"
)

const (
	ComponentConfig   = "config"
	ComponentAuth     = "auth"
	ComponentSecurity = "security"
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
