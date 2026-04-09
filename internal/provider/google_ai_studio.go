package provider

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

const (
	defaultGoogleAIStudioBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	aiStudioModelCacheTTL        = 10 * time.Minute
)

type GoogleAIStudioOptions struct {
	BaseURL       string
	ContextWindow int
	HTTPClient    *http.Client
}

type GoogleAIStudioProvider struct {
	client        *http.Client
	baseURL       string
	contextWindow int

	modelCacheMu sync.Mutex
	modelCache   map[string]aiStudioModelCacheEntry
}

type aiStudioModelCacheEntry struct {
	Models     []string
	ExpiresAt  time.Time
	CachedFrom string
}

type aiStudioModelResponse struct {
	Models []struct {
		Name                       string   `json:"name"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
}

type aiStudioUsageMetadata struct {
	PromptTokenCount        int  `json:"promptTokenCount"`
	CandidatesTokenCount    int  `json:"candidatesTokenCount"`
	TotalTokenCount         int  `json:"totalTokenCount"`
	CachedContentTokenCount *int `json:"cachedContentTokenCount,omitempty"`
}

type aiStudioGenerateResponse struct {
	PromptFeedback struct {
		BlockReason        string `json:"blockReason"`
		BlockReasonMessage string `json:"blockReasonMessage"`
	} `json:"promptFeedback"`
	Candidates []struct {
		FinishReason  string `json:"finishReason"`
		FinishMessage string `json:"finishMessage"`
		Content       struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata aiStudioUsageMetadata `json:"usageMetadata"`
}

func NewGoogleAIStudioProvider(opts GoogleAIStudioOptions) *GoogleAIStudioProvider {
	baseURL := strings.TrimSpace(strings.TrimRight(opts.BaseURL, "/"))
	if baseURL == "" {
		baseURL = defaultGoogleAIStudioBaseURL
	}
	contextWindow := opts.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 1_000_000
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &GoogleAIStudioProvider{
		client:        client,
		baseURL:       baseURL,
		contextWindow: contextWindow,
		modelCache:    map[string]aiStudioModelCacheEntry{},
	}
}

func (p *GoogleAIStudioProvider) ID() string {
	return "google-ai-studio"
}

func (p *GoogleAIStudioProvider) ContextWindow(model string) int {
	if cw := lookupModelContextWindow(model); cw > 0 {
		return cw
	}
	return p.contextWindow
}

func (p *GoogleAIStudioProvider) BaseURL() string {
	return p.baseURL
}

func (p *GoogleAIStudioProvider) ToolCapabilities() ToolCapabilities {
	return ToolCapabilities{
		SupportsTools:         true,
		SupportsParallelCalls: true,
		MaxToolCalls:          8,
	}
}

func (p *GoogleAIStudioProvider) GenerateToolTurn(ctx context.Context, req ToolTurnRequest) (ToolTurnResponse, error) {
	apiKey := strings.TrimSpace(req.Account.Token)
	if apiKey == "" {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing API key",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}
	modelID := normalizeAIStudioModelID(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = "gemini-2.5-pro"
	}

	systemInstruction, contents := toGeminiToolContents(req.Messages)
	payload := map[string]any{
		"contents": contents,
	}
	if tools := toGeminiFunctionDeclarations(req.Tools); len(tools) > 0 {
		payload["tools"] = tools
	}
	if systemInstruction != "" {
		payload["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": systemInstruction}},
		}
	}
	if genConfig := buildGeminiGenerationConfig(p.ID(), modelID, req.Generation); genConfig != nil {
		payload["generationConfig"] = genConfig
	}

	raw, _ := json.Marshal(payload)
	callURL, err := p.buildURL("models/"+url.PathEscape(modelID)+":generateContent", apiKey)
	if err != nil {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}

	resp, err := doWithRetry(ctx, DefaultRetryConfig(), func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, callURL, bytes.NewReader(raw))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("User-Agent", "nekoclaw/1.0")
		return p.client.Do(httpReq)
	}, nil)
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
		reason := classifyAIStudioStatus(resp.StatusCode, string(body))
		return ToolTurnResponse{}, &FailureError{
			Reason:     reason,
			Message:    summarizeAIStudioError(body),
			Endpoint:   p.baseURL,
			Status:     resp.StatusCode,
			RetryAfter: parseGeminiRetryAfter(resp, body),
		}
	}

	result := extractToolCallsFromGeminiResponse(body)
	if !result.OK {
		if blocked := aiStudioBlockedFailure(body, p.baseURL, resp.StatusCode); blocked != nil {
			return ToolTurnResponse{}, blocked
		}
		if empty := aiStudioEmptyCandidateFailure(body, p.baseURL, resp.StatusCode); empty != nil {
			return ToolTurnResponse{}, empty
		}
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "google ai studio tool response did not include text or tool calls: " + summarizeForError(body, 280),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}

	stopReason := "end_turn"
	if len(result.Calls) > 0 {
		stopReason = "tool_calls"
	}
	return ToolTurnResponse{
		Text:            result.Text,
		Endpoint:        p.baseURL,
		Raw:             body,
		Usage:           result.Usage,
		StopReason:      stopReason,
		ToolCalls:       result.Calls,
		RawModelContent: result.RawModelContent,
	}, nil
}

func (p *GoogleAIStudioProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	apiKey := strings.TrimSpace(req.Account.Token)
	if apiKey == "" {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing API key",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}
	modelID := normalizeAIStudioModelID(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = "gemini-2.5-pro"
	}

	payload := map[string]any{
		"contents": toAIStudioContents(req.Messages),
	}
	if genConfig := buildGeminiGenerationConfig(p.ID(), modelID, req.Generation); genConfig != nil {
		payload["generationConfig"] = genConfig
	}
	raw, _ := json.Marshal(payload)
	callURL, err := p.buildURL("models/"+url.PathEscape(modelID)+":generateContent", apiKey)
	if err != nil {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}

	resp, err := doWithRetry(ctx, DefaultRetryConfig(), func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, callURL, bytes.NewReader(raw))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
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
		reason := classifyAIStudioStatus(resp.StatusCode, string(body))
		return GenerateResponse{}, &FailureError{
			Reason:     reason,
			Message:    summarizeAIStudioError(body),
			Endpoint:   p.baseURL,
			Status:     resp.StatusCode,
			RetryAfter: parseGeminiRetryAfter(resp, body),
		}
	}

	text, usage, ok := extractTextAndUsageFromAIStudio(body)
	if !ok {
		if blocked := aiStudioBlockedFailure(body, p.baseURL, resp.StatusCode); blocked != nil {
			return GenerateResponse{}, blocked
		}
		if empty := aiStudioEmptyCandidateFailure(body, p.baseURL, resp.StatusCode); empty != nil {
			return GenerateResponse{}, empty
		}
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "google ai studio response did not include text: " + summarizeForError(body, 280),
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

// GenerateStream sends a streaming request to the AI Studio SSE endpoint and
// returns a channel that yields incremental text chunks as they arrive.
func (p *GoogleAIStudioProvider) GenerateStream(ctx context.Context, req GenerateRequest) (<-chan GenerateStreamChunk, error) {
	apiKey := strings.TrimSpace(req.Account.Token)
	if apiKey == "" {
		return nil, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing API key",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}
	modelID := normalizeAIStudioModelID(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = "gemini-2.5-pro"
	}

	// Build the same payload as Generate().
	payload := map[string]any{
		"contents": toAIStudioContents(req.Messages),
	}
	if genConfig := buildGeminiGenerationConfig(p.ID(), modelID, req.Generation); genConfig != nil {
		payload["generationConfig"] = genConfig
	}
	raw, _ := json.Marshal(payload)

	// Use the SSE streaming endpoint instead of :generateContent.
	streamPath := "models/" + url.PathEscape(modelID) + ":streamGenerateContent"
	callURL, err := p.buildURL(streamPath, apiKey)
	if err != nil {
		return nil, &FailureError{
			Reason:   core.FailureFormat,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}
	// Append alt=sse; buildURL already sets ?key=..., so use "&".
	callURL += "&alt=sse"

	resp, err := doWithRetry(ctx, DefaultRetryConfig(), func() (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, callURL, bytes.NewReader(raw))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("User-Agent", "nekoclaw/1.0")
		return p.client.Do(httpReq)
	}, nil)
	if err != nil {
		return nil, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}

	// For non-2xx responses, read the error body and return immediately.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		reason := classifyAIStudioStatus(resp.StatusCode, string(body))
		return nil, &FailureError{
			Reason:     reason,
			Message:    summarizeAIStudioError(body),
			Endpoint:   p.baseURL,
			Status:     resp.StatusCode,
			RetryAfter: parseGeminiRetryAfter(resp, body),
		}
	}

	ch := make(chan GenerateStreamChunk, 16)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var accumulatedUsage core.UsageInfo
		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer size for large SSE data lines.
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

		var eventData []string

		flush := func() {
			if len(eventData) == 0 {
				return
			}
			joined := strings.Join(eventData, "")
			eventData = eventData[:0]

			var root map[string]any
			if err := json.Unmarshal([]byte(joined), &root); err != nil {
				return
			}

			// Extract text from the Gemini-style SSE chunk.
			if text, ok := extractTextFromGeminiMap(root); ok {
				select {
				case ch <- GenerateStreamChunk{Text: text}:
				case <-ctx.Done():
					return
				}
			}

			// Accumulate usage from every chunk; the final chunk typically
			// carries the full token counts.
			if usage := parseUsageMetadata(root); usage.TotalTokens > 0 {
				accumulatedUsage = usage
			}
		}

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				ch <- GenerateStreamChunk{Error: ctx.Err()}
				return
			default:
			}

			line := scanner.Text()
			trimmed := strings.TrimSpace(line)

			// Empty line signals end of an SSE event.
			if trimmed == "" {
				flush()
				continue
			}

			// Collect data: lines.
			if strings.HasPrefix(trimmed, "data:") {
				eventData = append(eventData, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		// Flush any remaining buffered event data.
		flush()

		if err := scanner.Err(); err != nil {
			select {
			case ch <- GenerateStreamChunk{Error: err}:
			case <-ctx.Done():
			}
			return
		}

		// Send the final done chunk with accumulated usage.
		select {
		case ch <- GenerateStreamChunk{
			Done:     true,
			Endpoint: p.baseURL,
			Usage:    accumulatedUsage,
		}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

func (p *GoogleAIStudioProvider) DiscoverPreferredModel(
	ctx context.Context,
	account core.Account,
) (modelID string, source string, err error) {
	models, _, _, err := p.ListModelsWithSource(ctx, account)
	if err != nil {
		return "", "", err
	}
	if model, ok := pickPreferredAIStudioModel(models); ok {
		return model, "models.list", nil
	}
	return "gemini-2.5-pro", "fallback", nil
}

func (p *GoogleAIStudioProvider) ListModels(ctx context.Context, account core.Account) ([]string, error) {
	models, _, _, err := p.ListModelsWithSource(ctx, account)
	return models, err
}

func (p *GoogleAIStudioProvider) ListModelsWithSource(
	ctx context.Context,
	account core.Account,
) (models []string, source string, cachedUntil time.Time, err error) {
	cacheKey := aiStudioCacheKey(account)
	if cacheKey != "" {
		if cached, expiresAt, ok := p.loadModelCache(cacheKey); ok {
			return cached, "cache", expiresAt, nil
		}
	}

	models, err = p.fetchModels(ctx, strings.TrimSpace(account.Token))
	if err != nil {
		return nil, "", time.Time{}, err
	}
	cachedUntil = time.Now().Add(aiStudioModelCacheTTL)
	if cacheKey != "" {
		p.storeModelCache(cacheKey, models, "live", cachedUntil)
	}
	return models, "live", cachedUntil, nil
}

func (p *GoogleAIStudioProvider) fetchModels(ctx context.Context, apiKey string) ([]string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing API key",
			Endpoint: p.baseURL,
			Status:   http.StatusUnauthorized,
		}
	}
	callURL, err := p.buildURL("models", apiKey)
	if err != nil {
		return nil, &FailureError{
			Reason:   core.FailureFormat,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, callURL, nil)
	if err != nil {
		return nil, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}
	httpReq.Header.Set("User-Agent", "nekoclaw/1.0")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  err.Error(),
			Endpoint: p.baseURL,
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		reason := classifyAIStudioStatus(resp.StatusCode, string(body))
		return nil, &FailureError{
			Reason:   reason,
			Message:  summarizeAIStudioError(body),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}

	var payload aiStudioModelResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "decode models.list response failed: " + err.Error(),
			Endpoint: p.baseURL,
			Status:   resp.StatusCode,
		}
	}

	set := map[string]struct{}{}
	for _, item := range payload.Models {
		modelID := normalizeAIStudioModelID(item.Name)
		if modelID == "" {
			continue
		}
		if !supportsGenerateContent(item.SupportedGenerationMethods) {
			continue
		}
		set[modelID] = struct{}{}
	}
	models := make([]string, 0, len(set))
	for modelID := range set {
		models = append(models, modelID)
	}
	sort.Strings(models)
	return models, nil
}

func (p *GoogleAIStudioProvider) buildURL(path string, apiKey string) (string, error) {
	base, err := url.Parse(strings.TrimRight(p.baseURL, "/") + "/")
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return "", err
	}
	full := base.ResolveReference(ref)
	q := full.Query()
	q.Set("key", apiKey)
	full.RawQuery = q.Encode()
	return full.String(), nil
}

func (p *GoogleAIStudioProvider) loadModelCache(cacheKey string) ([]string, time.Time, bool) {
	p.modelCacheMu.Lock()
	defer p.modelCacheMu.Unlock()
	entry, ok := p.modelCache[cacheKey]
	if !ok {
		return nil, time.Time{}, false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(p.modelCache, cacheKey)
		return nil, time.Time{}, false
	}
	return append([]string(nil), entry.Models...), entry.ExpiresAt, true
}

func (p *GoogleAIStudioProvider) storeModelCache(cacheKey string, models []string, source string, expiresAt time.Time) {
	p.modelCacheMu.Lock()
	defer p.modelCacheMu.Unlock()
	p.modelCache[cacheKey] = aiStudioModelCacheEntry{
		Models:     append([]string(nil), models...),
		ExpiresAt:  expiresAt,
		CachedFrom: strings.TrimSpace(source),
	}
}

func toAIStudioContents(messages []core.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Role == core.RoleAssistant {
			role = "model"
		}
		parts := make([]map[string]any, 0, len(msg.Images)+1)
		for _, img := range msg.Images {
			parts = append(parts, map[string]any{
				"inline_data": map[string]any{
					"mime_type": img.MimeType,
					"data":      img.Data,
				},
			})
		}
		text := strings.TrimSpace(msg.Content)
		if text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
		if len(parts) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"role":  role,
			"parts": parts,
		})
	}
	return out
}

func extractTextAndUsageFromAIStudio(body []byte) (string, core.UsageInfo, bool) {
	var payload aiStudioGenerateResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", core.UsageInfo{}, false
	}
	parts := make([]string, 0, 4)
	for _, candidate := range payload.Candidates {
		for _, part := range candidate.Content.Parts {
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", core.UsageInfo{}, false
	}
	return strings.Join(parts, "\n"), buildAIStudioUsage(payload.UsageMetadata), true
}

func buildAIStudioUsage(meta aiStudioUsageMetadata) core.UsageInfo {
	return buildGeminiUsage(
		meta.PromptTokenCount,
		meta.CandidatesTokenCount,
		meta.TotalTokenCount,
		meta.CachedContentTokenCount,
	)
}

func aiStudioBlockedFailure(body []byte, endpoint string, status int) *FailureError {
	message, ok := detectAIStudioBlockedMessage(body)
	if !ok {
		return nil
	}
	return &FailureError{
		Reason:   core.FailureFormat,
		Message:  message,
		Endpoint: endpoint,
		Status:   status,
	}
}

func aiStudioEmptyCandidateFailure(body []byte, endpoint string, status int) *FailureError {
	message, ok := detectAIStudioEmptyCandidateMessage(body)
	if !ok {
		return nil
	}
	return &FailureError{
		Reason:   core.FailureUnknown,
		Message:  message,
		Endpoint: endpoint,
		Status:   status,
	}
}

func detectAIStudioBlockedMessage(body []byte) (string, bool) {
	var payload aiStudioGenerateResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}

	if reason := strings.TrimSpace(payload.PromptFeedback.BlockReason); reason != "" &&
		!strings.EqualFold(reason, "BLOCK_REASON_UNSPECIFIED") {
		return formatAIStudioBlockMessage(reason, payload.PromptFeedback.BlockReasonMessage), true
	}

	for _, candidate := range payload.Candidates {
		reason := strings.TrimSpace(candidate.FinishReason)
		switch {
		case reason == "":
			continue
		case strings.EqualFold(reason, "STOP"),
			strings.EqualFold(reason, "MAX_TOKENS"):
			continue
		case strings.EqualFold(reason, "SAFETY"),
			strings.EqualFold(reason, "PROHIBITED_CONTENT"),
			strings.EqualFold(reason, "RECITATION"),
			strings.EqualFold(reason, "SPII"):
			return formatAIStudioBlockMessage(reason, candidate.FinishMessage), true
		}
	}

	return "", false
}

func detectAIStudioEmptyCandidateMessage(body []byte) (string, bool) {
	var payload aiStudioGenerateResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}
	if len(payload.Candidates) == 0 {
		return "", false
	}

	for _, candidate := range payload.Candidates {
		for _, part := range candidate.Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				return "", false
			}
		}
	}

	finishReason := ""
	for _, candidate := range payload.Candidates {
		if reason := strings.TrimSpace(candidate.FinishReason); reason != "" {
			finishReason = reason
			break
		}
	}
	if finishReason == "" {
		return "google ai studio returned an empty candidate response", true
	}
	return "google ai studio returned an empty candidate response with finishReason=" + finishReason, true
}

func formatAIStudioBlockMessage(reason string, detail string) string {
	reason = strings.TrimSpace(reason)
	detail = strings.TrimSpace(detail)
	if detail == "" || strings.EqualFold(detail, reason) {
		return "google ai studio blocked request: " + reason
	}
	return fmt.Sprintf("google ai studio blocked request: %s (%s)", reason, detail)
}

func supportsGenerateContent(methods []string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, method := range methods {
		if strings.EqualFold(strings.TrimSpace(method), "generateContent") {
			return true
		}
	}
	return false
}

func normalizeAIStudioModelID(model string) string {
	trimmed := strings.TrimSpace(model)
	trimmed = strings.TrimPrefix(trimmed, "models/")
	return strings.TrimSpace(trimmed)
}

func aiStudioCacheKey(account core.Account) string {
	if id := strings.TrimSpace(account.ID); id != "" {
		return id
	}
	token := strings.TrimSpace(account.Token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "token:" + hex.EncodeToString(sum[:8])
}

func pickPreferredAIStudioModel(modelIDs []string) (string, bool) {
	normalized := make([]string, 0, len(modelIDs))
	seen := map[string]struct{}{}
	for _, modelID := range modelIDs {
		modelID = normalizeAIStudioModelID(modelID)
		if modelID == "" {
			continue
		}
		if _, ok := seen[modelID]; ok {
			continue
		}
		seen[modelID] = struct{}{}
		normalized = append(normalized, modelID)
	}
	if len(normalized) == 0 {
		return "", false
	}
	for _, preferred := range []string{"gemini-2.5-pro", "gemini-2.5-flash"} {
		for _, candidate := range normalized {
			if candidate == preferred {
				return candidate, true
			}
		}
	}
	sort.Strings(normalized)
	return normalized[0], true
}

func classifyAIStudioStatus(status int, body string) core.FailureReason {
	lower := strings.ToLower(strings.TrimSpace(body))
	switch status {
	case http.StatusUnauthorized:
		return core.FailureAuthPermanent
	case http.StatusForbidden:
		// Distinguish real billing problems from transient quota/rate limits.
		// Google returns 403 for both; only treat explicit billing account
		// issues as billing failures (5h cooldown). Quota exhaustion at 403
		// is effectively a rate limit (15s-2min cooldown).
		if strings.Contains(lower, "billing account") ||
			strings.Contains(lower, "billing enabled") ||
			strings.Contains(lower, "billing not enabled") {
			return core.FailureBilling
		}
		if strings.Contains(lower, "quota") || strings.Contains(lower, "resource exhausted") {
			return core.FailureRateLimit
		}
		return core.FailureAuthPermanent
	case http.StatusTooManyRequests:
		return core.FailureRateLimit
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return core.FailureTimeout
	case http.StatusBadRequest:
		if strings.Contains(lower, "api key not valid") ||
			strings.Contains(lower, "invalid api key") {
			return core.FailureAuthPermanent
		}
		return core.FailureFormat
	case http.StatusNotFound:
		return core.FailureModelNotFound
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

func summarizeAIStudioError(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" {
			return msg
		}
		if status := strings.TrimSpace(payload.Error.Status); status != "" {
			return status
		}
	}
	return summarizeForError(body, 280)
}
