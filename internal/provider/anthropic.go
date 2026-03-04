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
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type anthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicImageBlock struct {
	Type   string                   `json:"type"`
	Source anthropicImageBlockSource `json:"source"`
}

type anthropicImageBlockSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicToolRequest struct {
	Model     string                    `json:"model"`
	MaxTokens int                       `json:"max_tokens"`
	System    string                    `json:"system,omitempty"`
	Messages  []anthropicToolMessage    `json:"messages"`
	Tools     []anthropicToolDefinition `json:"tools,omitempty"`
}

type anthropicToolMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type anthropicToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicToolResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
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

func (p *AnthropicProvider) ToolCapabilities() ToolCapabilities {
	return ToolCapabilities{
		SupportsTools:         true,
		SupportsParallelCalls: true,
		MaxToolCalls:          16,
	}
}

func (p *AnthropicProvider) DiscoverPreferredModel(_ context.Context, _ core.Account) (string, string, error) {
	return "claude-sonnet-4-5", "fallback", nil
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
		modelID = "claude-sonnet-4-5"
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

	authType := resolveAnthropicCredentialType(req.Account, secret)
	setAnthropicHeaders(httpReq, secret, authType)

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
		message := summarizeAnthropicError(body)
		if authType == core.AccountToken && looksLikeAnthropicInvalidBearer(body, message) {
			message = "Invalid bearer token. Your Claude setup-token may have expired. Please run 'claude setup-token' again to get a new one."
		}
		return GenerateResponse{}, &FailureError{
			Reason:   classifyAnthropicStatus(resp.StatusCode, string(body)),
			Message:  message,
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

func (p *AnthropicProvider) GenerateToolTurn(ctx context.Context, req ToolTurnRequest) (ToolTurnResponse, error) {
	secret := strings.TrimSpace(req.Account.Token)
	if secret == "" {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing anthropic credential",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}
	modelID := strings.TrimSpace(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = "claude-sonnet-4-5"
	}
	system, turns := splitAnthropicToolMessages(req.Messages)
	if len(turns) == 0 {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "anthropic tool request has no chat turns",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}
	tools := make([]anthropicToolDefinition, 0, len(req.Tools))
	for _, tool := range req.Tools {
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
		}
		tools = append(tools, anthropicToolDefinition{
			Name:        strings.TrimSpace(tool.Name),
			Description: strings.TrimSpace(tool.Description),
			InputSchema: schema,
		})
	}
	payload := anthropicToolRequest{
		Model:     modelID,
		MaxTokens: p.maxTokens,
		System:    system,
		Messages:  turns,
		Tools:     tools,
	}
	raw, _ := json.Marshal(payload)
	targetURL := strings.TrimRight(p.baseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}
	authType := resolveAnthropicCredentialType(req.Account, secret)
	setAnthropicHeaders(httpReq, secret, authType)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := summarizeAnthropicError(body)
		if authType == core.AccountToken && looksLikeAnthropicInvalidBearer(body, message) {
			message = "Invalid bearer token. Your Claude setup-token may have expired. Please run 'claude setup-token' again to get a new one."
		}
		return ToolTurnResponse{}, &FailureError{
			Reason:   classifyAnthropicStatus(resp.StatusCode, string(body)),
			Message:  message,
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}
	var decoded anthropicToolResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "anthropic tool response decode failed: " + err.Error(),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}
	textParts := make([]string, 0, len(decoded.Content))
	calls := make([]ToolCall, 0, 4)
	for _, block := range decoded.Content {
		switch strings.TrimSpace(block.Type) {
		case "text":
			txt := strings.TrimSpace(block.Text)
			if txt != "" {
				textParts = append(textParts, txt)
			}
		case "tool_use":
			callID := strings.TrimSpace(block.ID)
			if callID == "" {
				callID = "toolcall-" + NewEntryIDForProvider()
			}
			calls = append(calls, ToolCall{
				ID:        callID,
				Name:      strings.TrimSpace(block.Name),
				Arguments: block.Input,
			})
		}
	}
	usage := core.UsageInfo{
		InputTokens:  decoded.Usage.InputTokens,
		OutputTokens: decoded.Usage.OutputTokens,
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	return ToolTurnResponse{
		Text:       strings.Join(textParts, "\n"),
		Endpoint:   p.baseURL,
		Raw:        body,
		Usage:      usage,
		StopReason: strings.TrimSpace(decoded.StopReason),
		ToolCalls:  calls,
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

func setAnthropicHeaders(httpReq *http.Request, secret string, authType core.AccountType) {
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "nekoclaw/1.0")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if authType == core.AccountToken {
		httpReq.Header.Set("Authorization", "Bearer "+secret)
		httpReq.Header.Set("anthropic-beta", strings.Join(anthropicOAuthRequiredBetas, ","))
		return
	}
	httpReq.Header.Set("x-api-key", secret)
	httpReq.Header.Set("anthropic-beta", strings.Join(anthropicDefaultBetas, ","))
}

func splitAnthropicMessages(messages []core.Message) (string, []anthropicMessage) {
	systemParts := make([]string, 0, 4)
	turns := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		if text == "" && len(msg.Images) == 0 {
			continue
		}
		switch msg.Role {
		case core.RoleSystem:
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case core.RoleAssistant:
			if text != "" {
				turns = append(turns, anthropicMessage{
					Role:    "assistant",
					Content: []any{anthropicTextBlock{Type: "text", Text: text}},
				})
			}
		default:
			content := buildAnthropicUserContent(text, msg.Images)
			turns = append(turns, anthropicMessage{
				Role:    "user",
				Content: content,
			})
		}
	}
	return strings.Join(systemParts, "\n\n"), turns
}

// buildAnthropicUserContent creates a multimodal content array with images + text.
func buildAnthropicUserContent(text string, images []core.ImageData) []any {
	content := make([]any, 0, len(images)+1)
	for _, img := range images {
		content = append(content, anthropicImageBlock{
			Type: "image",
			Source: anthropicImageBlockSource{
				Type:      "base64",
				MediaType: img.MimeType,
				Data:      img.Data,
			},
		})
	}
	if text != "" {
		content = append(content, anthropicTextBlock{Type: "text", Text: text})
	}
	return content
}

func splitAnthropicToolMessages(messages []core.Message) (string, []anthropicToolMessage) {
	systemParts := make([]string, 0, 4)
	turns := make([]anthropicToolMessage, 0, len(messages))
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		switch msg.Role {
		case core.RoleSystem:
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case core.RoleAssistant:
			if strings.TrimSpace(msg.ToolCallID) != "" && strings.TrimSpace(msg.ToolName) != "" {
				input := rawObjectOrString(text)
				turns = append(turns, anthropicToolMessage{
					Role: "assistant",
					Content: []any{map[string]any{
						"type":  "tool_use",
						"id":    strings.TrimSpace(msg.ToolCallID),
						"name":  strings.TrimSpace(msg.ToolName),
						"input": input,
					}},
				})
				continue
			}
			if text == "" {
				continue
			}
			turns = append(turns, anthropicToolMessage{
				Role: "assistant",
				Content: []any{map[string]any{
					"type": "text",
					"text": text,
				}},
			})
		case core.RoleTool:
			if text == "" {
				text = "(no output)"
			}
			toolUseID := strings.TrimSpace(msg.ToolCallID)
			if toolUseID == "" {
				turns = append(turns, anthropicToolMessage{
					Role: "user",
					Content: []any{map[string]any{
						"type": "text",
						"text": text,
					}},
				})
				continue
			}
			turns = append(turns, anthropicToolMessage{
				Role: "user",
				Content: []any{map[string]any{
					"type":        "tool_result",
					"tool_use_id": toolUseID,
					"content":     text,
					"is_error":    false,
				}},
			})
		default:
			if text == "" && len(msg.Images) == 0 {
				continue
			}
			var content []any
			for _, img := range msg.Images {
				content = append(content, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": img.MimeType,
						"data":       img.Data,
					},
				})
			}
			if text != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": text,
				})
			}
			turns = append(turns, anthropicToolMessage{
				Role:    "user",
				Content: content,
			})
		}
	}
	return strings.Join(systemParts, "\n\n"), turns
}

func rawObjectOrString(input string) any {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return map[string]any{}
	}
	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		return payload
	}
	return map[string]any{"value": trimmed}
}

// NewEntryIDForProvider keeps provider package independent from core entry IDs.
func NewEntryIDForProvider() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
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

func looksLikeAnthropicInvalidBearer(body []byte, summarized string) bool {
	lower := strings.ToLower(strings.TrimSpace(summarized + "\n" + string(body)))
	return strings.Contains(lower, "invalid bearer token") ||
		(strings.Contains(lower, "bearer") && strings.Contains(lower, "invalid_token")) ||
		(strings.Contains(lower, "unauthorized") && strings.Contains(lower, "bearer"))
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
