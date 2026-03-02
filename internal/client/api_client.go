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

type AIStudioModelsResponse struct {
	Provider    string    `json:"provider"`
	ProfileID   string    `json:"profile_id"`
	Models      []string  `json:"models"`
	Source      string    `json:"source"`
	CachedUntil time.Time `json:"cached_until,omitempty"`
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
			Timeout: 30 * time.Second,
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
			if strings.TrimSpace(value) != "" {
				return fmt.Errorf("api error (%d): %s", status, strings.TrimSpace(value))
			}
		case map[string]any:
			raw, _ := json.Marshal(value)
			var obj errObject
			if err := json.Unmarshal(raw, &obj); err == nil {
				msg := strings.TrimSpace(obj.Message)
				code := strings.TrimSpace(obj.Code)
				switch {
				case msg != "" && code != "":
					return fmt.Errorf("api error (%d): %s: %s", status, code, msg)
				case msg != "":
					return fmt.Errorf("api error (%d): %s", status, msg)
				case code != "":
					return fmt.Errorf("api error (%d): %s", status, code)
				}
			}
		}
	}
	return fmt.Errorf("api error (%d): %s", status, strings.TrimSpace(string(body)))
}

func urlQueryEscape(raw string) string {
	return url.QueryEscape(strings.TrimSpace(raw))
}
