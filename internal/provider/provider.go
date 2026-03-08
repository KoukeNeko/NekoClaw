package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

var ErrProjectDiscoveryFailed = errors.New("project discovery failed")

// GenerationParams holds optional sampling parameters from persona configs.
// Pointer fields distinguish "not set" (nil) from explicit zero values.
type GenerationParams struct {
	Temperature      *float64
	TopP             *float64
	FrequencyPenalty *float64
	PresencePenalty  *float64
}

type GenerateRequest struct {
	Model      string
	Messages   []core.Message
	Account    core.Account
	Generation *GenerationParams // optional persona-driven sampling overrides
}

type GenerateResponse struct {
	Text     string
	Endpoint string
	Raw      json.RawMessage
	Usage    core.UsageInfo
}

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ToolTurnRequest struct {
	Model      string
	Messages   []core.Message
	Account    core.Account
	Tools      []ToolDefinition
	Generation *GenerationParams // optional persona-driven sampling overrides
}

type ToolTurnResponse struct {
	Text            string
	Endpoint        string
	Raw             json.RawMessage
	Usage           core.UsageInfo
	StopReason      string
	ToolCalls       []ToolCall
	RawModelContent json.RawMessage // raw model content block (e.g. Gemini candidate content with thought_signature)
}

type ToolCapabilities struct {
	SupportsTools         bool
	SupportsParallelCalls bool
	MaxToolCalls          int
}

type Provider interface {
	ID() string
	ContextWindow(model string) int
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}

// ToolCallingProvider optionally supports native provider-side tool calling.
// Providers that do not support tools should omit this interface.
type ToolCallingProvider interface {
	Provider
	ToolCapabilities() ToolCapabilities
	GenerateToolTurn(ctx context.Context, req ToolTurnRequest) (ToolTurnResponse, error)
}

// GenerateStreamChunk is a single piece of a streaming LLM response.
type GenerateStreamChunk struct {
	Text     string         // incremental text delta
	Done     bool           // true on the final chunk
	Endpoint string         // populated only when Done=true
	Usage    core.UsageInfo // populated only when Done=true
	Error    error          // non-nil signals a streaming error
}

// StreamingProvider optionally supports token-level streaming generation.
// Providers that do not support streaming should omit this interface;
// the service layer falls back to Generate() for them.
type StreamingProvider interface {
	Provider
	GenerateStream(ctx context.Context, req GenerateRequest) (<-chan GenerateStreamChunk, error)
}

// ModelDiscoveryProvider optionally resolves a provider-specific default model
// at runtime for a specific account/profile.
type ModelDiscoveryProvider interface {
	Provider
	DiscoverPreferredModel(ctx context.Context, account core.Account) (modelID string, source string, err error)
}

// ModelCatalogProvider optionally exposes a model list for a provider/account.
type ModelCatalogProvider interface {
	Provider
	ListModels(ctx context.Context, account core.Account) ([]string, error)
}

type OAuthStartRequest struct {
	State       string
	Challenge   string
	RedirectURI string
}

type OAuthStartResponse struct {
	AuthURL string
}

type OAuthCompleteRequest struct {
	Code               string
	Verifier           string
	RedirectURI        string
	ProjectID          string
	EndpointPreference string
}

type OAuthCredential struct {
	AccessToken    string
	RefreshToken   string
	ExpiresAt      time.Time
	Email          string
	ProjectID      string
	ActiveEndpoint string
}

type AuthProvider interface {
	Provider
	StartOAuth(ctx context.Context, req OAuthStartRequest) (OAuthStartResponse, error)
	CompleteOAuth(ctx context.Context, req OAuthCompleteRequest) (OAuthCredential, error)
	RefreshOAuthIfNeeded(ctx context.Context, credential OAuthCredential) (OAuthCredential, bool, error)
}

type FailureError struct {
	Reason     core.FailureReason
	Message    string
	Endpoint   string
	Status     int
	RetryAfter time.Duration // Parsed from Retry-After header, zero if not present.
}

func (e *FailureError) Error() string {
	if e == nil {
		return ""
	}
	msg := summarizeErrorMessage(e.Message)
	if e.Status > 0 {
		return fmt.Sprintf("%s (status=%d endpoint=%s)", msg, e.Status, e.Endpoint)
	}
	return fmt.Sprintf("%s (endpoint=%s)", msg, e.Endpoint)
}

// summarizeErrorMessage extracts a human-readable message from a raw API
// response body. If the body is JSON with an error.message field, that short
// message is returned instead of the full JSON blob.
func summarizeErrorMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	// Try to extract "error.message" from JSON API responses.
	if strings.HasPrefix(raw, "{") {
		var parsed struct {
			Error struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(raw), &parsed) == nil && parsed.Error.Message != "" {
			return parsed.Error.Message
		}
	}
	// Truncate overly long non-JSON messages.
	const maxLen = 300
	if len(raw) > maxLen {
		return raw[:maxLen] + "…"
	}
	return raw
}
