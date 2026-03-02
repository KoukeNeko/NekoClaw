package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/contextwindow"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

var ErrProviderNotFound = errors.New("provider not found")
var ErrNoAvailableAccount = errors.New("no available account")

type Service struct {
	mu                sync.RWMutex
	providers         map[string]provider.Provider
	pools             map[string]*core.AccountPool
	sessions          *core.SessionStore
	oauthManager      *auth.GeminiOAuthManager
	authStore         *auth.Store
	preferredProfiles map[string]string
}

func NewService() *Service {
	return &Service{
		providers:         map[string]provider.Provider{},
		pools:             map[string]*core.AccountPool{},
		sessions:          core.NewSessionStore(),
		preferredProfiles: map[string]string{},
	}
}

func (s *Service) RegisterProvider(p provider.Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[p.ID()] = p
}

func (s *Service) RegisterPool(pool *core.AccountPool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pools[pool.Provider()] = pool
}

func (s *Service) SetAuthIntegration(manager *auth.GeminiOAuthManager, store *auth.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.oauthManager = manager
	s.authStore = store
}

func (s *Service) Providers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.providers))
	for id := range s.providers {
		ids = append(ids, id)
	}
	return ids
}

func (s *Service) Provider(id string) provider.Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.providers[id]
}

func (s *Service) Pool(id string) *core.AccountPool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pools[id]
}

func (s *Service) Accounts(providerID string) []core.AccountSnapshot {
	s.mu.RLock()
	pool := s.pools[providerID]
	s.mu.RUnlock()
	if pool == nil {
		return nil
	}
	return pool.Snapshot()
}

type GeminiOAuthStartRequest struct {
	ProfileID   string `json:"profile_id,omitempty"`
	Mode        string `json:"mode,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

type GeminiOAuthCompleteRequest struct {
	State             string `json:"state"`
	CallbackURLOrCode string `json:"callback_url_or_code"`
}

type GeminiOAuthCompleteResult struct {
	ProfileID      string `json:"profile_id"`
	Provider       string `json:"provider"`
	Email          string `json:"email,omitempty"`
	ProjectID      string `json:"project_id"`
	ActiveEndpoint string `json:"active_endpoint,omitempty"`
}

type GeminiProfileStatus struct {
	ProfileID      string    `json:"profile_id"`
	Provider       string    `json:"provider"`
	Type           string    `json:"type"`
	Email          string    `json:"email,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	Endpoint       string    `json:"endpoint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Available      bool      `json:"available"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil  time.Time `json:"disabled_until,omitempty"`
	DisabledReason string    `json:"disabled_reason,omitempty"`
	Preferred      bool      `json:"preferred"`
}

func (s *Service) StartGeminiOAuth(ctx context.Context, req GeminiOAuthStartRequest) (auth.StartResult, error) {
	authProvider, err := s.resolveGeminiAuthProvider()
	if err != nil {
		return auth.StartResult{}, err
	}
	manager := s.oauthManagerSafe()
	if manager == nil {
		return auth.StartResult{}, fmt.Errorf("oauth manager not configured")
	}

	start, err := manager.Start(ctx, auth.StartRequest{
		ProfileID:   strings.TrimSpace(req.ProfileID),
		Mode:        strings.TrimSpace(req.Mode),
		RedirectURI: strings.TrimSpace(req.RedirectURI),
	}, func(challenge, state, redirectURI string) (string, error) {
		result, err := authProvider.StartOAuth(ctx, provider.OAuthStartRequest{
			State:       state,
			Challenge:   challenge,
			RedirectURI: redirectURI,
		})
		if err != nil {
			return "", err
		}
		return result.AuthURL, nil
	})
	if err != nil {
		return auth.StartResult{}, err
	}

	log.Printf(
		"event=oauth_start provider=google-gemini-cli profile_id=%s oauth_mode=%s mode=%s expires_at=%s",
		strings.TrimSpace(req.ProfileID),
		start.OAuthMode,
		start.Mode,
		start.ExpiresAt.Format(time.RFC3339),
	)
	return start, nil
}

func (s *Service) CompleteGeminiOAuthFromCallback(ctx context.Context, state string, code string) (GeminiOAuthCompleteResult, error) {
	manager := s.oauthManagerSafe()
	if manager == nil {
		return GeminiOAuthCompleteResult{}, fmt.Errorf("oauth manager not configured")
	}
	pending, err := manager.ConsumeFromCallback(state, code)
	if err != nil {
		return GeminiOAuthCompleteResult{}, err
	}
	return s.completeGeminiOAuth(ctx, pending, strings.TrimSpace(code))
}

func (s *Service) CompleteGeminiOAuthManual(ctx context.Context, req GeminiOAuthCompleteRequest) (GeminiOAuthCompleteResult, error) {
	manager := s.oauthManagerSafe()
	if manager == nil {
		return GeminiOAuthCompleteResult{}, fmt.Errorf("oauth manager not configured")
	}
	pending, code, err := manager.ConsumeFromManual(strings.TrimSpace(req.State), strings.TrimSpace(req.CallbackURLOrCode))
	if err != nil {
		return GeminiOAuthCompleteResult{}, err
	}
	return s.completeGeminiOAuth(ctx, pending, code)
}

func (s *Service) ListGeminiProfiles() ([]GeminiProfileStatus, error) {
	store := s.authStoreSafe()
	if store == nil {
		return nil, fmt.Errorf("auth store not configured")
	}
	profiles, err := store.ListProfiles("google-gemini-cli")
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	preferred := s.preferredProfiles["google-gemini-cli"]
	pool := s.pools["google-gemini-cli"]
	s.mu.RUnlock()

	snapByID := map[string]core.AccountSnapshot{}
	if pool != nil {
		for _, snap := range pool.Snapshot() {
			snapByID[snap.ID] = snap
		}
	}

	result := make([]GeminiProfileStatus, 0, len(profiles))
	now := time.Now()
	for _, profile := range profiles {
		status := GeminiProfileStatus{
			ProfileID: profile.ProfileID,
			Provider:  profile.Provider,
			Type:      profile.Type,
			Email:     profile.Email,
			ProjectID: profile.ProjectID,
			Endpoint:  profile.Endpoint,
			CreatedAt: profile.CreatedAt,
			UpdatedAt: profile.UpdatedAt,
			Preferred: profile.ProfileID == preferred,
		}
		if snap, ok := snapByID[profile.ProfileID]; ok && snap.Usage != nil {
			status.CooldownUntil = snap.Usage.CooldownUntil
			status.DisabledUntil = snap.Usage.DisabledUntil
			status.DisabledReason = string(snap.Usage.DisabledReason)
			status.Available = (snap.Usage.CooldownUntil.IsZero() || now.After(snap.Usage.CooldownUntil)) &&
				(snap.Usage.DisabledUntil.IsZero() || now.After(snap.Usage.DisabledUntil))
		} else {
			status.CooldownUntil = profile.CooldownUntil
			status.DisabledUntil = profile.DisabledUntil
			status.DisabledReason = profile.DisabledReason
			status.Available = (profile.CooldownUntil.IsZero() || now.After(profile.CooldownUntil)) &&
				(profile.DisabledUntil.IsZero() || now.After(profile.DisabledUntil))
		}
		result = append(result, status)
	}
	return result, nil
}

func (s *Service) UseGeminiProfile(profileID string) error {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("profile_id is required")
	}
	store := s.authStoreSafe()
	if store == nil {
		return fmt.Errorf("auth store not configured")
	}
	profile, err := store.GetProfile("google-gemini-cli", profileID)
	if err != nil {
		return err
	}
	if profile.ProfileID == "" {
		return auth.ErrProfileNotFound
	}

	s.mu.Lock()
	pool := s.pools["google-gemini-cli"]
	s.preferredProfiles["google-gemini-cli"] = profileID
	s.mu.Unlock()
	if pool != nil {
		if ok := pool.SetPreferred(profileID); !ok {
			return fmt.Errorf("profile %q not loaded in runtime pool", profileID)
		}
	}
	return nil
}

func (s *Service) HandleChat(ctx context.Context, req core.ChatRequest) (core.ChatResponse, error) {
	providerID := strings.TrimSpace(req.Provider)
	if providerID == "" {
		providerID = "mock"
	}
	modelID := strings.TrimSpace(req.Model)
	if modelID == "" {
		modelID = "default"
	}
	if providerID == "google-gemini-cli" && strings.EqualFold(modelID, "default") {
		modelID = "gemini-2.5-pro"
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "main"
	}

	prov, pool, err := s.resolveProviderPool(providerID)
	if err != nil {
		return core.ChatResponse{}, err
	}

	prompt := strings.TrimSpace(req.Message)
	if prompt == "" {
		return core.ChatResponse{}, fmt.Errorf("message is required")
	}

	history := s.sessions.History(sessionID)
	userMessage := core.Message{Role: core.RoleUser, Content: prompt, CreatedAt: time.Now()}
	baseMessages := append(history, userMessage)
	policy := contextwindow.DefaultPolicy(prov.ContextWindow(modelID))
	policy = s.adjustCompressionPolicy(providerID, modelID, policy)
	compressedMessages, compressionMeta, compressed := contextwindow.Compress(baseMessages, policy)

	attemptLimit := len(pool.Snapshot())
	if attemptLimit < 1 {
		attemptLimit = 1
	}

	preferredProfile := s.preferredProfile(providerID)
	var lastErr error
	for attempt := 0; attempt < attemptLimit; attempt++ {
		account, ok := pool.Acquire(preferredProfile)
		if !ok {
			reason := pool.ResolveUnavailableReason()
			if lastErr == nil {
				soonest := pool.SoonestAvailableAt()
				if soonest.IsZero() {
					lastErr = fmt.Errorf("%w: provider=%s reason=%s", ErrNoAvailableAccount, providerID, reason)
				} else {
					lastErr = fmt.Errorf("%w: provider=%s reason=%s retry_at=%s", ErrNoAvailableAccount, providerID, reason, soonest.Format(time.RFC3339))
				}
			}
			break
		}

		account, refreshErr := s.maybeRefreshAccountCredential(ctx, providerID, modelID, prov, pool, account)
		if refreshErr != nil {
			reason := deriveFailureReason(refreshErr)
			pool.MarkFailure(account.ID, reason)
			logFailureEvent(providerID, account.ID, reason, pool)
			s.syncProfileState(providerID, account.ID)
			lastErr = refreshErr
			if !core.IsRetriable(reason) {
				break
			}
			continue
		}

		resp, err := prov.Generate(ctx, provider.GenerateRequest{
			Model:    modelID,
			Messages: compressedMessages,
			Account:  account,
		})
		if err == nil {
			assistant := core.Message{Role: core.RoleAssistant, Content: resp.Text, CreatedAt: time.Now()}
			s.sessions.Append(sessionID, userMessage, assistant)
			pool.MarkUsed(account.ID)
			s.syncProfileState(providerID, account.ID)
			return core.ChatResponse{
				SessionID:   sessionID,
				Provider:    providerID,
				Model:       modelID,
				Reply:       resp.Text,
				Compressed:  compressed,
				Compression: compressionMeta,
				AccountID:   account.ID,
			}, nil
		}

		reason := deriveFailureReason(err)
		pool.MarkFailure(account.ID, reason)
		logFailureEvent(providerID, account.ID, reason, pool)
		s.syncProfileState(providerID, account.ID)
		lastErr = err
		if !core.IsRetriable(reason) {
			break
		}
		if preferredProfile == account.ID {
			preferredProfile = ""
		}
	}
	if lastErr == nil {
		lastErr = ErrNoAvailableAccount
	}
	return core.ChatResponse{}, lastErr
}

func (s *Service) completeGeminiOAuth(
	ctx context.Context,
	pending auth.PendingState,
	code string,
) (GeminiOAuthCompleteResult, error) {
	authProvider, err := s.resolveGeminiAuthProvider()
	if err != nil {
		return GeminiOAuthCompleteResult{}, err
	}
	store := s.authStoreSafe()
	if store == nil {
		return GeminiOAuthCompleteResult{}, fmt.Errorf("auth store not configured")
	}

	credential, err := authProvider.CompleteOAuth(ctx, provider.OAuthCompleteRequest{
		Code:        code,
		Verifier:    pending.Verifier,
		RedirectURI: pending.RedirectURI,
	})
	if err != nil {
		return GeminiOAuthCompleteResult{}, err
	}

	profileID := pending.ProfileID
	if profileID == "" {
		profileID = deriveProfileID(credential.Email)
	}
	profileID = strings.TrimSpace(profileID)

	savedCredential := auth.Credential{
		AccessToken:  credential.AccessToken,
		RefreshToken: credential.RefreshToken,
		ExpiresAt:    credential.ExpiresAt,
	}
	if err := store.SaveCredential("google-gemini-cli", profileID, savedCredential); err != nil {
		log.Printf("event=oauth_complete provider=google-gemini-cli profile_id=%s status=credential_store_failed error=%q", profileID, err)
		return GeminiOAuthCompleteResult{}, err
	}

	meta := auth.ProfileMetadata{
		ProfileID: profileID,
		Provider:  "google-gemini-cli",
		Type:      string(core.AccountOAuth),
		Email:     strings.TrimSpace(credential.Email),
		ProjectID: strings.TrimSpace(credential.ProjectID),
		Endpoint:  strings.TrimSpace(credential.ActiveEndpoint),
	}
	if err := store.UpsertProfile(meta); err != nil {
		_ = store.DeleteCredential("google-gemini-cli", profileID)
		log.Printf("event=oauth_complete provider=google-gemini-cli profile_id=%s status=metadata_store_failed error=%q", profileID, err)
		return GeminiOAuthCompleteResult{}, err
	}

	s.mu.Lock()
	pool := s.pools["google-gemini-cli"]
	s.preferredProfiles["google-gemini-cli"] = profileID
	s.mu.Unlock()
	if pool != nil {
		pool.SetCredential(profileID, core.Account{
			ID:       profileID,
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    credential.AccessToken,
			Email:    strings.TrimSpace(credential.Email),
			Metadata: core.Metadata{
				"project_id": strings.TrimSpace(credential.ProjectID),
				"endpoint":   strings.TrimSpace(credential.ActiveEndpoint),
				"profile_id": profileID,
			},
		})
		pool.SetPreferred(profileID)
		s.syncProfileState("google-gemini-cli", profileID)
	}

	log.Printf("event=oauth_complete provider=google-gemini-cli profile_id=%s endpoint=%s", profileID, credential.ActiveEndpoint)
	return GeminiOAuthCompleteResult{
		ProfileID:      profileID,
		Provider:       "google-gemini-cli",
		Email:          strings.TrimSpace(credential.Email),
		ProjectID:      strings.TrimSpace(credential.ProjectID),
		ActiveEndpoint: strings.TrimSpace(credential.ActiveEndpoint),
	}, nil
}

func (s *Service) resolveProviderPool(providerID string) (provider.Provider, *core.AccountPool, error) {
	s.mu.RLock()
	prov := s.providers[providerID]
	pool := s.pools[providerID]
	s.mu.RUnlock()
	if prov == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrProviderNotFound, providerID)
	}
	if pool == nil {
		return nil, nil, fmt.Errorf("%w: provider=%s", ErrNoAvailableAccount, providerID)
	}
	return prov, pool, nil
}

func (s *Service) resolveGeminiAuthProvider() (provider.AuthProvider, error) {
	s.mu.RLock()
	prov := s.providers["google-gemini-cli"]
	s.mu.RUnlock()
	if prov == nil {
		return nil, fmt.Errorf("%w: google-gemini-cli", ErrProviderNotFound)
	}
	authProvider, ok := prov.(provider.AuthProvider)
	if !ok {
		return nil, fmt.Errorf("provider google-gemini-cli does not support oauth")
	}
	return authProvider, nil
}

func (s *Service) oauthManagerSafe() *auth.GeminiOAuthManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.oauthManager
}

func (s *Service) authStoreSafe() *auth.Store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authStore
}

func (s *Service) preferredProfile(providerID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.preferredProfiles[providerID]
}

func (s *Service) maybeRefreshAccountCredential(
	ctx context.Context,
	providerID string,
	modelID string,
	prov provider.Provider,
	pool *core.AccountPool,
	account core.Account,
) (core.Account, error) {
	if providerID != "google-gemini-cli" {
		return account, nil
	}
	store := s.authStoreSafe()
	if store == nil {
		return account, nil
	}
	authProv, ok := prov.(provider.AuthProvider)
	if !ok {
		return account, nil
	}

	credential, err := store.LoadCredential(providerID, account.ID)
	if err != nil {
		// Fallback to in-memory token only.
		return account, nil
	}

	oauthCredential := provider.OAuthCredential{
		AccessToken:    credential.AccessToken,
		RefreshToken:   credential.RefreshToken,
		ExpiresAt:      credential.ExpiresAt,
		Email:          account.Email,
		ProjectID:      strings.TrimSpace(account.Metadata["project_id"]),
		ActiveEndpoint: strings.TrimSpace(account.Metadata["endpoint"]),
	}
	refreshed, changed, err := authProv.RefreshOAuthIfNeeded(ctx, oauthCredential)
	if err != nil {
		log.Printf("event=token_refresh provider=%s profile_id=%s status=failed error=%q", providerID, account.ID, err)
		return account, err
	}

	account.Token = refreshed.AccessToken
	if strings.TrimSpace(refreshed.Email) != "" {
		account.Email = strings.TrimSpace(refreshed.Email)
	}
	if account.Metadata == nil {
		account.Metadata = core.Metadata{}
	}
	if strings.TrimSpace(refreshed.ProjectID) != "" {
		account.Metadata["project_id"] = strings.TrimSpace(refreshed.ProjectID)
	}
	if strings.TrimSpace(refreshed.ActiveEndpoint) != "" {
		account.Metadata["endpoint"] = strings.TrimSpace(refreshed.ActiveEndpoint)
	}

	if !changed {
		pool.SetCredential(account.ID, account)
		return account, nil
	}

	saved := auth.Credential{
		AccessToken:  refreshed.AccessToken,
		RefreshToken: refreshed.RefreshToken,
		ExpiresAt:    refreshed.ExpiresAt,
	}
	if err := store.SaveCredential(providerID, account.ID, saved); err != nil {
		return account, err
	}
	if err := store.UpsertProfile(auth.ProfileMetadata{
		ProfileID: account.ID,
		Provider:  providerID,
		Type:      string(core.AccountOAuth),
		Email:     account.Email,
		ProjectID: strings.TrimSpace(account.Metadata["project_id"]),
		Endpoint:  strings.TrimSpace(account.Metadata["endpoint"]),
	}); err != nil {
		return account, err
	}
	pool.SetCredential(account.ID, account)
	log.Printf("event=token_refresh provider=%s profile_id=%s status=ok model=%s", providerID, account.ID, modelID)
	return account, nil
}

func (s *Service) syncProfileState(providerID string, profileID string) {
	if providerID != "google-gemini-cli" || strings.TrimSpace(profileID) == "" {
		return
	}
	store := s.authStoreSafe()
	if store == nil {
		return
	}
	s.mu.RLock()
	pool := s.pools[providerID]
	s.mu.RUnlock()
	if pool == nil {
		return
	}

	for _, snapshot := range pool.Snapshot() {
		if snapshot.ID != profileID || snapshot.Usage == nil {
			continue
		}
		_ = store.UpdateProfileState(providerID, profileID, auth.ProfileMetadata{
			CooldownUntil:  snapshot.Usage.CooldownUntil,
			DisabledUntil:  snapshot.Usage.DisabledUntil,
			DisabledReason: string(snapshot.Usage.DisabledReason),
		})
		return
	}
}

func (s *Service) adjustCompressionPolicy(providerID, _ string, policy contextwindow.Policy) contextwindow.Policy {
	if providerID == "google-gemini-cli" {
		if policy.MaxContextTokens >= 128000 {
			policy.ReserveTokens = 4096
		} else if policy.MaxContextTokens >= 32000 {
			policy.ReserveTokens = 3072
		}
	}
	return policy
}

func deriveFailureReason(err error) core.FailureReason {
	var failureErr *provider.FailureError
	if errors.As(err, &failureErr) && failureErr.Reason != "" {
		return failureErr.Reason
	}
	return core.ClassifyFailure(err.Error())
}

func chooseFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func deriveProfileID(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	if email != "" {
		safe := strings.NewReplacer("@", "_at_", ".", "_", "+", "_", "-", "_").Replace(email)
		return "google-gemini-cli:" + safe
	}
	return fmt.Sprintf("google-gemini-cli:%d", time.Now().Unix())
}

func logFailureEvent(providerID, profileID string, reason core.FailureReason, pool *core.AccountPool) {
	if pool == nil || strings.TrimSpace(profileID) == "" {
		return
	}
	for _, snapshot := range pool.Snapshot() {
		if snapshot.ID != profileID || snapshot.Usage == nil {
			continue
		}
		log.Printf(
			"event=profile_failure provider=%s profile_id=%s failure_reason=%s cooldown_until=%s disabled_until=%s",
			providerID,
			profileID,
			reason,
			snapshot.Usage.CooldownUntil.Format(time.RFC3339),
			snapshot.Usage.DisabledUntil.Format(time.RFC3339),
		)
		return
	}
}
