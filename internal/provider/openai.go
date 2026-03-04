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
	defaultOpenAIBaseURL       = "https://api.openai.com/v1"
	defaultOpenAIContextWindow = 200_000
)

type OpenAIOptions struct {
	ProviderID    string
	BaseURL       string
	ContextWindow int
	DefaultModel  string
	HTTPClient    *http.Client
}

type OpenAIProvider struct {
	providerID    string
	baseURL       string
	contextWindow int
	defaultModel  string
	client        *http.Client
}

type openAIResponsesRequest struct {
	Model string `json:"model"`
	Input []any  `json:"input"`
}

type openAIToolResponsesRequest struct {
	Model      string `json:"model"`
	Input      []any  `json:"input"`
	ToolChoice string `json:"tool_choice,omitempty"`
	Tools      []struct {
		Type        string          `json:"type"`
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"tools,omitempty"`
}

// openAIResponsesMessage is used for simple text-only messages.
type openAIResponsesMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIMultimodalMessage is used when images are attached (content is an array).
type openAIMultimodalMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type openAIResponsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			OutputText  string `json:"output_text"`
			InputText   string `json:"input_text"`
			RefusalText string `json:"refusal"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func NewOpenAIProvider(opts OpenAIOptions) *OpenAIProvider {
	providerID := strings.TrimSpace(opts.ProviderID)
	if providerID == "" {
		providerID = "openai"
	}
	baseURL := strings.TrimSpace(strings.TrimRight(opts.BaseURL, "/"))
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	contextWindow := opts.ContextWindow
	if contextWindow <= 0 {
		contextWindow = defaultOpenAIContextWindow
	}
	defaultModel := strings.TrimSpace(opts.DefaultModel)
	if defaultModel == "" {
		if providerID == "openai-codex" {
			defaultModel = "gpt-5.3-codex"
		} else {
			defaultModel = "gpt-5.1-codex"
		}
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OpenAIProvider{
		providerID:    providerID,
		baseURL:       baseURL,
		contextWindow: contextWindow,
		defaultModel:  defaultModel,
		client:        client,
	}
}

func (p *OpenAIProvider) ID() string {
	return p.providerID
}

func (p *OpenAIProvider) BaseURL() string {
	return p.baseURL
}

func (p *OpenAIProvider) ContextWindow(_ string) int {
	return p.contextWindow
}

func (p *OpenAIProvider) ToolCapabilities() ToolCapabilities {
	return ToolCapabilities{
		SupportsTools:         true,
		SupportsParallelCalls: true,
		MaxToolCalls:          16,
	}
}

func (p *OpenAIProvider) DiscoverPreferredModel(_ context.Context, _ core.Account) (string, string, error) {
	return p.defaultModel, "fallback", nil
}

func (p *OpenAIProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	secret := strings.TrimSpace(req.Account.Token)
	if secret == "" {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing openai credential",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}

	modelID := strings.TrimSpace(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = p.defaultModel
	}

	input := toOpenAIInput(req.Messages)
	if len(input) == 0 {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "openai request has no chat turns",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}

	payload := openAIResponsesRequest{
		Model: modelID,
		Input: input,
	}
	raw, _ := json.Marshal(payload)

	targetURL := strings.TrimRight(p.baseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+secret)
	httpReq.Header.Set("User-Agent", "nekoclaw/1.0")

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
			Reason:   classifyOpenAIStatus(resp.StatusCode, string(body)),
			Message:  summarizeOpenAIError(body),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}

	text, usage, ok := extractTextAndUsageFromOpenAI(body)
	if !ok {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "openai response did not include text: " + summarizeForError(body, 280),
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

func (p *OpenAIProvider) GenerateToolTurn(ctx context.Context, req ToolTurnRequest) (ToolTurnResponse, error) {
	secret := strings.TrimSpace(req.Account.Token)
	if secret == "" {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing openai credential",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}

	modelID := strings.TrimSpace(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = p.defaultModel
	}
	input := toOpenAIToolInput(req.Messages)
	if len(input) == 0 {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "openai tool request has no chat turns",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}
	payload := openAIToolResponsesRequest{
		Model:      modelID,
		Input:      input,
		ToolChoice: "auto",
	}
	if len(req.Tools) > 0 {
		payload.Tools = make([]struct {
			Type        string          `json:"type"`
			Name        string          `json:"name"`
			Description string          `json:"description,omitempty"`
			Parameters  json.RawMessage `json:"parameters"`
		}, 0, len(req.Tools))
		for _, tool := range req.Tools {
			params := tool.InputSchema
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","additionalProperties":false}`)
			}
			payload.Tools = append(payload.Tools, struct {
				Type        string          `json:"type"`
				Name        string          `json:"name"`
				Description string          `json:"description,omitempty"`
				Parameters  json.RawMessage `json:"parameters"`
			}{
				Type:        "function",
				Name:        strings.TrimSpace(tool.Name),
				Description: strings.TrimSpace(tool.Description),
				Parameters:  params,
			})
		}
	}
	raw, _ := json.Marshal(payload)
	targetURL := strings.TrimRight(p.baseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+secret)
	httpReq.Header.Set("User-Agent", "nekoclaw/1.0")

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
		return ToolTurnResponse{}, &FailureError{
			Reason:   classifyOpenAIStatus(resp.StatusCode, string(body)),
			Message:  summarizeOpenAIError(body),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}

	text, usage, calls, ok := extractTextUsageAndToolCallsFromOpenAI(body)
	if !ok {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "openai response did not include text or tool calls: " + summarizeForError(body, 280),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}
	stopReason := "end_turn"
	if len(calls) > 0 {
		stopReason = "tool_calls"
	}
	return ToolTurnResponse{
		Text:       text,
		Endpoint:   p.baseURL,
		Raw:        body,
		Usage:      usage,
		StopReason: stopReason,
		ToolCalls:  calls,
	}, nil
}

func toOpenAIInput(messages []core.Message) []any {
	out := make([]any, 0, len(messages))
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		if text == "" && len(msg.Images) == 0 {
			continue
		}
		role := mapOpenAIRole(msg.Role)
		if len(msg.Images) > 0 && role == "user" {
			out = append(out, buildOpenAIMultimodalMessage(role, text, msg.Images))
		} else {
			out = append(out, openAIResponsesMessage{Role: role, Content: text})
		}
	}
	return out
}

func buildOpenAIMultimodalMessage(role, text string, images []core.ImageData) openAIMultimodalMessage {
	content := make([]any, 0, len(images)+1)
	for _, img := range images {
		dataURI := "data:" + img.MimeType + ";base64," + img.Data
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": dataURI,
		})
	}
	if text != "" {
		content = append(content, map[string]any{
			"type": "input_text",
			"text": text,
		})
	}
	return openAIMultimodalMessage{Role: role, Content: content}
}

func toOpenAIToolInput(messages []core.Message) []any {
	out := make([]any, 0, len(messages))
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		switch msg.Role {
		case core.RoleTool:
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID == "" {
				continue
			}
			if text == "" {
				text = "(no output)"
			}
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  text,
			})
		case core.RoleAssistant:
			if strings.TrimSpace(msg.ToolCallID) != "" && strings.TrimSpace(msg.ToolName) != "" {
				args := text
				if args == "" {
					args = "{}"
				}
				out = append(out, map[string]any{
					"type":      "function_call",
					"call_id":   strings.TrimSpace(msg.ToolCallID),
					"name":      strings.TrimSpace(msg.ToolName),
					"arguments": args,
				})
				continue
			}
			if text == "" {
				continue
			}
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": text,
			})
		case core.RoleSystem:
			if text == "" {
				continue
			}
			out = append(out, map[string]any{
				"role":    "system",
				"content": text,
			})
		default:
			if text == "" && len(msg.Images) == 0 {
				continue
			}
			if len(msg.Images) > 0 {
				mm := buildOpenAIMultimodalMessage("user", text, msg.Images)
				out = append(out, mm)
			} else {
				out = append(out, map[string]any{
					"role":    "user",
					"content": text,
				})
			}
		}
	}
	return out
}

func mapOpenAIRole(role core.MessageRole) string {
	switch role {
	case core.RoleSystem:
		return "system"
	case core.RoleAssistant:
		return "assistant"
	default:
		return "user"
	}
}

func extractTextAndUsageFromOpenAI(body []byte) (string, core.UsageInfo, bool) {
	var payload openAIResponsesResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", core.UsageInfo{}, false
	}

	text := strings.TrimSpace(payload.OutputText)
	if text == "" {
		parts := make([]string, 0, 4)
		for _, item := range payload.Output {
			for _, content := range item.Content {
				candidate := strings.TrimSpace(content.Text)
				if candidate == "" {
					candidate = strings.TrimSpace(content.OutputText)
				}
				if candidate == "" {
					candidate = strings.TrimSpace(content.InputText)
				}
				if candidate == "" {
					continue
				}
				parts = append(parts, candidate)
			}
		}
		text = strings.TrimSpace(strings.Join(parts, "\n"))
	}
	if text == "" {
		return "", core.UsageInfo{}, false
	}

	usage := core.UsageInfo{
		InputTokens:  payload.Usage.InputTokens,
		OutputTokens: payload.Usage.OutputTokens,
		TotalTokens:  payload.Usage.TotalTokens,
	}
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return text, usage, true
}

func extractTextUsageAndToolCallsFromOpenAI(body []byte) (string, core.UsageInfo, []ToolCall, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", core.UsageInfo{}, nil, false
	}

	calls := make([]ToolCall, 0, 4)
	if output, ok := payload["output"].([]any); ok {
		for _, raw := range output {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			itemType := strings.TrimSpace(anyToString(item["type"]))
			if itemType != "function_call" && itemType != "tool_call" {
				continue
			}
			callID := strings.TrimSpace(anyToString(item["call_id"]))
			if callID == "" {
				callID = strings.TrimSpace(anyToString(item["id"]))
			}
			if callID == "" {
				callID = fmt.Sprintf("toolcall-%d", time.Now().UnixNano())
			}
			name := strings.TrimSpace(anyToString(item["name"]))
			args := strings.TrimSpace(anyToString(item["arguments"]))
			if args == "" {
				args = "{}"
			}
			calls = append(calls, ToolCall{
				ID:        callID,
				Name:      name,
				Arguments: json.RawMessage(args),
			})
		}
	}

	text, usage, ok := extractTextAndUsageFromOpenAI(body)
	if ok {
		return text, usage, calls, true
	}
	usage = extractOpenAIUsage(payload)
	if len(calls) > 0 {
		return "", usage, calls, true
	}
	return "", usage, nil, false
}

func extractOpenAIUsage(payload map[string]any) core.UsageInfo {
	usage := core.UsageInfo{}
	raw, ok := payload["usage"].(map[string]any)
	if !ok {
		return usage
	}
	usage.InputTokens = int(anyToFloat(raw["input_tokens"]))
	usage.OutputTokens = int(anyToFloat(raw["output_tokens"]))
	usage.TotalTokens = int(anyToFloat(raw["total_tokens"]))
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}

func anyToFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return 0
	}
}

func summarizeOpenAIError(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		msg := strings.TrimSpace(payload.Error.Message)
		typ := strings.TrimSpace(payload.Error.Type)
		code := strings.TrimSpace(anyToString(payload.Error.Code))
		if msg != "" {
			if code != "" {
				return msg + " (code=" + code + ")"
			}
			if typ != "" {
				return msg + " (type=" + typ + ")"
			}
			return msg
		}
		if code != "" {
			return code
		}
		if typ != "" {
			return typ
		}
	}
	return summarizeForError(body, 280)
}

func classifyOpenAIStatus(status int, body string) core.FailureReason {
	lower := strings.ToLower(strings.TrimSpace(body))
	switch status {
	case http.StatusUnauthorized:
		return core.FailureAuthPermanent
	case http.StatusForbidden:
		if strings.Contains(lower, "billing") || strings.Contains(lower, "quota") || strings.Contains(lower, "insufficient_quota") {
			return core.FailureBilling
		}
		return core.FailureAuthPermanent
	case http.StatusTooManyRequests:
		return core.FailureRateLimit
	case http.StatusBadRequest:
		if strings.Contains(lower, "invalid api key") || strings.Contains(lower, "incorrect api key") || strings.Contains(lower, "invalid_authentication") {
			return core.FailureAuthPermanent
		}
		return core.FailureFormat
	case http.StatusNotFound:
		return core.FailureModelNotFound
	case http.StatusRequestTimeout:
		return core.FailureTimeout
	default:
		if status >= 500 {
			return core.FailureUnknown
		}
		if strings.Contains(lower, "rate limit") || strings.Contains(lower, "resource exhausted") {
			return core.FailureRateLimit
		}
		if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") {
			return core.FailureTimeout
		}
		if strings.Contains(lower, "model_not_found") || strings.Contains(lower, "model not found") {
			return core.FailureModelNotFound
		}
		if strings.Contains(lower, "invalid") || strings.Contains(lower, "bad request") {
			return core.FailureFormat
		}
		return core.FailureUnknown
	}
}

func anyToString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
