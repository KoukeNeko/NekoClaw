package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

const (
	defaultGeminiProdEndpoint  = "https://cloudcode-pa.googleapis.com"
	defaultGeminiDailyEndpoint = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	defaultGeminiAutoEndpoint  = "https://autopush-cloudcode-pa.sandbox.googleapis.com"

	tierFree     = "free-tier"
	tierLegacy   = "legacy-tier"
	tierStandard = "standard-tier"

	discoveryPollMaxAttempts = 24
	discoveryPollInterval    = 5 * time.Second
	modelDiscoveryTTL        = 10 * time.Minute
)

type GeminiInternalOptions struct {
	Endpoints     []string
	GeneratePath  string
	ContextWindow int
	HTTPClient    *http.Client
}

type GeminiInternalProvider struct {
	client        *http.Client
	endpoints     []string
	generatePath  string
	contextWindow int
	modelCacheMu  sync.Mutex
	modelCache    map[string]geminiModelCacheEntry

	listCacheMu sync.Mutex
	listCache   map[string]geminiListCacheEntry
}

type geminiModelCacheEntry struct {
	Model     string
	Source    string
	ExpiresAt time.Time
}

type geminiListCacheEntry struct {
	Models    []string
	ExpiresAt time.Time
}

type GeminiQuotaResponse struct {
	Buckets []GeminiQuotaBucket `json:"buckets"`
}

type GeminiQuotaBucket struct {
	ModelID           string  `json:"modelId"`
	RemainingFraction float64 `json:"remainingFraction"`
}

type DiscoverProjectRequest struct {
	Token string
}

type DiscoverProjectResult struct {
	ProjectID      string `json:"project_id"`
	ActiveEndpoint string `json:"active_endpoint"`
	TierID         string `json:"tier_id,omitempty"`
}

func NewGeminiInternalProvider(opts GeminiInternalOptions) *GeminiInternalProvider {
	endpoints := sanitizeEndpoints(opts.Endpoints)
	if len(endpoints) == 0 {
		endpoints = []string{defaultGeminiProdEndpoint, defaultGeminiDailyEndpoint, defaultGeminiAutoEndpoint}
	}
	generatePath := strings.TrimSpace(opts.GeneratePath)
	if generatePath == "" {
		generatePath = "/v1internal:streamGenerateContent?alt=sse"
	}
	if !strings.HasPrefix(generatePath, "/") {
		generatePath = "/" + generatePath
	}
	contextWindow := opts.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 1_000_000
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &GeminiInternalProvider{
		client:        client,
		endpoints:     endpoints,
		generatePath:  generatePath,
		contextWindow: contextWindow,
		modelCache:    map[string]geminiModelCacheEntry{},
		listCache:     map[string]geminiListCacheEntry{},
	}
}

func (p *GeminiInternalProvider) ID() string {
	return "google-gemini-cli"
}

func (p *GeminiInternalProvider) ContextWindow(_ string) int {
	return p.contextWindow
}

func (p *GeminiInternalProvider) Endpoints() []string {
	return append([]string(nil), p.endpoints...)
}

func (p *GeminiInternalProvider) ToolCapabilities() ToolCapabilities {
	return ToolCapabilities{
		SupportsTools:         true,
		SupportsParallelCalls: true,
		MaxToolCalls:          8,
	}
}

func (p *GeminiInternalProvider) GenerateToolTurn(ctx context.Context, req ToolTurnRequest) (ToolTurnResponse, error) {
	token := strings.TrimSpace(req.Account.Token)
	if token == "" {
		return ToolTurnResponse{}, &FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing account token",
			Endpoint: strings.Join(p.endpoints, ","),
			Status:   http.StatusUnauthorized,
		}
	}

	modelID := strings.TrimSpace(req.Model)
	if modelID == "" || strings.EqualFold(modelID, "default") {
		modelID = "gemini-3-pro-preview"
	}

	systemInstruction, contents := toGeminiToolContents(req.Messages)
	requestBody := map[string]any{
		"contents": contents,
	}
	if tools := toGeminiFunctionDeclarations(req.Tools); len(tools) > 0 {
		requestBody["tools"] = tools
	}
	if systemInstruction != "" {
		requestBody["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": systemInstruction}},
		}
	}
	if genConfig := buildGeminiGenerationConfig(req.Generation); genConfig != nil {
		requestBody["generationConfig"] = genConfig
	}

	outerPayload := map[string]any{
		"model":   modelID,
		"request": requestBody,
	}
	if projectID := strings.TrimSpace(req.Account.Metadata["project_id"]); projectID != "" {
		outerPayload["project"] = projectID
	}
	body, _ := json.Marshal(outerPayload)

	endpointOrder := p.resolveEndpointOrder(req.Account)

	var lastErr error
	for _, endpoint := range endpointOrder {
		endpointURL := strings.TrimRight(endpoint, "/") + p.generatePath

		resp, err := doWithRetry(ctx, DefaultRetryConfig(), func() (*http.Response, error) {
			httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(body))
			if reqErr != nil {
				return nil, reqErr
			}
			httpReq.Header.Set("Authorization", "Bearer "+token)
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "text/event-stream")
			httpReq.Header.Set("User-Agent", "google-cloud-sdk vscode_cloudshelleditor/0.1")
			httpReq.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
			httpReq.Header.Set("Client-Metadata", `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`)
			return p.client.Do(httpReq)
		}, nil)
		if err != nil {
			lastErr = &FailureError{Reason: core.FailureUnknown, Message: err.Error(), Endpoint: endpoint}
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			reason := classifyStatus(resp.StatusCode, string(respBody))
			lastErr = &FailureError{
				Reason:     reason,
				Message:    strings.TrimSpace(string(respBody)),
				Endpoint:   endpoint,
				Status:     resp.StatusCode,
				RetryAfter: parseRetryAfter(resp),
			}
			if shouldFallbackEndpoint(resp.StatusCode, respBody) {
				continue
			}
			return ToolTurnResponse{}, lastErr
		}

		result := extractToolCallsFromGeminiResponse(respBody)
		if !result.OK {
			lastErr = &FailureError{
				Reason:   core.FailureFormat,
				Message:  "gemini internal tool response did not include text or tool calls: " + summarizeForError(respBody, 280),
				Endpoint: endpoint,
				Status:   resp.StatusCode,
			}
			continue
		}

		stopReason := "end_turn"
		if len(result.Calls) > 0 {
			stopReason = "tool_calls"
		}
		return ToolTurnResponse{
			Text:            result.Text,
			Endpoint:        endpoint,
			Raw:             respBody,
			Usage:           result.Usage,
			StopReason:      stopReason,
			ToolCalls:       result.Calls,
			RawModelContent: result.RawModelContent,
		}, nil
	}

	if lastErr == nil {
		lastErr = &FailureError{Reason: core.FailureUnknown, Message: "gemini tool generate failed", Endpoint: ""}
	}
	return ToolTurnResponse{}, lastErr
}

func (p *GeminiInternalProvider) DiscoverPreferredModel(
	ctx context.Context,
	account core.Account,
) (string, string, error) {
	if strings.TrimSpace(account.Token) == "" {
		return "", "", fmt.Errorf("missing account token")
	}
	cacheKey := strings.TrimSpace(account.ID)
	if cacheKey != "" {
		if model, source, ok := p.loadModelCache(cacheKey); ok {
			return model, source, nil
		}
	}

	model, source := p.discoverPreferredModelNoCache(ctx, account)
	if model == "" {
		model = "gemini-3-pro-preview"
		source = "fallback"
	}
	if source == "" {
		source = "fallback"
	}
	if cacheKey != "" {
		p.storeModelCache(cacheKey, model, source)
	}
	return model, source, nil
}

// ---------------------------------------------------------------------------
// ModelCatalogProvider — dynamic model listing
// ---------------------------------------------------------------------------

// ListModels returns all available model IDs by querying the fetchAvailableModels
// and quota endpoints. Results are cached for 10 minutes.
func (p *GeminiInternalProvider) ListModels(ctx context.Context, account core.Account) ([]string, error) {
	token := strings.TrimSpace(account.Token)
	if token == "" {
		return nil, fmt.Errorf("missing account token")
	}

	cacheKey := strings.TrimSpace(account.ID)
	if cacheKey != "" {
		if cached, ok := p.loadListCache(cacheKey); ok {
			return cached, nil
		}
	}

	models := p.fetchAllModels(ctx, account)
	if len(models) == 0 {
		return nil, fmt.Errorf("no models available from gemini internal endpoints")
	}

	if cacheKey != "" {
		p.storeListCache(cacheKey, models)
	}
	return models, nil
}

// fetchAllModels collects all available model IDs from fetchAvailableModels
// and quota endpoints, returning a deduplicated sorted list.
func (p *GeminiInternalProvider) fetchAllModels(ctx context.Context, account core.Account) []string {
	seen := map[string]struct{}{}
	var models []string

	// Try fetchAvailableModels endpoint first.
	endpointOrder := p.resolveEndpointOrder(account)
	for _, endpoint := range endpointOrder {
		payload, status, err := p.postJSON(
			ctx,
			strings.TrimRight(endpoint, "/")+"/v1internal:fetchAvailableModels",
			account.Token,
			map[string]any{},
			nil,
		)
		if err != nil || status < 200 || status >= 300 {
			continue
		}
		modelsRaw, ok := payload["models"].(map[string]any)
		if !ok || len(modelsRaw) == 0 {
			continue
		}
		for modelID := range modelsRaw {
			trimmed := strings.TrimSpace(modelID)
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; !exists {
				seen[trimmed] = struct{}{}
				models = append(models, trimmed)
			}
		}
		break // success — no need to try other endpoints
	}

	// Also try quota endpoint for additional models.
	quota, err := p.RetrieveQuota(ctx, account.Token)
	if err == nil {
		for _, bucket := range quota.Buckets {
			modelID := strings.TrimSpace(bucket.ModelID)
			if modelID == "" {
				continue
			}
			if _, exists := seen[modelID]; !exists {
				seen[modelID] = struct{}{}
				models = append(models, modelID)
			}
		}
	}

	sort.Strings(models)
	return models
}

func (p *GeminiInternalProvider) loadListCache(cacheKey string) ([]string, bool) {
	p.listCacheMu.Lock()
	defer p.listCacheMu.Unlock()
	entry, ok := p.listCache[cacheKey]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(p.listCache, cacheKey)
		return nil, false
	}
	return entry.Models, true
}

func (p *GeminiInternalProvider) storeListCache(cacheKey string, models []string) {
	p.listCacheMu.Lock()
	defer p.listCacheMu.Unlock()
	p.listCache[cacheKey] = geminiListCacheEntry{
		Models:    models,
		ExpiresAt: time.Now().Add(modelDiscoveryTTL),
	}
}

func (p *GeminiInternalProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	endpointOrder := p.resolveEndpointOrder(req.Account)
	requestBody := map[string]any{
		"contents": toGeminiContents(req.Messages),
	}
	if req.Generation != nil {
		genConfig := map[string]any{}
		if req.Generation.Temperature != nil {
			genConfig["temperature"] = *req.Generation.Temperature
		}
		if req.Generation.TopP != nil {
			genConfig["topP"] = *req.Generation.TopP
		}
		if req.Generation.FrequencyPenalty != nil {
			genConfig["frequencyPenalty"] = *req.Generation.FrequencyPenalty
		}
		if req.Generation.PresencePenalty != nil {
			genConfig["presencePenalty"] = *req.Generation.PresencePenalty
		}
		if len(genConfig) > 0 {
			requestBody["generationConfig"] = genConfig
		}
	}
	payload := map[string]any{
		"model":   strings.TrimSpace(req.Model),
		"request": requestBody,
	}
	if projectID := strings.TrimSpace(req.Account.Metadata["project_id"]); projectID != "" {
		payload["project"] = projectID
	}
	body, _ := json.Marshal(payload)

	var lastErr error
	for _, endpoint := range endpointOrder {
		endpointURL := strings.TrimRight(endpoint, "/") + p.generatePath

		resp, err := doWithRetry(ctx, DefaultRetryConfig(), func() (*http.Response, error) {
			httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(body))
			if reqErr != nil {
				return nil, reqErr
			}
			httpReq.Header.Set("Authorization", "Bearer "+req.Account.Token)
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "text/event-stream")
			httpReq.Header.Set("User-Agent", "google-cloud-sdk vscode_cloudshelleditor/0.1")
			httpReq.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
			httpReq.Header.Set("Client-Metadata", `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`)
			return p.client.Do(httpReq)
		}, nil)
		if err != nil {
			lastErr = &FailureError{Reason: core.FailureUnknown, Message: err.Error(), Endpoint: endpoint}
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			reason := classifyStatus(resp.StatusCode, string(respBody))
			lastErr = &FailureError{
				Reason:     reason,
				Message:    strings.TrimSpace(string(respBody)),
				Endpoint:   endpoint,
				Status:     resp.StatusCode,
				RetryAfter: parseRetryAfter(resp),
			}
			if shouldFallbackEndpoint(resp.StatusCode, respBody) {
				continue
			}
			return GenerateResponse{}, lastErr
		}

		text, ok := extractTextFromGeminiResponse(respBody)
		if !ok {
			lastErr = &FailureError{
				Reason:   core.FailureFormat,
				Message:  "gemini internal response did not include text: " + summarizeForError(respBody, 280),
				Endpoint: endpoint,
				Status:   resp.StatusCode,
			}
			continue
		}
		usage := extractUsageFromGeminiResponse(respBody)
		return GenerateResponse{Text: text, Endpoint: endpoint, Raw: respBody, Usage: usage}, nil
	}

	if lastErr == nil {
		lastErr = &FailureError{Reason: core.FailureUnknown, Message: "gemini generate failed", Endpoint: ""}
	}
	return GenerateResponse{}, lastErr
}

func (p *GeminiInternalProvider) RetrieveQuota(ctx context.Context, token string) (GeminiQuotaResponse, error) {
	url := strings.TrimRight(defaultGeminiProdEndpoint, "/") + "/v1internal:retrieveUserQuota"
	request, _ := json.Marshal(map[string]any{})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(request))
	if err != nil {
		return GeminiQuotaResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return GeminiQuotaResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GeminiQuotaResponse{}, &FailureError{
			Reason:   classifyStatus(resp.StatusCode, string(body)),
			Message:  strings.TrimSpace(string(body)),
			Endpoint: defaultGeminiProdEndpoint,
			Status:   resp.StatusCode,
		}
	}
	var quota GeminiQuotaResponse
	if err := json.Unmarshal(body, &quota); err != nil {
		return GeminiQuotaResponse{}, err
	}
	return quota, nil
}

func (p *GeminiInternalProvider) discoverPreferredModelNoCache(
	ctx context.Context,
	account core.Account,
) (string, string) {
	endpointOrder := p.resolveEndpointOrder(account)
	for _, endpoint := range endpointOrder {
		payload, status, err := p.postJSON(
			ctx,
			strings.TrimRight(endpoint, "/")+"/v1internal:fetchAvailableModels",
			account.Token,
			map[string]any{},
			nil,
		)
		if err != nil || status < 200 || status >= 300 {
			continue
		}
		if model, ok := selectPreferredModelFromFetchAvailable(payload); ok {
			return model, "fetchAvailableModels"
		}
	}

	quota, err := p.RetrieveQuota(ctx, account.Token)
	if err == nil {
		modelIDs := make([]string, 0, len(quota.Buckets))
		for _, bucket := range quota.Buckets {
			if modelID := strings.TrimSpace(bucket.ModelID); modelID != "" {
				modelIDs = append(modelIDs, modelID)
			}
		}
		if model, ok := pickPreferredGeminiModel(modelIDs); ok {
			return model, "quota"
		}
	}
	return "gemini-3-pro-preview", "fallback"
}

func (p *GeminiInternalProvider) loadModelCache(cacheKey string) (string, string, bool) {
	p.modelCacheMu.Lock()
	defer p.modelCacheMu.Unlock()
	entry, ok := p.modelCache[cacheKey]
	if !ok {
		return "", "", false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(p.modelCache, cacheKey)
		return "", "", false
	}
	return entry.Model, entry.Source, true
}

func (p *GeminiInternalProvider) storeModelCache(cacheKey, model, source string) {
	p.modelCacheMu.Lock()
	defer p.modelCacheMu.Unlock()
	p.modelCache[cacheKey] = geminiModelCacheEntry{
		Model:     strings.TrimSpace(model),
		Source:    strings.TrimSpace(source),
		ExpiresAt: time.Now().Add(modelDiscoveryTTL),
	}
}

func (p *GeminiInternalProvider) DiscoverProject(
	ctx context.Context,
	req DiscoverProjectRequest,
) (DiscoverProjectResult, error) {
	if strings.TrimSpace(req.Token) == "" {
		return DiscoverProjectResult{}, fmt.Errorf("token is required")
	}

	projectIDFromEnv := resolveGoogleCloudProject()
	endpointOrder := append([]string(nil), p.endpoints...)
	if len(endpointOrder) == 0 {
		endpointOrder = []string{defaultGeminiProdEndpoint, defaultGeminiDailyEndpoint, defaultGeminiAutoEndpoint}
	}

	var activeEndpoint string
	var loadData map[string]any
	var metadata map[string]any
	var loadErr error
	platformCandidates := resolveCodeAssistPlatformCandidates()
	for _, platform := range platformCandidates {
		metadata = map[string]any{
			"ideType":    "ANTIGRAVITY",
			"platform":   platform,
			"pluginType": "GEMINI",
		}
		loadBody := map[string]any{
			"metadata": map[string]any{
				"ideType":    "ANTIGRAVITY",
				"platform":   platform,
				"pluginType": "GEMINI",
			},
		}
		if projectIDFromEnv != "" {
			loadBody["cloudaicompanionProject"] = projectIDFromEnv
			loadMeta, _ := loadBody["metadata"].(map[string]any)
			loadMeta["duetProject"] = projectIDFromEnv
		}

		activeEndpoint = ""
		loadData = nil
		loadErr = nil
		invalidPlatform := false
		for _, endpoint := range endpointOrder {
			payload, status, err := p.postJSON(ctx, endpoint+"/v1internal:loadCodeAssist", req.Token, loadBody, metadata)
			if err != nil {
				loadErr = err
				continue
			}
			if status < 200 || status >= 300 {
				if isSecurityPolicyViolated(payload) {
					activeEndpoint = endpoint
					loadData = map[string]any{"currentTier": map[string]any{"id": tierStandard}}
					loadErr = nil
					break
				}
				loadErr = fmt.Errorf("loadCodeAssist failed: status=%d%s", status, renderErrorMessageSuffix(payload))
				if isInvalidMetadataPlatform(payload) {
					invalidPlatform = true
					break
				}
				continue
			}
			activeEndpoint = endpoint
			loadData = payload
			loadErr = nil
			break
		}
		if activeEndpoint != "" {
			break
		}
		if !invalidPlatform {
			break
		}
	}

	if activeEndpoint == "" {
		if projectIDFromEnv != "" {
			return DiscoverProjectResult{
				ProjectID:      projectIDFromEnv,
				ActiveEndpoint: "",
			}, nil
		}
		if loadErr != nil {
			return DiscoverProjectResult{}, fmt.Errorf("loadCodeAssist failed on all configured endpoints: %w", loadErr)
		}
		return DiscoverProjectResult{}, fmt.Errorf("loadCodeAssist failed on all configured endpoints")
	}

	if projectID := extractProjectID(loadData); projectID != "" {
		tier := extractTierID(loadData)
		return DiscoverProjectResult{ProjectID: projectID, ActiveEndpoint: activeEndpoint, TierID: tier}, nil
	}

	tier := extractTierID(loadData)
	if hasCurrentTier(loadData) {
		if projectIDFromEnv != "" {
			return DiscoverProjectResult{
				ProjectID:      projectIDFromEnv,
				ActiveEndpoint: activeEndpoint,
				TierID:         tier,
			}, nil
		}
		return DiscoverProjectResult{}, fmt.Errorf(
			"This account requires GOOGLE_CLOUD_PROJECT or GOOGLE_CLOUD_PROJECT_ID to be set.",
		)
	}

	if tier == "" {
		tier = tierLegacy
	}
	if tier != tierFree && projectIDFromEnv == "" {
		return DiscoverProjectResult{}, fmt.Errorf(
			"This account requires GOOGLE_CLOUD_PROJECT or GOOGLE_CLOUD_PROJECT_ID to be set.",
		)
	}

	onboardMetadata := map[string]any{
		"ideType":    "ANTIGRAVITY",
		"platform":   resolveCodeAssistPlatform(),
		"pluginType": "GEMINI",
	}
	onboardBody := map[string]any{
		"tierId":   tier,
		"metadata": onboardMetadata,
	}
	if tier != tierFree && projectIDFromEnv != "" {
		onboardBody["cloudaicompanionProject"] = projectIDFromEnv
		onboardMetadata["duetProject"] = projectIDFromEnv
	}
	payload, status, err := p.postJSON(
		ctx,
		activeEndpoint+"/v1internal:onboardUser",
		req.Token,
		onboardBody,
		metadata,
	)
	if err != nil {
		return DiscoverProjectResult{}, err
	}
	if status < 200 || status >= 300 {
		return DiscoverProjectResult{}, fmt.Errorf("onboardUser failed: status=%d%s", status, renderErrorMessageSuffix(payload))
	}

	projectID := extractProjectID(payload)
	if projectID != "" {
		return DiscoverProjectResult{ProjectID: projectID, ActiveEndpoint: activeEndpoint, TierID: tier}, nil
	}
	if name := extractOperationName(payload); name != "" {
		projectID, pollErr := p.pollOperationProject(ctx, activeEndpoint, req.Token, name, metadata)
		if pollErr == nil && projectID != "" {
			return DiscoverProjectResult{ProjectID: projectID, ActiveEndpoint: activeEndpoint, TierID: tier}, nil
		}
	}
	if projectIDFromEnv != "" {
		return DiscoverProjectResult{ProjectID: projectIDFromEnv, ActiveEndpoint: activeEndpoint, TierID: tier}, nil
	}
	return DiscoverProjectResult{}, fmt.Errorf(
		"Could not discover or provision a Google Cloud project. Set GOOGLE_CLOUD_PROJECT or GOOGLE_CLOUD_PROJECT_ID.",
	)
}

func (p *GeminiInternalProvider) pollOperationProject(
	ctx context.Context,
	endpoint string,
	token string,
	opName string,
	metadata map[string]any,
) (string, error) {
	url := strings.TrimRight(endpoint, "/") + "/v1internal/" + strings.TrimLeft(opName, "/")
	for i := 0; i < discoveryPollMaxAttempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(discoveryPollInterval):
			}
		}
		payload, status, err := p.getJSON(ctx, url, token, metadata)
		if err != nil || status < 200 || status >= 300 {
			continue
		}
		if done, _ := payload["done"].(bool); !done {
			continue
		}
		if response, _ := payload["response"].(map[string]any); response != nil {
			if projectID := extractProjectID(response); projectID != "" {
				return projectID, nil
			}
		}
		if projectID := extractProjectID(payload); projectID != "" {
			return projectID, nil
		}
	}
	return "", fmt.Errorf("operation polling timeout")
}

func (p *GeminiInternalProvider) postJSON(
	ctx context.Context,
	url string,
	token string,
	body map[string]any,
	metadata map[string]any,
) (map[string]any, int, error) {
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "google-api-nodejs-client/9.15.1")
	httpReq.Header.Set("X-Goog-Api-Client", resolveGoogleAPIClientHeader())
	if len(metadata) > 0 {
		if metaRaw, err := json.Marshal(metadata); err == nil {
			httpReq.Header.Set("Client-Metadata", string(metaRaw))
		}
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	content, _ := io.ReadAll(resp.Body)
	payload := map[string]any{}
	_ = json.Unmarshal(content, &payload)
	return payload, resp.StatusCode, nil
}

func (p *GeminiInternalProvider) getJSON(
	ctx context.Context,
	url string,
	token string,
	metadata map[string]any,
) (map[string]any, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("User-Agent", "google-api-nodejs-client/9.15.1")
	httpReq.Header.Set("X-Goog-Api-Client", resolveGoogleAPIClientHeader())
	if len(metadata) > 0 {
		if metaRaw, err := json.Marshal(metadata); err == nil {
			httpReq.Header.Set("Client-Metadata", string(metaRaw))
		}
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	content, _ := io.ReadAll(resp.Body)
	payload := map[string]any{}
	_ = json.Unmarshal(content, &payload)
	return payload, resp.StatusCode, nil
}

func sanitizeEndpoints(endpoints []string) []string {
	if len(endpoints) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		normalized := strings.TrimSpace(strings.TrimRight(endpoint, "/"))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func selectPreferredModelFromFetchAvailable(payload map[string]any) (string, bool) {
	modelsRaw, ok := payload["models"].(map[string]any)
	if !ok || len(modelsRaw) == 0 {
		return "", false
	}
	modelIDs := make([]string, 0, len(modelsRaw))
	for modelID := range modelsRaw {
		trimmed := strings.TrimSpace(modelID)
		if trimmed != "" {
			modelIDs = append(modelIDs, trimmed)
		}
	}
	return pickPreferredGeminiModel(modelIDs)
}

func pickPreferredGeminiModel(modelIDs []string) (string, bool) {
	priority := []string{
		"gemini-3-pro-preview",
		"gemini-2.5-pro",
		"gemini-3-flash-preview",
		"gemini-2.5-flash",
	}
	normalized := make([]string, 0, len(modelIDs))
	seen := map[string]struct{}{}
	for _, modelID := range modelIDs {
		m := strings.TrimSpace(modelID)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		normalized = append(normalized, m)
	}
	if len(normalized) == 0 {
		return "", false
	}
	for _, target := range priority {
		for _, candidate := range normalized {
			if candidate == target {
				return candidate, true
			}
		}
	}
	// Keep deterministic fallback if only unknown IDs are available.
	sort.Strings(normalized)
	return normalized[0], true
}

func (p *GeminiInternalProvider) resolveEndpointOrder(account core.Account) []string {
	preferred := strings.TrimSpace(account.Metadata["endpoint"])
	return p.resolveEndpointOrderFromPreference(preferred)
}

func (p *GeminiInternalProvider) resolveEndpointOrderFromPreference(preferred string) []string {
	preferred = strings.TrimSpace(strings.TrimRight(preferred, "/"))
	if preferred == "" {
		return append([]string(nil), p.endpoints...)
	}
	out := []string{preferred}
	for _, endpoint := range p.endpoints {
		if endpoint == preferred {
			continue
		}
		out = append(out, endpoint)
	}
	return sanitizeEndpoints(out)
}

func toGeminiContents(messages []core.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Role == core.RoleAssistant {
			role = "model"
		}
		text := strings.TrimSpace(msg.Content)

		// Build parts list: images first (inline_data), then text.
		parts := make([]map[string]any, 0, len(msg.Images)+1)
		for _, img := range msg.Images {
			parts = append(parts, map[string]any{
				"inline_data": map[string]any{
					"mime_type": img.MimeType,
					"data":      img.Data,
				},
			})
		}
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

func classifyStatus(status int, body string) core.FailureReason {
	if status == http.StatusUnauthorized {
		return core.FailureAuth
	}
	if status == http.StatusForbidden {
		lower := strings.ToLower(body)
		if strings.Contains(lower, "service_disabled") ||
			strings.Contains(lower, "staging-cloudaicompanion") ||
			strings.Contains(lower, "autopush-cloudcode-pa.sandbox.googleapis.com") {
			// Treat sandbox API/service-disabled errors as endpoint/profile-scoped.
			// They should rotate/fallback instead of permanently disabling the profile.
			return core.FailureAuth
		}
		if strings.Contains(lower, "billing") {
			return core.FailureBilling
		}
		return core.FailureAuthPermanent
	}
	if status == http.StatusTooManyRequests {
		return core.FailureRateLimit
	}
	if status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout {
		return core.FailureTimeout
	}
	if status == http.StatusNotFound {
		return core.FailureModelNotFound
	}
	if status >= 400 && status < 500 {
		return core.FailureFormat
	}
	return core.FailureUnknown
}

func isTransientStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

func shouldFallbackEndpoint(status int, body []byte) bool {
	if isTransientStatus(status) {
		return true
	}
	if status != http.StatusForbidden {
		return false
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "service_disabled") ||
		strings.Contains(lower, "staging-cloudaicompanion") ||
		strings.Contains(lower, "autopush-cloudcode-pa.sandbox.googleapis.com")
}

// extractUsageFromGeminiResponse parses usageMetadata from Gemini API responses.
// For SSE streams, it takes the last event's usage (which contains cumulative totals).
func extractUsageFromGeminiResponse(body []byte) core.UsageInfo {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return core.UsageInfo{}
	}
	if strings.Contains(trimmed, "data:") {
		return extractUsageFromGeminiSSE(trimmed)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return core.UsageInfo{}
	}
	if response, ok := root["response"].(map[string]any); ok {
		return parseUsageMetadata(response)
	}
	return parseUsageMetadata(root)
}

func extractUsageFromGeminiSSE(raw string) core.UsageInfo {
	lines := strings.Split(raw, "\n")
	var usage core.UsageInfo
	var eventData []string
	flush := func() {
		if len(eventData) == 0 {
			return
		}
		chunk := strings.TrimSpace(strings.Join(eventData, "\n"))
		eventData = eventData[:0]
		if chunk == "" || chunk == "[DONE]" {
			return
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(chunk), &payload); err != nil {
			return
		}
		root := payload
		if response, ok := payload["response"].(map[string]any); ok {
			root = response
		}
		if u := parseUsageMetadata(root); u.TotalTokens > 0 {
			usage = u
		}
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			flush()
			continue
		}
		if !strings.HasPrefix(t, "data:") {
			continue
		}
		eventData = append(eventData, strings.TrimSpace(strings.TrimPrefix(t, "data:")))
	}
	flush()
	return usage
}

func parseUsageMetadata(root map[string]any) core.UsageInfo {
	meta, ok := root["usageMetadata"].(map[string]any)
	if !ok {
		return core.UsageInfo{}
	}
	return core.UsageInfo{
		InputTokens:  jsonInt(meta, "promptTokenCount"),
		OutputTokens: jsonInt(meta, "candidatesTokenCount"),
		TotalTokens:  jsonInt(meta, "totalTokenCount"),
	}
}

func jsonInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func extractTextFromGeminiResponse(body []byte) (string, bool) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", false
	}

	// SSE mode from streamGenerateContent emits lines like: data: {...}
	if strings.Contains(trimmed, "data:") {
		if text, ok := extractTextFromGeminiSSE(trimmed); ok {
			return text, true
		}
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return "", false
	}
	if response, ok := root["response"].(map[string]any); ok {
		if text, ok := extractTextFromGeminiMap(response); ok {
			return text, true
		}
	}
	return extractTextFromGeminiMap(root)
}

func extractTextFromGeminiSSE(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	var joined strings.Builder
	var eventData []string
	flush := func() {
		if len(eventData) == 0 {
			return
		}
		chunk := strings.TrimSpace(strings.Join(eventData, "\n"))
		eventData = eventData[:0]
		if chunk == "" || chunk == "[DONE]" {
			return
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(chunk), &payload); err != nil {
			return
		}
		root := payload
		if response, ok := payload["response"].(map[string]any); ok {
			root = response
		}
		if text, ok := extractTextFromGeminiMap(root); ok {
			joined.WriteString(text)
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			continue
		}
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		eventData = append(eventData, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
	}
	flush()

	result := strings.TrimSpace(joined.String())
	if result == "" {
		return "", false
	}
	return result, true
}

func extractTextFromGeminiMap(root map[string]any) (string, bool) {
	if root == nil {
		return "", false
	}
	if v, ok := root["reply"].(string); ok && strings.TrimSpace(v) != "" {
		return v, true
	}
	if v, ok := root["text"].(string); ok && strings.TrimSpace(v) != "" {
		return v, true
	}
	if cand, ok := root["candidates"].([]any); ok {
		for _, raw := range cand {
			candidate, _ := raw.(map[string]any)
			if candidate == nil {
				continue
			}
			content, _ := candidate["content"].(map[string]any)
			if content == nil {
				continue
			}
			parts, _ := content["parts"].([]any)
			for _, partRaw := range parts {
				part, _ := partRaw.(map[string]any)
				if part == nil {
					continue
				}
				if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
					return text, true
				}
			}
		}
	}
	return "", false
}

func summarizeForError(body []byte, limit int) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "<empty>"
	}
	if limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:limit]) + "..."
}

func extractProjectID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if s, ok := payload["cloudaicompanionProject"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	if m, ok := payload["cloudaicompanionProject"].(map[string]any); ok {
		if id, ok := m["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	}
	if r, ok := payload["response"].(map[string]any); ok {
		return extractProjectID(r)
	}
	return ""
}

func extractTierID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if current, ok := payload["currentTier"].(string); ok && strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current)
	}
	if current, ok := payload["currentTier"].(map[string]any); ok {
		if id, ok := current["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	}
	if allowed, ok := payload["allowedTiers"].([]any); ok {
		for _, item := range allowed {
			tier, _ := item.(map[string]any)
			if tier == nil {
				continue
			}
			if isDefault, _ := tier["isDefault"].(bool); isDefault {
				if id, ok := tier["id"].(string); ok {
					return strings.TrimSpace(id)
				}
			}
		}
		return tierLegacy
	}
	return ""
}

func hasCurrentTier(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	_, ok := payload["currentTier"]
	return ok
}

func resolveGoogleCloudProject() string {
	if project := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")); project != "" {
		return project
	}
	if project := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT_ID")); project != "" {
		return project
	}
	return ""
}

func resolveGoogleAPIClientHeader() string {
	// OpenClaw's Gemini OAuth extension sends "gl-node/<version>" here.
	// Keep the same shape to match backend heuristics.
	if explicit := strings.TrimSpace(os.Getenv("NEKOCLAW_GEMINI_X_GOOG_API_CLIENT")); explicit != "" {
		return explicit
	}
	if nodeVersion := strings.TrimPrefix(strings.TrimSpace(os.Getenv("NODE_VERSION")), "v"); nodeVersion != "" {
		return "gl-node/" + nodeVersion
	}
	return "gl-node/20.0.0"
}

func resolveCodeAssistPlatform() string {
	candidates := resolveCodeAssistPlatformCandidates()
	if len(candidates) == 0 {
		return "PLATFORM_UNSPECIFIED"
	}
	return candidates[0]
}

func resolveCodeAssistPlatformCandidates() []string {
	return resolveCodeAssistPlatformCandidatesFor(runtime.GOOS, runtime.GOARCH)
}

func resolveCodeAssistPlatformCandidatesFor(goos, goarch string) []string {
	normalizedGOOS := strings.ToLower(strings.TrimSpace(goos))
	normalizedGOARCH := strings.ToLower(strings.TrimSpace(goarch))
	primary := resolveCodeAssistPlatformFor(normalizedGOOS, normalizedGOARCH)
	candidates := []string{primary}
	switch normalizedGOOS {
	case "darwin":
		switch normalizedGOARCH {
		case "amd64":
			candidates = append(candidates, "DARWIN_AMD64")
		case "arm64":
			candidates = append(candidates, "DARWIN_ARM64")
		}
	case "linux":
		switch normalizedGOARCH {
		case "amd64":
			candidates = append(candidates, "LINUX_AMD64")
		case "arm64":
			candidates = append(candidates, "LINUX_ARM64")
		}
	case "windows":
		if normalizedGOARCH == "amd64" {
			candidates = append(candidates, "WINDOWS_AMD64")
		}
	}
	candidates = append(candidates, "PLATFORM_UNSPECIFIED")
	out := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{"PLATFORM_UNSPECIFIED"}
	}
	return out
}

func resolveCodeAssistPlatformFor(goos, _ string) string {
	switch goos {
	case "darwin":
		return "MACOS"
	case "linux":
		return "LINUX"
	case "windows":
		return "WINDOWS"
	}
	return "PLATFORM_UNSPECIFIED"
}

func extractOperationName(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if done, _ := payload["done"].(bool); done {
		return ""
	}
	if name, ok := payload["name"].(string); ok {
		return strings.TrimSpace(name)
	}
	return ""
}

func isSecurityPolicyViolated(payload map[string]any) bool {
	errRoot, _ := payload["error"].(map[string]any)
	if errRoot == nil {
		return false
	}
	details, _ := errRoot["details"].([]any)
	for _, item := range details {
		detail, _ := item.(map[string]any)
		if detail == nil {
			continue
		}
		if reason, _ := detail["reason"].(string); strings.TrimSpace(reason) == "SECURITY_POLICY_VIOLATED" {
			return true
		}
	}
	return false
}

func isInvalidMetadataPlatform(payload map[string]any) bool {
	message := strings.ToLower(strings.TrimSpace(extractErrorMessage(payload)))
	if message == "" {
		return false
	}
	return strings.Contains(message, "metadata.platform") ||
		strings.Contains(message, "clientmetadata.platform")
}

func renderErrorMessageSuffix(payload map[string]any) string {
	if message := extractErrorMessage(payload); message != "" {
		return ": " + message
	}
	return ""
}

func extractErrorMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	errRoot, _ := payload["error"].(map[string]any)
	if errRoot == nil {
		return ""
	}
	if message, _ := errRoot["message"].(string); strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	return ""
}
