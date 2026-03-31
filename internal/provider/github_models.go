package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

const (
	defaultGitHubModelsBaseURL       = "https://models.github.ai"
	defaultGitHubModelsContextWindow = 200_000
	defaultGitHubModelsModel         = "openai/gpt-5-chat"
	githubModelsCatalogCacheTTL      = 10 * time.Minute
)

type GitHubModelsOptions struct {
	BaseURL       string
	ContextWindow int
	DefaultModel  string
	HTTPClient    *http.Client
}

type GitHubModelsProvider struct {
	baseURL       string
	contextWindow int
	defaultModel  string
	client        *http.Client

	modelCacheMu sync.Mutex
	modelCache   githubModelsCatalogCacheEntry
}

type githubModelsCatalogCacheEntry struct {
	Models    []string
	ExpiresAt time.Time
}

type githubModelsChatRequest struct {
	Model            string                    `json:"model"`
	Messages         []githubModelsChatMessage `json:"messages"`
	Temperature      *float64                  `json:"temperature,omitempty"`
	TopP             *float64                  `json:"top_p,omitempty"`
	FrequencyPenalty *float64                  `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64                  `json:"presence_penalty,omitempty"`
}

type githubModelsChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type githubModelsChatResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type githubModelsCatalogModel struct {
	ID                        string   `json:"id"`
	SupportedInputModalities  []string `json:"supported_input_modalities"`
	SupportedOutputModalities []string `json:"supported_output_modalities"`
}

func NewGitHubModelsProvider(opts GitHubModelsOptions) *GitHubModelsProvider {
	baseURL := strings.TrimSpace(strings.TrimRight(opts.BaseURL, "/"))
	if baseURL == "" {
		baseURL = defaultGitHubModelsBaseURL
	}
	contextWindow := opts.ContextWindow
	if contextWindow <= 0 {
		contextWindow = defaultGitHubModelsContextWindow
	}
	defaultModel := strings.TrimSpace(opts.DefaultModel)
	if defaultModel == "" {
		defaultModel = defaultGitHubModelsModel
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &GitHubModelsProvider{
		baseURL:       baseURL,
		contextWindow: contextWindow,
		defaultModel:  defaultModel,
		client:        client,
	}
}

func (p *GitHubModelsProvider) ID() string {
	return "github-models"
}

func (p *GitHubModelsProvider) BaseURL() string {
	return p.baseURL
}

func (p *GitHubModelsProvider) ContextWindow(model string) int {
	normalized := stripGitHubModelsNamespace(model)
	if cw := lookupModelContextWindow(normalized); cw > 0 {
		return cw
	}
	return p.contextWindow
}

func (p *GitHubModelsProvider) DiscoverPreferredModel(_ context.Context, _ core.Account) (string, string, error) {
	return p.defaultModel, "fallback", nil
}

func (p *GitHubModelsProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	token := strings.TrimSpace(req.Account.Token)
	if token == "" {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing GitHub token / PAT",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}

	if hasImages(req.Messages) {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "github models provider does not support image input in v1",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}

	modelID := normalizeGitHubModelsModelID(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = p.defaultModel
	}

	messages := toGitHubModelsMessages(req.Messages)
	if len(messages) == 0 {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "github models request has no chat turns",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}

	payload := githubModelsChatRequest{
		Model:    modelID,
		Messages: messages,
	}
	if req.Generation != nil {
		payload.Temperature = req.Generation.Temperature
		payload.TopP = req.Generation.TopP
		payload.FrequencyPenalty = req.Generation.FrequencyPenalty
		payload.PresencePenalty = req.Generation.PresencePenalty
	}
	raw, _ := json.Marshal(payload)

	targetURL := strings.TrimRight(p.baseURL, "/") + "/inference/chat/completions"
	resp, err := doWithRetry(ctx, DefaultRetryConfig(), func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("User-Agent", "nekoclaw/1.0")
		return p.client.Do(httpReq)
	}, nil)
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
			Reason:     classifyGitHubModelsStatus(resp.StatusCode, string(body)),
			Message:    summarizeGitHubModelsError(body),
			Endpoint:   p.baseURL,
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp),
		}
	}

	text, usage, ok := extractTextAndUsageFromGitHubModels(body)
	if !ok {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "github models response did not include text: " + summarizeForError(body, 280),
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

func (p *GitHubModelsProvider) ListModels(ctx context.Context, _ core.Account) ([]string, error) {
	if cached, ok := p.loadModelCache(); ok {
		return cached, nil
	}

	targetURL := strings.TrimRight(p.baseURL, "/") + "/catalog/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build github models catalog request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "nekoclaw/1.0")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github models catalog request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github models catalog returned %d: %s", resp.StatusCode, summarizeForError(body, 280))
	}

	var parsed []githubModelsCatalogModel
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode github models catalog response: %w", err)
	}

	seen := make(map[string]struct{}, len(parsed))
	models := make([]string, 0, len(parsed))
	for _, model := range parsed {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if !containsTrimmedFold(model.SupportedInputModalities, "text") {
			continue
		}
		if !containsTrimmedFold(model.SupportedOutputModalities, "text") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	p.storeModelCache(models)
	return models, nil
}

func (p *GitHubModelsProvider) loadModelCache() ([]string, bool) {
	p.modelCacheMu.Lock()
	defer p.modelCacheMu.Unlock()
	if len(p.modelCache.Models) == 0 || time.Now().After(p.modelCache.ExpiresAt) {
		return nil, false
	}
	return append([]string(nil), p.modelCache.Models...), true
}

func (p *GitHubModelsProvider) storeModelCache(models []string) {
	p.modelCacheMu.Lock()
	defer p.modelCacheMu.Unlock()
	p.modelCache = githubModelsCatalogCacheEntry{
		Models:    append([]string(nil), models...),
		ExpiresAt: time.Now().Add(githubModelsCatalogCacheTTL),
	}
}

func normalizeGitHubModelsModelID(model string) string {
	return strings.TrimSpace(model)
}

func stripGitHubModelsNamespace(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if strings.Count(model, "/") < 1 {
		return model
	}
	_, tail, ok := strings.Cut(model, "/")
	if !ok {
		return model
	}
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return model
	}
	return tail
}

func hasImages(messages []core.Message) bool {
	for _, msg := range messages {
		if len(msg.Images) > 0 {
			return true
		}
	}
	return false
}

func toGitHubModelsMessages(messages []core.Message) []githubModelsChatMessage {
	out := make([]githubModelsChatMessage, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		out = append(out, githubModelsChatMessage{
			Role:    mapOpenAIRole(msg.Role),
			Content: content,
		})
	}
	return out
}

func extractTextAndUsageFromGitHubModels(body []byte) (string, core.UsageInfo, bool) {
	var payload githubModelsChatResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", core.UsageInfo{}, false
	}

	parts := make([]string, 0, len(payload.Choices))
	for _, choice := range payload.Choices {
		text := strings.TrimSpace(extractGitHubModelsContentText(choice.Message.Content))
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", core.UsageInfo{}, false
	}

	return strings.Join(parts, "\n"), core.UsageInfo{
		InputTokens:  payload.Usage.PromptTokens,
		OutputTokens: payload.Usage.CompletionTokens,
		TotalTokens:  payload.Usage.TotalTokens,
	}, true
}

func extractGitHubModelsContentText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if part, ok := item.(map[string]any); ok {
				if text := strings.TrimSpace(stringField(part, "text")); text != "" {
					parts = append(parts, text)
					continue
				}
				if text := strings.TrimSpace(stringField(part, "content")); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text := strings.TrimSpace(stringField(value, "text")); text != "" {
			return text
		}
		if text := strings.TrimSpace(stringField(value, "content")); text != "" {
			return text
		}
	}
	return ""
}

func stringField(input map[string]any, key string) string {
	raw, _ := input[key]
	text, _ := raw.(string)
	return text
}

func summarizeGitHubModelsError(body []byte) string {
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return raw
	}

	var parsed struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		parts := make([]string, 0, 2)
		if msg := strings.TrimSpace(parsed.Error.Message); msg != "" {
			parts = append(parts, msg)
		} else if msg := strings.TrimSpace(parsed.Message); msg != "" {
			parts = append(parts, msg)
		}
		if parsed.Error.Code != nil {
			code := strings.TrimSpace(fmt.Sprint(parsed.Error.Code))
			if code != "" && code != "<nil>" {
				parts = append(parts, "code="+code)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	return summarizeErrorMessage(raw)
}

func classifyGitHubModelsStatus(status int, body string) core.FailureReason {
	body = strings.ToLower(strings.TrimSpace(body))

	switch status {
	case http.StatusUnauthorized:
		return core.FailureAuthPermanent
	case http.StatusForbidden:
		if strings.Contains(body, "billing") || strings.Contains(body, "quota") || strings.Contains(body, "spending") {
			return core.FailureBilling
		}
		return core.FailureAuthPermanent
	case http.StatusBadRequest:
		return core.FailureFormat
	case http.StatusNotFound:
		if strings.Contains(body, "model") || strings.Contains(body, "not_found") || strings.Contains(body, "not found") {
			return core.FailureModelNotFound
		}
		return core.FailureUnknown
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return core.FailureTimeout
	case http.StatusTooManyRequests:
		return core.FailureRateLimit
	}
	if status >= 500 {
		return core.FailureUnknown
	}
	return core.FailureUnknown
}

func containsTrimmedFold(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}
