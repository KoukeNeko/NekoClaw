package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/termclean"
)

type AnthropicLoginMode string

const (
	AnthropicLoginModeCLIBridge AnthropicLoginMode = "cli_bridge"
	AnthropicLoginModeManual    AnthropicLoginMode = "manual"
)

type AnthropicLoginStatus string

const (
	AnthropicLoginStatusRunning        AnthropicLoginStatus = "running"
	AnthropicLoginStatusManualRequired AnthropicLoginStatus = "manual_required"
	AnthropicLoginStatusCompleted      AnthropicLoginStatus = "completed"
	AnthropicLoginStatusFailed         AnthropicLoginStatus = "failed"
	AnthropicLoginStatusCancelled      AnthropicLoginStatus = "cancelled"
)

var ErrAnthropicLoginJobNotFound = errors.New("anthropic login job not found")
var ErrAnthropicLoginJobExpired = errors.New("anthropic login job expired")
var ErrAnthropicLoginJobCancelled = errors.New("anthropic login job cancelled")
var ErrAnthropicLoginJobCompleted = errors.New("anthropic login job completed")
var ErrAnthropicLoginManualRequired = errors.New("anthropic login manual completion required")
var ErrAnthropicCLINotFound = errors.New("anthropic login cli not found")
var ErrAnthropicPTYUnavailable = errors.New("anthropic login pty unavailable")
var ErrAnthropicTokenNotDetected = errors.New("anthropic login token not detected")
var ErrAnthropicInvalidSetupToken = errors.New("anthropic login invalid setup token")

type AnthropicLoginEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type AnthropicPersistResult struct {
	ProfileID   string
	DisplayName string
	KeyHint     string
	Preferred   bool
}

type AnthropicTokenPersistFn func(
	ctx context.Context,
	token string,
	displayName string,
	profileID string,
	setPreferred bool,
) (AnthropicPersistResult, error)

type AnthropicLoginStartRequest struct {
	DisplayName  string
	ProfileID    string
	SetPreferred bool
	Mode         string // auto|local|remote
	OnToken      AnthropicTokenPersistFn
}

type AnthropicLoginManualCompleteRequest struct {
	JobID        string
	SetupToken   string
	DisplayName  string
	ProfileID    string
	SetPreferred bool
	OnToken      AnthropicTokenPersistFn
}

type AnthropicLoginCancelResult struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type AnthropicLoginSnapshot struct {
	JobID        string                `json:"job_id"`
	Provider     string                `json:"provider"`
	Mode         string                `json:"mode"`
	Status       string                `json:"status"`
	Message      string                `json:"message,omitempty"`
	ManualHint   string                `json:"manual_hint,omitempty"`
	Events       []AnthropicLoginEvent `json:"events,omitempty"`
	ProfileID    string                `json:"profile_id,omitempty"`
	KeyHint      string                `json:"key_hint,omitempty"`
	ErrorCode    string                `json:"error_code,omitempty"`
	ErrorMessage string                `json:"error_message,omitempty"`
	ExpiresAt    time.Time             `json:"expires_at"`
}

type AnthropicCLIRunner interface {
	Available(ctx context.Context) error
	RunSetupToken(ctx context.Context, emit func(message string)) (string, error)
}

type AnthropicLoginManagerOptions struct {
	JobTTL             time.Duration
	Now                func() time.Time
	IsRemote           func() bool
	Runner             AnthropicCLIRunner
	MaxEvents          int
	MaxEventMessageLen int
}

type anthropicLoginJob struct {
	jobID        string
	provider     string
	mode         AnthropicLoginMode
	status       AnthropicLoginStatus
	message      string
	manualHint   string
	events       []AnthropicLoginEvent
	profileID    string
	keyHint      string
	errorCode    string
	errorMessage string
	expiresAt    time.Time
	displayName  string
	targetID     string
	setPreferred bool
	cancel       context.CancelFunc
}

type AnthropicLoginManager struct {
	mu                 sync.Mutex
	jobs               map[string]*anthropicLoginJob
	jobTTL             time.Duration
	now                func() time.Time
	isRemote           func() bool
	runner             AnthropicCLIRunner
	maxEvents          int
	maxEventMessageLen int
}

func NewAnthropicLoginManager(opts AnthropicLoginManagerOptions) *AnthropicLoginManager {
	jobTTL := opts.JobTTL
	if jobTTL <= 0 {
		jobTTL = 10 * time.Minute
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	isRemote := opts.IsRemote
	if isRemote == nil {
		isRemote = defaultRemoteDetector
	}
	runner := opts.Runner
	if runner == nil {
		runner = NewAnthropicCLIRunner()
	}
	maxEvents := opts.MaxEvents
	if maxEvents <= 0 {
		maxEvents = 200
	}
	maxEventMessageLen := opts.MaxEventMessageLen
	if maxEventMessageLen <= 0 {
		maxEventMessageLen = 512
	}
	return &AnthropicLoginManager{
		jobs:               map[string]*anthropicLoginJob{},
		jobTTL:             jobTTL,
		now:                now,
		isRemote:           isRemote,
		runner:             runner,
		maxEvents:          maxEvents,
		maxEventMessageLen: maxEventMessageLen,
	}
}

func (m *AnthropicLoginManager) Start(
	ctx context.Context,
	req AnthropicLoginStartRequest,
) (AnthropicLoginSnapshot, error) {
	mode, err := normalizeAnthropicLoginMode(req.Mode)
	if err != nil {
		return AnthropicLoginSnapshot{}, err
	}
	jobID, err := randomURLSafe(18)
	if err != nil {
		return AnthropicLoginSnapshot{}, err
	}
	now := m.now()
	job := &anthropicLoginJob{
		jobID:        jobID,
		provider:     "anthropic",
		mode:         AnthropicLoginModeManual,
		status:       AnthropicLoginStatusManualRequired,
		message:      "Manual setup-token required.",
		manualHint:   "Run `claude setup-token`, then submit via /v1/auth/anthropic/browser/manual/complete.",
		expiresAt:    now.Add(m.jobTTL),
		displayName:  strings.TrimSpace(req.DisplayName),
		targetID:     strings.TrimSpace(req.ProfileID),
		profileID:    strings.TrimSpace(req.ProfileID),
		setPreferred: req.SetPreferred,
	}

	manualReasonCode, manualReasonText, useBridge := m.resolveStartMode(ctx, mode)
	if useBridge {
		job.mode = AnthropicLoginModeCLIBridge
		job.status = AnthropicLoginStatusRunning
		job.message = "Running `claude setup-token`..."
		job.manualHint = ""
	}
	if !useBridge {
		job.mode = AnthropicLoginModeManual
		job.status = AnthropicLoginStatusManualRequired
		job.errorCode = manualReasonCode
		job.errorMessage = manualReasonText
		if manualReasonText != "" {
			job.message = manualReasonText
		}
	}

	m.mu.Lock()
	m.clearExpiredLocked(now)
	m.jobs[jobID] = job
	m.mu.Unlock()

	if useBridge {
		runCtx, cancel := context.WithCancel(context.Background())
		m.mu.Lock()
		if current, ok := m.jobs[jobID]; ok {
			current.cancel = cancel
		}
		m.mu.Unlock()
		go m.runBridge(runCtx, jobID, req.OnToken)
	}
	return m.snapshot(job), nil
}

func (m *AnthropicLoginManager) Get(jobID string) (AnthropicLoginSnapshot, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return AnthropicLoginSnapshot{}, fmt.Errorf("job_id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getLocked(jobID)
}

func (m *AnthropicLoginManager) CompleteManual(
	ctx context.Context,
	req AnthropicLoginManualCompleteRequest,
) (AnthropicLoginSnapshot, error) {
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		return AnthropicLoginSnapshot{}, fmt.Errorf("job_id is required")
	}
	token := strings.TrimSpace(req.SetupToken)
	if token == "" {
		return AnthropicLoginSnapshot{}, fmt.Errorf("setup_token is required")
	}

	m.mu.Lock()
	now := m.now()
	if m.expireJobIfNeededLocked(jobID, now) {
		m.clearExpiredLocked(now)
		m.mu.Unlock()
		return AnthropicLoginSnapshot{}, ErrAnthropicLoginJobExpired
	}
	m.clearExpiredLocked(now)
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return AnthropicLoginSnapshot{}, ErrAnthropicLoginJobNotFound
	}
	if now.After(job.expiresAt) {
		delete(m.jobs, jobID)
		m.mu.Unlock()
		return AnthropicLoginSnapshot{}, ErrAnthropicLoginJobExpired
	}
	switch job.status {
	case AnthropicLoginStatusCancelled:
		m.mu.Unlock()
		return AnthropicLoginSnapshot{}, ErrAnthropicLoginJobCancelled
	case AnthropicLoginStatusCompleted:
		m.mu.Unlock()
		return AnthropicLoginSnapshot{}, ErrAnthropicLoginJobCompleted
	}
	if job.cancel != nil {
		job.cancel()
		job.cancel = nil
	}
	job.mode = AnthropicLoginModeManual
	job.status = AnthropicLoginStatusManualRequired
	job.message = "Completing manual setup-token..."
	displayName := chooseNonEmpty(req.DisplayName, job.displayName)
	profileID := chooseNonEmpty(req.ProfileID, job.targetID)
	setPreferred := req.SetPreferred || job.setPreferred
	m.appendEventLocked(job, "Manual token submitted.")
	m.mu.Unlock()

	if req.OnToken == nil {
		return AnthropicLoginSnapshot{}, fmt.Errorf("manual completion callback is required")
	}
	if err := validateAnthropicSetupTokenRaw(token); err != nil {
		m.failJob(jobID, "invalid_setup_token", err.Error(), "Please paste a valid setup-token.")
		return AnthropicLoginSnapshot{}, fmt.Errorf("%w: %v", ErrAnthropicInvalidSetupToken, err)
	}
	result, err := req.OnToken(ctx, token, displayName, profileID, setPreferred)
	if err != nil {
		m.failJob(jobID, "manual_required", err.Error(), "Retry browser login or provide another setup-token.")
		return AnthropicLoginSnapshot{}, err
	}
	m.completeJob(jobID, result, "Manual setup-token completed.")
	return m.Get(jobID)
}

func (m *AnthropicLoginManager) Cancel(jobID string) (AnthropicLoginCancelResult, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return AnthropicLoginCancelResult{}, fmt.Errorf("job_id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if m.expireJobIfNeededLocked(jobID, now) {
		m.clearExpiredLocked(now)
		return AnthropicLoginCancelResult{}, ErrAnthropicLoginJobExpired
	}
	m.clearExpiredLocked(now)
	job, ok := m.jobs[jobID]
	if !ok {
		return AnthropicLoginCancelResult{}, ErrAnthropicLoginJobNotFound
	}
	if now.After(job.expiresAt) {
		delete(m.jobs, jobID)
		return AnthropicLoginCancelResult{}, ErrAnthropicLoginJobExpired
	}
	if job.status == AnthropicLoginStatusCancelled {
		return AnthropicLoginCancelResult{JobID: jobID, Status: string(job.status)}, nil
	}
	if job.status == AnthropicLoginStatusCompleted {
		return AnthropicLoginCancelResult{}, ErrAnthropicLoginJobCompleted
	}
	if job.cancel != nil {
		job.cancel()
		job.cancel = nil
	}
	job.status = AnthropicLoginStatusCancelled
	job.message = "Login job cancelled."
	job.errorCode = "job_cancelled"
	job.errorMessage = "Login job cancelled."
	m.appendEventLocked(job, "Job cancelled.")
	return AnthropicLoginCancelResult{JobID: jobID, Status: string(job.status)}, nil
}

func (m *AnthropicLoginManager) resolveStartMode(
	ctx context.Context,
	mode string,
) (errorCode string, message string, useBridge bool) {
	if mode == "remote" {
		return "manual_required", "Remote mode selected; switch to manual setup-token flow.", false
	}
	if mode == "auto" && m.isRemote() {
		return "manual_required", "Remote environment detected; switch to manual setup-token flow.", false
	}
	if m.runner == nil {
		return "manual_required", "CLI bridge unavailable; switch to manual setup-token flow.", false
	}
	if err := m.runner.Available(ctx); err != nil {
		code := anthropicLoginErrorCode(err)
		return code, err.Error(), false
	}
	return "", "", true
}

func (m *AnthropicLoginManager) runBridge(
	ctx context.Context,
	jobID string,
	onToken AnthropicTokenPersistFn,
) {
	emit := func(msg string) {
		m.appendEvent(jobID, msg)
	}
	emit("Executing `claude setup-token`.")
	token, err := m.runner.RunSetupToken(ctx, emit)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		code := anthropicLoginErrorCode(err)
		hint := "Run `claude setup-token` manually, then use manual complete."
		m.failJob(jobID, code, err.Error(), hint)
		return
	}
	if err := validateAnthropicSetupTokenRaw(token); err != nil {
		m.failJob(jobID, "invalid_setup_token", err.Error(), "Generated token is invalid. Please retry.")
		return
	}
	emit("Setup-token captured from Claude CLI output.")
	if onToken == nil {
		m.completeJob(jobID, AnthropicPersistResult{}, "Setup-token captured.")
		return
	}
	displayName, profileID, setPreferred, ok := m.persistInput(jobID)
	if !ok {
		return
	}
	result, err := onToken(ctx, token, displayName, profileID, setPreferred)
	if err != nil {
		m.failJob(jobID, "manual_required", err.Error(), "Retry browser login or complete manually with setup-token.")
		return
	}
	m.completeJob(jobID, result, "Browser login completed.")
}

func (m *AnthropicLoginManager) getLocked(jobID string) (AnthropicLoginSnapshot, error) {
	now := m.now()
	if m.expireJobIfNeededLocked(jobID, now) {
		m.clearExpiredLocked(now)
		return AnthropicLoginSnapshot{}, ErrAnthropicLoginJobExpired
	}
	m.clearExpiredLocked(now)
	job, ok := m.jobs[jobID]
	if !ok {
		return AnthropicLoginSnapshot{}, ErrAnthropicLoginJobNotFound
	}
	if now.After(job.expiresAt) {
		delete(m.jobs, jobID)
		return AnthropicLoginSnapshot{}, ErrAnthropicLoginJobExpired
	}
	return m.snapshot(job), nil
}

func (m *AnthropicLoginManager) completeJob(jobID string, result AnthropicPersistResult, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return
	}
	if job.status == AnthropicLoginStatusCancelled {
		return
	}
	job.status = AnthropicLoginStatusCompleted
	job.message = chooseNonEmpty(message, "Browser login completed.")
	job.errorCode = ""
	job.errorMessage = ""
	if strings.TrimSpace(result.ProfileID) != "" {
		job.profileID = strings.TrimSpace(result.ProfileID)
	}
	if job.profileID == "" && strings.TrimSpace(job.targetID) != "" {
		job.profileID = strings.TrimSpace(job.targetID)
	}
	if strings.TrimSpace(result.KeyHint) != "" {
		job.keyHint = strings.TrimSpace(result.KeyHint)
	}
	if strings.TrimSpace(result.DisplayName) != "" {
		job.displayName = strings.TrimSpace(result.DisplayName)
	}
	job.cancel = nil
	m.appendEventLocked(job, job.message)
}

func (m *AnthropicLoginManager) failJob(jobID, code, message, manualHint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return
	}
	if job.status == AnthropicLoginStatusCancelled {
		return
	}
	job.status = AnthropicLoginStatusFailed
	job.errorCode = strings.TrimSpace(code)
	job.errorMessage = strings.TrimSpace(message)
	job.message = chooseNonEmpty(job.errorMessage, "Browser login failed.")
	job.manualHint = strings.TrimSpace(manualHint)
	job.cancel = nil
	m.appendEventLocked(job, job.message)
}

func (m *AnthropicLoginManager) expireJobIfNeededLocked(jobID string, now time.Time) bool {
	job, ok := m.jobs[jobID]
	if !ok || job == nil {
		return false
	}
	if !now.After(job.expiresAt) {
		return false
	}
	if job.cancel != nil {
		job.cancel()
	}
	delete(m.jobs, jobID)
	return true
}

func (m *AnthropicLoginManager) persistInput(jobID string) (displayName, profileID string, setPreferred, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if m.expireJobIfNeededLocked(jobID, now) {
		m.clearExpiredLocked(now)
		return "", "", false, false
	}
	m.clearExpiredLocked(now)
	job, exists := m.jobs[jobID]
	if !exists || job == nil {
		return "", "", false, false
	}
	if job.status != AnthropicLoginStatusRunning && job.status != AnthropicLoginStatusManualRequired {
		return "", "", false, false
	}
	return strings.TrimSpace(job.displayName), strings.TrimSpace(job.targetID), job.setPreferred, true
}

func (m *AnthropicLoginManager) appendEvent(jobID string, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return
	}
	m.appendEventLocked(job, message)
}

func (m *AnthropicLoginManager) appendEventLocked(job *anthropicLoginJob, message string) {
	if job == nil {
		return
	}
	msg := sanitizeAnthropicEvent(message, m.maxEventMessageLen)
	if msg == "" {
		return
	}
	job.events = append(job.events, AnthropicLoginEvent{
		At:      m.now(),
		Message: msg,
	})
	if len(job.events) > m.maxEvents {
		job.events = job.events[len(job.events)-m.maxEvents:]
	}
}

func (m *AnthropicLoginManager) clearExpiredLocked(now time.Time) {
	for id, job := range m.jobs {
		if job == nil {
			delete(m.jobs, id)
			continue
		}
		if now.After(job.expiresAt) {
			if job.cancel != nil {
				job.cancel()
			}
			delete(m.jobs, id)
		}
	}
}

func (m *AnthropicLoginManager) snapshot(job *anthropicLoginJob) AnthropicLoginSnapshot {
	if job == nil {
		return AnthropicLoginSnapshot{}
	}
	events := make([]AnthropicLoginEvent, len(job.events))
	copy(events, job.events)
	return AnthropicLoginSnapshot{
		JobID:        job.jobID,
		Provider:     job.provider,
		Mode:         string(job.mode),
		Status:       string(job.status),
		Message:      job.message,
		ManualHint:   job.manualHint,
		Events:       events,
		ProfileID:    job.profileID,
		KeyHint:      job.keyHint,
		ErrorCode:    job.errorCode,
		ErrorMessage: job.errorMessage,
		ExpiresAt:    job.expiresAt,
	}
}

func normalizeAnthropicLoginMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return "auto", nil
	}
	switch mode {
	case "auto", "local", "remote":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid mode %q (expected auto|local|remote)", raw)
	}
}

func anthropicLoginErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrAnthropicCLINotFound):
		return "cli_not_found"
	case errors.Is(err, ErrAnthropicPTYUnavailable):
		return "pty_unavailable"
	case errors.Is(err, ErrAnthropicTokenNotDetected):
		return "token_not_detected"
	default:
		return "manual_required"
	}
}

func sanitizeAnthropicEvent(message string, maxLen int) string {
	msg := termclean.SanitizeDisplayText(message)
	if msg == "" {
		return ""
	}
	msg = redactAnthropicSetupToken(msg)
	if maxLen > 0 && len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	return strings.TrimSpace(msg)
}

func redactAnthropicSetupToken(input string) string {
	return replaceAnthropicTokenString(input, func(token string) string {
		return "****" + safeTokenSuffix(token)
	})
}

func validateAnthropicSetupTokenRaw(token string) error {
	trimmed := strings.TrimSpace(token)
	if !strings.HasPrefix(trimmed, "sk-ant-oat01-") {
		return fmt.Errorf("%w: expected sk-ant-oat01- prefix", ErrAnthropicTokenNotDetected)
	}
	if len(trimmed) < 80 {
		return fmt.Errorf("%w: token too short", ErrAnthropicTokenNotDetected)
	}
	return nil
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
