package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Text       string
	Endpoint   string
	Raw        json.RawMessage
	Usage      core.UsageInfo
	StopReason string
	ToolCalls  []ToolCall
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
	Reason   core.FailureReason
	Message  string
	Endpoint string
	Status   int
}

func (e *FailureError) Error() string {
	if e == nil {
		return ""
	}
	if e.Status > 0 {
		return fmt.Sprintf("%s (status=%d endpoint=%s)", e.Message, e.Status, e.Endpoint)
	}
	return fmt.Sprintf("%s (endpoint=%s)", e.Message, e.Endpoint)
}
