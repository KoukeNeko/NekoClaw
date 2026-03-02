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
	"github.com/doeshing/nekoclaw/internal/provider"
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
		status := http.StatusBadGateway
		if errors.Is(err, app.ErrNoAvailableAccount) {
			status = http.StatusConflict
		}
		respondError(w, status, err.Error())
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
		sessionID = fmt.Sprintf("discord:%s:%s", payload.ChannelID, payload.UserID)
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

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]any{"error": strings.TrimSpace(message)})
}

func respondPlain(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message))
}
