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

type OpenAICodexLoginMode string

const (
	OpenAICodexLoginModeCLIBridge OpenAICodexLoginMode = "cli_bridge"
	OpenAICodexLoginModeManual    OpenAICodexLoginMode = "manual"
)

type OpenAICodexLoginStatus string

const (
	OpenAICodexLoginStatusRunning        OpenAICodexLoginStatus = "running"
	OpenAICodexLoginStatusManualRequired OpenAICodexLoginStatus = "manual_required"
	OpenAICodexLoginStatusCompleted      OpenAICodexLoginStatus = "completed"
	OpenAICodexLoginStatusFailed         OpenAICodexLoginStatus = "failed"
	OpenAICodexLoginStatusCancelled      OpenAICodexLoginStatus = "cancelled"
)

var ErrOpenAICodexLoginJobNotFound = errors.New("openai codex login job not found")
var ErrOpenAICodexLoginJobExpired = errors.New("openai codex login job expired")
var ErrOpenAICodexLoginJobCancelled = errors.New("openai codex login job cancelled")
var ErrOpenAICodexLoginJobCompleted = errors.New("openai codex login job completed")
var ErrOpenAICodexLoginManualRequired = errors.New("openai codex login manual completion required")
var ErrOpenAICodexCLINotFound = errors.New("openai codex login cli not found")
var ErrOpenAICodexPTYUnavailable = errors.New("openai codex login pty unavailable")
var ErrOpenAICodexTokenNotDetected = errors.New("openai codex login oauth token not detected")
var ErrOpenAICodexInvalidToken = errors.New("openai codex login invalid oauth token")

type OpenAICodexLoginEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type OpenAICodexPersistResult struct {
	ProfileID   string
	DisplayName string
	KeyHint     string
	Preferred   bool
}

type OpenAICodexTokenPersistFn func(
	ctx context.Context,
	token string,
	displayName string,
	profileID string,
	setPreferred bool,
) (OpenAICodexPersistResult, error)

type OpenAICodexLoginStartRequest struct {
	DisplayName  string
	ProfileID    string
	SetPreferred bool
	Mode         string // auto|local|remote
	OnToken      OpenAICodexTokenPersistFn
}

type OpenAICodexLoginManualCompleteRequest struct {
	JobID        string
	Token        string
	DisplayName  string
	ProfileID    string
	SetPreferred bool
	OnToken      OpenAICodexTokenPersistFn
}

type OpenAICodexLoginCancelResult struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type OpenAICodexLoginSnapshot struct {
	JobID        string                  `json:"job_id"`
	Provider     string                  `json:"provider"`
	Mode         string                  `json:"mode"`
	Status       string                  `json:"status"`
	Message      string                  `json:"message,omitempty"`
	ManualHint   string                  `json:"manual_hint,omitempty"`
	Events       []OpenAICodexLoginEvent `json:"events,omitempty"`
	ProfileID    string                  `json:"profile_id,omitempty"`
	KeyHint      string                  `json:"key_hint,omitempty"`
	ErrorCode    string                  `json:"error_code,omitempty"`
	ErrorMessage string                  `json:"error_message,omitempty"`
	ExpiresAt    time.Time               `json:"expires_at"`
}

type OpenAICodexCLIRunner interface {
	Available(ctx context.Context) error
	RunLogin(ctx context.Context, emit func(message string)) (string, error)
}

type OpenAICodexLoginManagerOptions struct {
	JobTTL             time.Duration
	Now                func() time.Time
	IsRemote           func() bool
	Runner             OpenAICodexCLIRunner
	MaxEvents          int
	MaxEventMessageLen int
}

type openAICodexLoginJob struct {
	jobID        string
	provider     string
	mode         OpenAICodexLoginMode
	status       OpenAICodexLoginStatus
	message      string
	manualHint   string
	events       []OpenAICodexLoginEvent
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

type OpenAICodexLoginManager struct {
	mu                 sync.Mutex
	jobs               map[string]*openAICodexLoginJob
	jobTTL             time.Duration
	now                func() time.Time
	isRemote           func() bool
	runner             OpenAICodexCLIRunner
	maxEvents          int
	maxEventMessageLen int
}

func NewOpenAICodexLoginManager(opts OpenAICodexLoginManagerOptions) *OpenAICodexLoginManager {
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
		runner = NewOpenAICodexCLIRunner()
	}
	maxEvents := opts.MaxEvents
	if maxEvents <= 0 {
		maxEvents = 200
	}
	maxEventMessageLen := opts.MaxEventMessageLen
	if maxEventMessageLen <= 0 {
		maxEventMessageLen = 512
	}
	return &OpenAICodexLoginManager{
		jobs:               map[string]*openAICodexLoginJob{},
		jobTTL:             jobTTL,
		now:                now,
		isRemote:           isRemote,
		runner:             runner,
		maxEvents:          maxEvents,
		maxEventMessageLen: maxEventMessageLen,
	}
}

func (m *OpenAICodexLoginManager) Start(
	ctx context.Context,
	req OpenAICodexLoginStartRequest,
) (OpenAICodexLoginSnapshot, error) {
	mode, err := normalizeOpenAICodexLoginMode(req.Mode)
	if err != nil {
		return OpenAICodexLoginSnapshot{}, err
	}
	jobID, err := randomURLSafe(18)
	if err != nil {
		return OpenAICodexLoginSnapshot{}, err
	}
	now := m.now()
	job := &openAICodexLoginJob{
		jobID:        jobID,
		provider:     "openai-codex",
		mode:         OpenAICodexLoginModeManual,
		status:       OpenAICodexLoginStatusManualRequired,
		message:      "Manual OpenAI Codex token required.",
		manualHint:   "Run `codex login --device-auth`, then submit via /v1/auth/openai-codex/browser/manual/complete.",
		expiresAt:    now.Add(m.jobTTL),
		displayName:  strings.TrimSpace(req.DisplayName),
		targetID:     strings.TrimSpace(req.ProfileID),
		profileID:    strings.TrimSpace(req.ProfileID),
		setPreferred: req.SetPreferred,
	}

	manualReasonCode, manualReasonText, useBridge := m.resolveStartMode(ctx, mode)
	if useBridge {
		job.mode = OpenAICodexLoginModeCLIBridge
		job.status = OpenAICodexLoginStatusRunning
		job.message = "Running `codex login --device-auth`..."
		job.manualHint = ""
	}
	if !useBridge {
		job.mode = OpenAICodexLoginModeManual
		job.status = OpenAICodexLoginStatusManualRequired
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

func (m *OpenAICodexLoginManager) Get(jobID string) (OpenAICodexLoginSnapshot, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return OpenAICodexLoginSnapshot{}, fmt.Errorf("job_id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getLocked(jobID)
}

func (m *OpenAICodexLoginManager) CompleteManual(
	ctx context.Context,
	req OpenAICodexLoginManualCompleteRequest,
) (OpenAICodexLoginSnapshot, error) {
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		return OpenAICodexLoginSnapshot{}, fmt.Errorf("job_id is required")
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return OpenAICodexLoginSnapshot{}, fmt.Errorf("token is required")
	}

	m.mu.Lock()
	now := m.now()
	if m.expireJobIfNeededLocked(jobID, now) {
		m.clearExpiredLocked(now)
		m.mu.Unlock()
		return OpenAICodexLoginSnapshot{}, ErrOpenAICodexLoginJobExpired
	}
	m.clearExpiredLocked(now)
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return OpenAICodexLoginSnapshot{}, ErrOpenAICodexLoginJobNotFound
	}
	if now.After(job.expiresAt) {
		delete(m.jobs, jobID)
		m.mu.Unlock()
		return OpenAICodexLoginSnapshot{}, ErrOpenAICodexLoginJobExpired
	}
	switch job.status {
	case OpenAICodexLoginStatusCancelled:
		m.mu.Unlock()
		return OpenAICodexLoginSnapshot{}, ErrOpenAICodexLoginJobCancelled
	case OpenAICodexLoginStatusCompleted:
		m.mu.Unlock()
		return OpenAICodexLoginSnapshot{}, ErrOpenAICodexLoginJobCompleted
	}
	if job.cancel != nil {
		job.cancel()
		job.cancel = nil
	}
	job.mode = OpenAICodexLoginModeManual
	job.status = OpenAICodexLoginStatusManualRequired
	job.message = "Completing manual OpenAI Codex token..."
	displayName := chooseNonEmpty(req.DisplayName, job.displayName)
	profileID := chooseNonEmpty(req.ProfileID, job.targetID)
	setPreferred := req.SetPreferred || job.setPreferred
	m.appendEventLocked(job, "Manual token submitted.")
	m.mu.Unlock()

	if req.OnToken == nil {
		return OpenAICodexLoginSnapshot{}, fmt.Errorf("manual completion callback is required")
	}
	if err := validateOpenAICodexTokenRaw(token); err != nil {
		m.failJob(jobID, "invalid_oauth_token", err.Error(), "Please paste a valid OpenAI Codex OAuth token.")
		return OpenAICodexLoginSnapshot{}, fmt.Errorf("%w: %v", ErrOpenAICodexInvalidToken, err)
	}
	result, err := req.OnToken(ctx, token, displayName, profileID, setPreferred)
	if err != nil {
		m.failJob(jobID, "manual_required", err.Error(), "Retry browser login or provide another OAuth token.")
		return OpenAICodexLoginSnapshot{}, err
	}
	m.completeJob(jobID, result, "Manual OAuth token completed.")
	return m.Get(jobID)
}

func (m *OpenAICodexLoginManager) Cancel(jobID string) (OpenAICodexLoginCancelResult, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return OpenAICodexLoginCancelResult{}, fmt.Errorf("job_id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if m.expireJobIfNeededLocked(jobID, now) {
		m.clearExpiredLocked(now)
		return OpenAICodexLoginCancelResult{}, ErrOpenAICodexLoginJobExpired
	}
	m.clearExpiredLocked(now)
	job, ok := m.jobs[jobID]
	if !ok {
		return OpenAICodexLoginCancelResult{}, ErrOpenAICodexLoginJobNotFound
	}
	if now.After(job.expiresAt) {
		delete(m.jobs, jobID)
		return OpenAICodexLoginCancelResult{}, ErrOpenAICodexLoginJobExpired
	}
	if job.status == OpenAICodexLoginStatusCancelled {
		return OpenAICodexLoginCancelResult{JobID: jobID, Status: string(job.status)}, nil
	}
	if job.status == OpenAICodexLoginStatusCompleted {
		return OpenAICodexLoginCancelResult{}, ErrOpenAICodexLoginJobCompleted
	}
	if job.cancel != nil {
		job.cancel()
		job.cancel = nil
	}
	job.status = OpenAICodexLoginStatusCancelled
	job.message = "Login job cancelled."
	job.errorCode = "job_cancelled"
	job.errorMessage = "Login job cancelled."
	m.appendEventLocked(job, "Job cancelled.")
	return OpenAICodexLoginCancelResult{JobID: jobID, Status: string(job.status)}, nil
}

func (m *OpenAICodexLoginManager) resolveStartMode(
	ctx context.Context,
	mode string,
) (errorCode string, message string, useBridge bool) {
	if mode == "remote" {
		return "manual_required", "Remote mode selected; switch to manual OAuth token flow.", false
	}
	if mode == "auto" && m.isRemote() {
		return "manual_required", "Remote environment detected; switch to manual OAuth token flow.", false
	}
	if m.runner == nil {
		return "manual_required", "CLI bridge unavailable; switch to manual OAuth token flow.", false
	}
	if err := m.runner.Available(ctx); err != nil {
		code := openAICodexLoginErrorCode(err)
		return code, err.Error(), false
	}
	return "", "", true
}

func (m *OpenAICodexLoginManager) runBridge(
	ctx context.Context,
	jobID string,
	onToken OpenAICodexTokenPersistFn,
) {
	emit := func(msg string) {
		m.appendEvent(jobID, msg)
	}
	emit("Executing `codex login --device-auth`.")
	token, err := m.runner.RunLogin(ctx, emit)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		code := openAICodexLoginErrorCode(err)
		hint := "Run `codex login --device-auth` manually, then use manual complete."
		m.failJob(jobID, code, err.Error(), hint)
		return
	}
	if err := validateOpenAICodexTokenRaw(token); err != nil {
		m.failJob(jobID, "invalid_oauth_token", err.Error(), "Generated token is invalid. Please retry.")
		return
	}
	emit("OAuth token captured from Codex CLI output.")
	if onToken == nil {
		m.completeJob(jobID, OpenAICodexPersistResult{}, "OAuth token captured.")
		return
	}
	displayName, profileID, setPreferred, ok := m.persistInput(jobID)
	if !ok {
		return
	}
	result, err := onToken(ctx, token, displayName, profileID, setPreferred)
	if err != nil {
		m.failJob(jobID, "manual_required", err.Error(), "Retry browser login or complete manually with OAuth token.")
		return
	}
	m.completeJob(jobID, result, "Browser login completed.")
}

func (m *OpenAICodexLoginManager) getLocked(jobID string) (OpenAICodexLoginSnapshot, error) {
	now := m.now()
	if m.expireJobIfNeededLocked(jobID, now) {
		m.clearExpiredLocked(now)
		return OpenAICodexLoginSnapshot{}, ErrOpenAICodexLoginJobExpired
	}
	m.clearExpiredLocked(now)
	job, ok := m.jobs[jobID]
	if !ok {
		return OpenAICodexLoginSnapshot{}, ErrOpenAICodexLoginJobNotFound
	}
	if now.After(job.expiresAt) {
		delete(m.jobs, jobID)
		return OpenAICodexLoginSnapshot{}, ErrOpenAICodexLoginJobExpired
	}
	return m.snapshot(job), nil
}

func (m *OpenAICodexLoginManager) completeJob(jobID string, result OpenAICodexPersistResult, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return
	}
	if job.status == OpenAICodexLoginStatusCancelled {
		return
	}
	job.status = OpenAICodexLoginStatusCompleted
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

func (m *OpenAICodexLoginManager) failJob(jobID, code, message, manualHint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return
	}
	if job.status == OpenAICodexLoginStatusCancelled {
		return
	}
	job.status = OpenAICodexLoginStatusFailed
	job.errorCode = strings.TrimSpace(code)
	job.errorMessage = strings.TrimSpace(message)
	job.message = chooseNonEmpty(job.errorMessage, "Browser login failed.")
	job.manualHint = strings.TrimSpace(manualHint)
	job.cancel = nil
	m.appendEventLocked(job, job.message)
}

func (m *OpenAICodexLoginManager) expireJobIfNeededLocked(jobID string, now time.Time) bool {
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

func (m *OpenAICodexLoginManager) persistInput(jobID string) (displayName, profileID string, setPreferred, ok bool) {
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
	if job.status != OpenAICodexLoginStatusRunning && job.status != OpenAICodexLoginStatusManualRequired {
		return "", "", false, false
	}
	return strings.TrimSpace(job.displayName), strings.TrimSpace(job.targetID), job.setPreferred, true
}

func (m *OpenAICodexLoginManager) appendEvent(jobID string, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return
	}
	m.appendEventLocked(job, message)
}

func (m *OpenAICodexLoginManager) appendEventLocked(job *openAICodexLoginJob, message string) {
	if job == nil {
		return
	}
	msg := sanitizeOpenAICodexEvent(message, m.maxEventMessageLen)
	if msg == "" {
		return
	}
	job.events = append(job.events, OpenAICodexLoginEvent{
		At:      m.now(),
		Message: msg,
	})
	if len(job.events) > m.maxEvents {
		job.events = job.events[len(job.events)-m.maxEvents:]
	}
}

func (m *OpenAICodexLoginManager) clearExpiredLocked(now time.Time) {
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

func (m *OpenAICodexLoginManager) snapshot(job *openAICodexLoginJob) OpenAICodexLoginSnapshot {
	if job == nil {
		return OpenAICodexLoginSnapshot{}
	}
	events := make([]OpenAICodexLoginEvent, len(job.events))
	copy(events, job.events)
	return OpenAICodexLoginSnapshot{
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

func normalizeOpenAICodexLoginMode(raw string) (string, error) {
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

func openAICodexLoginErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrOpenAICodexCLINotFound):
		return "cli_not_found"
	case errors.Is(err, ErrOpenAICodexPTYUnavailable):
		return "pty_unavailable"
	case errors.Is(err, ErrOpenAICodexTokenNotDetected):
		return "token_not_detected"
	default:
		return "manual_required"
	}
}

func sanitizeOpenAICodexEvent(message string, maxLen int) string {
	msg := termclean.SanitizeDisplayText(message)
	if msg == "" {
		return ""
	}
	msg = redactOpenAICodexToken(msg)
	if maxLen > 0 && len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	return strings.TrimSpace(msg)
}

func redactOpenAICodexToken(input string) string {
	return replaceOpenAICodexTokenString(input, func(token string) string {
		return "****" + safeTokenSuffix(token)
	})
}

func validateOpenAICodexTokenRaw(token string) error {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return fmt.Errorf("%w: empty token", ErrOpenAICodexTokenNotDetected)
	}
	if len(trimmed) < 20 {
		return fmt.Errorf("%w: token too short", ErrOpenAICodexTokenNotDetected)
	}
	if strings.ContainsAny(trimmed, " \t\n\r") {
		return fmt.Errorf("%w: token contains whitespace", ErrOpenAICodexTokenNotDetected)
	}
	return nil
}
