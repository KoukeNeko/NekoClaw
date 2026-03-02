package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/compaction"
	"github.com/doeshing/nekoclaw/internal/contextwindow"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/memory"
	"github.com/doeshing/nekoclaw/internal/provider"
)

var ErrProviderNotFound = errors.New("provider not found")
var ErrNoAvailableAccount = errors.New("no available account")
var ErrGeminiMissingProject = errors.New("gemini project is required")
var ErrInvalidAPIKey = errors.New("invalid api key")
var ErrKeyValidationFailed = errors.New("key validation failed")
var ErrProfileNotFound = errors.New("profile not found")
var ErrProfileInUse = errors.New("profile in use")
var ErrProviderNotReady = errors.New("provider not ready")

type Service struct {
	mu                sync.RWMutex
	providers         map[string]provider.Provider
	pools             map[string]*core.AccountPool
	sessions          *core.SessionStore
	lifecycle         *core.SessionLifecycle
	oauthManager      *auth.GeminiOAuthManager
	authStore         *auth.Store
	memoryDir         string
	searchIndex       *memory.SearchIndex
	preferredProfiles map[string]string
	fallbacks         map[string][]string // primary provider -> fallback provider IDs
}

type ServiceOptions struct {
	SessionStore *core.SessionStore
	Lifecycle    *core.SessionLifecycle
	MemoryDir    string
	SearchIndex  *memory.SearchIndex
}

func NewService(opts ServiceOptions) *Service {
	sessions := opts.SessionStore
	if sessions == nil {
		sessions = core.NewSessionStore()
	}
	return &Service{
		providers:         map[string]provider.Provider{},
		pools:             map[string]*core.AccountPool{},
		sessions:          sessions,
		lifecycle:         opts.Lifecycle,
		memoryDir:         opts.MemoryDir,
		searchIndex:       opts.SearchIndex,
		preferredProfiles: map[string]string{},
		fallbacks:         map[string][]string{},
	}
}

func (s *Service) ListSessions() []core.SessionMetadata {
	return s.sessions.ListSessions()
}

func (s *Service) DeleteSession(sessionID string) error {
	return s.sessions.DeleteSession(sessionID)
}

func (s *Service) SearchMemory(query string, limit int) ([]memory.SearchResult, error) {
	if s.searchIndex == nil {
		return nil, fmt.Errorf("search index not configured")
	}
	return s.searchIndex.Search(query, limit)
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

// RegisterFallback declares that when all accounts for primaryProvider are exhausted,
// the system should attempt fallbackProvider before returning an error.
func (s *Service) RegisterFallback(primaryProvider, fallbackProvider string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fallbacks[primaryProvider] = append(s.fallbacks[primaryProvider], fallbackProvider)
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
	ProfileID         string    `json:"profile_id"`
	Provider          string    `json:"provider"`
	Type              string    `json:"type"`
	Email             string    `json:"email,omitempty"`
	ProjectID         string    `json:"project_id,omitempty"`
	Endpoint          string    `json:"endpoint,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Available         bool      `json:"available"`
	CooldownUntil     time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil     time.Time `json:"disabled_until,omitempty"`
	DisabledReason    string    `json:"disabled_reason,omitempty"`
	Preferred         bool      `json:"preferred"`
	ProjectReady      bool      `json:"project_ready"`
	UnavailableReason string    `json:"unavailable_reason,omitempty"`
}

type AIStudioAddKeyRequest struct {
	APIKey       string `json:"api_key"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type AIStudioAddKeyResult struct {
	ProfileID   string `json:"profile_id"`
	Provider    string `json:"provider"`
	DisplayName string `json:"display_name"`
	KeyHint     string `json:"key_hint"`
	Preferred   bool   `json:"preferred"`
	Available   bool   `json:"available"`
}

type AIStudioProfileStatus struct {
	ProfileID      string    `json:"profile_id"`
	Provider       string    `json:"provider"`
	Type           string    `json:"type"`
	DisplayName    string    `json:"display_name,omitempty"`
	KeyHint        string    `json:"key_hint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Available      bool      `json:"available"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil  time.Time `json:"disabled_until,omitempty"`
	DisabledReason string    `json:"disabled_reason,omitempty"`
	Preferred      bool      `json:"preferred"`
}

type AIStudioModelsResult struct {
	Provider    string    `json:"provider"`
	ProfileID   string    `json:"profile_id"`
	Models      []string  `json:"models"`
	Source      string    `json:"source"`
	CachedUntil time.Time `json:"cached_until,omitempty"`
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
	envProject := resolveGoogleCloudProject()
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
		if snap, ok := snapByID[profile.ProfileID]; ok {
			if projectID := strings.TrimSpace(snap.Metadata["project_id"]); projectID != "" {
				status.ProjectID = projectID
			}
			if endpoint := strings.TrimSpace(snap.Metadata["endpoint"]); endpoint != "" {
				status.Endpoint = endpoint
			}
			if snap.Usage != nil {
				status.CooldownUntil = snap.Usage.CooldownUntil
				status.DisabledUntil = snap.Usage.DisabledUntil
				status.DisabledReason = string(snap.Usage.DisabledReason)
				status.Available = (snap.Usage.CooldownUntil.IsZero() || now.After(snap.Usage.CooldownUntil)) &&
					(snap.Usage.DisabledUntil.IsZero() || now.After(snap.Usage.DisabledUntil))
			} else {
				status.Available = true
			}
		} else {
			status.CooldownUntil = profile.CooldownUntil
			status.DisabledUntil = profile.DisabledUntil
			status.DisabledReason = profile.DisabledReason
			status.Available = (profile.CooldownUntil.IsZero() || now.After(profile.CooldownUntil)) &&
				(profile.DisabledUntil.IsZero() || now.After(profile.DisabledUntil))
		}
		status.ProjectReady = strings.TrimSpace(status.ProjectID) != "" || envProject != ""
		if !status.ProjectReady {
			status.UnavailableReason = "missing_project"
			status.Available = false
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
	if strings.TrimSpace(profile.ProjectID) == "" && resolveGoogleCloudProject() == "" {
		return fmt.Errorf(
			"%w: profile=%s. Re-run Gemini OAuth or set GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_PROJECT_ID.",
			ErrGeminiMissingProject,
			profileID,
		)
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

func (s *Service) AddAIStudioKey(ctx context.Context, req AIStudioAddKeyRequest) (AIStudioAddKeyResult, error) {
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		return AIStudioAddKeyResult{}, fmt.Errorf("%w: api_key is required", ErrInvalidAPIKey)
	}
	store := s.authStoreSafe()
	if store == nil {
		return AIStudioAddKeyResult{}, fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	prov, pool, err := s.resolveProviderPool("google-ai-studio")
	if err != nil {
		return AIStudioAddKeyResult{}, fmt.Errorf("%w: %v", ErrProviderNotReady, err)
	}
	catalogProvider, ok := prov.(provider.ModelCatalogProvider)
	if !ok {
		return AIStudioAddKeyResult{}, fmt.Errorf("%w: provider does not support model catalog", ErrProviderNotReady)
	}

	profileID := strings.TrimSpace(req.ProfileID)
	displayName := strings.TrimSpace(req.DisplayName)
	if profileID == "" {
		profileID = deriveAIStudioProfileID(displayName, apiKey)
	}
	keyHint := maskAPIKeyForHint(apiKey)
	if displayName == "" {
		displayName = "AI Studio " + keyHint
	}

	validationAccount := core.Account{
		ID:       "",
		Provider: "google-ai-studio",
		Type:     core.AccountAPIKey,
		Token:    apiKey,
	}
	models, err := catalogProvider.ListModels(ctx, validationAccount)
	if err != nil {
		return AIStudioAddKeyResult{}, fmt.Errorf("%w: %v", ErrKeyValidationFailed, err)
	}
	if len(models) == 0 {
		return AIStudioAddKeyResult{}, fmt.Errorf("%w: no generateContent-capable models returned", ErrKeyValidationFailed)
	}

	if err := store.SaveCredential("google-ai-studio", profileID, auth.Credential{
		AccessToken: apiKey,
	}); err != nil {
		return AIStudioAddKeyResult{}, err
	}

	meta := auth.ProfileMetadata{
		ProfileID:   profileID,
		Provider:    "google-ai-studio",
		Type:        string(core.AccountAPIKey),
		DisplayName: displayName,
		KeyHint:     keyHint,
		Endpoint:    pEndpoint(prov),
	}
	if err := store.UpsertProfile(meta); err != nil {
		_ = store.DeleteCredential("google-ai-studio", profileID)
		return AIStudioAddKeyResult{}, err
	}

	pool.SetCredential(profileID, core.Account{
		ID:       profileID,
		Provider: "google-ai-studio",
		Type:     core.AccountAPIKey,
		Token:    apiKey,
		Metadata: core.Metadata{
			"display_name": displayName,
			"key_hint":     keyHint,
		},
	})

	preferred := req.SetPreferred
	if !preferred {
		snapshots := pool.Snapshot()
		preferred = len(snapshots) == 1
	}
	if preferred {
		s.mu.Lock()
		s.preferredProfiles["google-ai-studio"] = profileID
		s.mu.Unlock()
		_ = pool.SetPreferred(profileID)
	}
	s.syncProfileState("google-ai-studio", profileID)
	log.Printf(
		"event=ai_studio_key_add provider=google-ai-studio profile_id=%s key_hint=%s preferred=%t",
		profileID,
		keyHint,
		preferred,
	)
	return AIStudioAddKeyResult{
		ProfileID:   profileID,
		Provider:    "google-ai-studio",
		DisplayName: displayName,
		KeyHint:     keyHint,
		Preferred:   preferred,
		Available:   true,
	}, nil
}

func (s *Service) ListAIStudioProfiles() ([]AIStudioProfileStatus, error) {
	store := s.authStoreSafe()
	if store == nil {
		return nil, fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	profiles, err := store.ListProfiles("google-ai-studio")
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	preferred := s.preferredProfiles["google-ai-studio"]
	pool := s.pools["google-ai-studio"]
	s.mu.RUnlock()

	snapByID := map[string]core.AccountSnapshot{}
	if pool != nil {
		for _, snap := range pool.Snapshot() {
			snapByID[snap.ID] = snap
		}
	}
	now := time.Now()
	result := make([]AIStudioProfileStatus, 0, len(profiles))
	for _, profile := range profiles {
		status := AIStudioProfileStatus{
			ProfileID:   profile.ProfileID,
			Provider:    profile.Provider,
			Type:        profile.Type,
			DisplayName: strings.TrimSpace(profile.DisplayName),
			KeyHint:     strings.TrimSpace(profile.KeyHint),
			CreatedAt:   profile.CreatedAt,
			UpdatedAt:   profile.UpdatedAt,
			Preferred:   profile.ProfileID == preferred,
		}
		if snap, ok := snapByID[profile.ProfileID]; ok {
			if status.DisplayName == "" {
				status.DisplayName = strings.TrimSpace(snap.Metadata["display_name"])
			}
			if status.KeyHint == "" {
				status.KeyHint = strings.TrimSpace(snap.Metadata["key_hint"])
			}
			if snap.Usage != nil {
				status.CooldownUntil = snap.Usage.CooldownUntil
				status.DisabledUntil = snap.Usage.DisabledUntil
				status.DisabledReason = string(snap.Usage.DisabledReason)
				status.Available = (snap.Usage.CooldownUntil.IsZero() || now.After(snap.Usage.CooldownUntil)) &&
					(snap.Usage.DisabledUntil.IsZero() || now.After(snap.Usage.DisabledUntil))
			} else {
				status.Available = true
			}
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

func (s *Service) UseAIStudioProfile(profileID string) error {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("profile_id is required")
	}
	store := s.authStoreSafe()
	if store == nil {
		return fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	if _, err := store.GetProfile("google-ai-studio", profileID); err != nil {
		if errors.Is(err, auth.ErrProfileNotFound) {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
		return err
	}

	s.mu.Lock()
	pool := s.pools["google-ai-studio"]
	s.preferredProfiles["google-ai-studio"] = profileID
	s.mu.Unlock()
	if pool != nil {
		if ok := pool.SetPreferred(profileID); !ok {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
	}
	log.Printf("event=ai_studio_key_use provider=google-ai-studio profile_id=%s", profileID)
	return nil
}

func (s *Service) DeleteAIStudioProfile(profileID string) error {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("profile_id is required")
	}
	store := s.authStoreSafe()
	if store == nil {
		return fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	if _, err := store.GetProfile("google-ai-studio", profileID); err != nil {
		if errors.Is(err, auth.ErrProfileNotFound) {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
		return err
	}

	_ = store.DeleteCredential("google-ai-studio", profileID)
	if err := store.DeleteProfile("google-ai-studio", profileID); err != nil && !errors.Is(err, auth.ErrProfileNotFound) {
		return err
	}
	s.mu.Lock()
	if s.preferredProfiles["google-ai-studio"] == profileID {
		delete(s.preferredProfiles, "google-ai-studio")
	}
	pool := s.pools["google-ai-studio"]
	s.mu.Unlock()
	if pool != nil {
		pool.RemoveAccount(profileID)
	}
	log.Printf("event=ai_studio_key_delete provider=google-ai-studio profile_id=%s", profileID)
	return nil
}

func (s *Service) ListAIStudioModels(ctx context.Context, profileID string) (AIStudioModelsResult, error) {
	prov, pool, err := s.resolveProviderPool("google-ai-studio")
	if err != nil {
		return AIStudioModelsResult{}, fmt.Errorf("%w: %v", ErrProviderNotReady, err)
	}
	catalogProvider, ok := prov.(provider.ModelCatalogProvider)
	if !ok {
		return AIStudioModelsResult{}, fmt.Errorf("%w: provider does not support model catalog", ErrProviderNotReady)
	}

	profileID = strings.TrimSpace(profileID)
	var account core.Account
	var found bool
	if profileID != "" {
		account, found = pool.GetAccount(profileID)
		if !found {
			return AIStudioModelsResult{}, fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
	} else {
		preferred := s.preferredProfile("google-ai-studio")
		account, found = pool.Acquire(preferred)
		if !found {
			reason := pool.ResolveUnavailableReason()
			return AIStudioModelsResult{}, fmt.Errorf("%w: provider=google-ai-studio reason=%s", ErrNoAvailableAccount, reason)
		}
		profileID = account.ID
	}

	source := "live"
	var cachedUntil time.Time
	var models []string
	if withSource, ok := prov.(aiStudioModelCatalogProvider); ok {
		models, source, cachedUntil, err = withSource.ListModelsWithSource(ctx, account)
	} else {
		models, err = catalogProvider.ListModels(ctx, account)
	}
	if err != nil {
		return AIStudioModelsResult{}, err
	}
	return AIStudioModelsResult{
		Provider:    "google-ai-studio",
		ProfileID:   profileID,
		Models:      models,
		Source:      chooseFirstNonEmpty(source, "live"),
		CachedUntil: cachedUntil,
	}, nil
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
	isDefaultModel := strings.EqualFold(modelID, "default")
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "main"
	}

	// Check session lifecycle before processing.
	if s.lifecycle != nil && s.lifecycle.ShouldReset(sessionID) {
		log.Printf("event=session_auto_reset session_id=%s", sessionID)
		_ = s.lifecycle.RotateSession(sessionID)
	}

	prompt := strings.TrimSpace(req.Message)
	if prompt == "" {
		return core.ChatResponse{}, fmt.Errorf("message is required")
	}

	// Build provider chain: primary first, then registered fallbacks.
	s.mu.RLock()
	chain := []string{providerID}
	chain = append(chain, s.fallbacks[providerID]...)
	s.mu.RUnlock()

	var lastErr error
	for i, candidateProvider := range chain {
		isFallback := i > 0

		// For fallback providers with "default" model, re-resolve per provider.
		candidateModel := modelID
		if isFallback && isDefaultModel {
			candidateModel = "default"
		}

		resp, err := s.attemptSingleProvider(ctx, attemptSingleProviderParams{
			providerID:     candidateProvider,
			modelID:        candidateModel,
			isDefaultModel: isDefaultModel || (isFallback && strings.EqualFold(candidateModel, "default")),
			sessionID:      sessionID,
			prompt:         prompt,
		})
		if err == nil {
			if isFallback {
				log.Printf(
					"event=fallback_success primary_provider=%s fallback_provider=%s model=%s",
					providerID, candidateProvider, resp.Model,
				)
			}
			return resp, nil
		}
		lastErr = err

		// Only fallback on "no available account" exhaustion.
		// Non-retriable errors (format, model_not_found, missing_project) should not fallback.
		if !errors.Is(err, ErrNoAvailableAccount) {
			break
		}
		if isFallback {
			log.Printf(
				"event=fallback_exhausted primary_provider=%s fallback_provider=%s error=%q",
				providerID, candidateProvider, err,
			)
		}
	}

	if lastErr == nil {
		lastErr = ErrNoAvailableAccount
	}
	return core.ChatResponse{}, lastErr
}

type attemptSingleProviderParams struct {
	providerID     string
	modelID        string
	isDefaultModel bool
	sessionID      string
	prompt         string
}

// attemptSingleProvider tries all accounts in one provider's pool.
// Returns (response, nil) on success, or (zero, error) when all accounts are exhausted.
func (s *Service) attemptSingleProvider(
	ctx context.Context,
	params attemptSingleProviderParams,
) (core.ChatResponse, error) {
	providerID := params.providerID
	modelID := params.modelID
	isDefaultModel := params.isDefaultModel
	sessionID := params.sessionID

	prov, pool, err := s.resolveProviderPool(providerID)
	if err != nil {
		return core.ChatResponse{}, err
	}

	userMessage := core.Message{Role: core.RoleUser, Content: params.prompt, CreatedAt: time.Now()}

	// Try LLM-based compaction first; fall back to token-based sliding window.
	var compressedMessages []core.Message
	var compressionMeta core.CompressionMeta
	var compressed bool

	entries := s.sessions.History(sessionID)
	contextWindow := prov.ContextWindow(modelID)
	compactor := compaction.NewCompactor(prov, modelID, core.Account{})

	// Pre-compaction memory flush: extract durable notes before messages are dropped.
	if s.memoryDir != "" {
		flusher := compaction.NewMemoryFlusher(prov, modelID, core.Account{}, s.memoryDir)
		currentTokens := compaction.EstimateEntriesTokens(entries)
		if flusher.ShouldFlush(currentTokens, contextWindow, compaction.DefaultReserveTokens) {
			if _, flushErr := flusher.Flush(ctx, entries); flushErr != nil {
				log.Printf("event=memory_flush_error session_id=%s error=%q", sessionID, flushErr)
			}
		}
	}

	if compactor.ShouldCompact(entries, contextWindow, compaction.DefaultReserveTokens) {
		result, compactErr := compactor.Compact(ctx, compaction.CompactionRequest{
			Entries:       entries,
			ContextWindow: contextWindow,
			ReserveTokens: compaction.DefaultReserveTokens,
		})
		if compactErr == nil && result.DroppedCount > 0 {
			s.sessions.Append(sessionID, result.CompactionEntry)
			compressedMessages = entriesToMessages(result.KeptEntries)
			compressedMessages = append(compressedMessages, userMessage)
			compressionMeta = core.CompressionMeta{
				OriginalTokens:   compaction.EstimateEntriesTokens(entries),
				CompressedTokens: compaction.EstimateEntriesTokens(result.KeptEntries) + result.SummaryTokens,
				DroppedMessages:  result.DroppedCount,
			}
			compressed = true
			log.Printf("event=llm_compaction session_id=%s dropped=%d", sessionID, result.DroppedCount)
		} else if compactErr != nil {
			log.Printf("event=llm_compaction_fallback session_id=%s error=%q", sessionID, compactErr)
		}
	}

	// Fallback: token-based sliding window compression.
	if compressedMessages == nil {
		history := s.sessions.HistoryAsMessages(sessionID)
		baseMessages := append(history, userMessage)
		policy := contextwindow.DefaultPolicy(contextWindow)
		policy = s.adjustCompressionPolicy(providerID, modelID, policy)
		compressedMessages, compressionMeta, compressed = contextwindow.Compress(baseMessages, policy)
	}

	// Inject memory context (MEMORY.md + daily logs) as a leading system message.
	if s.memoryDir != "" {
		memCtx, memErr := memory.LoadMemoryContext(s.memoryDir)
		if memErr != nil {
			log.Printf("event=memory_load_error error=%q", memErr)
		} else if !memCtx.IsEmpty() {
			systemMsg := core.Message{
				Role:    core.RoleSystem,
				Content: memory.BuildSystemPrompt(memCtx),
			}
			compressedMessages = append([]core.Message{systemMsg}, compressedMessages...)
		}
	}

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
				if inferred := s.inferGeminiMissingProjectFromPool(providerID, reason, pool); inferred != nil {
					lastErr = inferred
					break
				}
				soonest := pool.SoonestAvailableAt()
				if soonest.IsZero() {
					lastErr = fmt.Errorf("%w: provider=%s reason=%s", ErrNoAvailableAccount, providerID, reason)
				} else {
					lastErr = fmt.Errorf("%w: provider=%s reason=%s retry_at=%s", ErrNoAvailableAccount, providerID, reason, soonest.Format(time.RFC3339))
				}
			}
			break
		}

		attemptModelID := modelID
		if isDefaultModel {
			resolvedModel, source := s.resolveDefaultModel(ctx, prov, providerID, account)
			attemptModelID = resolvedModel
			log.Printf(
				"event=default_model_resolved provider=%s profile_id=%s source=%s model=%s",
				providerID,
				account.ID,
				source,
				attemptModelID,
			)
		}

		account, refreshErr := s.maybeRefreshAccountCredential(ctx, providerID, attemptModelID, prov, pool, account)
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

		if providerID == "google-gemini-cli" {
			account, err = s.ensureGeminiProject(ctx, prov, pool, account)
			if err != nil {
				lastErr = err
				break
			}
		}

		resp, err := prov.Generate(ctx, provider.GenerateRequest{
			Model:    attemptModelID,
			Messages: compressedMessages,
			Account:  account,
		})
		if err == nil {
			assistant := core.Message{Role: core.RoleAssistant, Content: resp.Text, CreatedAt: time.Now()}
			s.sessions.AppendMessage(sessionID, userMessage, assistant)
			// Async index for memory search.
			if s.searchIndex != nil {
				newEntries := []core.SessionEntry{
					core.MessageToEntry(userMessage),
					core.MessageToEntry(assistant),
				}
				go func() {
					if idxErr := s.searchIndex.Index(sessionID, newEntries); idxErr != nil {
						log.Printf("event=search_index_error session_id=%s error=%q", sessionID, idxErr)
					}
				}()
			}
			if providerID == "google-gemini-cli" {
				if endpoint := strings.TrimSpace(resp.Endpoint); endpoint != "" {
					if account.Metadata == nil {
						account.Metadata = core.Metadata{}
					}
					if strings.TrimSpace(account.Metadata["endpoint"]) != endpoint {
						account.Metadata["endpoint"] = endpoint
						pool.SetCredential(account.ID, account)
						if store := s.authStoreSafe(); store != nil {
							_ = store.UpsertProfile(auth.ProfileMetadata{
								ProfileID: account.ID,
								Provider:  providerID,
								Type:      string(core.AccountOAuth),
								Email:     account.Email,
								ProjectID: strings.TrimSpace(account.Metadata["project_id"]),
								Endpoint:  endpoint,
							})
						}
					}
				}
			}
			pool.MarkUsed(account.ID)
			s.syncProfileState(providerID, account.ID)
			return core.ChatResponse{
				SessionID:   sessionID,
				Provider:    providerID,
				Model:       attemptModelID,
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

// entriesToMessages converts kept SessionEntry slice to Messages for the
// provider, injecting compaction summaries as system messages.
func entriesToMessages(entries []core.SessionEntry) []core.Message {
	msgs := make([]core.Message, 0, len(entries))
	for _, e := range entries {
		switch e.Type {
		case core.EntryMessage:
			msgs = append(msgs, e.ToMessage())
		case core.EntryCompaction:
			if e.Summary != "" {
				msgs = append(msgs, core.Message{
					Role:      core.RoleSystem,
					Content:   e.Summary,
					CreatedAt: e.Timestamp,
				})
			}
		}
	}
	return msgs
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
		if errors.Is(err, provider.ErrProjectDiscoveryFailed) {
			log.Printf("event=project_discovery_failed provider=google-gemini-cli error=%q", err)
		}
		return GeminiOAuthCompleteResult{}, err
	}
	credential.ProjectID = strings.TrimSpace(credential.ProjectID)
	if credential.ProjectID == "" {
		err := fmt.Errorf(
			"%w: Could not discover or provision a Google Cloud project. Set GOOGLE_CLOUD_PROJECT or GOOGLE_CLOUD_PROJECT_ID, then retry OAuth.",
			provider.ErrProjectDiscoveryFailed,
		)
		log.Printf("event=project_discovery_failed provider=google-gemini-cli error=%q", err)
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
		ProjectID: credential.ProjectID,
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
				"project_id": credential.ProjectID,
				"endpoint":   strings.TrimSpace(credential.ActiveEndpoint),
				"profile_id": profileID,
			},
		})
		pool.SetPreferred(profileID)
		s.syncProfileState("google-gemini-cli", profileID)
	}

	log.Printf(
		"event=oauth_complete provider=google-gemini-cli profile_id=%s endpoint=%s project_hash=%s",
		profileID,
		credential.ActiveEndpoint,
		hashProjectIDForLog(credential.ProjectID),
	)
	return GeminiOAuthCompleteResult{
		ProfileID:      profileID,
		Provider:       "google-gemini-cli",
		Email:          strings.TrimSpace(credential.Email),
		ProjectID:      credential.ProjectID,
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

func (s *Service) ensureGeminiProject(
	ctx context.Context,
	prov provider.Provider,
	pool *core.AccountPool,
	account core.Account,
) (core.Account, error) {
	if account.Metadata == nil {
		account.Metadata = core.Metadata{}
	}
	projectID := strings.TrimSpace(account.Metadata["project_id"])
	if projectID == "" {
		if envProject := resolveGoogleCloudProject(); envProject != "" {
			projectID = envProject
			account.Metadata["project_id"] = envProject
		}
	}

	if projectID == "" {
		discoveryProvider, ok := prov.(geminiProjectDiscoveryProvider)
		if !ok {
			return account, fmt.Errorf(
				"%w: profile=%s. Re-run Gemini OAuth or set GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_PROJECT_ID.",
				ErrGeminiMissingProject,
				account.ID,
			)
		}
		discovered, err := discoveryProvider.DiscoverProject(ctx, provider.DiscoverProjectRequest{
			Token: account.Token,
		})
		if err != nil {
			log.Printf(
				"event=project_discovery_failed provider=google-gemini-cli profile_id=%s error=%q",
				account.ID,
				err,
			)
			return account, fmt.Errorf(
				"%w: profile=%s. %v. Re-run Gemini OAuth or set GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_PROJECT_ID.",
				ErrGeminiMissingProject,
				account.ID,
				err,
			)
		}
		projectID = strings.TrimSpace(discovered.ProjectID)
		if projectID != "" {
			account.Metadata["project_id"] = projectID
		}
		if endpoint := strings.TrimSpace(discovered.ActiveEndpoint); endpoint != "" {
			account.Metadata["endpoint"] = endpoint
		}
	}

	projectID = strings.TrimSpace(account.Metadata["project_id"])
	if projectID == "" {
		return account, fmt.Errorf(
			"%w: profile=%s. Re-run Gemini OAuth or set GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_PROJECT_ID.",
			ErrGeminiMissingProject,
			account.ID,
		)
	}

	pool.SetCredential(account.ID, account)
	if store := s.authStoreSafe(); store != nil {
		_ = store.UpsertProfile(auth.ProfileMetadata{
			ProfileID: account.ID,
			Provider:  "google-gemini-cli",
			Type:      string(core.AccountOAuth),
			Email:     account.Email,
			ProjectID: projectID,
			Endpoint:  strings.TrimSpace(account.Metadata["endpoint"]),
		})
	}
	return account, nil
}

func (s *Service) syncProfileState(providerID string, profileID string) {
	if strings.TrimSpace(providerID) == "" || strings.TrimSpace(profileID) == "" {
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
		// OpenClaw-style headroom: keep a large reserve so long contexts have
		// enough room for tool/result growth and provider-side serialization.
		reserveFloor := 20_000
		if policy.MaxContextTokens > 0 {
			maxReserve := policy.MaxContextTokens / 2
			if maxReserve > 0 && reserveFloor > maxReserve {
				reserveFloor = maxReserve
			}
		}
		if policy.ReserveTokens < reserveFloor {
			policy.ReserveTokens = reserveFloor
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

type geminiProjectDiscoveryProvider interface {
	DiscoverProject(ctx context.Context, req provider.DiscoverProjectRequest) (provider.DiscoverProjectResult, error)
}

type aiStudioModelCatalogProvider interface {
	provider.ModelCatalogProvider
	ListModelsWithSource(ctx context.Context, account core.Account) (models []string, source string, cachedUntil time.Time, err error)
}

func (s *Service) resolveDefaultModel(
	ctx context.Context,
	prov provider.Provider,
	providerID string,
	account core.Account,
) (string, string) {
	fallbackModel := fallbackDefaultModel(providerID)
	discoveryProvider, ok := prov.(provider.ModelDiscoveryProvider)
	if !ok {
		return fallbackModel, "fallback"
	}
	modelID, source, err := discoveryProvider.DiscoverPreferredModel(ctx, account)
	if err != nil || strings.TrimSpace(modelID) == "" {
		return fallbackModel, "fallback"
	}
	return strings.TrimSpace(modelID), chooseFirstNonEmpty(source, "discovery")
}

func fallbackDefaultModel(providerID string) string {
	switch strings.TrimSpace(providerID) {
	case "google-gemini-cli":
		return "gemini-3-pro-preview"
	case "google-ai-studio":
		return "gemini-2.5-pro"
	default:
		return "default"
	}
}

func chooseFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *Service) inferGeminiMissingProjectFromPool(
	providerID string,
	reason core.FailureReason,
	pool *core.AccountPool,
) error {
	if providerID != "google-gemini-cli" || reason != core.FailureUnknown {
		return nil
	}
	if resolveGoogleCloudProject() != "" {
		return nil
	}
	if pool != nil && len(pool.Snapshot()) > 0 {
		return nil
	}
	store := s.authStoreSafe()
	if store == nil {
		return nil
	}
	profiles, err := store.ListProfiles("google-gemini-cli")
	if err != nil || len(profiles) == 0 {
		return nil
	}
	readyCount := 0
	missingCount := 0
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ProjectID) == "" {
			missingCount++
			continue
		}
		readyCount++
	}
	if readyCount == 0 && missingCount > 0 {
		return fmt.Errorf(
			"%w: all gemini profiles are missing project_id. Re-run Gemini OAuth or set GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_PROJECT_ID.",
			ErrGeminiMissingProject,
		)
	}
	return nil
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

func hashProjectIDForLog(projectID string) string {
	trimmed := strings.TrimSpace(projectID)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:8])
}

func deriveProfileID(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	if email != "" {
		safe := strings.NewReplacer("@", "_at_", ".", "_", "+", "_", "-", "_").Replace(email)
		return "google-gemini-cli:" + safe
	}
	return fmt.Sprintf("google-gemini-cli:%d", time.Now().Unix())
}

func deriveAIStudioProfileID(displayName, apiKey string) string {
	base := strings.TrimSpace(strings.ToLower(displayName))
	if base == "" {
		base = "key"
	}
	base = sanitizeProfileSlug(base)
	suffix := strings.TrimSpace(apiKey)
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}
	suffix = sanitizeProfileSlug(strings.ToLower(suffix))
	if suffix == "" {
		suffix = fmt.Sprintf("%d", time.Now().Unix())
	}
	return "google-ai-studio:" + base + "_" + suffix
}

func sanitizeProfileSlug(raw string) string {
	replacer := strings.NewReplacer(
		" ", "_",
		".", "_",
		"-", "_",
		":", "_",
		"/", "_",
		"\\", "_",
		"@", "_",
	)
	slug := replacer.Replace(strings.TrimSpace(raw))
	var out strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '_':
			out.WriteRune(r)
		}
	}
	clean := strings.Trim(out.String(), "_")
	if clean == "" {
		return "profile"
	}
	return clean
}

func maskAPIKeyForHint(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "****"
	}
	if len(apiKey) <= 6 {
		return "****" + apiKey
	}
	return "****" + apiKey[len(apiKey)-6:]
}

func pEndpoint(prov provider.Provider) string {
	switch p := prov.(type) {
	case interface{ BaseURL() string }:
		return strings.TrimSpace(p.BaseURL())
	default:
		return ""
	}
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
