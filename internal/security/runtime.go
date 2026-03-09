package security

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultBaseDirName = ".nekoclaw/auth"
	stateFileName      = "security-state.json"
	CookieName         = "nekoclaw_session"
	minPasswordLength  = 12
	maxPasswordLength  = 128
)

var (
	ErrAlreadyInitialized = errors.New("admin password already configured")
	ErrAuthDisabled       = errors.New("browser auth disabled")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrLoginRateLimited   = errors.New("login rate limited")
	ErrSessionExpired     = errors.New("session expired")
	ErrSessionNotFound    = errors.New("session not found")
	ErrSetupRequired      = errors.New("admin setup required")
	ErrSetupTokenInvalid  = errors.New("invalid setup token")
)

type RuntimeOptions struct {
	BaseDir string
	Config  core.SecurityConfig
	Now     func() time.Time
}

type Runtime struct {
	mu                sync.Mutex
	baseDir           string
	statePath         string
	now               func() time.Time
	config            core.SecurityConfig
	passwordHash      string
	passwordUpdatedAt time.Time
	setupToken        string
	setupIssuedAt     time.Time
	sessions          map[string]sessionState
	loginFailures     map[string]loginFailureState
}

type persistedState struct {
	PasswordHash      string    `json:"password_hash"`
	PasswordUpdatedAt time.Time `json:"password_updated_at,omitempty"`
}

type sessionState struct {
	ID                string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	AbsoluteExpiresAt time.Time
}

type loginFailureState struct {
	Attempts    int
	BlockedUntil time.Time
	LastFailedAt time.Time
}

type SessionSnapshot struct {
	ID                  string    `json:"id"`
	CreatedAt           time.Time `json:"created_at"`
	LastSeenAt          time.Time `json:"last_seen_at"`
	IdleExpiresAt       time.Time `json:"idle_expires_at"`
	AbsoluteExpiresAt   time.Time `json:"absolute_expires_at"`
}

type LoginStatusSnapshot struct {
	FailedAttempts    int        `json:"failed_attempts"`
	RemainingAttempts int        `json:"remaining_attempts"`
	BlockedUntil      *time.Time `json:"blocked_until,omitempty"`
}

type StatusSnapshot struct {
	AuthEnabled       bool                 `json:"auth_enabled"`
	Initialized       bool                 `json:"initialized"`
	SetupRequired     bool                 `json:"setup_required"`
	Authenticated     bool                 `json:"authenticated"`
	PasswordUpdatedAt time.Time            `json:"password_updated_at,omitempty"`
	SessionCount      int                  `json:"session_count"`
	CurrentSession    *SessionSnapshot     `json:"current_session,omitempty"`
	LoginStatus       LoginStatusSnapshot  `json:"login_status"`
}

func NewRuntime(opts RuntimeOptions) (*Runtime, error) {
	baseDir, err := resolveBaseDir(opts.BaseDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, err
	}

	rt := &Runtime{
		baseDir:       baseDir,
		statePath:     filepath.Join(baseDir, stateFileName),
		now:           opts.Now,
		config:        sanitizeConfig(opts.Config),
		sessions:      map[string]sessionState{},
		loginFailures: map[string]loginFailureState{},
	}
	if rt.now == nil {
		rt.now = time.Now
	}
	if err := rt.loadState(); err != nil {
		return nil, err
	}
	return rt, nil
}

func (r *Runtime) SetConfig(cfg core.SecurityConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = sanitizeConfig(cfg)
}

func (r *Runtime) Config() core.SecurityConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.config
}

func (r *Runtime) EnsureSetupToken() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.passwordHash != "" {
		return "", ErrAlreadyInitialized
	}
	if r.setupToken != "" {
		return r.setupToken, nil
	}
	token, err := randomToken(24)
	if err != nil {
		return "", err
	}
	r.setupToken = token
	r.setupIssuedAt = r.now().UTC()
	return token, nil
}

func (r *Runtime) Setup(token, password string) (SessionSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.passwordHash != "" {
		return SessionSnapshot{}, ErrAlreadyInitialized
	}
	if subtleTrim(token) == "" || subtleTrim(token) != r.setupToken {
		return SessionSnapshot{}, ErrSetupTokenInvalid
	}
	hash, err := hashPassword(password)
	if err != nil {
		return SessionSnapshot{}, err
	}
	r.passwordHash = hash
	r.passwordUpdatedAt = r.now().UTC()
	r.setupToken = ""
	r.setupIssuedAt = time.Time{}
	if err := r.saveStateLocked(); err != nil {
		return SessionSnapshot{}, err
	}
	session, err := r.newSessionLocked()
	if err != nil {
		return SessionSnapshot{}, err
	}
	return session, nil
}

func (r *Runtime) Login(clientKey, password string) (SessionSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.passwordHash == "" {
		return SessionSnapshot{}, ErrSetupRequired
	}
	if !r.config.AuthEnabled {
		return SessionSnapshot{}, ErrAuthDisabled
	}

	now := r.now().UTC()
	failure := r.loginFailures[clientKey]
	if !failure.BlockedUntil.IsZero() && now.Before(failure.BlockedUntil) {
		return SessionSnapshot{}, ErrLoginRateLimited
	}

	if bcrypt.CompareHashAndPassword([]byte(r.passwordHash), []byte(password)) != nil {
		failure.Attempts++
		failure.LastFailedAt = now
		if failure.Attempts >= r.config.LoginMaxAttempts {
			failure.BlockedUntil = now.Add(time.Duration(r.config.LoginBlockMinutes) * time.Minute)
		}
		r.loginFailures[clientKey] = failure
		if !failure.BlockedUntil.IsZero() && now.Before(failure.BlockedUntil) {
			return SessionSnapshot{}, ErrLoginRateLimited
		}
		return SessionSnapshot{}, ErrInvalidCredentials
	}

	delete(r.loginFailures, clientKey)
	return r.newSessionLocked()
}

func (r *Runtime) Authenticate(sessionID string) (SessionSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sessionID = subtleTrim(sessionID)
	if sessionID == "" {
		return SessionSnapshot{}, ErrSessionNotFound
	}
	session, ok := r.sessions[sessionID]
	if !ok {
		return SessionSnapshot{}, ErrSessionNotFound
	}
	now := r.now().UTC()
	if r.sessionExpiredLocked(session, now) {
		delete(r.sessions, sessionID)
		return SessionSnapshot{}, ErrSessionExpired
	}
	session.LastSeenAt = now
	r.sessions[sessionID] = session
	return snapshotSession(session, r.config), nil
}

func (r *Runtime) Status(sessionID, clientKey string) StatusSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now().UTC()
	status := StatusSnapshot{
		AuthEnabled:       r.config.AuthEnabled,
		Initialized:       r.passwordHash != "",
		SetupRequired:     r.config.AuthEnabled && r.passwordHash == "",
		PasswordUpdatedAt: r.passwordUpdatedAt,
	}

	for id, session := range r.sessions {
		if r.sessionExpiredLocked(session, now) {
			delete(r.sessions, id)
			continue
		}
		status.SessionCount++
	}

	sessionID = subtleTrim(sessionID)
	if sessionID != "" {
		if session, ok := r.sessions[sessionID]; ok && !r.sessionExpiredLocked(session, now) {
			session.LastSeenAt = now
			r.sessions[sessionID] = session
			snapshot := snapshotSession(session, r.config)
			status.Authenticated = true
			status.CurrentSession = &snapshot
		}
	}

	failure := r.loginFailures[clientKey]
	if !failure.BlockedUntil.IsZero() && !now.Before(failure.BlockedUntil) {
		delete(r.loginFailures, clientKey)
		failure = loginFailureState{}
	}
	remaining := r.config.LoginMaxAttempts - failure.Attempts
	if remaining < 0 {
		remaining = 0
	}
	status.LoginStatus = LoginStatusSnapshot{
		FailedAttempts:    failure.Attempts,
		RemainingAttempts: remaining,
	}
	if !failure.BlockedUntil.IsZero() {
		blockedUntil := failure.BlockedUntil
		status.LoginStatus.BlockedUntil = &blockedUntil
	}
	return status
}

func (r *Runtime) ChangePassword(currentPassword, newPassword, currentSessionID string) (SessionSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.passwordHash == "" {
		return SessionSnapshot{}, ErrSetupRequired
	}
	if bcrypt.CompareHashAndPassword([]byte(r.passwordHash), []byte(currentPassword)) != nil {
		return SessionSnapshot{}, ErrInvalidCredentials
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return SessionSnapshot{}, err
	}
	r.passwordHash = hash
	r.passwordUpdatedAt = r.now().UTC()
	r.sessions = map[string]sessionState{}
	if err := r.saveStateLocked(); err != nil {
		return SessionSnapshot{}, err
	}
	if subtleTrim(currentSessionID) == "" {
		return SessionSnapshot{}, nil
	}
	return r.newSessionLocked()
}

func (r *Runtime) Logout(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, subtleTrim(sessionID))
}

func (r *Runtime) LogoutAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = map[string]sessionState{}
}

func (r *Runtime) loadState() error {
	data, err := os.ReadFile(r.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	r.passwordHash = strings.TrimSpace(state.PasswordHash)
	r.passwordUpdatedAt = state.PasswordUpdatedAt.UTC()
	return nil
}

func (r *Runtime) saveStateLocked() error {
	state := persistedState{
		PasswordHash:      r.passwordHash,
		PasswordUpdatedAt: r.passwordUpdatedAt,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.statePath, data, 0o600)
}

func (r *Runtime) newSessionLocked() (SessionSnapshot, error) {
	id, err := randomToken(32)
	if err != nil {
		return SessionSnapshot{}, err
	}
	now := r.now().UTC()
	session := sessionState{
		ID:                id,
		CreatedAt:         now,
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(time.Duration(r.config.SessionAbsoluteHours) * time.Hour),
	}
	r.sessions[id] = session
	return snapshotSession(session, r.config), nil
}

func (r *Runtime) sessionExpiredLocked(session sessionState, now time.Time) bool {
	if !session.AbsoluteExpiresAt.IsZero() && !now.Before(session.AbsoluteExpiresAt) {
		return true
	}
	idleExpiry := session.LastSeenAt.Add(time.Duration(r.config.SessionIdleMinutes) * time.Minute)
	return !now.Before(idleExpiry)
}

func snapshotSession(session sessionState, cfg core.SecurityConfig) SessionSnapshot {
	idleExpiry := session.LastSeenAt.Add(time.Duration(cfg.SessionIdleMinutes) * time.Minute)
	return SessionSnapshot{
		ID:                session.ID,
		CreatedAt:         session.CreatedAt,
		LastSeenAt:        session.LastSeenAt,
		IdleExpiresAt:     idleExpiry,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt,
	}
}

func hashPassword(password string) (string, error) {
	password = subtleTrim(password)
	if len(password) < minPasswordLength {
		return "", fmt.Errorf("%w: password must be at least %d characters", ErrInvalidPassword, minPasswordLength)
	}
	if len(password) > maxPasswordLength {
		return "", fmt.Errorf("%w: password must be at most %d characters", ErrInvalidPassword, maxPasswordLength)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func sanitizeConfig(cfg core.SecurityConfig) core.SecurityConfig {
	defaults := core.DefaultSecurityConfig()
	if cfg == (core.SecurityConfig{}) {
		return defaults
	}
	if cfg.SessionIdleMinutes <= 0 {
		cfg.SessionIdleMinutes = defaults.SessionIdleMinutes
	}
	if cfg.SessionAbsoluteHours <= 0 {
		cfg.SessionAbsoluteHours = defaults.SessionAbsoluteHours
	}
	if cfg.LoginMaxAttempts <= 0 {
		cfg.LoginMaxAttempts = defaults.LoginMaxAttempts
	}
	if cfg.LoginBlockMinutes <= 0 {
		cfg.LoginBlockMinutes = defaults.LoginBlockMinutes
	}
	return cfg
}

func resolveBaseDir(baseDir string) (string, error) {
	baseDir = subtleTrim(baseDir)
	if baseDir != "" {
		return baseDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultBaseDirName), nil
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func subtleTrim(value string) string {
	return strings.TrimSpace(value)
}
