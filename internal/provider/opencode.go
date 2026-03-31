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
	defaultOpenCodeBaseURL       = "https://opencode.ai/zen/v1"
	defaultOpenCodeContextWindow = 200_000
	defaultOpenCodeModel         = "gpt-5.3-codex"
	openCodeModelCacheTTL        = 10 * time.Minute
)

type OpenCodeOptions struct {
	BaseURL       string
	ContextWindow int
	DefaultModel  string
	HTTPClient    *http.Client
}

type OpenCodeProvider struct {
	baseURL       string
	contextWindow int
	defaultModel  string
	client        *http.Client

	responsesProvider *OpenAIProvider
	anthropicProvider *AnthropicProvider
	googleProvider    *GoogleAIStudioProvider

	modelCacheMu sync.Mutex
	modelCache   openCodeModelCacheEntry
}

type openCodeModelCacheEntry struct {
	Models    []string
	ExpiresAt time.Time
}

type openCodeModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func NewOpenCodeProvider(opts OpenCodeOptions) *OpenCodeProvider {
	baseURL := strings.TrimSpace(strings.TrimRight(opts.BaseURL, "/"))
	if baseURL == "" {
		baseURL = defaultOpenCodeBaseURL
	}
	contextWindow := opts.ContextWindow
	if contextWindow <= 0 {
		contextWindow = defaultOpenCodeContextWindow
	}
	defaultModel := normalizeOpenCodeModelID(opts.DefaultModel)
	if defaultModel == "" {
		defaultModel = defaultOpenCodeModel
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}

	return &OpenCodeProvider{
		baseURL:       baseURL,
		contextWindow: contextWindow,
		defaultModel:  defaultModel,
		client:        client,
		responsesProvider: NewOpenAIProvider(OpenAIOptions{
			ProviderID:    "opencode",
			BaseURL:       baseURL,
			ContextWindow: contextWindow,
			DefaultModel:  defaultModel,
			HTTPClient:    client,
		}),
		anthropicProvider: NewAnthropicProvider(AnthropicOptions{
			BaseURL:       baseURL,
			ContextWindow: contextWindow,
			HTTPClient:    client,
		}),
		googleProvider: NewGoogleAIStudioProvider(GoogleAIStudioOptions{
			BaseURL:       baseURL,
			ContextWindow: 1_000_000,
			HTTPClient:    client,
		}),
	}
}

func (p *OpenCodeProvider) ID() string {
	return "opencode"
}

func (p *OpenCodeProvider) BaseURL() string {
	return p.baseURL
}

func (p *OpenCodeProvider) ContextWindow(model string) int {
	model = normalizeOpenCodeModelID(model)
	if cw := lookupModelContextWindow(model); cw > 0 {
		return cw
	}
	return p.contextWindow
}

func (p *OpenCodeProvider) ToolCapabilities() ToolCapabilities {
	return ToolCapabilities{
		SupportsTools:         true,
		SupportsParallelCalls: true,
		MaxToolCalls:          16,
	}
}

func (p *OpenCodeProvider) DiscoverPreferredModel(_ context.Context, _ core.Account) (string, string, error) {
	return p.defaultModel, "fallback", nil
}

func (p *OpenCodeProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	if strings.TrimSpace(req.Account.Token) == "" {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing OpenCode API key",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}

	normalized := req
	normalized.Model = p.resolveModel(req.Model)

	switch openCodeRouteForModel(normalized.Model) {
	case openCodeRouteAnthropic:
		return p.anthropicProvider.Generate(ctx, normalized)
	case openCodeRouteGoogle:
		return p.googleProvider.Generate(ctx, normalized)
	case openCodeRouteChatCompletions:
		return p.generateChatCompletions(ctx, normalized)
	default:
		return p.responsesProvider.Generate(ctx, normalized)
	}
}

func (p *OpenCodeProvider) GenerateToolTurn(ctx context.Context, req ToolTurnRequest) (ToolTurnResponse, error) {
	if strings.TrimSpace(req.Account.Token) == "" {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing OpenCode API key",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}

	normalized := req
	normalized.Model = p.resolveModel(req.Model)

	switch openCodeRouteForModel(normalized.Model) {
	case openCodeRouteAnthropic:
		return p.anthropicProvider.GenerateToolTurn(ctx, normalized)
	case openCodeRouteGoogle:
		return p.googleProvider.GenerateToolTurn(ctx, normalized)
	case openCodeRouteResponses:
		return p.responsesProvider.GenerateToolTurn(ctx, normalized)
	default:
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "OpenCode chat-completions models do not support tool calling in NekoClaw yet",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}
}

func (p *OpenCodeProvider) ListModels(ctx context.Context, _ core.Account) ([]string, error) {
	if cached, ok := p.loadModelCache(); ok {
		return cached, nil
	}

	targetURL := strings.TrimRight(p.baseURL, "/") + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build opencode models request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "nekoclaw/1.0")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("opencode models request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("opencode models API returned %d: %s", resp.StatusCode, summarizeForError(body, 280))
	}

	var parsed openCodeModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode opencode models response: %w", err)
	}

	seen := make(map[string]struct{}, len(parsed.Data))
	models := make([]string, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		id := normalizeOpenCodeModelID(item.ID)
		if id == "" {
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

func (p *OpenCodeProvider) resolveModel(model string) string {
	model = normalizeOpenCodeModelID(model)
	if model == "" || strings.EqualFold(model, "default") {
		return p.defaultModel
	}
	return model
}

func (p *OpenCodeProvider) generateChatCompletions(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	if hasImages(req.Messages) {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "OpenCode chat-completions models do not support image input in NekoClaw yet",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}

	messages := toGitHubModelsMessages(req.Messages)
	if len(messages) == 0 {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "opencode request has no chat turns",
			Endpoint: p.baseURL,
			Status:   http.StatusBadRequest,
		}
	}

	payload := githubModelsChatRequest{
		Model:    req.Model,
		Messages: messages,
	}
	if req.Generation != nil {
		payload.Temperature = req.Generation.Temperature
		payload.TopP = req.Generation.TopP
		payload.FrequencyPenalty = req.Generation.FrequencyPenalty
		payload.PresencePenalty = req.Generation.PresencePenalty
	}
	raw, _ := json.Marshal(payload)

	targetURL := strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	resp, err := doWithRetry(ctx, DefaultRetryConfig(), func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(req.Account.Token))
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
			Message:  "opencode response did not include text: " + summarizeForError(body, 280),
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

func (p *OpenCodeProvider) loadModelCache() ([]string, bool) {
	p.modelCacheMu.Lock()
	defer p.modelCacheMu.Unlock()
	if len(p.modelCache.Models) == 0 || time.Now().After(p.modelCache.ExpiresAt) {
		return nil, false
	}
	return append([]string(nil), p.modelCache.Models...), true
}

func (p *OpenCodeProvider) storeModelCache(models []string) {
	p.modelCacheMu.Lock()
	defer p.modelCacheMu.Unlock()
	p.modelCache = openCodeModelCacheEntry{
		Models:    append([]string(nil), models...),
		ExpiresAt: time.Now().Add(openCodeModelCacheTTL),
	}
}

type openCodeRoute string

const (
	openCodeRouteResponses       openCodeRoute = "responses"
	openCodeRouteAnthropic       openCodeRoute = "anthropic"
	openCodeRouteGoogle          openCodeRoute = "google"
	openCodeRouteChatCompletions openCodeRoute = "chat_completions"
)

func openCodeRouteForModel(model string) openCodeRoute {
	model = normalizeOpenCodeModelID(model)
	switch {
	case strings.HasPrefix(model, "gpt-"):
		return openCodeRouteResponses
	case strings.HasPrefix(model, "claude-"):
		return openCodeRouteAnthropic
	case strings.HasPrefix(model, "gemini-"):
		return openCodeRouteGoogle
	default:
		return openCodeRouteChatCompletions
	}
}

func normalizeOpenCodeModelID(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if strings.Count(model, "/") < 1 {
		return model
	}
	providerID, tail, ok := strings.Cut(model, "/")
	if ok && strings.EqualFold(strings.TrimSpace(providerID), "opencode") {
		return strings.TrimSpace(tail)
	}
	return model
}
