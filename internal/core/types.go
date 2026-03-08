package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// ImageData holds a base64-encoded image for multimodal messages.
type ImageData struct {
	MimeType string `json:"mime_type"`           // e.g. "image/png", "image/jpeg"
	Data     string `json:"data"`                // base64-encoded image bytes
	FileName string `json:"file_name,omitempty"` // original file name (display only)
}

type Message struct {
	Role         MessageRole     `json:"role"`
	Content      string          `json:"content"`
	Images       []ImageData     `json:"images,omitempty"`
	ToolName     string          `json:"tool_name,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
	ProviderMeta json.RawMessage `json:"provider_meta,omitempty"` // raw provider-specific data (e.g. Gemini model content with thought_signature)
	CreatedAt    time.Time       `json:"created_at"`
}

type Surface string

const (
	SurfaceTUI      Surface = "tui"
	SurfaceDiscord  Surface = "discord"
	SurfaceTelegram Surface = "telegram"
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

type ToolApprovalDecision struct {
	ApprovalID string `json:"approval_id"`
	Decision   string `json:"decision"` // allow | deny
}

type ChatStatus string

const (
	ChatStatusCompleted        ChatStatus = "completed"
	ChatStatusApprovalRequired ChatStatus = "approval_required"
)

type PendingToolApproval struct {
	ApprovalID       string `json:"approval_id"`
	ToolCallID       string `json:"tool_call_id,omitempty"`
	ToolName         string `json:"tool_name"`
	ArgumentsPreview string `json:"arguments_preview,omitempty"`
	RiskLevel        string `json:"risk_level,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

type ToolEvent struct {
	At            time.Time `json:"at"`
	ToolCallID    string    `json:"tool_call_id,omitempty"`
	ToolName      string    `json:"tool_name,omitempty"`
	Phase         string    `json:"phase,omitempty"` // requested | approved | denied | executed | failed
	Mutating      bool      `json:"mutating,omitempty"`
	Decision      string    `json:"decision,omitempty"`
	OutputPreview string    `json:"output_preview,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// FallbackEntry defines one fallback provider+model combination.
type FallbackEntry struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type ChatRequest struct {
	SessionID         string                 `json:"session_id"`
	DisableSession    bool                   `json:"disable_session,omitempty"`
	EphemeralMessages []Message              `json:"ephemeral_messages,omitempty"`
	Surface           Surface                `json:"surface"`
	Provider          string                 `json:"provider"`
	Model             string                 `json:"model"`
	Message           string                 `json:"message"`
	Images            []ImageData            `json:"images,omitempty"`
	EnableTools       bool                   `json:"enable_tools,omitempty"`
	RunID             string                 `json:"run_id,omitempty"`
	ToolApprovals     []ToolApprovalDecision `json:"tool_approvals,omitempty"`
}

type ChatResponse struct {
	SessionID        string                `json:"session_id"`
	Provider         string                `json:"provider"`
	Model            string                `json:"model"`
	Reply            string                `json:"reply"`
	Compressed       bool                  `json:"compressed"`
	Compression      CompressionMeta       `json:"compression"`
	AccountID        string                `json:"account_id,omitempty"`
	Usage            UsageInfo             `json:"usage"`
	Status           ChatStatus            `json:"status,omitempty"`
	RunID            string                `json:"run_id,omitempty"`
	PendingApprovals []PendingToolApproval `json:"pending_approvals,omitempty"`
	ToolEvents       []ToolEvent           `json:"tool_events,omitempty"`
}

// UsageInfo holds token usage from a single API call.
type UsageInfo struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Streaming types
// ---------------------------------------------------------------------------

// StreamChunkType discriminates the payload within a StreamChunk.
type StreamChunkType string

const (
	ChunkText        StreamChunkType = "text"
	ChunkToolStatus  StreamChunkType = "tool_status"
	ChunkRetryStatus StreamChunkType = "retry_status"
	ChunkError       StreamChunkType = "error"
	ChunkDone        StreamChunkType = "done"
)

// StreamChunk is a single event emitted by HandleChatStream.
type StreamChunk struct {
	Type        StreamChunkType `json:"type"`
	Content     string          `json:"content,omitempty"`      // text delta (ChunkText)
	ToolName    string          `json:"tool_name,omitempty"`    // active tool (ChunkToolStatus)
	ToolPhase   string          `json:"tool_phase,omitempty"`   // requested | executed | failed
	RetryStatus string          `json:"retry_status,omitempty"` // fallback message (ChunkRetryStatus)
	Response    *ChatResponse   `json:"response,omitempty"`     // final metadata (ChunkDone)
	Error       string          `json:"error,omitempty"`        // error message (ChunkError)
}

type CompressionMeta struct {
	OriginalTokens   int `json:"original_tokens"`
	CompressedTokens int `json:"compressed_tokens"`
	DroppedMessages  int `json:"dropped_messages"`
	SoftTrimmed      int `json:"soft_trimmed"`
	HardCleared      int `json:"hard_cleared"`
}

type SessionMetadata struct {
	SessionID       string    `json:"session_id"`
	Title           string    `json:"title,omitempty"`
	MessageCount    int       `json:"message_count"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	ContextTokens   int       `json:"context_tokens"`
	CompactionCount int       `json:"compaction_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Session lifecycle configuration
// ---------------------------------------------------------------------------

type SessionLifecycleConfig struct {
	IdleTimeout    time.Duration `json:"idle_timeout"`     // default 60min (TUI sessions)
	BotIdleTimeout time.Duration `json:"bot_idle_timeout"` // default 24h (Discord/Telegram sessions)
	RetentionDays  int           `json:"retention_days"`   // default 30
	MaxEntries     int           `json:"max_entries"`      // default 500
	MaxFileSize    int64         `json:"max_file_size"`    // default 10MB
}

func DefaultLifecycleConfig() SessionLifecycleConfig {
	return SessionLifecycleConfig{
		IdleTimeout:    60 * time.Minute,
		BotIdleTimeout: 24 * time.Hour,
		RetentionDays:  30,
		MaxEntries:     500,
		MaxFileSize:    10 * 1024 * 1024,
	}
}

// ---------------------------------------------------------------------------
// Typed JSONL entries (OpenClaw v3 format)
// ---------------------------------------------------------------------------

type EntryType string

const (
	EntrySession     EntryType = "session"
	EntryMessage     EntryType = "message"
	EntryCompaction  EntryType = "compaction"
	EntryModelChange EntryType = "model_change"
)

const sessionVersion = 3

// SessionEntry is the universal JSONL line format. Fields are populated
// based on the Type discriminator; unused fields are omitted via omitempty.
type SessionEntry struct {
	Type      EntryType `json:"type"`
	ID        string    `json:"id"`
	ParentID  string    `json:"parentId,omitempty"`
	Timestamp time.Time `json:"timestamp"`

	// type=session
	Version  int    `json:"version,omitempty"`
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`

	// type=message
	Role       MessageRole `json:"role,omitempty"`
	Content    string      `json:"content,omitempty"`
	Images     []ImageData `json:"images,omitempty"`
	ToolName   string      `json:"tool_name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`

	// type=message (assistant response metadata — populated for role=assistant)
	MsgProvider  string      `json:"msg_provider,omitempty"`
	MsgModel     string      `json:"msg_model,omitempty"`
	MsgUsage     *UsageInfo  `json:"msg_usage,omitempty"`
	MsgToolEvents []ToolEvent `json:"msg_tool_events,omitempty"`

	// type=compaction
	Summary          string `json:"summary,omitempty"`
	DroppedCount     int    `json:"dropped_count,omitempty"`
	DroppedTokens    int    `json:"dropped_tokens,omitempty"`
	FirstKeptEntryID string `json:"first_kept_entry_id,omitempty"`

	// type=model_change
	FromModel string `json:"from_model,omitempty"`
	ToModel   string `json:"to_model,omitempty"`
}

// NewEntryID generates a cryptographically random 8-character hex ID.
func NewEntryID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func NewSessionHeader(model, prov string) SessionEntry {
	return SessionEntry{
		Type:      EntrySession,
		ID:        NewEntryID(),
		Timestamp: time.Now(),
		Version:   sessionVersion,
		Model:     model,
		Provider:  prov,
	}
}

func NewMessageEntry(role MessageRole, content string) SessionEntry {
	return SessionEntry{
		Type:      EntryMessage,
		ID:        NewEntryID(),
		Timestamp: time.Now(),
		Role:      role,
		Content:   content,
	}
}

// NewImageMessageEntry creates a message entry with attached images.
func NewImageMessageEntry(role MessageRole, content string, images []ImageData) SessionEntry {
	e := NewMessageEntry(role, content)
	e.Images = images
	return e
}

func NewCompactionEntry(summary string, droppedCount, droppedTokens int, firstKeptID string) SessionEntry {
	return SessionEntry{
		Type:             EntryCompaction,
		ID:               NewEntryID(),
		Timestamp:        time.Now(),
		Summary:          summary,
		DroppedCount:     droppedCount,
		DroppedTokens:    droppedTokens,
		FirstKeptEntryID: firstKeptID,
	}
}

func NewModelChangeEntry(from, to string) SessionEntry {
	return SessionEntry{
		Type:      EntryModelChange,
		ID:        NewEntryID(),
		Timestamp: time.Now(),
		FromModel: from,
		ToModel:   to,
	}
}

// ToMessage converts a message-type entry back to a Message for the chat pipeline.
func (e SessionEntry) ToMessage() Message {
	return Message{
		Role:       e.Role,
		Content:    e.Content,
		Images:     e.Images,
		ToolName:   e.ToolName,
		ToolCallID: e.ToolCallID,
		CreatedAt:  e.Timestamp,
	}
}

// MessageToEntry converts a legacy Message into a typed SessionEntry.
func MessageToEntry(msg Message) SessionEntry {
	ts := msg.CreatedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	return SessionEntry{
		Type:       EntryMessage,
		ID:         NewEntryID(),
		Timestamp:  ts,
		Role:       msg.Role,
		Content:    msg.Content,
		Images:     msg.Images,
		ToolName:   msg.ToolName,
		ToolCallID: msg.ToolCallID,
	}
}

// AssistantResponseMeta holds per-message metadata for assistant responses.
type AssistantResponseMeta struct {
	Provider   string
	Model      string
	Usage      UsageInfo
	ToolEvents []ToolEvent
}

// NewAssistantEntryWithMeta creates an assistant message entry with response metadata.
func NewAssistantEntryWithMeta(content string, meta AssistantResponseMeta) SessionEntry {
	e := NewMessageEntry(RoleAssistant, content)
	e.MsgProvider = meta.Provider
	e.MsgModel = meta.Model
	e.MsgUsage = &meta.Usage
	if len(meta.ToolEvents) > 0 {
		e.MsgToolEvents = meta.ToolEvents
	}
	return e
}
