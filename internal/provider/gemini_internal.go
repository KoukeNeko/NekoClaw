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
	"strings"
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
		client = &http.Client{Timeout: 25 * time.Second}
	}
	return &GeminiInternalProvider{
		client:        client,
		endpoints:     endpoints,
		generatePath:  generatePath,
		contextWindow: contextWindow,
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

func (p *GeminiInternalProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	endpointOrder := p.resolveEndpointOrder(req.Account)
	payload := map[string]any{
		"model": strings.TrimSpace(req.Model),
		"request": map[string]any{
			"contents": toGeminiContents(req.Messages),
		},
	}
	if projectID := strings.TrimSpace(req.Account.Metadata["project_id"]); projectID != "" {
		payload["project"] = projectID
	}
	body, _ := json.Marshal(payload)

	var lastErr error
	for _, endpoint := range endpointOrder {
		url := strings.TrimRight(endpoint, "/") + p.generatePath
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Authorization", "Bearer "+req.Account.Token)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("User-Agent", "google-cloud-sdk vscode_cloudshelleditor/0.1")
		httpReq.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
		httpReq.Header.Set("Client-Metadata", `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = &FailureError{Reason: core.FailureUnknown, Message: err.Error(), Endpoint: endpoint}
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			reason := classifyStatus(resp.StatusCode, string(respBody))
			lastErr = &FailureError{
				Reason:   reason,
				Message:  strings.TrimSpace(string(respBody)),
				Endpoint: endpoint,
				Status:   resp.StatusCode,
			}
			if isTransientStatus(resp.StatusCode) {
				continue
			}
			return GenerateResponse{}, lastErr
		}

		text, ok := extractTextFromGeminiResponse(respBody)
		if !ok {
			return GenerateResponse{}, &FailureError{
				Reason:   core.FailureFormat,
				Message:  "gemini internal response did not include text",
				Endpoint: endpoint,
				Status:   resp.StatusCode,
			}
		}
		return GenerateResponse{Text: text, Endpoint: endpoint, Raw: respBody}, nil
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

func (p *GeminiInternalProvider) DiscoverProject(
	ctx context.Context,
	req DiscoverProjectRequest,
) (DiscoverProjectResult, error) {
	if strings.TrimSpace(req.Token) == "" {
		return DiscoverProjectResult{}, fmt.Errorf("token is required")
	}

	projectIDFromEnv := resolveGoogleCloudProject()
	metadata := map[string]any{
		"ideType":    "ANTIGRAVITY",
		"platform":   resolveCodeAssistPlatform(),
		"pluginType": "GEMINI",
	}
	loadBody := map[string]any{
		"metadata": map[string]any{
			"ideType":    "ANTIGRAVITY",
			"platform":   resolveCodeAssistPlatform(),
			"pluginType": "GEMINI",
		},
	}
	if projectIDFromEnv != "" {
		loadBody["cloudaicompanionProject"] = projectIDFromEnv
		metadata["duetProject"] = projectIDFromEnv
		loadMeta, _ := loadBody["metadata"].(map[string]any)
		loadMeta["duetProject"] = projectIDFromEnv
	}

	var activeEndpoint string
	var loadData map[string]any
	endpointOrder := append([]string(nil), p.endpoints...)
	if len(endpointOrder) == 0 {
		endpointOrder = []string{defaultGeminiProdEndpoint, defaultGeminiDailyEndpoint, defaultGeminiAutoEndpoint}
	}
	var loadErr error
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
			loadErr = fmt.Errorf("loadCodeAssist failed: status=%d", status)
			continue
		}
		activeEndpoint = endpoint
		loadData = payload
		loadErr = nil
		break
	}

	if activeEndpoint == "" {
		if projectIDFromEnv != "" {
			return DiscoverProjectResult{
				ProjectID:      projectIDFromEnv,
				ActiveEndpoint: resolveEndpointFallback(endpointOrder),
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
		return DiscoverProjectResult{}, fmt.Errorf("onboardUser failed: status=%d", status)
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
	httpReq.Header.Set("X-Goog-Api-Client", "gl-go/"+runtime.Version())
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
	httpReq.Header.Set("X-Goog-Api-Client", "gl-go/"+runtime.Version())
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
		out = append(out, map[string]any{
			"role": role,
			"parts": []map[string]any{
				{
					"text": text,
				},
			},
		})
	}
	return out
}

func classifyStatus(status int, body string) core.FailureReason {
	if status == http.StatusUnauthorized {
		return core.FailureAuth
	}
	if status == http.StatusForbidden {
		if strings.Contains(strings.ToLower(body), "billing") {
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

func extractTextFromGeminiResponse(body []byte) (string, bool) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", false
	}

	// SSE mode from streamGenerateContent emits lines like: data: {...}
	if strings.Contains(trimmed, "\n") && strings.Contains(trimmed, "data:") {
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
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		chunk := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if chunk == "" || chunk == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(chunk), &payload); err != nil {
			continue
		}
		root := payload
		if response, ok := payload["response"].(map[string]any); ok {
			root = response
		}
		if text, ok := extractTextFromGeminiMap(root); ok {
			joined.WriteString(text)
		}
	}
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

func resolveCodeAssistPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "WINDOWS"
	case "darwin":
		return "MACOS"
	default:
		return "LINUX"
	}
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
