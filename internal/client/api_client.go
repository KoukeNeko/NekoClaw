package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	ProfileID      string    `json:"profile_id"`
	Provider       string    `json:"provider"`
	Type           string    `json:"type"`
	Email          string    `json:"email,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	Endpoint       string    `json:"endpoint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Available      bool      `json:"available"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil  time.Time `json:"disabled_until,omitempty"`
	DisabledReason string    `json:"disabled_reason,omitempty"`
	Preferred      bool      `json:"preferred"`
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
		Error string `json:"error"`
	}
	var payload errPayload
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return fmt.Errorf("api error (%d): %s", status, strings.TrimSpace(payload.Error))
	}
	return fmt.Errorf("api error (%d): %s", status, strings.TrimSpace(string(body)))
}
