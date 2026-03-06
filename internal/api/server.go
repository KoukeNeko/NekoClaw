package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/mcp"
	"github.com/doeshing/nekoclaw/internal/persona"
	"github.com/doeshing/nekoclaw/internal/provider"
	"github.com/doeshing/nekoclaw/internal/tooling"
)

type Server struct {
	svc *app.Service
}

func NewServer(svc *app.Service) *Server {
	return &Server{svc: svc}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/oauth2callback", s.handleOAuthCallback)
	mux.HandleFunc("/v1/providers", s.handleProviders)
	mux.HandleFunc("/v1/accounts", s.handleAccounts)
	mux.HandleFunc("/v1/chat", s.handleChat)
	mux.HandleFunc("/v1/integrations/discord/events", s.handleDiscordEvent)
	mux.HandleFunc("/v1/gemini/quota", s.handleGeminiQuota)
	mux.HandleFunc("/v1/gemini/discover-project", s.handleGeminiDiscoverProject)
	mux.HandleFunc("/v1/auth/gemini/start", s.handleGeminiAuthStart)
	mux.HandleFunc("/v1/auth/gemini/manual/complete", s.handleGeminiAuthManualComplete)
	mux.HandleFunc("/v1/auth/gemini/profiles", s.handleGeminiAuthProfiles)
	mux.HandleFunc("/v1/auth/gemini/use", s.handleGeminiAuthUse)
	mux.HandleFunc("/v1/auth/ai-studio/add-key", s.handleAIStudioAddKey)
	mux.HandleFunc("/v1/auth/ai-studio/profiles", s.handleAIStudioProfiles)
	mux.HandleFunc("/v1/auth/ai-studio/use", s.handleAIStudioUse)
	mux.HandleFunc("/v1/auth/ai-studio/delete", s.handleAIStudioDelete)
	mux.HandleFunc("/v1/models", s.handleListModels)
	mux.HandleFunc("/v1/fallbacks", s.handleFallbacks)
	mux.HandleFunc("/v1/discord/config", s.handleDiscordConfig)
	mux.HandleFunc("/v1/telegram/config", s.handleTelegramConfig)
	mux.HandleFunc("/v1/tools/config", s.handleToolsConfig)
	mux.HandleFunc("/v1/default-provider", s.handleDefaultProvider)
	mux.HandleFunc("/v1/ai-studio/models", s.handleAIStudioModels)
	mux.HandleFunc("/v1/auth/anthropic/add-token", s.handleAnthropicAddToken)
	mux.HandleFunc("/v1/auth/anthropic/add-api-key", s.handleAnthropicAddAPIKey)
	mux.HandleFunc("/v1/auth/anthropic/profiles", s.handleAnthropicProfiles)
	mux.HandleFunc("/v1/auth/anthropic/use", s.handleAnthropicUse)
	mux.HandleFunc("/v1/auth/anthropic/delete", s.handleAnthropicDelete)
	mux.HandleFunc("/v1/auth/anthropic/browser/start", s.handleAnthropicBrowserStart)
	mux.HandleFunc("/v1/auth/anthropic/browser/manual/complete", s.handleAnthropicBrowserManualComplete)
	mux.HandleFunc("/v1/auth/anthropic/browser/cancel", s.handleAnthropicBrowserCancel)
	mux.HandleFunc("/v1/auth/anthropic/browser/jobs/", s.handleAnthropicBrowserJob)
	mux.HandleFunc("/v1/auth/openai/add-key", s.handleOpenAIAddKey)
	mux.HandleFunc("/v1/auth/openai-codex/add-token", s.handleOpenAICodexAddToken)
	mux.HandleFunc("/v1/auth/openai-codex/browser/start", s.handleOpenAICodexBrowserStart)
	mux.HandleFunc("/v1/auth/openai-codex/browser/manual/complete", s.handleOpenAICodexBrowserManualComplete)
	mux.HandleFunc("/v1/auth/openai-codex/browser/cancel", s.handleOpenAICodexBrowserCancel)
	mux.HandleFunc("/v1/auth/openai-codex/browser/jobs/", s.handleOpenAICodexBrowserJob)
	mux.HandleFunc("/v1/auth/openai/profiles", s.handleOpenAIProfiles)
	mux.HandleFunc("/v1/auth/openai-codex/profiles", s.handleOpenAICodexProfiles)
	mux.HandleFunc("/v1/auth/openai/use", s.handleOpenAIUse)
	mux.HandleFunc("/v1/auth/openai-codex/use", s.handleOpenAICodexUse)
	mux.HandleFunc("/v1/auth/openai/delete", s.handleOpenAIDelete)
	mux.HandleFunc("/v1/auth/openai-codex/delete", s.handleOpenAICodexDelete)
	mux.HandleFunc("/v1/sessions", s.handleSessions)
	mux.HandleFunc("/v1/sessions/delete", s.handleSessionDelete)
	mux.HandleFunc("/v1/sessions/rename", s.handleSessionRename)
	mux.HandleFunc("/v1/sessions/transcript", s.handleSessionTranscript)
	mux.HandleFunc("/v1/memory/search", s.handleMemorySearch)
	mux.HandleFunc("/v1/mcp/servers", s.handleMCPServers)
	mux.HandleFunc("/v1/mcp/tools", s.handleMCPTools)
	mux.HandleFunc("/v1/mcp/builtin", s.handleMCPBuiltin)
	mux.HandleFunc("/v1/mcp/builtin/toggle", s.handleMCPBuiltinToggle)
	mux.HandleFunc("/v1/personas", s.handlePersonas)
	mux.HandleFunc("/v1/personas/active", s.handlePersonaActive)
	mux.HandleFunc("/v1/personas/use", s.handlePersonaUse)
	mux.HandleFunc("/v1/personas/clear", s.handlePersonaClear)
	mux.HandleFunc("/v1/personas/reload", s.handlePersonaReload)
	mux.HandleFunc("/v1/tool-status", s.handleToolStatus)
	return mux
}

func (s *Server) Run(ctx context.Context, addr string) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "nekoclaw"})
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondPlain(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if oauthErr := strings.TrimSpace(r.URL.Query().Get("error")); oauthErr != "" {
		respondPlain(w, http.StatusBadRequest, "OAuth failed: "+oauthErr)
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if state == "" || code == "" {
		respondPlain(w, http.StatusBadRequest, "Missing state or code")
		return
	}
	_, err := s.svc.CompleteGeminiOAuthFromCallback(r.Context(), state, code)
	if err != nil {
		if errors.Is(err, auth.ErrStateMismatch) ||
			errors.Is(err, auth.ErrStateNotFound) ||
			errors.Is(err, auth.ErrStateExpired) ||
			errors.Is(err, auth.ErrStateConsumed) {
			respondPlain(w, http.StatusBadRequest, "State mismatch. Please restart OAuth login.")
			return
		}
		if errors.Is(err, provider.ErrProjectDiscoveryFailed) {
			respondGeminiProjectDiscoveryError(w, err)
			return
		}
		respondPlain(w, http.StatusBadGateway, "OAuth completion failed: "+err.Error())
		return
	}
	respondPlain(w, http.StatusOK, "Gemini OAuth complete. You can close this window.")
}

func (s *Server) handleProviders(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{"providers": s.svc.Providers()})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	providerID := strings.TrimSpace(r.URL.Query().Get("provider"))
	if providerID == "" {
		respondError(w, http.StatusBadRequest, "provider query is required")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"accounts": s.svc.Accounts(providerID)})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req core.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Surface == "" {
		req.Surface = core.SurfaceTUI
	}
	resp, err := s.svc.HandleChat(r.Context(), req)
	if err != nil {
		respondChatError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

type discordEvent struct {
	SessionID string `json:"session_id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Message   string `json:"message"`
}

func (s *Server) handleDiscordEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload discordEvent
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid discord payload")
		return
	}
	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("discord:%s", payload.ChannelID)
	}
	providerID := strings.TrimSpace(payload.Provider)
	resp, err := s.svc.HandleChat(r.Context(), core.ChatRequest{
		SessionID: sessionID,
		Surface:   core.SurfaceDiscord,
		Provider:  payload.Provider,
		Model:     payload.Model,
		Message:   payload.Message,
	})
	if err != nil {
		if providerID == "google-gemini-cli" && errors.Is(err, app.ErrGeminiMissingProject) {
			respondJSON(w, http.StatusOK, map[string]any{
				"session_id": sessionID,
				"provider":   providerID,
				"reply":      "Gemini profiles 缺少 project_id。請重新 OAuth 或設定 GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_PROJECT_ID。",
				"is_error":   true,
			})
			return
		}
		if providerID == "google-gemini-cli" && errors.Is(err, app.ErrNoAvailableAccount) {
			respondJSON(w, http.StatusOK, map[string]any{
				"session_id": sessionID,
				"provider":   providerID,
				"reply":      "Gemini 尚未登入或所有 profile 暫時不可用。請先在 TUI 或 API 完成 OAuth：POST /v1/auth/gemini/start",
				"is_error":   true,
			})
			return
		}
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGeminiQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	providerID := strings.TrimSpace(r.URL.Query().Get("provider"))
	if providerID == "" {
		providerID = "google-gemini-cli"
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	profileID := strings.TrimSpace(r.URL.Query().Get("profile_id"))
	if profileID != "" && accountID == "" {
		accountID = profileID
	}
	geminiProvider, account, err := s.resolveGeminiProviderAndAccount(providerID, accountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	quota, err := geminiProvider.RetrieveQuota(r.Context(), account.Token)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"provider": providerID,
		"account":  account.ID,
		"quota":    quota,
	})
}

type discoverProjectRequest struct {
	Provider  string `json:"provider"`
	AccountID string `json:"account_id"`
}

func (s *Server) handleGeminiDiscoverProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req discoverProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	providerID := strings.TrimSpace(req.Provider)
	if providerID == "" {
		providerID = "google-gemini-cli"
	}
	geminiProvider, account, err := s.resolveGeminiProviderAndAccount(providerID, req.AccountID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := geminiProvider.DiscoverProject(r.Context(), provider.DiscoverProjectRequest{
		Token: account.Token,
	})
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"provider": providerID,
		"account":  account.ID,
		"result":   result,
	})
}

func (s *Server) handleGeminiAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.GeminiOAuthStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.StartGeminiOAuth(r.Context(), req)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleGeminiAuthManualComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.GeminiOAuthCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.CompleteGeminiOAuthManual(r.Context(), req)
	if err != nil {
		if errors.Is(err, provider.ErrProjectDiscoveryFailed) {
			respondGeminiProjectDiscoveryError(w, err)
			return
		}
		status := http.StatusBadGateway
		if errors.Is(err, auth.ErrStateMismatch) || errors.Is(err, auth.ErrStateExpired) || errors.Is(err, auth.ErrStateNotFound) {
			status = http.StatusBadRequest
		}
		respondError(w, status, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleGeminiAuthProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	profiles, err := s.svc.ListGeminiProfiles()
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

type useProfileRequest struct {
	ProfileID string `json:"profile_id"`
}

func (s *Server) handleGeminiAuthUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.UseGeminiProfile(req.ProfileID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "profile_id": strings.TrimSpace(req.ProfileID)})
}

func (s *Server) handleAIStudioAddKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.AIStudioAddKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.AddAIStudioKey(r.Context(), req)
	if err != nil {
		respondAIStudioError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleAIStudioProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	profiles, err := s.svc.ListAIStudioProfiles()
	if err != nil {
		respondAIStudioError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (s *Server) handleAIStudioUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.UseAIStudioProfile(req.ProfileID); err != nil {
		respondAIStudioError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"profile_id": strings.TrimSpace(req.ProfileID),
		"provider":   "google-ai-studio",
	})
}

func (s *Server) handleAIStudioDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.DeleteAIStudioProfile(req.ProfileID); err != nil {
		respondAIStudioError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"profile_id": strings.TrimSpace(req.ProfileID),
		"provider":   "google-ai-studio",
	})
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	providerID := strings.TrimSpace(r.URL.Query().Get("provider"))
	if providerID == "" {
		respondError(w, http.StatusBadRequest, "provider query parameter is required")
		return
	}
	profileID := strings.TrimSpace(r.URL.Query().Get("profile_id"))
	result, err := s.svc.ListModels(r.Context(), providerID, profileID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleAIStudioModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := s.svc.ListAIStudioModels(r.Context(), strings.TrimSpace(r.URL.Query().Get("profile_id")))
	if err != nil {
		respondAIStudioError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleAnthropicAddToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.AnthropicAddTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.AddAnthropicToken(r.Context(), req)
	if err != nil {
		respondAnthropicError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleAnthropicAddAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.AnthropicAddAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.AddAnthropicAPIKey(r.Context(), req)
	if err != nil {
		respondAnthropicError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleAnthropicProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	profiles, err := s.svc.ListAnthropicProfiles()
	if err != nil {
		respondAnthropicError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (s *Server) handleAnthropicUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.UseAnthropicProfile(req.ProfileID); err != nil {
		respondAnthropicError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"profile_id": strings.TrimSpace(req.ProfileID),
		"provider":   "anthropic",
	})
}

func (s *Server) handleAnthropicDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.DeleteAnthropicProfile(req.ProfileID); err != nil {
		respondAnthropicError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"profile_id": strings.TrimSpace(req.ProfileID),
		"provider":   "anthropic",
	})
}

func (s *Server) handleAnthropicBrowserStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.AnthropicBrowserStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.StartAnthropicBrowserLogin(r.Context(), req)
	if err != nil {
		respondAnthropicBrowserError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleAnthropicBrowserJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path := strings.TrimSpace(r.URL.Path)
	prefix := "/v1/auth/anthropic/browser/jobs/"
	if !strings.HasPrefix(path, prefix) {
		respondError(w, http.StatusNotFound, "not found")
		return
	}
	jobID := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if jobID == "" {
		respondError(w, http.StatusBadRequest, "job_id is required")
		return
	}
	result, err := s.svc.GetAnthropicBrowserLoginJob(r.Context(), jobID)
	if err != nil {
		respondAnthropicBrowserError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleAnthropicBrowserManualComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.AnthropicBrowserManualCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.CompleteAnthropicBrowserLoginManual(r.Context(), req)
	if err != nil {
		respondAnthropicBrowserError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleAnthropicBrowserCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.CancelAnthropicBrowserLogin(r.Context(), req.JobID)
	if err != nil {
		respondAnthropicBrowserError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"job_id": result.JobID,
		"status": result.Status,
	})
}

func (s *Server) handleOpenAIAddKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.OpenAIAddKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.AddOpenAIKey(r.Context(), req)
	if err != nil {
		respondOpenAIError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleOpenAICodexAddToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.OpenAICodexAddTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.AddOpenAICodexToken(r.Context(), req)
	if err != nil {
		respondOpenAIError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleOpenAICodexBrowserStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.OpenAICodexBrowserStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.StartOpenAICodexBrowserLogin(r.Context(), req)
	if err != nil {
		respondOpenAICodexBrowserError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleOpenAICodexBrowserJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path := strings.TrimSpace(r.URL.Path)
	prefix := "/v1/auth/openai-codex/browser/jobs/"
	if !strings.HasPrefix(path, prefix) {
		respondError(w, http.StatusNotFound, "not found")
		return
	}
	jobID := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if jobID == "" {
		respondError(w, http.StatusBadRequest, "job_id is required")
		return
	}
	result, err := s.svc.GetOpenAICodexBrowserLoginJob(r.Context(), jobID)
	if err != nil {
		respondOpenAICodexBrowserError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleOpenAICodexBrowserManualComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req app.OpenAICodexBrowserManualCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.CompleteOpenAICodexBrowserLoginManual(r.Context(), req)
	if err != nil {
		respondOpenAICodexBrowserError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleOpenAICodexBrowserCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	result, err := s.svc.CancelOpenAICodexBrowserLogin(r.Context(), req.JobID)
	if err != nil {
		respondOpenAICodexBrowserError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"job_id": result.JobID,
		"status": result.Status,
	})
}

func (s *Server) handleOpenAIProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	profiles, err := s.svc.ListOpenAIProfiles("openai")
	if err != nil {
		respondOpenAIError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (s *Server) handleOpenAICodexProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	profiles, err := s.svc.ListOpenAIProfiles("openai-codex")
	if err != nil {
		respondOpenAIError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (s *Server) handleOpenAIUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.UseOpenAIProfile("openai", req.ProfileID); err != nil {
		respondOpenAIError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"profile_id": strings.TrimSpace(req.ProfileID),
		"provider":   "openai",
	})
}

func (s *Server) handleOpenAICodexUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.UseOpenAIProfile("openai-codex", req.ProfileID); err != nil {
		respondOpenAIError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"profile_id": strings.TrimSpace(req.ProfileID),
		"provider":   "openai-codex",
	})
}

func (s *Server) handleOpenAIDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.DeleteOpenAIProfile("openai", req.ProfileID); err != nil {
		respondOpenAIError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"profile_id": strings.TrimSpace(req.ProfileID),
		"provider":   "openai",
	})
}

func (s *Server) handleOpenAICodexDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req useProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.DeleteOpenAIProfile("openai-codex", req.ProfileID); err != nil {
		respondOpenAIError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"profile_id": strings.TrimSpace(req.ProfileID),
		"provider":   "openai-codex",
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sessions := s.svc.ListSessions()
	respondJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

type deleteSessionRequest struct {
	SessionID string `json:"session_id"`
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req deleteSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	if err := s.svc.DeleteSession(sessionID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID})
}

type renameSessionRequest struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

func (s *Server) handleSessionRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req renameSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := s.svc.RenameSession(req.SessionID, req.Title); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"session_id": strings.TrimSpace(req.SessionID),
		"title":      strings.TrimSpace(req.Title),
	})
}

func (s *Server) handleSessionTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	messages := s.svc.GetSessionTranscript(sessionID)
	respondJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

type memorySearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (s *Server) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req memorySearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		respondError(w, http.StatusBadRequest, "query is required")
		return
	}
	results, err := s.svc.SearchMemory(query, req.Limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"results": results})
}

func respondAIStudioError(w http.ResponseWriter, err error) {
	if err == nil {
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch {
	case errors.Is(err, app.ErrInvalidAPIKey):
		respondErrorDetail(w, http.StatusBadRequest, "invalid_api_key", err.Error())
	case errors.Is(err, app.ErrKeyValidationFailed):
		respondErrorDetail(w, http.StatusBadRequest, "key_validation_failed", err.Error())
	case errors.Is(err, app.ErrProfileNotFound):
		respondErrorDetail(w, http.StatusNotFound, "profile_not_found", err.Error())
	case errors.Is(err, app.ErrProfileInUse):
		respondErrorDetail(w, http.StatusConflict, "profile_in_use", err.Error())
	case errors.Is(err, app.ErrProviderNotReady):
		respondErrorDetail(w, http.StatusServiceUnavailable, "provider_not_ready", err.Error())
	case errors.Is(err, app.ErrNoAvailableAccount):
		respondErrorDetail(w, http.StatusConflict, "provider_not_ready", err.Error())
	default:
		var failureErr *provider.FailureError
		if errors.As(err, &failureErr) {
			if failureErr.Reason == core.FailureAuthPermanent && (failureErr.Status == http.StatusUnauthorized || failureErr.Status == http.StatusBadRequest) {
				respondErrorDetail(w, http.StatusBadRequest, "invalid_api_key", err.Error())
				return
			}
		}
		respondError(w, http.StatusBadGateway, err.Error())
	}
}

func respondAnthropicError(w http.ResponseWriter, err error) {
	if err == nil {
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch {
	case errors.Is(err, app.ErrInvalidSetupToken):
		respondErrorDetail(w, http.StatusBadRequest, "invalid_setup_token", err.Error())
	case errors.Is(err, app.ErrInvalidAPIKey):
		respondErrorDetail(w, http.StatusBadRequest, "invalid_api_key", err.Error())
	case errors.Is(err, app.ErrProfileNotFound):
		respondErrorDetail(w, http.StatusNotFound, "profile_not_found", err.Error())
	case errors.Is(err, app.ErrProfileInUse):
		respondErrorDetail(w, http.StatusConflict, "profile_in_use", err.Error())
	case errors.Is(err, app.ErrProviderNotReady):
		respondErrorDetail(w, http.StatusServiceUnavailable, "provider_not_ready", err.Error())
	case errors.Is(err, app.ErrNoAvailableAccount):
		respondErrorDetail(w, http.StatusConflict, "provider_not_ready", err.Error())
	default:
		respondError(w, http.StatusBadGateway, err.Error())
	}
}

func respondOpenAIError(w http.ResponseWriter, err error) {
	if err == nil {
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch {
	case errors.Is(err, app.ErrInvalidAPIKey):
		respondErrorDetail(w, http.StatusBadRequest, "invalid_api_key", err.Error())
	case errors.Is(err, app.ErrInvalidOAuthToken):
		respondErrorDetail(w, http.StatusBadRequest, "invalid_oauth_token", err.Error())
	case errors.Is(err, app.ErrProfileNotFound):
		respondErrorDetail(w, http.StatusNotFound, "profile_not_found", err.Error())
	case errors.Is(err, app.ErrProfileInUse):
		respondErrorDetail(w, http.StatusConflict, "profile_in_use", err.Error())
	case errors.Is(err, app.ErrProviderNotReady):
		respondErrorDetail(w, http.StatusServiceUnavailable, "provider_not_ready", err.Error())
	case errors.Is(err, app.ErrNoAvailableAccount):
		respondErrorDetail(w, http.StatusConflict, "provider_not_ready", err.Error())
	default:
		respondError(w, http.StatusBadGateway, err.Error())
	}
}

func respondAnthropicBrowserError(w http.ResponseWriter, err error) {
	if err == nil {
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch {
	case errors.Is(err, auth.ErrAnthropicInvalidSetupToken):
		respondErrorDetail(w, http.StatusBadRequest, "invalid_setup_token", err.Error())
	case errors.Is(err, auth.ErrAnthropicCLINotFound):
		respondErrorDetail(w, http.StatusServiceUnavailable, "cli_not_found", err.Error())
	case errors.Is(err, auth.ErrAnthropicPTYUnavailable):
		respondErrorDetail(w, http.StatusServiceUnavailable, "pty_unavailable", err.Error())
	case errors.Is(err, auth.ErrAnthropicTokenNotDetected):
		respondErrorDetail(w, http.StatusBadRequest, "token_not_detected", err.Error())
	case errors.Is(err, auth.ErrAnthropicLoginManualRequired):
		respondErrorDetail(w, http.StatusBadRequest, "manual_required", err.Error())
	case errors.Is(err, auth.ErrAnthropicLoginJobNotFound):
		respondErrorDetail(w, http.StatusNotFound, "job_not_found", err.Error())
	case errors.Is(err, auth.ErrAnthropicLoginJobExpired):
		respondErrorDetail(w, http.StatusGone, "job_expired", err.Error())
	case errors.Is(err, auth.ErrAnthropicLoginJobCancelled):
		respondErrorDetail(w, http.StatusConflict, "job_cancelled", err.Error())
	case errors.Is(err, auth.ErrAnthropicLoginJobCompleted):
		respondErrorDetail(w, http.StatusConflict, "job_completed", err.Error())
	default:
		respondAnthropicError(w, err)
	}
}

func respondOpenAICodexBrowserError(w http.ResponseWriter, err error) {
	if err == nil {
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch {
	case errors.Is(err, auth.ErrOpenAICodexInvalidToken):
		respondErrorDetail(w, http.StatusBadRequest, "invalid_oauth_token", err.Error())
	case errors.Is(err, auth.ErrOpenAICodexCLINotFound):
		respondErrorDetail(w, http.StatusServiceUnavailable, "cli_not_found", err.Error())
	case errors.Is(err, auth.ErrOpenAICodexPTYUnavailable):
		respondErrorDetail(w, http.StatusServiceUnavailable, "pty_unavailable", err.Error())
	case errors.Is(err, auth.ErrOpenAICodexTokenNotDetected):
		respondErrorDetail(w, http.StatusBadRequest, "token_not_detected", err.Error())
	case errors.Is(err, auth.ErrOpenAICodexLoginManualRequired):
		respondErrorDetail(w, http.StatusBadRequest, "manual_required", err.Error())
	case errors.Is(err, auth.ErrOpenAICodexLoginJobNotFound):
		respondErrorDetail(w, http.StatusNotFound, "job_not_found", err.Error())
	case errors.Is(err, auth.ErrOpenAICodexLoginJobExpired):
		respondErrorDetail(w, http.StatusGone, "job_expired", err.Error())
	case errors.Is(err, auth.ErrOpenAICodexLoginJobCancelled):
		respondErrorDetail(w, http.StatusConflict, "job_cancelled", err.Error())
	case errors.Is(err, auth.ErrOpenAICodexLoginJobCompleted):
		respondErrorDetail(w, http.StatusConflict, "job_completed", err.Error())
	default:
		respondOpenAIError(w, err)
	}
}

func respondChatError(w http.ResponseWriter, err error) {
	if err == nil {
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch {
	case errors.Is(err, app.ErrGeminiMissingProject):
		respondErrorDetail(w, http.StatusBadRequest, "missing_project", err.Error())
		return
	case errors.Is(err, app.ErrToolsNotSupported):
		respondErrorDetail(w, http.StatusBadRequest, "tools_not_supported", err.Error())
		return
	case errors.Is(err, tooling.ErrRunNotFound):
		respondErrorDetail(w, http.StatusConflict, "run_not_found", err.Error())
		return
	case errors.Is(err, tooling.ErrRunExpired):
		respondErrorDetail(w, http.StatusConflict, "run_expired", err.Error())
		return
	case errors.Is(err, app.ErrNoAvailableAccount):
		respondError(w, http.StatusConflict, err.Error())
		return
	}

	var failureErr *provider.FailureError
	if errors.As(err, &failureErr) {
		code := strings.TrimSpace(string(failureErr.Reason))
		if code == "" {
			code = "unknown"
		}
		respondErrorDetail(w, mapChatFailureStatus(failureErr), code, failureErr.Message)
		return
	}

	respondError(w, http.StatusBadGateway, err.Error())
}

func mapChatFailureStatus(failureErr *provider.FailureError) int {
	if failureErr == nil {
		return http.StatusBadGateway
	}
	switch failureErr.Reason {
	case core.FailureFormat:
		return http.StatusBadRequest
	case core.FailureModelNotFound:
		return http.StatusNotFound
	case core.FailureRateLimit:
		return http.StatusTooManyRequests
	case core.FailureTimeout:
		return http.StatusGatewayTimeout
	case core.FailureBilling:
		return http.StatusForbidden
	case core.FailureAuth, core.FailureAuthPermanent:
		if failureErr.Status == http.StatusForbidden {
			return http.StatusForbidden
		}
		return http.StatusUnauthorized
	}
	if failureErr.Status >= http.StatusBadRequest && failureErr.Status <= http.StatusNetworkAuthenticationRequired {
		return failureErr.Status
	}
	return http.StatusBadGateway
}

func (s *Server) resolveGeminiProviderAndAccount(providerID, accountID string) (*provider.GeminiInternalProvider, core.Account, error) {
	prov := s.svc.Provider(providerID)
	geminiProvider, ok := prov.(*provider.GeminiInternalProvider)
	if !ok || geminiProvider == nil {
		return nil, core.Account{}, fmt.Errorf("provider %q is not a gemini-internal provider", providerID)
	}
	pool := s.svc.Pool(providerID)
	if pool == nil {
		return nil, core.Account{}, fmt.Errorf("provider %q has no account pool", providerID)
	}
	if strings.TrimSpace(accountID) != "" {
		account, ok := pool.GetAccount(accountID)
		if !ok {
			return nil, core.Account{}, fmt.Errorf("account %q not found", accountID)
		}
		return geminiProvider, account, nil
	}
	account, ok := pool.Acquire("")
	if !ok {
		return nil, core.Account{}, fmt.Errorf("provider %q has no available accounts", providerID)
	}
	return geminiProvider, account, nil
}

func (s *Server) handleFallbacks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		entries := s.svc.GetFallbacks()
		respondJSON(w, http.StatusOK, map[string]any{"fallbacks": entries})
	case http.MethodPut:
		var body struct {
			Fallbacks []core.FallbackEntry `json:"fallbacks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if len(body.Fallbacks) > 5 {
			respondError(w, http.StatusBadRequest, "maximum 5 fallback entries")
			return
		}
		if err := s.svc.SaveFallbacks(body.Fallbacks); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"fallbacks": s.svc.GetFallbacks()})
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDefaultProvider(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respondJSON(w, http.StatusOK, map[string]string{
			"provider": s.svc.GetDefaultProvider(),
			"model":    s.svc.GetDefaultModel(),
		})
	case http.MethodPut:
		var body struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if p := strings.TrimSpace(body.Provider); p != "" {
			s.svc.SetDefaultProvider(p)
		}
		if m := strings.TrimSpace(body.Model); m != "" {
			s.svc.SetDefaultModel(m)
		}
		respondJSON(w, http.StatusOK, map[string]string{
			"provider": s.svc.GetDefaultProvider(),
			"model":    s.svc.GetDefaultModel(),
		})
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDiscordConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.svc.GetDiscordConfig()
		respondJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var cfg core.DiscordConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			respondError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if err := s.svc.SaveDiscordConfig(cfg); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, s.svc.GetDiscordConfig())
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleTelegramConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.svc.GetTelegramConfig()
		respondJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var cfg core.TelegramConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			respondError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if err := s.svc.SaveTelegramConfig(cfg); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, s.svc.GetTelegramConfig())
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleToolsConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.svc.GetToolsConfig()
		respondJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var cfg core.ToolsConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			respondError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if err := s.svc.SaveToolsConfig(cfg); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, s.svc.GetToolsConfig())
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]any{"error": strings.TrimSpace(message)})
}

func respondErrorDetail(w http.ResponseWriter, status int, code, message string) {
	respondJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    strings.TrimSpace(code),
			"message": strings.TrimSpace(message),
		},
	})
}

func respondGeminiProjectDiscoveryError(w http.ResponseWriter, err error) {
	respondErrorDetail(
		w,
		http.StatusBadRequest,
		"project_discovery_failed",
		chooseNonEmpty(
			strings.TrimSpace(err.Error()),
			"Could not discover or provision a Google Cloud project. Set GOOGLE_CLOUD_PROJECT or GOOGLE_CLOUD_PROJECT_ID, then retry OAuth.",
		),
	)
}

func respondPlain(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message))
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// MCP
// ---------------------------------------------------------------------------

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	servers := s.svc.MCPServers()
	if servers == nil {
		servers = []mcp.ServerInfo{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tools := s.svc.MCPToolDefinitions()
	if tools == nil {
		tools = []mcp.ToolInfo{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (s *Server) handleMCPBuiltin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	servers := s.svc.MCPBuiltinServers()
	if servers == nil {
		servers = []mcp.BuiltinServerInfo{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func (s *Server) handleMCPBuiltinToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.svc.ToggleMCPBuiltin(r.Context(), strings.TrimSpace(req.Name), req.Enabled); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Persona endpoints
// ---------------------------------------------------------------------------

func (s *Server) handlePersonas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	personas := s.svc.ListPersonas()
	if personas == nil {
		personas = []persona.PersonaInfo{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"personas": personas})
}

func (s *Server) handlePersonaActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	active := s.svc.ActivePersona()
	respondJSON(w, http.StatusOK, map[string]any{"persona": active})
}

func (s *Server) handlePersonaUse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		DirName string `json:"dir_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	dirName := strings.TrimSpace(req.DirName)
	if dirName == "" {
		respondError(w, http.StatusBadRequest, "dir_name is required")
		return
	}
	if err := s.svc.SetActivePersona(dirName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePersonaClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.svc.ClearActivePersona(); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePersonaReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.svc.ReloadPersonas(); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleToolStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	toolName := s.svc.GetActiveToolStatus(sessionID)
	retryStatus := s.svc.GetActiveRetryStatus(sessionID)
	respondJSON(w, http.StatusOK, map[string]any{
		"tool_name":    toolName,
		"retry_status": retryStatus,
	})
}
