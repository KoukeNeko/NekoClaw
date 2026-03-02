package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type OAuthFlowMode string

const (
	OAuthFlowLoopback OAuthFlowMode = "loopback"
	OAuthFlowManual   OAuthFlowMode = "manual"
)

var ErrStateNotFound = errors.New("oauth state not found")
var ErrStateExpired = errors.New("oauth state expired")
var ErrStateConsumed = errors.New("oauth state already consumed")
var ErrStateMismatch = errors.New("oauth state mismatch")

type StartRequest struct {
	ProfileID   string
	Mode        string
	RedirectURI string
}

type StartResult struct {
	AuthURL     string        `json:"auth_url"`
	State       string        `json:"state"`
	RedirectURI string        `json:"redirect_uri"`
	ExpiresAt   time.Time     `json:"expires_at"`
	Mode        OAuthFlowMode `json:"mode"`
	OAuthMode   string        `json:"oauth_mode,omitempty"`
}

type PendingState struct {
	State       string
	Verifier    string
	RedirectURI string
	ProfileID   string
	ExpiresAt   time.Time
	Consumed    bool
	CreatedAt   time.Time
}

type GeminiOAuthManager struct {
	mu       sync.Mutex
	pending  map[string]*PendingState
	stateTTL time.Duration
	host     string
	port     int
	now      func() time.Time
	isRemote func() bool
	dialer   net.Dialer
}

type ManagerOptions struct {
	StateTTL time.Duration
	Host     string
	Port     int
	Now      func() time.Time
	IsRemote func() bool
}

func NewGeminiOAuthManager(opts ManagerOptions) *GeminiOAuthManager {
	stateTTL := opts.StateTTL
	if stateTTL <= 0 {
		stateTTL = 5 * time.Minute
	}
	host := strings.TrimSpace(opts.Host)
	if host == "" {
		host = "localhost"
	}
	port := opts.Port
	if port <= 0 {
		port = 8085
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	isRemote := opts.IsRemote
	if isRemote == nil {
		isRemote = defaultRemoteDetector
	}
	return &GeminiOAuthManager{
		pending:  map[string]*PendingState{},
		stateTTL: stateTTL,
		host:     host,
		port:     port,
		now:      now,
		isRemote: isRemote,
		dialer: net.Dialer{
			Timeout: 1200 * time.Millisecond,
		},
	}
}

func (m *GeminiOAuthManager) RedirectURI() string {
	return fmt.Sprintf("http://%s:%d/oauth2callback", m.host, m.port)
}

func (m *GeminiOAuthManager) Start(
	ctx context.Context,
	req StartRequest,
	buildAuthURL func(challenge, state, redirectURI string) (string, error),
) (StartResult, error) {
	if buildAuthURL == nil {
		return StartResult{}, fmt.Errorf("buildAuthURL is required")
	}

	state, err := randomURLSafe(32)
	if err != nil {
		return StartResult{}, err
	}
	verifier, err := randomURLSafe(48)
	if err != nil {
		return StartResult{}, err
	}
	challenge := pkceChallenge(verifier)
	redirectURI := strings.TrimSpace(req.RedirectURI)
	if redirectURI == "" {
		redirectURI = m.RedirectURI()
	}
	parsedRedirect, err := url.Parse(redirectURI)
	if err != nil {
		return StartResult{}, fmt.Errorf("invalid redirect_uri: %w", err)
	}
	if parsedRedirect.Scheme == "" || parsedRedirect.Host == "" {
		return StartResult{}, fmt.Errorf("invalid redirect_uri: must be absolute URL")
	}
	authURL, err := buildAuthURL(challenge, state, redirectURI)
	if err != nil {
		return StartResult{}, err
	}

	requestedMode, err := normalizeStartMode(req.Mode)
	if err != nil {
		return StartResult{}, err
	}

	mode := OAuthFlowManual
	isLocalRedirect, host, port := parseRedirectHostPort(redirectURI)
	switch requestedMode {
	case "local":
		if isLocalRedirect && m.loopbackAvailableAt(ctx, host, port) {
			mode = OAuthFlowLoopback
		}
	case "remote":
		mode = OAuthFlowManual
	default:
		if !m.isRemote() && isLocalRedirect && m.loopbackAvailableAt(ctx, host, port) {
			mode = OAuthFlowLoopback
		}
	}

	now := m.now()
	pending := &PendingState{
		State:       state,
		Verifier:    verifier,
		RedirectURI: redirectURI,
		ProfileID:   strings.TrimSpace(req.ProfileID),
		CreatedAt:   now,
		ExpiresAt:   now.Add(m.stateTTL),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearExpiredLocked(now)
	m.pending[state] = pending

	return StartResult{
		AuthURL:     authURL,
		State:       state,
		RedirectURI: redirectURI,
		ExpiresAt:   pending.ExpiresAt,
		Mode:        mode,
		OAuthMode:   requestedMode,
	}, nil
}

func (m *GeminiOAuthManager) ConsumeFromCallback(state, code string) (PendingState, error) {
	state = strings.TrimSpace(state)
	code = strings.TrimSpace(code)
	if state == "" || code == "" {
		return PendingState{}, fmt.Errorf("state and code are required")
	}
	return m.consumeState(state)
}

func (m *GeminiOAuthManager) ConsumeFromManual(state, callbackURLOrCode string) (PendingState, string, error) {
	state = strings.TrimSpace(state)
	input := strings.TrimSpace(callbackURLOrCode)
	if state == "" {
		return PendingState{}, "", fmt.Errorf("state is required")
	}
	if input == "" {
		return PendingState{}, "", fmt.Errorf("callback_url_or_code is required")
	}

	code := input
	if parsed, err := url.Parse(input); err == nil && parsed != nil && parsed.Scheme != "" {
		urlCode := strings.TrimSpace(parsed.Query().Get("code"))
		if urlCode == "" {
			return PendingState{}, "", fmt.Errorf("callback url missing code query")
		}
		urlState := strings.TrimSpace(parsed.Query().Get("state"))
		if urlState != "" && urlState != state {
			return PendingState{}, "", ErrStateMismatch
		}
		code = urlCode
	}
	pending, err := m.consumeState(state)
	if err != nil {
		return PendingState{}, "", err
	}
	return pending, code, nil
}

func (m *GeminiOAuthManager) Pending(state string) (PendingState, bool) {
	state = strings.TrimSpace(state)
	if state == "" {
		return PendingState{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearExpiredLocked(m.now())
	pending, ok := m.pending[state]
	if !ok {
		return PendingState{}, false
	}
	cp := *pending
	return cp, true
}

func (m *GeminiOAuthManager) consumeState(state string) (PendingState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	pending, ok := m.pending[state]
	if !ok {
		m.clearExpiredLocked(now)
		return PendingState{}, ErrStateNotFound
	}
	if pending.Consumed {
		return PendingState{}, ErrStateConsumed
	}
	if now.After(pending.ExpiresAt) {
		delete(m.pending, state)
		m.clearExpiredLocked(now)
		return PendingState{}, ErrStateExpired
	}
	pending.Consumed = true
	cp := *pending
	delete(m.pending, state)
	m.clearExpiredLocked(now)
	return cp, nil
}

func (m *GeminiOAuthManager) loopbackAvailableAt(ctx context.Context, host string, port int) bool {
	if strings.TrimSpace(host) == "" || port <= 0 {
		return false
	}
	conn, err := m.dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err == nil {
		_ = conn.Close()
		return true
	}
	listener, listenErr := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if listenErr != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func normalizeStartMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return "auto", nil
	}
	switch mode {
	case "auto", "local", "remote":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid oauth mode %q (expected auto|local|remote)", raw)
	}
}

func parseRedirectHostPort(redirectURI string) (bool, string, int) {
	parsed, err := url.Parse(strings.TrimSpace(redirectURI))
	if err != nil || parsed == nil {
		return false, "", 0
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return false, "", 0
	}
	if !isLocalHost(host) {
		return false, "", 0
	}
	portText := strings.TrimSpace(parsed.Port())
	if portText == "" {
		if strings.EqualFold(parsed.Scheme, "https") {
			return true, host, 443
		}
		return true, host, 80
	}
	port, convErr := strconv.Atoi(portText)
	if convErr != nil || port <= 0 {
		return false, "", 0
	}
	return true, host, port
}

func isLocalHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(strings.ToLower(host)), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (m *GeminiOAuthManager) clearExpiredLocked(now time.Time) {
	for state, pending := range m.pending {
		if pending == nil {
			delete(m.pending, state)
			continue
		}
		if now.After(pending.ExpiresAt) {
			delete(m.pending, state)
		}
	}
}

func defaultRemoteDetector() bool {
	if strings.TrimSpace(os.Getenv("NEKOCLAW_FORCE_MANUAL_OAUTH")) == "1" {
		return true
	}
	if strings.TrimSpace(os.Getenv("SSH_CONNECTION")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("SSH_TTY")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		return true
	}
	return false
}

func randomURLSafe(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
