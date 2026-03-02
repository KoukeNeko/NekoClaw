package core

import "time"

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

type Message struct {
	Role      MessageRole `json:"role"`
	Content   string      `json:"content"`
	ToolName  string      `json:"tool_name,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type Surface string

const (
	SurfaceTUI     Surface = "tui"
	SurfaceDiscord Surface = "discord"
)

type AccountType string

const (
	AccountOAuth  AccountType = "oauth"
	AccountToken  AccountType = "token"
	AccountAPIKey AccountType = "api_key"
)

type Account struct {
	ID       string      `json:"id"`
	Provider string      `json:"provider"`
	Type     AccountType `json:"type"`
	Token    string      `json:"token,omitempty"`
	Email    string      `json:"email,omitempty"`
	Metadata Metadata    `json:"metadata,omitempty"`
}

type Metadata map[string]string

type ChatRequest struct {
	SessionID string  `json:"session_id"`
	Surface   Surface `json:"surface"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	Message   string  `json:"message"`
}

type ChatResponse struct {
	SessionID   string          `json:"session_id"`
	Provider    string          `json:"provider"`
	Model       string          `json:"model"`
	Reply       string          `json:"reply"`
	Compressed  bool            `json:"compressed"`
	Compression CompressionMeta `json:"compression"`
	AccountID   string          `json:"account_id,omitempty"`
}

type CompressionMeta struct {
	OriginalTokens   int `json:"original_tokens"`
	CompressedTokens int `json:"compressed_tokens"`
	DroppedMessages  int `json:"dropped_messages"`
	SoftTrimmed      int `json:"soft_trimmed"`
	HardCleared      int `json:"hard_cleared"`
}
