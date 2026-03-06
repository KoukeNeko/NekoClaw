package tooling

import (
	"context"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type Backend interface {
	ListSessions() []core.SessionMetadata
	SearchMemory(query string, limit int) ([]MemoryResult, error)
	ReadMemoryFile(relPath string, from, lines int) (string, error)
	SaveMemory(content string) error
	Providers() []string
	Accounts(providerID string) []core.AccountSnapshot
}

type MemoryResult struct {
	SessionID string  `json:"session_id"`
	Path      string  `json:"path,omitempty"`
	StartLine int     `json:"start_line,omitempty"`
	EndLine   int     `json:"end_line,omitempty"`
	Snippet   string  `json:"snippet"`
	Score     float64 `json:"score"`
	Role      string  `json:"role"`
	Source    string  `json:"source,omitempty"`
}

type ToolSpec struct {
	Definition provider.ToolDefinition
	Mutating   bool
}

type RunRequest struct {
	SessionID    string
	Surface      core.Surface
	ProviderID   string
	ModelID      string
	Account      core.Account
	ToolProvider provider.ToolCallingProvider
	Messages     []core.Message
	UserMessage  core.Message
	EnableTools  bool
	RunID        string
	Approvals    []core.ToolApprovalDecision
	Compressed   bool
	Compression  core.CompressionMeta
	Generation   *provider.GenerationParams // optional persona-driven sampling overrides

	// OnToolEvent is an optional callback invoked synchronously when tool
	// execution phases change (e.g. "requested", "executed", "failed").
	// Used by the service layer to track active tool status for real-time
	// display in the TUI spinner.
	OnToolEvent func(core.ToolEvent)
}

type RunResult struct {
	Response        core.ChatResponse
	SessionMessages []core.Message
	Pending         bool
}

type Executor interface {
	Run(ctx context.Context, call provider.ToolCall) (content string, err error)
	IsMutating(toolName string) bool
	IsCallMutating(call provider.ToolCall) bool
	Definitions() []provider.ToolDefinition
	HasTool(toolName string) bool
	ArgumentPreview(call provider.ToolCall) string
}
