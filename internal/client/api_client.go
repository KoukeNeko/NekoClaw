package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

type GeminiAuthStartRequest struct {
	ProfileID   string `json:"profile_id,omitempty"`
	Mode        string `json:"mode,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

type GeminiAuthStartResponse struct {
	AuthURL     string    `json:"auth_url"`
	State       string    `json:"state"`
	RedirectURI string    `json:"redirect_uri"`
	ExpiresAt   time.Time `json:"expires_at"`
	Mode        string    `json:"mode"`
	OAuthMode   string    `json:"oauth_mode,omitempty"`
}

type GeminiAuthManualCompleteRequest struct {
	State             string `json:"state"`
	CallbackURLOrCode string `json:"callback_url_or_code"`
}

type GeminiAuthCompleteResponse struct {
	ProfileID      string `json:"profile_id"`
	Provider       string `json:"provider"`
	Email          string `json:"email,omitempty"`
	ProjectID      string `json:"project_id"`
	ActiveEndpoint string `json:"active_endpoint,omitempty"`
}

type GeminiAuthProfile struct {
	ProfileID         string    `json:"profile_id"`
	Provider          string    `json:"provider"`
	Type              string    `json:"type"`
	Email             string    `json:"email,omitempty"`
	ProjectID         string    `json:"project_id,omitempty"`
	ProjectReady      bool      `json:"project_ready"`
	UnavailableReason string    `json:"unavailable_reason,omitempty"`
	Endpoint          string    `json:"endpoint,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Available         bool      `json:"available"`
	CooldownUntil     time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil     time.Time `json:"disabled_until,omitempty"`
	DisabledReason    string    `json:"disabled_reason,omitempty"`
	Preferred         bool      `json:"preferred"`
}

type AIStudioAddKeyRequest struct {
	APIKey       string `json:"api_key"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type AIStudioAddKeyResponse struct {
	ProfileID   string `json:"profile_id"`
	Provider    string `json:"provider"`
	DisplayName string `json:"display_name"`
	KeyHint     string `json:"key_hint"`
	Preferred   bool   `json:"preferred"`
	Available   bool   `json:"available"`
}

type AIStudioProfile struct {
	ProfileID      string    `json:"profile_id"`
	Provider       string    `json:"provider"`
	Type           string    `json:"type"`
	DisplayName    string    `json:"display_name,omitempty"`
	KeyHint        string    `json:"key_hint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Available      bool      `json:"available"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil  time.Time `json:"disabled_until,omitempty"`
	DisabledReason string    `json:"disabled_reason,omitempty"`
	Preferred      bool      `json:"preferred"`
}

type SessionInfo struct {
	SessionID    string    `json:"session_id"`
	Title        string    `json:"title,omitempty"`
	MessageCount int       `json:"message_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AIStudioModelsResponse struct {
	Provider    string    `json:"provider"`
	ProfileID   string    `json:"profile_id"`
	Models      []string  `json:"models"`
	Source      string    `json:"source"`
	CachedUntil time.Time `json:"cached_until,omitempty"`
}

type AnthropicAddTokenRequest struct {
	SetupToken   string `json:"setup_token"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type AnthropicAddAPIKeyRequest struct {
	APIKey       string `json:"api_key"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type AnthropicAddCredentialResponse struct {
	ProfileID   string `json:"profile_id"`
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	KeyHint     string `json:"key_hint"`
	Preferred   bool   `json:"preferred"`
	Available   bool   `json:"available"`
}

type AnthropicProfile struct {
	ProfileID      string    `json:"profile_id"`
	Provider       string    `json:"provider"`
	Type           string    `json:"type"`
	DisplayName    string    `json:"display_name,omitempty"`
	KeyHint        string    `json:"key_hint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Available      bool      `json:"available"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil  time.Time `json:"disabled_until,omitempty"`
	DisabledReason string    `json:"disabled_reason,omitempty"`
	Preferred      bool      `json:"preferred"`
}

type AnthropicBrowserStartRequest struct {
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
	Mode         string `json:"mode,omitempty"`
}

type AnthropicBrowserStartResponse struct {
	JobID      string    `json:"job_id"`
	Provider   string    `json:"provider"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	Message    string    `json:"message,omitempty"`
	ManualHint string    `json:"manual_hint,omitempty"`
}

type AnthropicBrowserJobEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type AnthropicBrowserJobResponse struct {
	JobID        string                     `json:"job_id"`
	Provider     string                     `json:"provider"`
	Mode         string                     `json:"mode"`
	Status       string                     `json:"status"`
	Events       []AnthropicBrowserJobEvent `json:"events,omitempty"`
	ProfileID    string                     `json:"profile_id,omitempty"`
	KeyHint      string                     `json:"key_hint,omitempty"`
	ExpiresAt    time.Time                  `json:"expires_at"`
	Message      string                     `json:"message,omitempty"`
	ManualHint   string                     `json:"manual_hint,omitempty"`
	ErrorCode    string                     `json:"error_code,omitempty"`
	ErrorMessage string                     `json:"error_message,omitempty"`
}

type AnthropicBrowserManualCompleteRequest struct {
	JobID        string `json:"job_id"`
	SetupToken   string `json:"setup_token"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type OpenAIAddKeyRequest struct {
	APIKey       string `json:"api_key"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type OpenAICodexAddTokenRequest struct {
	Token        string `json:"token"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type OpenAICodexBrowserStartRequest struct {
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
	Mode         string `json:"mode,omitempty"`
}

type OpenAICodexBrowserStartResponse struct {
	JobID      string    `json:"job_id"`
	Provider   string    `json:"provider"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	Message    string    `json:"message,omitempty"`
	ManualHint string    `json:"manual_hint,omitempty"`
}

type OpenAICodexBrowserJobEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type OpenAICodexBrowserJobResponse struct {
	JobID        string                       `json:"job_id"`
	Provider     string                       `json:"provider"`
	Mode         string                       `json:"mode"`
	Status       string                       `json:"status"`
	Events       []OpenAICodexBrowserJobEvent `json:"events,omitempty"`
	ProfileID    string                       `json:"profile_id,omitempty"`
	KeyHint      string                       `json:"key_hint,omitempty"`
	ExpiresAt    time.Time                    `json:"expires_at"`
	Message      string                       `json:"message,omitempty"`
	ManualHint   string                       `json:"manual_hint,omitempty"`
	ErrorCode    string                       `json:"error_code,omitempty"`
	ErrorMessage string                       `json:"error_message,omitempty"`
}

type OpenAICodexBrowserManualCompleteRequest struct {
	JobID        string `json:"job_id"`
	Token        string `json:"token"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type OpenAIAddCredentialResponse struct {
	ProfileID   string `json:"profile_id"`
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	KeyHint     string `json:"key_hint"`
	Preferred   bool   `json:"preferred"`
	Available   bool   `json:"available"`
}

type OpenAIProfile struct {
	ProfileID      string    `json:"profile_id"`
	Provider       string    `json:"provider"`
	Type           string    `json:"type"`
	DisplayName    string    `json:"display_name,omitempty"`
	KeyHint        string    `json:"key_hint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Available      bool      `json:"available"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil  time.Time `json:"disabled_until,omitempty"`
	DisabledReason string    `json:"disabled_reason,omitempty"`
	Preferred      bool      `json:"preferred"`
}

// APIError preserves the structured error information returned by the NekoClaw API server.
// Callers can use errors.As to inspect status code, error code, and message separately.
type APIError struct {
	StatusCode int
	Code       string // e.g., "missing_project", "provider_not_ready"; empty for plain string errors
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("api error (%d): %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("api error (%d): %s", e.StatusCode, e.Message)
}

type APIClient struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *APIClient {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		trimmed = "http://127.0.0.1:8085"
	}
	return &APIClient{
		baseURL: trimmed,
		http: &http.Client{
			// No Timeout here — each call site controls timeout via context.
			// ResponseHeaderTimeout acts as a safety net for connection-level hangs
			// but never fires before the caller's context timeout.
			Transport: &http.Transport{
				ResponseHeaderTimeout: 120 * time.Second,
			},
		},
	}
}

func (c *APIClient) Chat(ctx context.Context, req core.ChatRequest) (core.ChatResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return core.ChatResponse{}, fmt.Errorf("marshal chat request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat", bytes.NewReader(payload))
	if err != nil {
		return core.ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	var out core.ChatResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return core.ChatResponse{}, err
	}
	return out, nil
}

func (c *APIClient) Providers(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/providers", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Providers []string `json:"providers"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

func (c *APIClient) StartGeminiOAuth(ctx context.Context, req GeminiAuthStartRequest) (GeminiAuthStartResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return GeminiAuthStartResponse{}, fmt.Errorf("marshal auth start request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/gemini/start",
		bytes.NewReader(payload),
	)
	if err != nil {
		return GeminiAuthStartResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	var out GeminiAuthStartResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return GeminiAuthStartResponse{}, err
	}
	return out, nil
}

func (c *APIClient) CompleteGeminiOAuthManual(
	ctx context.Context,
	req GeminiAuthManualCompleteRequest,
) (GeminiAuthCompleteResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return GeminiAuthCompleteResponse{}, fmt.Errorf("marshal auth complete request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/gemini/manual/complete",
		bytes.NewReader(payload),
	)
	if err != nil {
		return GeminiAuthCompleteResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	var out GeminiAuthCompleteResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return GeminiAuthCompleteResponse{}, err
	}
	return out, nil
}

func (c *APIClient) ListGeminiProfiles(ctx context.Context) ([]GeminiAuthProfile, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/auth/gemini/profiles", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Profiles []GeminiAuthProfile `json:"profiles"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Profiles, nil
}

func (c *APIClient) UseGeminiProfile(ctx context.Context, profileID string) error {
	payload, err := json.Marshal(map[string]string{"profile_id": strings.TrimSpace(profileID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/gemini/use",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) AddAIStudioKey(ctx context.Context, req AIStudioAddKeyRequest) (AIStudioAddKeyResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return AIStudioAddKeyResponse{}, fmt.Errorf("marshal ai studio add key request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/ai-studio/add-key",
		bytes.NewReader(payload),
	)
	if err != nil {
		return AIStudioAddKeyResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out AIStudioAddKeyResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return AIStudioAddKeyResponse{}, err
	}
	return out, nil
}

func (c *APIClient) ListAIStudioProfiles(ctx context.Context) ([]AIStudioProfile, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/auth/ai-studio/profiles", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Profiles []AIStudioProfile `json:"profiles"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Profiles, nil
}

func (c *APIClient) UseAIStudioProfile(ctx context.Context, profileID string) error {
	payload, err := json.Marshal(map[string]string{"profile_id": strings.TrimSpace(profileID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/ai-studio/use",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) DeleteAIStudioProfile(ctx context.Context, profileID string) error {
	payload, err := json.Marshal(map[string]string{"profile_id": strings.TrimSpace(profileID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/ai-studio/delete",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) AddAnthropicToken(ctx context.Context, req AnthropicAddTokenRequest) (AnthropicAddCredentialResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return AnthropicAddCredentialResponse{}, fmt.Errorf("marshal anthropic add token request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/anthropic/add-token",
		bytes.NewReader(payload),
	)
	if err != nil {
		return AnthropicAddCredentialResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out AnthropicAddCredentialResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return AnthropicAddCredentialResponse{}, err
	}
	return out, nil
}

func (c *APIClient) AddAnthropicAPIKey(ctx context.Context, req AnthropicAddAPIKeyRequest) (AnthropicAddCredentialResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return AnthropicAddCredentialResponse{}, fmt.Errorf("marshal anthropic add api key request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/anthropic/add-api-key",
		bytes.NewReader(payload),
	)
	if err != nil {
		return AnthropicAddCredentialResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out AnthropicAddCredentialResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return AnthropicAddCredentialResponse{}, err
	}
	return out, nil
}

func (c *APIClient) ListAnthropicProfiles(ctx context.Context) ([]AnthropicProfile, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/auth/anthropic/profiles", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Profiles []AnthropicProfile `json:"profiles"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Profiles, nil
}

func (c *APIClient) UseAnthropicProfile(ctx context.Context, profileID string) error {
	payload, err := json.Marshal(map[string]string{"profile_id": strings.TrimSpace(profileID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/anthropic/use",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) DeleteAnthropicProfile(ctx context.Context, profileID string) error {
	payload, err := json.Marshal(map[string]string{"profile_id": strings.TrimSpace(profileID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/anthropic/delete",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) StartAnthropicBrowserLogin(
	ctx context.Context,
	req AnthropicBrowserStartRequest,
) (AnthropicBrowserStartResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return AnthropicBrowserStartResponse{}, fmt.Errorf("marshal anthropic browser start request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/anthropic/browser/start",
		bytes.NewReader(payload),
	)
	if err != nil {
		return AnthropicBrowserStartResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out AnthropicBrowserStartResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return AnthropicBrowserStartResponse{}, err
	}
	return out, nil
}

func (c *APIClient) GetAnthropicBrowserLoginJob(
	ctx context.Context,
	jobID string,
) (AnthropicBrowserJobResponse, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return AnthropicBrowserJobResponse{}, fmt.Errorf("job_id is required")
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/v1/auth/anthropic/browser/jobs/"+url.PathEscape(jobID),
		nil,
	)
	if err != nil {
		return AnthropicBrowserJobResponse{}, err
	}
	var out AnthropicBrowserJobResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return AnthropicBrowserJobResponse{}, err
	}
	return out, nil
}

func (c *APIClient) CompleteAnthropicBrowserManual(
	ctx context.Context,
	req AnthropicBrowserManualCompleteRequest,
) (AnthropicAddCredentialResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return AnthropicAddCredentialResponse{}, fmt.Errorf("marshal anthropic browser manual request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/anthropic/browser/manual/complete",
		bytes.NewReader(payload),
	)
	if err != nil {
		return AnthropicAddCredentialResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out AnthropicAddCredentialResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return AnthropicAddCredentialResponse{}, err
	}
	return out, nil
}

func (c *APIClient) CancelAnthropicBrowserLogin(ctx context.Context, jobID string) error {
	payload, err := json.Marshal(map[string]string{"job_id": strings.TrimSpace(jobID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/anthropic/browser/cancel",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) AddOpenAIKey(ctx context.Context, req OpenAIAddKeyRequest) (OpenAIAddCredentialResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return OpenAIAddCredentialResponse{}, fmt.Errorf("marshal openai add key request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/openai/add-key",
		bytes.NewReader(payload),
	)
	if err != nil {
		return OpenAIAddCredentialResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out OpenAIAddCredentialResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return OpenAIAddCredentialResponse{}, err
	}
	return out, nil
}

func (c *APIClient) AddOpenAICodexToken(ctx context.Context, req OpenAICodexAddTokenRequest) (OpenAIAddCredentialResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return OpenAIAddCredentialResponse{}, fmt.Errorf("marshal openai-codex add token request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/openai-codex/add-token",
		bytes.NewReader(payload),
	)
	if err != nil {
		return OpenAIAddCredentialResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out OpenAIAddCredentialResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return OpenAIAddCredentialResponse{}, err
	}
	return out, nil
}

func (c *APIClient) StartOpenAICodexBrowserLogin(
	ctx context.Context,
	req OpenAICodexBrowserStartRequest,
) (OpenAICodexBrowserStartResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return OpenAICodexBrowserStartResponse{}, fmt.Errorf("marshal openai-codex browser start request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/openai-codex/browser/start",
		bytes.NewReader(payload),
	)
	if err != nil {
		return OpenAICodexBrowserStartResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out OpenAICodexBrowserStartResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return OpenAICodexBrowserStartResponse{}, err
	}
	return out, nil
}

func (c *APIClient) GetOpenAICodexBrowserLoginJob(
	ctx context.Context,
	jobID string,
) (OpenAICodexBrowserJobResponse, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return OpenAICodexBrowserJobResponse{}, fmt.Errorf("job_id is required")
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/v1/auth/openai-codex/browser/jobs/"+url.PathEscape(jobID),
		nil,
	)
	if err != nil {
		return OpenAICodexBrowserJobResponse{}, err
	}
	var out OpenAICodexBrowserJobResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return OpenAICodexBrowserJobResponse{}, err
	}
	return out, nil
}

func (c *APIClient) CompleteOpenAICodexBrowserManual(
	ctx context.Context,
	req OpenAICodexBrowserManualCompleteRequest,
) (OpenAIAddCredentialResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return OpenAIAddCredentialResponse{}, fmt.Errorf("marshal openai-codex browser manual request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/openai-codex/browser/manual/complete",
		bytes.NewReader(payload),
	)
	if err != nil {
		return OpenAIAddCredentialResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out OpenAIAddCredentialResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return OpenAIAddCredentialResponse{}, err
	}
	return out, nil
}

func (c *APIClient) CancelOpenAICodexBrowserLogin(ctx context.Context, jobID string) error {
	payload, err := json.Marshal(map[string]string{"job_id": strings.TrimSpace(jobID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/auth/openai-codex/browser/cancel",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) ListOpenAIProfiles(ctx context.Context, providerID string) ([]OpenAIProfile, error) {
	providerID = strings.TrimSpace(providerID)
	endpoint := "/v1/auth/openai/profiles"
	if providerID == "openai-codex" {
		endpoint = "/v1/auth/openai-codex/profiles"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Profiles []OpenAIProfile `json:"profiles"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Profiles, nil
}

func (c *APIClient) UseOpenAIProfile(ctx context.Context, providerID, profileID string) error {
	providerID = strings.TrimSpace(providerID)
	endpoint := "/v1/auth/openai/use"
	if providerID == "openai-codex" {
		endpoint = "/v1/auth/openai-codex/use"
	}
	payload, err := json.Marshal(map[string]string{"profile_id": strings.TrimSpace(profileID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) DeleteOpenAIProfile(ctx context.Context, providerID, profileID string) error {
	providerID = strings.TrimSpace(providerID)
	endpoint := "/v1/auth/openai/delete"
	if providerID == "openai-codex" {
		endpoint = "/v1/auth/openai-codex/delete"
	}
	payload, err := json.Marshal(map[string]string{"profile_id": strings.TrimSpace(profileID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) ListAIStudioModels(ctx context.Context, profileID string) (AIStudioModelsResponse, error) {
	url := c.baseURL + "/v1/ai-studio/models"
	if profileID = strings.TrimSpace(profileID); profileID != "" {
		url += "?profile_id=" + urlQueryEscape(profileID)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return AIStudioModelsResponse{}, err
	}
	var out AIStudioModelsResponse
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return AIStudioModelsResponse{}, err
	}
	return out, nil
}

func (c *APIClient) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/sessions", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

func (c *APIClient) DeleteSession(ctx context.Context, sessionID string) error {
	payload, err := json.Marshal(map[string]string{"session_id": strings.TrimSpace(sessionID)})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/sessions/delete",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

func (c *APIClient) RenameSession(ctx context.Context, sessionID, title string) error {
	payload, err := json.Marshal(map[string]string{
		"session_id": strings.TrimSpace(sessionID),
		"title":      strings.TrimSpace(title),
	})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/sessions/rename",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.doAndDecodeJSON(httpReq, &out)
}

// TranscriptMessage represents a single message returned by the transcript API.
type TranscriptMessage struct {
	Role       string   `json:"role"`
	Content    string   `json:"content"`
	ImageNames []string `json:"image_names,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

// GetSessionTranscript fetches the user/assistant messages for a session.
func (c *APIClient) GetSessionTranscript(ctx context.Context, sessionID string) ([]TranscriptMessage, error) {
	url := c.baseURL + "/v1/sessions/transcript?session_id=" + urlQueryEscape(strings.TrimSpace(sessionID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Messages []TranscriptMessage `json:"messages"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

type MemorySearchResult struct {
	SessionID string    `json:"session_id"`
	EntryID   string    `json:"entry_id"`
	Content   string    `json:"content"`
	Role      string    `json:"role"`
	Score     float64   `json:"score"`
	Timestamp time.Time `json:"timestamp"`
}

func (c *APIClient) SearchMemory(ctx context.Context, query string, limit int) ([]MemorySearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	payload, err := json.Marshal(map[string]any{"query": strings.TrimSpace(query), "limit": limit})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/memory/search",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	var out struct {
		Results []MemorySearchResult `json:"results"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

func (c *APIClient) doAndDecodeJSON(httpReq *http.Request, out any) error {
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp.StatusCode, body)
	}
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func decodeAPIError(status int, body []byte) error {
	type errPayload struct {
		Error any `json:"error"`
	}
	type errObject struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	var payload errPayload
	if err := json.Unmarshal(body, &payload); err == nil {
		switch value := payload.Error.(type) {
		case string:
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return &APIError{StatusCode: status, Message: trimmed}
			}
		case map[string]any:
			raw, _ := json.Marshal(value)
			var obj errObject
			if err := json.Unmarshal(raw, &obj); err == nil {
				msg := strings.TrimSpace(obj.Message)
				code := strings.TrimSpace(obj.Code)
				if msg != "" || code != "" {
					if msg == "" {
						msg = code
					}
					return &APIError{StatusCode: status, Code: code, Message: msg}
				}
			}
		}
	}
	return &APIError{StatusCode: status, Message: strings.TrimSpace(string(body))}
}

func urlQueryEscape(raw string) string {
	return url.QueryEscape(strings.TrimSpace(raw))
}

// ---------------------------------------------------------------------------
// MCP
// ---------------------------------------------------------------------------

// MCPServerInfo represents an MCP server's status.
type MCPServerInfo struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Trust     string `json:"trust"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	ToolCount int    `json:"tool_count"`
}

// MCPToolInfo represents an MCP tool.
type MCPToolInfo struct {
	Server      string `json:"server"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListMCPServers returns the list of configured MCP servers and their status.
func (c *APIClient) ListMCPServers(ctx context.Context) ([]MCPServerInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/mcp/servers", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Servers []MCPServerInfo `json:"servers"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Servers, nil
}

// ListMCPTools returns all tools from connected MCP servers.
func (c *APIClient) ListMCPTools(ctx context.Context) ([]MCPToolInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/mcp/tools", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []MCPToolInfo `json:"tools"`
	}
	if err := c.doAndDecodeJSON(httpReq, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}
