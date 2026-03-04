package tooling

import (
	"context"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type Backend interface {
	ListSessions() []core.SessionMetadata
	SearchMemory(query string, limit int) ([]MemoryResult, error)
	Providers() []string
	Accounts(providerID string) []core.AccountSnapshot
}

type MemoryResult struct {
	SessionID string  `json:"session_id"`
	Snippet   string  `json:"snippet"`
	Score     float64 `json:"score"`
	Role      string  `json:"role"`
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
