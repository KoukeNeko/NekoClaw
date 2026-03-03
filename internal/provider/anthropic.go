package provider

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

const (
	defaultAnthropicBaseURL       = "https://api.anthropic.com"
	defaultAnthropicContextWindow = 200_000
	defaultAnthropicMaxTokens     = 4096

	AnthropicSetupTokenPrefix    = "sk-ant-oat01-"
	AnthropicSetupTokenMinLength = 80
)

var anthropicDefaultBetas = []string{
	"fine-grained-tool-streaming-2025-05-14",
	"interleaved-thinking-2025-05-14",
}

var anthropicOAuthRequiredBetas = []string{
	"claude-code-20250219",
	"oauth-2025-04-20",
	"fine-grained-tool-streaming-2025-05-14",
	"interleaved-thinking-2025-05-14",
}

type AnthropicOptions struct {
	BaseURL       string
	ContextWindow int
	MaxTokens     int
	HTTPClient    *http.Client
}

type AnthropicProvider struct {
	baseURL       string
	contextWindow int
	maxTokens     int
	client        *http.Client
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string               `json:"role"`
	Content []anthropicTextBlock `json:"content"`
}

type anthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func NewAnthropicProvider(opts AnthropicOptions) *AnthropicProvider {
	baseURL := strings.TrimSpace(strings.TrimRight(opts.BaseURL, "/"))
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	contextWindow := opts.ContextWindow
	if contextWindow <= 0 {
		contextWindow = defaultAnthropicContextWindow
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &AnthropicProvider{
		baseURL:       baseURL,
		contextWindow: contextWindow,
		maxTokens:     maxTokens,
		client:        client,
	}
}

func (p *AnthropicProvider) ID() string {
	return "anthropic"
}

func (p *AnthropicProvider) ContextWindow(_ string) int {
	return p.contextWindow
}

func (p *AnthropicProvider) BaseURL() string {
	return p.baseURL
}

func (p *AnthropicProvider) DiscoverPreferredModel(_ context.Context, _ core.Account) (string, string, error) {
	return "claude-sonnet-4-6", "fallback", nil
}

func (p *AnthropicProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	secret := strings.TrimSpace(req.Account.Token)
	if secret == "" {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing anthropic credential",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}

	modelID := strings.TrimSpace(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = "claude-sonnet-4-6"
	}

	system, turns := splitAnthropicMessages(req.Messages)
	if len(turns) == 0 {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "anthropic request has no chat turns",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}

	payload := anthropicRequest{
		Model:     modelID,
		MaxTokens: p.maxTokens,
		System:    system,
		Messages:  turns,
	}
	raw, _ := json.Marshal(payload)

	targetURL := strings.TrimRight(p.baseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "nekoclaw/1.0")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	authType := resolveAnthropicCredentialType(req.Account, secret)
	if authType == core.AccountToken {
		httpReq.Header.Set("Authorization", "Bearer "+secret)
		httpReq.Header.Set("anthropic-beta", strings.Join(anthropicOAuthRequiredBetas, ","))
	} else {
		httpReq.Header.Set("x-api-key", secret)
		httpReq.Header.Set("anthropic-beta", strings.Join(anthropicDefaultBetas, ","))
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GenerateResponse{}, &FailureError{
			Reason:   classifyAnthropicStatus(resp.StatusCode, string(body)),
			Message:  summarizeAnthropicError(body),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}

	text, usage, ok := extractTextAndUsageFromAnthropic(body)
	if !ok {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "anthropic response did not include text: " + summarizeForError(body, 280),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}

	return GenerateResponse{
		Text:     text,
		Endpoint: p.baseURL,
		Raw:      body,
		Usage:    usage,
	}, nil
}

func ValidateAnthropicSetupToken(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("setup token is required")
	}
	if !strings.HasPrefix(trimmed, AnthropicSetupTokenPrefix) {
		return fmt.Errorf("setup token must start with %s", AnthropicSetupTokenPrefix)
	}
	if len(trimmed) < AnthropicSetupTokenMinLength {
		return fmt.Errorf("setup token looks too short")
	}
	return nil
}

func IsAnthropicSetupToken(raw string) bool {
	return ValidateAnthropicSetupToken(raw) == nil
}

func resolveAnthropicCredentialType(account core.Account, secret string) core.AccountType {
	if account.Type == core.AccountToken || account.Type == core.AccountAPIKey {
		return account.Type
	}
	if strings.HasPrefix(strings.TrimSpace(secret), AnthropicSetupTokenPrefix) {
		return core.AccountToken
	}
	return core.AccountAPIKey
}

func splitAnthropicMessages(messages []core.Message) (string, []anthropicMessage) {
	systemParts := make([]string, 0, 4)
	turns := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		switch msg.Role {
		case core.RoleSystem:
			systemParts = append(systemParts, text)
		case core.RoleAssistant:
			turns = append(turns, anthropicMessage{
				Role: "assistant",
				Content: []anthropicTextBlock{{
					Type: "text",
					Text: text,
				}},
			})
		default:
			turns = append(turns, anthropicMessage{
				Role: "user",
				Content: []anthropicTextBlock{{
					Type: "text",
					Text: text,
				}},
			})
		}
	}
	return strings.Join(systemParts, "\n\n"), turns
}

func extractTextAndUsageFromAnthropic(body []byte) (string, core.UsageInfo, bool) {
	var payload anthropicResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", core.UsageInfo{}, false
	}
	parts := make([]string, 0, len(payload.Content))
	for _, block := range payload.Content {
		if !strings.EqualFold(strings.TrimSpace(block.Type), "text") {
			continue
		}
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "", core.UsageInfo{}, false
	}
	usage := core.UsageInfo{
		InputTokens:  payload.Usage.InputTokens,
		OutputTokens: payload.Usage.OutputTokens,
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	return strings.Join(parts, "\n"), usage, true
}

func summarizeAnthropicError(body []byte) string {
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" {
			return msg
		}
		if typ := strings.TrimSpace(payload.Error.Type); typ != "" {
			return typ
		}
	}
	return summarizeForError(body, 280)
}

func classifyAnthropicStatus(status int, body string) core.FailureReason {
	lower := strings.ToLower(strings.TrimSpace(body))
	switch status {
	case http.StatusUnauthorized:
		return core.FailureAuthPermanent
	case http.StatusForbidden:
		if strings.Contains(lower, "billing") ||
			strings.Contains(lower, "quota") ||
			strings.Contains(lower, "credit") ||
			strings.Contains(lower, "payment") {
			return core.FailureBilling
		}
		return core.FailureAuthPermanent
	case http.StatusTooManyRequests:
		return core.FailureRateLimit
	case http.StatusBadRequest:
		return core.FailureFormat
	case http.StatusNotFound:
		return core.FailureModelNotFound
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return core.FailureTimeout
	default:
		if status >= 500 {
			return core.FailureUnknown
		}
		if status >= 400 {
			return core.FailureFormat
		}
	}
	return core.FailureUnknown
}
