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
	"github.com/doeshing/nekoclaw/internal/mcp"
	"github.com/doeshing/nekoclaw/internal/memory"
	"github.com/doeshing/nekoclaw/internal/persona"
	"github.com/doeshing/nekoclaw/internal/provider"
	"github.com/doeshing/nekoclaw/internal/tooling"
)

var ErrProviderNotFound = errors.New("provider not found")
var ErrNoAvailableAccount = errors.New("no available account")
var ErrGeminiMissingProject = errors.New("gemini project is required")
var ErrInvalidAPIKey = errors.New("invalid api key")
var ErrInvalidOAuthToken = errors.New("invalid oauth token")
var ErrInvalidSetupToken = errors.New("invalid setup token")
var ErrKeyValidationFailed = errors.New("key validation failed")
var ErrProfileNotFound = errors.New("profile not found")
var ErrProfileInUse = errors.New("profile in use")
var ErrProviderNotReady = errors.New("provider not ready")
var ErrToolsNotSupported = errors.New("tools not supported by provider")

type Service struct {
	mu                sync.RWMutex
	providers         map[string]provider.Provider
	pools             map[string]*core.AccountPool
	sessions          *core.SessionStore
	lifecycle         *core.SessionLifecycle
	oauthManager      *auth.GeminiOAuthManager
	anthropicLoginMgr *auth.AnthropicLoginManager
	openAICodexLogin  *auth.OpenAICodexLoginManager
	authStore         *auth.Store
	memoryDir         string
	searchIndex       *memory.SearchIndex
	preferredProfiles map[string]string
	fallbacks         map[string][]string // primary provider -> fallback provider IDs
	toolRuntime       *tooling.Runtime
	mcpManager        *mcp.Manager
	personaManager    *persona.Manager
	titleGenPending   sync.Map // sessionID -> bool; dedup concurrent title generation
}

type ServiceOptions struct {
	SessionStore  *core.SessionStore
	Lifecycle     *core.SessionLifecycle
	MemoryDir     string
	SearchIndex   *memory.SearchIndex
	WorkspaceRoot string
	ToolRunTTL    time.Duration
	MCPConfigDir  string
	PersonasDir   string
}

func NewService(opts ServiceOptions) *Service {
	sessions := opts.SessionStore
	if sessions == nil {
		sessions = core.NewSessionStore()
	}
	svc := &Service{
		providers:         map[string]provider.Provider{},
		pools:             map[string]*core.AccountPool{},
		sessions:          sessions,
		lifecycle:         opts.Lifecycle,
		memoryDir:         opts.MemoryDir,
		searchIndex:       opts.SearchIndex,
		preferredProfiles: map[string]string{},
		fallbacks:         map[string][]string{},
	}
	policy := tooling.DefaultPolicy(opts.WorkspaceRoot)
	builtinExecutor := tooling.NewRuntimeExecutor(serviceToolBackend{svc: svc}, policy)

	// If MCP config directory is provided, create a Manager and wrap with CompositeExecutor.
	var executor tooling.Executor = builtinExecutor
	if opts.MCPConfigDir != "" {
		mcpMgr := mcp.NewManager(opts.MCPConfigDir)
		svc.mcpManager = mcpMgr
		executor = tooling.NewCompositeExecutor(builtinExecutor, mcpMgr)
	}

	svc.toolRuntime = tooling.NewRuntime(executor, tooling.NewApprovalStore(opts.ToolRunTTL))

	if opts.PersonasDir != "" {
		svc.personaManager = persona.NewManager(opts.PersonasDir)
	}

	return svc
}

func (s *Service) ListSessions() []core.SessionMetadata {
	return s.sessions.ListSessions()
}

func (s *Service) DeleteSession(sessionID string) error {
	return s.sessions.DeleteSession(sessionID)
}

// StartMCP initializes all MCP server connections. Non-fatal errors are logged.
func (s *Service) StartMCP(ctx context.Context) error {
	if s.mcpManager == nil {
		return nil
	}
	return s.mcpManager.Start(ctx)
}

// StopMCP gracefully shuts down all MCP server connections.
func (s *Service) StopMCP() error {
	if s.mcpManager == nil {
		return nil
	}
	return s.mcpManager.Stop()
}

// MCPServers returns status info for all configured MCP servers.
func (s *Service) MCPServers() []mcp.ServerInfo {
	if s.mcpManager == nil {
		return nil
	}
	return s.mcpManager.Servers()
}

// MCPToolDefinitions returns all tool definitions from connected MCP servers.
func (s *Service) MCPToolDefinitions() []mcp.ToolInfo {
	if s.mcpManager == nil {
		return nil
	}
	return s.mcpManager.ToolInfos()
}

// ReconnectMCPServer attempts to reconnect a specific MCP server.
func (s *Service) ReconnectMCPServer(ctx context.Context, serverName string) error {
	if s.mcpManager == nil {
		return fmt.Errorf("mcp not configured")
	}
	return s.mcpManager.Reconnect(ctx, serverName)
}

// MCPBuiltinServers returns all builtin MCP server definitions with their current state.
func (s *Service) MCPBuiltinServers() []mcp.BuiltinServerInfo {
	if s.mcpManager == nil {
		return nil
	}
	return s.mcpManager.BuiltinServers()
}

// ToggleMCPBuiltin enables or disables a builtin MCP server.
func (s *Service) ToggleMCPBuiltin(ctx context.Context, name string, enabled bool) error {
	if s.mcpManager == nil {
		return fmt.Errorf("mcp not configured")
	}
	return s.mcpManager.SetBuiltinEnabled(ctx, name, enabled)
}

// ---------------------------------------------------------------------------
// Persona management
// ---------------------------------------------------------------------------

// StartPersonas loads all persona definitions from disk.
func (s *Service) StartPersonas() error {
	if s.personaManager == nil {
		return nil
	}
	return s.personaManager.Start()
}

// ListPersonas returns lightweight info for every loaded persona.
func (s *Service) ListPersonas() []persona.PersonaInfo {
	if s.personaManager == nil {
		return nil
	}
	return s.personaManager.List()
}

// ActivePersona returns the currently active persona, or nil.
func (s *Service) ActivePersona() *persona.PersonaInfo {
	if s.personaManager == nil {
		return nil
	}
	return s.personaManager.ActiveInfo()
}

// SetActivePersona switches the active persona by directory name.
func (s *Service) SetActivePersona(dirName string) error {
	if s.personaManager == nil {
		return fmt.Errorf("personas not configured")
	}
	return s.personaManager.SetActive(dirName)
}

// ClearActivePersona deactivates the current persona.
func (s *Service) ClearActivePersona() error {
	if s.personaManager == nil {
		return fmt.Errorf("personas not configured")
	}
	return s.personaManager.ClearActive()
}

// ReloadPersonas re-scans the persona directory from disk.
func (s *Service) ReloadPersonas() error {
	if s.personaManager == nil {
		return fmt.Errorf("personas not configured")
	}
	return s.personaManager.Reload()
}

// PersonaManager exposes the persona manager for direct access (e.g. rendering).
func (s *Service) PersonaManager() *persona.Manager {
	return s.personaManager
}

// RenameSession sets a custom title for the given session.
func (s *Service) RenameSession(sessionID, title string) error {
	sessionID = strings.TrimSpace(sessionID)
	title = strings.TrimSpace(title)
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if _, exists := s.sessions.GetMetadata(sessionID); !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	s.sessions.SetTitle(sessionID, title)
	return nil
}

// TranscriptMessage is a lightweight message for TUI display (no base64 image data).
type TranscriptMessage struct {
	Role       string   `json:"role"`
	Content    string   `json:"content"`
	ImageNames []string `json:"image_names,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

// GetSessionTranscript returns user and assistant messages for display in the TUI.
// Image base64 data is stripped; only file names are included.
func (s *Service) GetSessionTranscript(sessionID string) []TranscriptMessage {
	msgs := s.sessions.HistoryAsMessages(sessionID)
	display := make([]TranscriptMessage, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case core.RoleUser, core.RoleAssistant:
			var imageNames []string
			for _, img := range m.Images {
				imageNames = append(imageNames, img.FileName)
			}
			display = append(display, TranscriptMessage{
				Role:       string(m.Role),
				Content:    m.Content,
				ImageNames: imageNames,
				CreatedAt:  m.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
			})
		}
	}
	return display
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

func (s *Service) SetAnthropicLoginManager(manager *auth.AnthropicLoginManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.anthropicLoginMgr = manager
}

func (s *Service) SetOpenAICodexLoginManager(manager *auth.OpenAICodexLoginManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openAICodexLogin = manager
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

type AnthropicAddTokenRequest struct {
	SetupToken   string `json:"setup_token"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type AnthropicAddAPIKeyRequest struct {
	APIKey       string `json:"api_key"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type AnthropicAddCredentialResult struct {
	ProfileID   string `json:"profile_id"`
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	KeyHint     string `json:"key_hint"`
	Preferred   bool   `json:"preferred"`
	Available   bool   `json:"available"`
}

type AnthropicBrowserStartRequest struct {
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
	Mode         string `json:"mode,omitempty"` // auto|local|remote
}

type AnthropicBrowserStartResult struct {
	JobID      string    `json:"job_id"`
	Provider   string    `json:"provider"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	Message    string    `json:"message,omitempty"`
	ManualHint string    `json:"manual_hint,omitempty"`
}

type AnthropicBrowserJobEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type AnthropicBrowserJobResult struct {
	JobID        string                     `json:"job_id"`
	Provider     string                     `json:"provider"`
	Mode         string                     `json:"mode"`
	Status       string                     `json:"status"`
	Events       []AnthropicBrowserJobEvent `json:"events,omitempty"`
	ProfileID    string                     `json:"profile_id,omitempty"`
	KeyHint      string                     `json:"key_hint,omitempty"`
	ExpiresAt    time.Time                  `json:"expires_at"`
	Message      string                     `json:"message,omitempty"`
	ManualHint   string                     `json:"manual_hint,omitempty"`
	ErrorCode    string                     `json:"error_code,omitempty"`
	ErrorMessage string                     `json:"error_message,omitempty"`
}

type AnthropicBrowserManualCompleteRequest struct {
	JobID        string `json:"job_id"`
	SetupToken   string `json:"setup_token"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type AnthropicProfileStatus struct {
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

type OpenAIAddKeyRequest struct {
	APIKey       string `json:"api_key"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type OpenAICodexAddTokenRequest struct {
	Token        string `json:"token"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type OpenAIAddCredentialResult struct {
	ProfileID   string `json:"profile_id"`
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	KeyHint     string `json:"key_hint"`
	Preferred   bool   `json:"preferred"`
	Available   bool   `json:"available"`
}

type OpenAICodexBrowserStartRequest struct {
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
	Mode         string `json:"mode,omitempty"` // auto|local|remote
}

type OpenAICodexBrowserStartResult struct {
	JobID      string    `json:"job_id"`
	Provider   string    `json:"provider"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	Message    string    `json:"message,omitempty"`
	ManualHint string    `json:"manual_hint,omitempty"`
}

type OpenAICodexBrowserJobEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type OpenAICodexBrowserJobResult struct {
	JobID        string                       `json:"job_id"`
	Provider     string                       `json:"provider"`
	Mode         string                       `json:"mode"`
	Status       string                       `json:"status"`
	Events       []OpenAICodexBrowserJobEvent `json:"events,omitempty"`
	ProfileID    string                       `json:"profile_id,omitempty"`
	KeyHint      string                       `json:"key_hint,omitempty"`
	ExpiresAt    time.Time                    `json:"expires_at"`
	Message      string                       `json:"message,omitempty"`
	ManualHint   string                       `json:"manual_hint,omitempty"`
	ErrorCode    string                       `json:"error_code,omitempty"`
	ErrorMessage string                       `json:"error_message,omitempty"`
}

type OpenAICodexBrowserManualCompleteRequest struct {
	JobID        string `json:"job_id"`
	Token        string `json:"token"`
	DisplayName  string `json:"display_name,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	SetPreferred bool   `json:"set_preferred,omitempty"`
}

type OpenAIProfileStatus struct {
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

func (s *Service) AddAnthropicToken(_ context.Context, req AnthropicAddTokenRequest) (AnthropicAddCredentialResult, error) {
	setupToken := strings.TrimSpace(req.SetupToken)
	if err := provider.ValidateAnthropicSetupToken(setupToken); err != nil {
		return AnthropicAddCredentialResult{}, fmt.Errorf("%w: %v", ErrInvalidSetupToken, err)
	}
	return s.addAnthropicCredential(commonAnthropicAddRequest{
		secret:       setupToken,
		accountType:  core.AccountToken,
		displayName:  req.DisplayName,
		profileID:    req.ProfileID,
		setPreferred: req.SetPreferred,
	})
}

func (s *Service) AddAnthropicAPIKey(_ context.Context, req AnthropicAddAPIKeyRequest) (AnthropicAddCredentialResult, error) {
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		return AnthropicAddCredentialResult{}, fmt.Errorf("%w: api_key is required", ErrInvalidAPIKey)
	}
	return s.addAnthropicCredential(commonAnthropicAddRequest{
		secret:       apiKey,
		accountType:  core.AccountAPIKey,
		displayName:  req.DisplayName,
		profileID:    req.ProfileID,
		setPreferred: req.SetPreferred,
	})
}

func (s *Service) ListAnthropicProfiles() ([]AnthropicProfileStatus, error) {
	store := s.authStoreSafe()
	if store == nil {
		return nil, fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	profiles, err := store.ListProfiles("anthropic")
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	preferred := s.preferredProfiles["anthropic"]
	pool := s.pools["anthropic"]
	s.mu.RUnlock()

	snapByID := map[string]core.AccountSnapshot{}
	if pool != nil {
		for _, snap := range pool.Snapshot() {
			snapByID[snap.ID] = snap
		}
	}
	now := time.Now()
	result := make([]AnthropicProfileStatus, 0, len(profiles))
	for _, profile := range profiles {
		status := AnthropicProfileStatus{
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

func (s *Service) UseAnthropicProfile(profileID string) error {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("profile_id is required")
	}
	store := s.authStoreSafe()
	if store == nil {
		return fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	if _, err := store.GetProfile("anthropic", profileID); err != nil {
		if errors.Is(err, auth.ErrProfileNotFound) {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
		return err
	}

	s.mu.Lock()
	pool := s.pools["anthropic"]
	s.preferredProfiles["anthropic"] = profileID
	s.mu.Unlock()
	if pool != nil {
		if ok := pool.SetPreferred(profileID); !ok {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
	}
	log.Printf("event=anthropic_profile_use provider=anthropic profile_id=%s", profileID)
	return nil
}

func (s *Service) DeleteAnthropicProfile(profileID string) error {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("profile_id is required")
	}
	store := s.authStoreSafe()
	if store == nil {
		return fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	if _, err := store.GetProfile("anthropic", profileID); err != nil {
		if errors.Is(err, auth.ErrProfileNotFound) {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
		return err
	}

	_ = store.DeleteCredential("anthropic", profileID)
	if err := store.DeleteProfile("anthropic", profileID); err != nil && !errors.Is(err, auth.ErrProfileNotFound) {
		return err
	}

	s.mu.Lock()
	if s.preferredProfiles["anthropic"] == profileID {
		delete(s.preferredProfiles, "anthropic")
	}
	pool := s.pools["anthropic"]
	s.mu.Unlock()
	if pool != nil {
		pool.RemoveAccount(profileID)
	}
	log.Printf("event=anthropic_profile_delete provider=anthropic profile_id=%s", profileID)
	return nil
}

func (s *Service) AddOpenAIKey(_ context.Context, req OpenAIAddKeyRequest) (OpenAIAddCredentialResult, error) {
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		return OpenAIAddCredentialResult{}, fmt.Errorf("%w: api_key is required", ErrInvalidAPIKey)
	}
	return s.addOpenAICredential(commonOpenAIAddRequest{
		providerID:   "openai",
		secret:       apiKey,
		accountType:  core.AccountAPIKey,
		displayName:  req.DisplayName,
		profileID:    req.ProfileID,
		setPreferred: req.SetPreferred,
	})
}

func (s *Service) AddOpenAICodexToken(_ context.Context, req OpenAICodexAddTokenRequest) (OpenAIAddCredentialResult, error) {
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return OpenAIAddCredentialResult{}, fmt.Errorf("%w: token is required", ErrInvalidOAuthToken)
	}
	return s.addOpenAICredential(commonOpenAIAddRequest{
		providerID:   "openai-codex",
		secret:       token,
		accountType:  core.AccountOAuth,
		displayName:  req.DisplayName,
		profileID:    req.ProfileID,
		setPreferred: req.SetPreferred,
	})
}

func (s *Service) ListOpenAIProfiles(providerID string) ([]OpenAIProfileStatus, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID != "openai" && providerID != "openai-codex" {
		return nil, fmt.Errorf("%w: unsupported provider %q", ErrProviderNotFound, providerID)
	}
	store := s.authStoreSafe()
	if store == nil {
		return nil, fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	profiles, err := store.ListProfiles(providerID)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	preferred := s.preferredProfiles[providerID]
	pool := s.pools[providerID]
	s.mu.RUnlock()

	snapByID := map[string]core.AccountSnapshot{}
	if pool != nil {
		for _, snap := range pool.Snapshot() {
			snapByID[snap.ID] = snap
		}
	}
	now := time.Now()
	result := make([]OpenAIProfileStatus, 0, len(profiles))
	for _, profile := range profiles {
		status := OpenAIProfileStatus{
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

func (s *Service) UseOpenAIProfile(providerID, profileID string) error {
	providerID = strings.TrimSpace(providerID)
	profileID = strings.TrimSpace(profileID)
	if providerID != "openai" && providerID != "openai-codex" {
		return fmt.Errorf("%w: unsupported provider %q", ErrProviderNotFound, providerID)
	}
	if profileID == "" {
		return fmt.Errorf("profile_id is required")
	}
	store := s.authStoreSafe()
	if store == nil {
		return fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	if _, err := store.GetProfile(providerID, profileID); err != nil {
		if errors.Is(err, auth.ErrProfileNotFound) {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
		return err
	}

	s.mu.Lock()
	pool := s.pools[providerID]
	s.preferredProfiles[providerID] = profileID
	s.mu.Unlock()
	if pool != nil {
		if ok := pool.SetPreferred(profileID); !ok {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
	}
	log.Printf("event=openai_profile_use provider=%s profile_id=%s", providerID, profileID)
	return nil
}

func (s *Service) DeleteOpenAIProfile(providerID, profileID string) error {
	providerID = strings.TrimSpace(providerID)
	profileID = strings.TrimSpace(profileID)
	if providerID != "openai" && providerID != "openai-codex" {
		return fmt.Errorf("%w: unsupported provider %q", ErrProviderNotFound, providerID)
	}
	if profileID == "" {
		return fmt.Errorf("profile_id is required")
	}
	store := s.authStoreSafe()
	if store == nil {
		return fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	if _, err := store.GetProfile(providerID, profileID); err != nil {
		if errors.Is(err, auth.ErrProfileNotFound) {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
		return err
	}

	_ = store.DeleteCredential(providerID, profileID)
	if err := store.DeleteProfile(providerID, profileID); err != nil && !errors.Is(err, auth.ErrProfileNotFound) {
		return err
	}

	s.mu.Lock()
	if s.preferredProfiles[providerID] == profileID {
		delete(s.preferredProfiles, providerID)
	}
	pool := s.pools[providerID]
	s.mu.Unlock()
	if pool != nil {
		pool.RemoveAccount(profileID)
	}
	log.Printf("event=openai_profile_delete provider=%s profile_id=%s", providerID, profileID)
	return nil
}

func (s *Service) StartAnthropicBrowserLogin(
	ctx context.Context,
	req AnthropicBrowserStartRequest,
) (AnthropicBrowserStartResult, error) {
	manager := s.anthropicLoginManagerSafe()
	if manager == nil {
		return AnthropicBrowserStartResult{}, fmt.Errorf("%w: anthropic login manager not configured", ErrProviderNotReady)
	}
	if _, _, err := s.resolveProviderPool("anthropic"); err != nil {
		return AnthropicBrowserStartResult{}, fmt.Errorf("%w: %v", ErrProviderNotReady, err)
	}

	snapshot, err := manager.Start(ctx, auth.AnthropicLoginStartRequest{
		DisplayName:  strings.TrimSpace(req.DisplayName),
		ProfileID:    strings.TrimSpace(req.ProfileID),
		SetPreferred: req.SetPreferred,
		Mode:         strings.TrimSpace(req.Mode),
		OnToken: func(_ context.Context, token, displayName, profileID string, setPreferred bool) (auth.AnthropicPersistResult, error) {
			added, addErr := s.addAnthropicCredential(commonAnthropicAddRequest{
				secret:       token,
				accountType:  core.AccountToken,
				displayName:  displayName,
				profileID:    profileID,
				setPreferred: setPreferred,
			})
			if addErr != nil {
				return auth.AnthropicPersistResult{}, addErr
			}
			return auth.AnthropicPersistResult{
				ProfileID:   added.ProfileID,
				DisplayName: added.DisplayName,
				KeyHint:     added.KeyHint,
				Preferred:   added.Preferred,
			}, nil
		},
	})
	if err != nil {
		return AnthropicBrowserStartResult{}, err
	}
	log.Printf(
		"event=anthropic_browser_login_start provider=anthropic job_id=%s mode=%s status=%s",
		snapshot.JobID,
		snapshot.Mode,
		snapshot.Status,
	)
	return AnthropicBrowserStartResult{
		JobID:      snapshot.JobID,
		Provider:   snapshot.Provider,
		Mode:       snapshot.Mode,
		Status:     snapshot.Status,
		ExpiresAt:  snapshot.ExpiresAt,
		Message:    snapshot.Message,
		ManualHint: snapshot.ManualHint,
	}, nil
}

func (s *Service) GetAnthropicBrowserLoginJob(
	_ context.Context,
	jobID string,
) (AnthropicBrowserJobResult, error) {
	manager := s.anthropicLoginManagerSafe()
	if manager == nil {
		return AnthropicBrowserJobResult{}, fmt.Errorf("%w: anthropic login manager not configured", ErrProviderNotReady)
	}
	snapshot, err := manager.Get(strings.TrimSpace(jobID))
	if err != nil {
		return AnthropicBrowserJobResult{}, err
	}
	events := make([]AnthropicBrowserJobEvent, 0, len(snapshot.Events))
	for _, event := range snapshot.Events {
		events = append(events, AnthropicBrowserJobEvent{
			At:      event.At,
			Message: event.Message,
		})
	}
	if snapshot.Status == string(auth.AnthropicLoginStatusCompleted) {
		log.Printf(
			"event=anthropic_browser_login_complete provider=anthropic job_id=%s profile_id=%s",
			snapshot.JobID,
			snapshot.ProfileID,
		)
	}
	if snapshot.Status == string(auth.AnthropicLoginStatusFailed) {
		log.Printf(
			"event=anthropic_browser_login_failed provider=anthropic job_id=%s failure_reason=%s",
			snapshot.JobID,
			snapshot.ErrorCode,
		)
	}
	return AnthropicBrowserJobResult{
		JobID:        snapshot.JobID,
		Provider:     snapshot.Provider,
		Mode:         snapshot.Mode,
		Status:       snapshot.Status,
		Events:       events,
		ProfileID:    snapshot.ProfileID,
		KeyHint:      snapshot.KeyHint,
		ExpiresAt:    snapshot.ExpiresAt,
		Message:      snapshot.Message,
		ManualHint:   snapshot.ManualHint,
		ErrorCode:    snapshot.ErrorCode,
		ErrorMessage: snapshot.ErrorMessage,
	}, nil
}

func (s *Service) CompleteAnthropicBrowserLoginManual(
	ctx context.Context,
	req AnthropicBrowserManualCompleteRequest,
) (AnthropicAddCredentialResult, error) {
	manager := s.anthropicLoginManagerSafe()
	if manager == nil {
		return AnthropicAddCredentialResult{}, fmt.Errorf("%w: anthropic login manager not configured", ErrProviderNotReady)
	}
	snapshot, err := manager.CompleteManual(ctx, auth.AnthropicLoginManualCompleteRequest{
		JobID:        strings.TrimSpace(req.JobID),
		SetupToken:   strings.TrimSpace(req.SetupToken),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		ProfileID:    strings.TrimSpace(req.ProfileID),
		SetPreferred: req.SetPreferred,
		OnToken: func(_ context.Context, token, displayName, profileID string, setPreferred bool) (auth.AnthropicPersistResult, error) {
			added, addErr := s.addAnthropicCredential(commonAnthropicAddRequest{
				secret:       token,
				accountType:  core.AccountToken,
				displayName:  displayName,
				profileID:    profileID,
				setPreferred: setPreferred,
			})
			if addErr != nil {
				return auth.AnthropicPersistResult{}, addErr
			}
			return auth.AnthropicPersistResult{
				ProfileID:   added.ProfileID,
				DisplayName: added.DisplayName,
				KeyHint:     added.KeyHint,
				Preferred:   added.Preferred,
			}, nil
		},
	})
	if err != nil {
		return AnthropicAddCredentialResult{}, err
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if store := s.authStoreSafe(); store != nil && strings.TrimSpace(snapshot.ProfileID) != "" {
		if profile, getErr := store.GetProfile("anthropic", snapshot.ProfileID); getErr == nil {
			if displayName == "" {
				displayName = strings.TrimSpace(profile.DisplayName)
			}
		}
	}
	if displayName == "" {
		displayName = chooseFirstNonEmpty(snapshot.ProfileID, "anthropic token")
	}
	preferred := s.preferredProfile("anthropic") == strings.TrimSpace(snapshot.ProfileID)
	return AnthropicAddCredentialResult{
		ProfileID:   snapshot.ProfileID,
		Provider:    "anthropic",
		Type:        string(core.AccountToken),
		DisplayName: displayName,
		KeyHint:     snapshot.KeyHint,
		Preferred:   preferred,
		Available:   true,
	}, nil
}

func (s *Service) CancelAnthropicBrowserLogin(
	_ context.Context,
	jobID string,
) (auth.AnthropicLoginCancelResult, error) {
	manager := s.anthropicLoginManagerSafe()
	if manager == nil {
		return auth.AnthropicLoginCancelResult{}, fmt.Errorf("%w: anthropic login manager not configured", ErrProviderNotReady)
	}
	result, err := manager.Cancel(strings.TrimSpace(jobID))
	if err != nil {
		return auth.AnthropicLoginCancelResult{}, err
	}
	log.Printf(
		"event=anthropic_browser_login_cancelled provider=anthropic job_id=%s status=%s",
		result.JobID,
		result.Status,
	)
	return result, nil
}

func (s *Service) StartOpenAICodexBrowserLogin(
	ctx context.Context,
	req OpenAICodexBrowserStartRequest,
) (OpenAICodexBrowserStartResult, error) {
	manager := s.openAICodexLoginManagerSafe()
	if manager == nil {
		return OpenAICodexBrowserStartResult{}, fmt.Errorf("%w: openai codex login manager not configured", ErrProviderNotReady)
	}
	if _, _, err := s.resolveProviderPool("openai-codex"); err != nil {
		return OpenAICodexBrowserStartResult{}, fmt.Errorf("%w: %v", ErrProviderNotReady, err)
	}

	snapshot, err := manager.Start(ctx, auth.OpenAICodexLoginStartRequest{
		DisplayName:  strings.TrimSpace(req.DisplayName),
		ProfileID:    strings.TrimSpace(req.ProfileID),
		SetPreferred: req.SetPreferred,
		Mode:         strings.TrimSpace(req.Mode),
		OnToken: func(_ context.Context, token, displayName, profileID string, setPreferred bool) (auth.OpenAICodexPersistResult, error) {
			added, addErr := s.addOpenAICredential(commonOpenAIAddRequest{
				providerID:   "openai-codex",
				secret:       token,
				accountType:  core.AccountOAuth,
				displayName:  displayName,
				profileID:    profileID,
				setPreferred: setPreferred,
			})
			if addErr != nil {
				return auth.OpenAICodexPersistResult{}, addErr
			}
			return auth.OpenAICodexPersistResult{
				ProfileID:   added.ProfileID,
				DisplayName: added.DisplayName,
				KeyHint:     added.KeyHint,
				Preferred:   added.Preferred,
			}, nil
		},
	})
	if err != nil {
		return OpenAICodexBrowserStartResult{}, err
	}
	log.Printf(
		"event=openai_codex_browser_login_start provider=openai-codex job_id=%s mode=%s status=%s",
		snapshot.JobID,
		snapshot.Mode,
		snapshot.Status,
	)
	return OpenAICodexBrowserStartResult{
		JobID:      snapshot.JobID,
		Provider:   snapshot.Provider,
		Mode:       snapshot.Mode,
		Status:     snapshot.Status,
		ExpiresAt:  snapshot.ExpiresAt,
		Message:    snapshot.Message,
		ManualHint: snapshot.ManualHint,
	}, nil
}

func (s *Service) GetOpenAICodexBrowserLoginJob(
	_ context.Context,
	jobID string,
) (OpenAICodexBrowserJobResult, error) {
	manager := s.openAICodexLoginManagerSafe()
	if manager == nil {
		return OpenAICodexBrowserJobResult{}, fmt.Errorf("%w: openai codex login manager not configured", ErrProviderNotReady)
	}
	snapshot, err := manager.Get(strings.TrimSpace(jobID))
	if err != nil {
		return OpenAICodexBrowserJobResult{}, err
	}
	events := make([]OpenAICodexBrowserJobEvent, 0, len(snapshot.Events))
	for _, event := range snapshot.Events {
		events = append(events, OpenAICodexBrowserJobEvent{
			At:      event.At,
			Message: event.Message,
		})
	}
	if snapshot.Status == string(auth.OpenAICodexLoginStatusCompleted) {
		log.Printf(
			"event=openai_codex_browser_login_complete provider=openai-codex job_id=%s profile_id=%s",
			snapshot.JobID,
			snapshot.ProfileID,
		)
	}
	if snapshot.Status == string(auth.OpenAICodexLoginStatusFailed) {
		log.Printf(
			"event=openai_codex_browser_login_failed provider=openai-codex job_id=%s failure_reason=%s",
			snapshot.JobID,
			snapshot.ErrorCode,
		)
	}
	return OpenAICodexBrowserJobResult{
		JobID:        snapshot.JobID,
		Provider:     snapshot.Provider,
		Mode:         snapshot.Mode,
		Status:       snapshot.Status,
		Events:       events,
		ProfileID:    snapshot.ProfileID,
		KeyHint:      snapshot.KeyHint,
		ExpiresAt:    snapshot.ExpiresAt,
		Message:      snapshot.Message,
		ManualHint:   snapshot.ManualHint,
		ErrorCode:    snapshot.ErrorCode,
		ErrorMessage: snapshot.ErrorMessage,
	}, nil
}

func (s *Service) CompleteOpenAICodexBrowserLoginManual(
	ctx context.Context,
	req OpenAICodexBrowserManualCompleteRequest,
) (OpenAIAddCredentialResult, error) {
	manager := s.openAICodexLoginManagerSafe()
	if manager == nil {
		return OpenAIAddCredentialResult{}, fmt.Errorf("%w: openai codex login manager not configured", ErrProviderNotReady)
	}
	snapshot, err := manager.CompleteManual(ctx, auth.OpenAICodexLoginManualCompleteRequest{
		JobID:        strings.TrimSpace(req.JobID),
		Token:        strings.TrimSpace(req.Token),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		ProfileID:    strings.TrimSpace(req.ProfileID),
		SetPreferred: req.SetPreferred,
		OnToken: func(_ context.Context, token, displayName, profileID string, setPreferred bool) (auth.OpenAICodexPersistResult, error) {
			added, addErr := s.addOpenAICredential(commonOpenAIAddRequest{
				providerID:   "openai-codex",
				secret:       token,
				accountType:  core.AccountOAuth,
				displayName:  displayName,
				profileID:    profileID,
				setPreferred: setPreferred,
			})
			if addErr != nil {
				return auth.OpenAICodexPersistResult{}, addErr
			}
			return auth.OpenAICodexPersistResult{
				ProfileID:   added.ProfileID,
				DisplayName: added.DisplayName,
				KeyHint:     added.KeyHint,
				Preferred:   added.Preferred,
			}, nil
		},
	})
	if err != nil {
		return OpenAIAddCredentialResult{}, err
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if store := s.authStoreSafe(); store != nil && strings.TrimSpace(snapshot.ProfileID) != "" {
		if profile, getErr := store.GetProfile("openai-codex", snapshot.ProfileID); getErr == nil {
			if displayName == "" {
				displayName = strings.TrimSpace(profile.DisplayName)
			}
		}
	}
	if displayName == "" {
		displayName = chooseFirstNonEmpty(snapshot.ProfileID, "OpenAI Codex OAuth token")
	}
	preferred := s.preferredProfile("openai-codex") == strings.TrimSpace(snapshot.ProfileID)
	return OpenAIAddCredentialResult{
		ProfileID:   snapshot.ProfileID,
		Provider:    "openai-codex",
		Type:        string(core.AccountOAuth),
		DisplayName: displayName,
		KeyHint:     snapshot.KeyHint,
		Preferred:   preferred,
		Available:   true,
	}, nil
}

func (s *Service) CancelOpenAICodexBrowserLogin(
	_ context.Context,
	jobID string,
) (auth.OpenAICodexLoginCancelResult, error) {
	manager := s.openAICodexLoginManagerSafe()
	if manager == nil {
		return auth.OpenAICodexLoginCancelResult{}, fmt.Errorf("%w: openai codex login manager not configured", ErrProviderNotReady)
	}
	result, err := manager.Cancel(strings.TrimSpace(jobID))
	if err != nil {
		return auth.OpenAICodexLoginCancelResult{}, err
	}
	log.Printf(
		"event=openai_codex_browser_login_cancelled provider=openai-codex job_id=%s status=%s",
		result.JobID,
		result.Status,
	)
	return result, nil
}

type commonAnthropicAddRequest struct {
	secret       string
	accountType  core.AccountType
	displayName  string
	profileID    string
	setPreferred bool
}

type commonOpenAIAddRequest struct {
	providerID   string
	secret       string
	accountType  core.AccountType
	displayName  string
	profileID    string
	setPreferred bool
}

func (s *Service) addOpenAICredential(req commonOpenAIAddRequest) (OpenAIAddCredentialResult, error) {
	store := s.authStoreSafe()
	if store == nil {
		return OpenAIAddCredentialResult{}, fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	providerID := strings.TrimSpace(req.providerID)
	if providerID != "openai" && providerID != "openai-codex" {
		return OpenAIAddCredentialResult{}, fmt.Errorf("%w: unsupported provider %q", ErrProviderNotFound, providerID)
	}
	prov, pool, err := s.resolveProviderPool(providerID)
	if err != nil {
		return OpenAIAddCredentialResult{}, fmt.Errorf("%w: %v", ErrProviderNotReady, err)
	}

	secret := strings.TrimSpace(req.secret)
	accountType := req.accountType
	displayName := strings.TrimSpace(req.displayName)
	profileID := strings.TrimSpace(req.profileID)
	if profileID == "" {
		profileID = deriveOpenAIProfileID(providerID, accountType, displayName, secret)
	}
	keyHint := maskAPIKeyForHint(secret)
	if displayName == "" {
		if providerID == "openai-codex" {
			displayName = "OpenAI Codex OAuth " + keyHint
		} else {
			displayName = "OpenAI API key " + keyHint
		}
	}

	if err := store.SaveCredential(providerID, profileID, auth.Credential{
		AccessToken: secret,
	}); err != nil {
		return OpenAIAddCredentialResult{}, err
	}
	meta := auth.ProfileMetadata{
		ProfileID:   profileID,
		Provider:    providerID,
		Type:        string(accountType),
		DisplayName: displayName,
		KeyHint:     keyHint,
		Endpoint:    pEndpoint(prov),
	}
	if err := store.UpsertProfile(meta); err != nil {
		_ = store.DeleteCredential(providerID, profileID)
		return OpenAIAddCredentialResult{}, err
	}

	pool.SetCredential(profileID, core.Account{
		ID:       profileID,
		Provider: providerID,
		Type:     accountType,
		Token:    secret,
		Metadata: core.Metadata{
			"display_name": displayName,
			"key_hint":     keyHint,
		},
	})

	preferred := req.setPreferred
	if !preferred {
		snapshots := pool.Snapshot()
		preferred = len(snapshots) == 1
	}
	if preferred {
		s.mu.Lock()
		s.preferredProfiles[providerID] = profileID
		s.mu.Unlock()
		_ = pool.SetPreferred(profileID)
	}

	s.syncProfileState(providerID, profileID)
	log.Printf(
		"event=openai_profile_add provider=%s profile_id=%s type=%s key_hint=%s preferred=%t",
		providerID,
		profileID,
		accountType,
		keyHint,
		preferred,
	)

	return OpenAIAddCredentialResult{
		ProfileID:   profileID,
		Provider:    providerID,
		Type:        string(accountType),
		DisplayName: displayName,
		KeyHint:     keyHint,
		Preferred:   preferred,
		Available:   true,
	}, nil
}

func (s *Service) addAnthropicCredential(req commonAnthropicAddRequest) (AnthropicAddCredentialResult, error) {
	store := s.authStoreSafe()
	if store == nil {
		return AnthropicAddCredentialResult{}, fmt.Errorf("%w: auth store not configured", ErrProviderNotReady)
	}
	prov, pool, err := s.resolveProviderPool("anthropic")
	if err != nil {
		return AnthropicAddCredentialResult{}, fmt.Errorf("%w: %v", ErrProviderNotReady, err)
	}

	secret := strings.TrimSpace(req.secret)
	accountType := req.accountType
	displayName := strings.TrimSpace(req.displayName)
	profileID := strings.TrimSpace(req.profileID)

	if profileID == "" {
		profileID = deriveAnthropicProfileID(accountType, displayName, secret)
	}
	keyHint := maskAPIKeyForHint(secret)
	if displayName == "" {
		if accountType == core.AccountToken {
			displayName = "Anthropic setup-token " + keyHint
		} else {
			displayName = "Anthropic api-key " + keyHint
		}
	}

	if err := store.SaveCredential("anthropic", profileID, auth.Credential{
		AccessToken: secret,
	}); err != nil {
		return AnthropicAddCredentialResult{}, err
	}

	meta := auth.ProfileMetadata{
		ProfileID:   profileID,
		Provider:    "anthropic",
		Type:        string(accountType),
		DisplayName: displayName,
		KeyHint:     keyHint,
		Endpoint:    pEndpoint(prov),
	}
	if err := store.UpsertProfile(meta); err != nil {
		_ = store.DeleteCredential("anthropic", profileID)
		return AnthropicAddCredentialResult{}, err
	}

	pool.SetCredential(profileID, core.Account{
		ID:       profileID,
		Provider: "anthropic",
		Type:     accountType,
		Token:    secret,
		Metadata: core.Metadata{
			"display_name": displayName,
			"key_hint":     keyHint,
		},
	})

	preferred := req.setPreferred
	if !preferred {
		snapshots := pool.Snapshot()
		preferred = len(snapshots) == 1
	}
	if preferred {
		s.mu.Lock()
		s.preferredProfiles["anthropic"] = profileID
		s.mu.Unlock()
		_ = pool.SetPreferred(profileID)
	}

	s.syncProfileState("anthropic", profileID)
	log.Printf(
		"event=anthropic_profile_add provider=anthropic profile_id=%s type=%s key_hint=%s preferred=%t",
		profileID,
		accountType,
		keyHint,
		preferred,
	)

	return AnthropicAddCredentialResult{
		ProfileID:   profileID,
		Provider:    "anthropic",
		Type:        string(accountType),
		DisplayName: displayName,
		KeyHint:     keyHint,
		Preferred:   preferred,
		Available:   true,
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
	surface := req.Surface
	if surface == "" {
		surface = core.SurfaceTUI
	}
	runID := strings.TrimSpace(req.RunID)

	// Check session lifecycle before processing.
	if s.lifecycle != nil && s.lifecycle.ShouldReset(sessionID) {
		log.Printf("event=session_auto_reset session_id=%s", sessionID)
		_ = s.lifecycle.RotateSession(sessionID)
	}

	prompt := strings.TrimSpace(req.Message)
	if prompt == "" && runID == "" {
		return core.ChatResponse{}, fmt.Errorf("message is required")
	}

	// Build provider chain: primary first, then registered fallbacks.
	s.mu.RLock()
	chain := []string{providerID}
	if runID == "" {
		chain = append(chain, s.fallbacks[providerID]...)
	}
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
			images:         req.Images,
			surface:        surface,
			enableTools:    req.EnableTools,
			runID:          runID,
			toolApprovals:  req.ToolApprovals,
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
	images         []core.ImageData
	surface        core.Surface
	enableTools    bool
	runID          string
	toolApprovals  []core.ToolApprovalDecision
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

	hasUserMessage := strings.TrimSpace(params.prompt) != "" || len(params.images) > 0
	userMessage := core.Message{}
	if hasUserMessage {
		userMessage = core.Message{
			Role:      core.RoleUser,
			Content:   params.prompt,
			Images:    params.images,
			CreatedAt: time.Now(),
		}
	}

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
			if hasUserMessage {
				compressedMessages = append(compressedMessages, userMessage)
			}
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
		baseMessages := append([]core.Message(nil), history...)
		if hasUserMessage {
			baseMessages = append(baseMessages, userMessage)
		}
		policy := contextwindow.DefaultPolicy(contextWindow)
		policy = s.adjustCompressionPolicy(providerID, modelID, policy)
		compressedMessages, compressionMeta, compressed = contextwindow.Compress(baseMessages, policy)
	}

	// Build memory prompt string (used by both persona-based and plain injection).
	var memoryPrompt string
	if s.memoryDir != "" {
		memCtx, memErr := memory.LoadMemoryContext(s.memoryDir)
		if memErr != nil {
			log.Printf("event=memory_load_error error=%q", memErr)
		} else if !memCtx.IsEmpty() {
			memoryPrompt = memory.BuildSystemPrompt(memCtx)
		}
	}

	// Inject system prompt: persona template (with embedded memory) or plain memory.
	var generationParams *provider.GenerationParams
	if activePersona := s.activePersona(); activePersona != nil {
		rendered, renderErr := persona.RenderSystemPrompt(activePersona, memoryPrompt)
		if renderErr != nil {
			log.Printf("event=persona_render_error persona=%s error=%q", activePersona.DirName, renderErr)
		} else {
			systemMsg := core.Message{
				Role:    core.RoleSystem,
				Content: rendered,
			}
			compressedMessages = append([]core.Message{systemMsg}, compressedMessages...)
		}
		generationParams = s.personaGenerationParams(activePersona)
	} else if memoryPrompt != "" {
		systemMsg := core.Message{
			Role:    core.RoleSystem,
			Content: memoryPrompt,
		}
		compressedMessages = append([]core.Message{systemMsg}, compressedMessages...)
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
			if inferred := s.inferOpenAIMissingAPIKey(providerID, pool); inferred != nil {
				lastErr = inferred
				break
			}
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

		if params.enableTools {
			toolProv, ok := prov.(provider.ToolCallingProvider)
			if !ok || !toolProv.ToolCapabilities().SupportsTools {
				return core.ChatResponse{}, fmt.Errorf("%w: provider=%s", ErrToolsNotSupported, providerID)
			}
			runResult, runErr := s.toolRuntime.Run(ctx, tooling.RunRequest{
				SessionID:    sessionID,
				Surface:      params.surface,
				ProviderID:   providerID,
				ModelID:      attemptModelID,
				Account:      account,
				ToolProvider: toolProv,
				Messages:     compressedMessages,
				UserMessage:  userMessage,
				EnableTools:  true,
				RunID:        params.runID,
				Approvals:    params.toolApprovals,
				Compressed:   compressed,
				Compression:  compressionMeta,
				Generation:   generationParams,
			})
			if runErr == nil {
				if !runResult.Pending && len(runResult.SessionMessages) > 0 {
					s.sessions.AppendMessage(sessionID, runResult.SessionMessages...)
					// Async title generation on first exchange (tool path).
					if hasUserMessage {
						var firstAssistant string
						for _, msg := range runResult.SessionMessages {
							if msg.Role == core.RoleAssistant && strings.TrimSpace(msg.Content) != "" {
								firstAssistant = msg.Content
								break
							}
						}
						if firstAssistant != "" {
							s.generateSessionTitleAsync(providerID, attemptModelID, sessionID, account, userMessage.Content, firstAssistant)
						}
					}
					if s.searchIndex != nil {
						newEntries := make([]core.SessionEntry, 0, len(runResult.SessionMessages))
						for _, msg := range runResult.SessionMessages {
							newEntries = append(newEntries, core.MessageToEntry(msg))
						}
						go func(entries []core.SessionEntry) {
							if idxErr := s.searchIndex.Index(sessionID, entries); idxErr != nil {
								log.Printf("event=search_index_error session_id=%s error=%q", sessionID, idxErr)
							}
						}(newEntries)
					}
				}
				pool.MarkUsed(account.ID)
				s.syncProfileState(providerID, account.ID)
				resp := runResult.Response
				if strings.TrimSpace(resp.SessionID) == "" {
					resp.SessionID = sessionID
				}
				if strings.TrimSpace(resp.Provider) == "" {
					resp.Provider = providerID
				}
				if strings.TrimSpace(resp.Model) == "" {
					resp.Model = attemptModelID
				}
				if strings.TrimSpace(resp.AccountID) == "" {
					resp.AccountID = account.ID
				}
				if resp.Status == "" {
					resp.Status = core.ChatStatusCompleted
				}
				return resp, nil
			}
			reason := deriveFailureReason(runErr)
			pool.MarkFailure(account.ID, reason)
			logFailureEvent(providerID, account.ID, reason, pool)
			s.syncProfileState(providerID, account.ID)
			lastErr = runErr
			if !core.IsRetriable(reason) {
				break
			}
			if preferredProfile == account.ID {
				preferredProfile = ""
			}
			continue
		}

		resp, err := prov.Generate(ctx, provider.GenerateRequest{
			Model:      attemptModelID,
			Messages:   compressedMessages,
			Account:    account,
			Generation: generationParams,
		})
		if err == nil {
			assistant := core.Message{Role: core.RoleAssistant, Content: resp.Text, CreatedAt: time.Now()}
			if hasUserMessage {
				s.sessions.AppendMessage(sessionID, userMessage, assistant)
			} else {
				s.sessions.AppendMessage(sessionID, assistant)
			}
			// Async title generation on first exchange.
			if hasUserMessage {
				s.generateSessionTitleAsync(providerID, attemptModelID, sessionID, account, userMessage.Content, assistant.Content)
			}
			// Async index for memory search.
			if s.searchIndex != nil {
				newEntries := make([]core.SessionEntry, 0, 2)
				if hasUserMessage {
					newEntries = append(newEntries, core.MessageToEntry(userMessage))
				}
				newEntries = append(newEntries, core.MessageToEntry(assistant))
				go func(entries []core.SessionEntry) {
					if idxErr := s.searchIndex.Index(sessionID, entries); idxErr != nil {
						log.Printf("event=search_index_error session_id=%s error=%q", sessionID, idxErr)
					}
				}(newEntries)
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
				Usage:       resp.Usage,
				Status:      core.ChatStatusCompleted,
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

func (s *Service) anthropicLoginManagerSafe() *auth.AnthropicLoginManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.anthropicLoginMgr
}

func (s *Service) openAICodexLoginManagerSafe() *auth.OpenAICodexLoginManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.openAICodexLogin
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
	case "anthropic":
		return "claude-sonnet-4-5"
	case "openai":
		return "gpt-5.1-codex"
	case "openai-codex":
		return "gpt-5.3-codex"
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

func (s *Service) inferOpenAIMissingAPIKey(providerID string, pool *core.AccountPool) error {
	if providerID != "openai" {
		return nil
	}
	if pool == nil {
		return nil
	}
	primarySnapshots := pool.Snapshot()
	if len(primarySnapshots) > 0 {
		return nil
	}
	s.mu.RLock()
	codexPool := s.pools["openai-codex"]
	s.mu.RUnlock()
	if codexPool == nil {
		return nil
	}
	if len(codexPool.Snapshot()) == 0 {
		return nil
	}
	return fmt.Errorf(
		`No API key found for provider "openai". You are authenticated with OpenAI Codex OAuth. Use openai-codex/gpt-5.3-codex (OAuth) or set OPENAI_API_KEY to use openai/gpt-5.1-codex.`,
	)
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

func deriveAnthropicProfileID(accountType core.AccountType, displayName, secret string) string {
	base := strings.TrimSpace(strings.ToLower(displayName))
	if base == "" {
		if accountType == core.AccountToken {
			base = "setup_token"
		} else {
			base = "api_key"
		}
	}
	base = sanitizeProfileSlug(base)

	suffix := strings.TrimSpace(secret)
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}
	suffix = sanitizeProfileSlug(strings.ToLower(suffix))
	if suffix == "" {
		suffix = fmt.Sprintf("%d", time.Now().Unix())
	}
	return "anthropic:" + base + "_" + suffix
}

func deriveOpenAIProfileID(providerID string, accountType core.AccountType, displayName, secret string) string {
	base := strings.TrimSpace(strings.ToLower(displayName))
	if base == "" {
		switch providerID {
		case "openai-codex":
			base = "oauth"
		default:
			base = "api_key"
		}
		if accountType == core.AccountOAuth && providerID == "openai" {
			base = "oauth"
		}
	}
	base = sanitizeProfileSlug(base)

	suffix := strings.TrimSpace(secret)
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}
	suffix = sanitizeProfileSlug(strings.ToLower(suffix))
	if suffix == "" {
		suffix = fmt.Sprintf("%d", time.Now().Unix())
	}

	prefix := "openai"
	if strings.TrimSpace(providerID) == "openai-codex" {
		prefix = "openai-codex"
	}
	return prefix + ":" + base + "_" + suffix
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

type serviceToolBackend struct {
	svc *Service
}

func (b serviceToolBackend) ListSessions() []core.SessionMetadata {
	if b.svc == nil {
		return nil
	}
	return b.svc.ListSessions()
}

func (b serviceToolBackend) SearchMemory(query string, limit int) ([]tooling.MemoryResult, error) {
	if b.svc == nil {
		return nil, fmt.Errorf("service not available")
	}
	results, err := b.svc.SearchMemory(query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]tooling.MemoryResult, 0, len(results))
	for _, item := range results {
		out = append(out, tooling.MemoryResult{
			SessionID: item.SessionID,
			Snippet:   item.Content,
			Score:     item.Score,
			Role:      item.Role,
		})
	}
	return out, nil
}

func (b serviceToolBackend) Providers() []string {
	if b.svc == nil {
		return nil
	}
	return b.svc.Providers()
}

func (b serviceToolBackend) Accounts(providerID string) []core.AccountSnapshot {
	if b.svc == nil {
		return nil
	}
	return b.svc.Accounts(providerID)
}

// ---------------------------------------------------------------------------
// Session title generation
// ---------------------------------------------------------------------------

const titleGenSystemPrompt = `Generate a short title (under 25 characters) for this conversation based on the first exchange. Reply with ONLY the title text, no quotes, no explanation. Use the same language as the user's message.`

// generateSessionTitleAsync spawns a background goroutine that uses the LLM
// to produce a short session title from the first user+assistant exchange.
// Fire-and-forget: errors are logged but never surface to the user.
func (s *Service) generateSessionTitleAsync(
	providerID, modelID, sessionID string,
	account core.Account,
	userContent, assistantContent string,
) {
	meta, exists := s.sessions.GetMetadata(sessionID)
	if !exists || meta.Title != "" {
		return
	}

	// Prevent duplicate goroutines for the same session.
	if _, loaded := s.titleGenPending.LoadOrStore(sessionID, true); loaded {
		return
	}

	prov, _, err := s.resolveProviderPool(providerID)
	if err != nil {
		s.titleGenPending.Delete(sessionID)
		return
	}

	go func() {
		defer s.titleGenPending.Delete(sessionID)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		userSnippet := truncateRunes(userContent, 500)
		assistantSnippet := truncateRunes(assistantContent, 500)
		prompt := fmt.Sprintf("User: %s\nAssistant: %s", userSnippet, assistantSnippet)

		resp, err := prov.Generate(ctx, provider.GenerateRequest{
			Model: modelID,
			Messages: []core.Message{
				{Role: core.RoleSystem, Content: titleGenSystemPrompt},
				{Role: core.RoleUser, Content: prompt},
			},
			Account: account,
		})
		if err != nil {
			log.Printf("event=title_gen_error session_id=%s error=%q", sessionID, err)
			return
		}

		title := strings.TrimSpace(resp.Text)
		if title == "" {
			return
		}
		runes := []rune(title)
		if len(runes) > 30 {
			title = string(runes[:29]) + "…"
		}

		s.sessions.SetTitle(sessionID, title)
		log.Printf("event=title_generated session_id=%s title=%q", sessionID, title)
	}()
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes-1]) + "…"
}

// ---------------------------------------------------------------------------
// Persona helpers (used inside attemptSingleProvider)
// ---------------------------------------------------------------------------

// activePersona returns the currently active Persona, or nil.
func (s *Service) activePersona() *persona.Persona {
	if s.personaManager == nil {
		return nil
	}
	return s.personaManager.Active()
}

// personaGenerationParams converts persona generation params to provider params.
// Returns nil when no overrides are configured.
func (s *Service) personaGenerationParams(p *persona.Persona) *provider.GenerationParams {
	gen := p.Config.Generation
	if gen.IsZero() {
		return nil
	}
	return &provider.GenerationParams{
		Temperature:      gen.Temperature,
		TopP:             gen.TopP,
		FrequencyPenalty: gen.FrequencyPenalty,
		PresencePenalty:  gen.PresencePenalty,
	}
}
