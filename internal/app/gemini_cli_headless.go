package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

const (
	geminiExecutionModeInternalAPI = "internal_api"
	geminiExecutionModeCLIHeadless = "cli_headless"

	geminiCLIProviderID        = "google-gemini-cli"
	geminiCLIHomeDirName       = "gemini-cli-home"
	geminiCLIOAuthFilename     = "oauth_creds.json"
	geminiCLIOutputJSON        = "json"
	geminiCLIOutputStreamJSON  = "stream-json"
	geminiCLIDefaultBinaryName = "gemini"
)

type geminiCLIHeadlessRunner interface {
	Generate(ctx context.Context, req geminiCLIHeadlessRequest) (geminiCLIHeadlessResponse, error)
	GenerateStream(ctx context.Context, req geminiCLIHeadlessRequest, onText func(string)) (geminiCLIHeadlessResponse, error)
}

type geminiCLIHeadlessRequest struct {
	ProfileID     string
	Model         string
	Prompt        string
	Credential    auth.Credential
	WorkspaceRoot string
	AuthBaseDir   string
}

type geminiCLIHeadlessResponse struct {
	Text  string
	Usage core.UsageInfo
}

type geminiCLIProcessRunner struct {
	binary string
}

type geminiCLIJSONResult struct {
	SessionID string              `json:"session_id"`
	Response  string              `json:"response"`
	Stats     json.RawMessage     `json:"stats"`
	Error     *geminiCLIJSONError `json:"error"`
}

type geminiCLIJSONError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    any    `json:"code,omitempty"`
}

type geminiCLIStreamMessageEvent struct {
	Type      string `json:"type"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Delta     bool   `json:"delta,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type geminiCLIStreamResultEvent struct {
	Type      string              `json:"type"`
	Status    string              `json:"status,omitempty"`
	Stats     json.RawMessage     `json:"stats"`
	Error     *geminiCLIJSONError `json:"error"`
	Timestamp string              `json:"timestamp,omitempty"`
}

func newGeminiCLIHeadlessRunner() geminiCLIHeadlessRunner {
	return &geminiCLIProcessRunner{binary: geminiCLIDefaultBinaryName}
}

func normalizeGeminiExecutionMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case geminiExecutionModeCLIHeadless:
		return geminiExecutionModeCLIHeadless
	default:
		return geminiExecutionModeInternalAPI
	}
}

func parseGeminiExecutionMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case geminiExecutionModeInternalAPI:
		return geminiExecutionModeInternalAPI, true
	case geminiExecutionModeCLIHeadless:
		return geminiExecutionModeCLIHeadless, true
	default:
		return "", false
	}
}

func renderGeminiCLIHeadlessPrompt(messages []core.Message) string {
	var builder strings.Builder
	builder.WriteString("Continue the following conversation transcript. Obey every system message. Reply only as the assistant to the latest user request.\n\n")
	for _, msg := range messages {
		builder.WriteString("## ")
		builder.WriteString(geminiCLITranscriptRoleLabel(msg))
		if toolName := strings.TrimSpace(msg.ToolName); toolName != "" {
			builder.WriteString(" (")
			builder.WriteString(toolName)
			builder.WriteString(")")
		}
		builder.WriteString("\n")
		content := strings.TrimSpace(msg.Content)
		if content != "" {
			builder.WriteString(content)
		}
		builder.WriteString("\n\n")
	}
	builder.WriteString("## Assistant\n")
	return strings.TrimSpace(builder.String())
}

func geminiCLITranscriptRoleLabel(msg core.Message) string {
	switch msg.Role {
	case core.RoleSystem:
		return "System"
	case core.RoleAssistant:
		return "Assistant"
	case core.RoleTool:
		return "Tool"
	default:
		return "User"
	}
}

func canUseGeminiCLIHeadless(messages []core.Message) bool {
	for _, msg := range messages {
		if len(msg.Images) > 0 {
			return false
		}
	}
	return true
}

func (r *geminiCLIProcessRunner) Generate(
	ctx context.Context,
	req geminiCLIHeadlessRequest,
) (geminiCLIHeadlessResponse, error) {
	stdout, stderr, exitCode, runErr := r.run(ctx, req, geminiCLIOutputJSON)
	if runErr != nil {
		return geminiCLIHeadlessResponse{}, mapGeminiCLIRunError(exitCode, stdout, stderr, runErr)
	}

	var payload geminiCLIJSONResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &payload); err != nil {
		return geminiCLIHeadlessResponse{}, &provider.FailureError{
			Reason:   core.FailureFormat,
			Message:  "invalid gemini CLI json output: " + err.Error(),
			Endpoint: geminiCLIProviderID,
		}
	}
	if payload.Error != nil {
		return geminiCLIHeadlessResponse{}, mapGeminiCLIErrorPayload(payload.Error, stderr)
	}

	return geminiCLIHeadlessResponse{
		Text:  payload.Response,
		Usage: extractGeminiCLIUsage(payload.Stats),
	}, nil
}

func (r *geminiCLIProcessRunner) GenerateStream(
	ctx context.Context,
	req geminiCLIHeadlessRequest,
	onText func(string),
) (geminiCLIHeadlessResponse, error) {
	cmd, err := r.buildCommand(ctx, req, geminiCLIOutputStreamJSON)
	if err != nil {
		return geminiCLIHeadlessResponse{}, err
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return geminiCLIHeadlessResponse{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return geminiCLIHeadlessResponse{}, err
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var fullText strings.Builder
	var finalResult *geminiCLIStreamResultEvent
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var eventType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &eventType); err != nil {
			return geminiCLIHeadlessResponse{}, &provider.FailureError{
				Reason:   core.FailureFormat,
				Message:  "invalid gemini CLI stream-json output: " + err.Error(),
				Endpoint: geminiCLIProviderID,
			}
		}

		switch eventType.Type {
		case "message":
			var evt geminiCLIStreamMessageEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				return geminiCLIHeadlessResponse{}, &provider.FailureError{
					Reason:   core.FailureFormat,
					Message:  "invalid gemini CLI message event: " + err.Error(),
					Endpoint: geminiCLIProviderID,
				}
			}
			if strings.EqualFold(strings.TrimSpace(evt.Role), "assistant") && evt.Content != "" {
				fullText.WriteString(evt.Content)
				if onText != nil {
					onText(evt.Content)
				}
			}
		case "result":
			var evt geminiCLIStreamResultEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				return geminiCLIHeadlessResponse{}, &provider.FailureError{
					Reason:   core.FailureFormat,
					Message:  "invalid gemini CLI result event: " + err.Error(),
					Endpoint: geminiCLIProviderID,
				}
			}
			finalResult = &evt
		}
	}
	if err := scanner.Err(); err != nil {
		return geminiCLIHeadlessResponse{}, err
	}

	waitErr := cmd.Wait()
	if waitErr != nil {
		return geminiCLIHeadlessResponse{}, mapGeminiCLIRunError(exitCodeFromError(waitErr), nil, stderr.Bytes(), waitErr)
	}
	if finalResult != nil && strings.EqualFold(strings.TrimSpace(finalResult.Status), "error") {
		return geminiCLIHeadlessResponse{}, mapGeminiCLIErrorPayload(finalResult.Error, stderr.Bytes())
	}

	return geminiCLIHeadlessResponse{
		Text:  fullText.String(),
		Usage: extractGeminiCLIUsage(resultStats(finalResult)),
	}, nil
}

func (r *geminiCLIProcessRunner) run(
	ctx context.Context,
	req geminiCLIHeadlessRequest,
	outputFormat string,
) ([]byte, []byte, int, error) {
	cmd, err := r.buildCommand(ctx, req, outputFormat)
	if err != nil {
		return nil, nil, 0, err
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), exitCodeFromError(err), err
}

func (r *geminiCLIProcessRunner) buildCommand(
	ctx context.Context,
	req geminiCLIHeadlessRequest,
	outputFormat string,
) (*exec.Cmd, error) {
	workspaceRoot := strings.TrimSpace(req.WorkspaceRoot)
	if workspaceRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		workspaceRoot = cwd
	}

	geminiHome := geminiCLIHomeDir(req.AuthBaseDir, req.ProfileID)
	if err := writeGeminiCLIOAuthCreds(geminiHome, req.Credential); err != nil {
		return nil, err
	}

	args := []string{
		"-m", strings.TrimSpace(req.Model),
		"-p", req.Prompt,
		"--output-format", outputFormat,
	}
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = workspaceRoot
	cmd.Env = append(os.Environ(),
		"GEMINI_CLI_HOME="+geminiHome,
		"GEMINI_FORCE_ENCRYPTED_FILE_STORAGE=false",
	)
	return cmd, nil
}

func geminiCLIHomeDir(authBaseDir, profileID string) string {
	return filepath.Join(strings.TrimSpace(authBaseDir), geminiCLIHomeDirName, sanitizeProfileSlug(profileID))
}

func writeGeminiCLIOAuthCreds(homeDir string, credential auth.Credential) error {
	if strings.TrimSpace(credential.AccessToken) == "" {
		return &provider.FailureError{
			Reason:   core.FailureAuthPermanent,
			Message:  "missing Gemini OAuth access token",
			Endpoint: geminiCLIProviderID,
		}
	}

	geminiDir := filepath.Join(homeDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		return err
	}

	payload := map[string]any{
		"access_token": credential.AccessToken,
		"token_type":   "Bearer",
	}
	if refreshToken := strings.TrimSpace(credential.RefreshToken); refreshToken != "" {
		payload["refresh_token"] = refreshToken
	}
	if !credential.ExpiresAt.IsZero() {
		payload["expiry_date"] = credential.ExpiresAt.UnixMilli()
	}

	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(geminiDir, geminiCLIOAuthFilename)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func mapGeminiCLIRunError(exitCode int, stdout, stderr []byte, runErr error) error {
	if runErr == nil {
		return nil
	}
	reason := geminiCLIFailureReason(exitCode, stdout, stderr)
	return &provider.FailureError{
		Reason:   reason,
		Message:  geminiCLIErrorMessage(stdout, stderr, runErr),
		Endpoint: geminiCLIProviderID,
	}
}

func mapGeminiCLIErrorPayload(errPayload *geminiCLIJSONError, stderr []byte) error {
	if errPayload == nil {
		return &provider.FailureError{
			Reason:   geminiCLIFailureReason(0, nil, stderr),
			Message:  geminiCLIErrorMessage(nil, stderr, nil),
			Endpoint: geminiCLIProviderID,
		}
	}
	message := strings.TrimSpace(errPayload.Message)
	if message == "" {
		message = "Gemini CLI returned an error"
	}
	reason := core.ClassifyFailure(message)
	if reason == core.FailureUnknown {
		reason = geminiCLIFailureReason(exitCodeFromAny(errPayload.Code), nil, stderr)
	}
	return &provider.FailureError{
		Reason:   reason,
		Message:  message,
		Endpoint: geminiCLIProviderID,
	}
}

func geminiCLIErrorMessage(stdout, stderr []byte, fallback error) string {
	if parsed := parseGeminiCLIJSONError(stdout); parsed != "" {
		return parsed
	}
	if message := strings.TrimSpace(string(stderr)); message != "" {
		return message
	}
	if fallback != nil {
		return fallback.Error()
	}
	return "gemini CLI request failed"
}

func parseGeminiCLIJSONError(stdout []byte) string {
	var payload geminiCLIJSONResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &payload); err != nil {
		return ""
	}
	if payload.Error == nil {
		return ""
	}
	return strings.TrimSpace(payload.Error.Message)
}

func geminiCLIFailureReason(exitCode int, stdout, stderr []byte) core.FailureReason {
	switch exitCode {
	case 41:
		return core.FailureAuthPermanent
	case 42, 52:
		return core.FailureFormat
	}
	message := geminiCLIErrorMessage(stdout, stderr, nil)
	if reason := core.ClassifyFailure(message); reason != core.FailureUnknown {
		return reason
	}
	return core.FailureUnknown
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0
	}
	return exitErr.ExitCode()
}

func exitCodeFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		code, _ := typed.Int64()
		return int(code)
	default:
		return 0
	}
}

func resultStats(result *geminiCLIStreamResultEvent) json.RawMessage {
	if result == nil {
		return nil
	}
	return result.Stats
}

func extractGeminiCLIUsage(raw json.RawMessage) core.UsageInfo {
	if len(bytes.TrimSpace(raw)) == 0 {
		return core.UsageInfo{}
	}

	var simplified struct {
		TotalTokens  int `json:"total_tokens"`
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		Cached       int `json:"cached"`
		Input        int `json:"input"`
	}
	if err := json.Unmarshal(raw, &simplified); err == nil && (simplified.TotalTokens > 0 || simplified.InputTokens > 0 || simplified.OutputTokens > 0 || simplified.Cached > 0 || simplified.Input > 0) {
		usage := core.UsageInfo{
			InputTokens:  simplified.InputTokens,
			OutputTokens: simplified.OutputTokens,
			TotalTokens:  simplified.TotalTokens,
		}
		if simplified.TotalTokens == 0 {
			usage.TotalTokens = simplified.InputTokens + simplified.OutputTokens
		}
		if simplified.Cached > 0 {
			cached := simplified.Cached
			usage.CachedTokens = &cached
		}
		if simplified.Input > 0 {
			uncached := simplified.Input
			usage.PromptTokensUncached = &uncached
		}
		return usage
	}

	var metrics struct {
		Models map[string]struct {
			Tokens struct {
				Input      int `json:"input"`
				Prompt     int `json:"prompt"`
				Candidates int `json:"candidates"`
				Total      int `json:"total"`
				Cached     int `json:"cached"`
			} `json:"tokens"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return core.UsageInfo{}
	}

	var usage core.UsageInfo
	var cachedTotal int
	var uncachedTotal int
	for _, model := range metrics.Models {
		usage.InputTokens += model.Tokens.Prompt
		usage.OutputTokens += model.Tokens.Candidates
		usage.TotalTokens += model.Tokens.Total
		cachedTotal += model.Tokens.Cached
		uncachedTotal += model.Tokens.Input
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if cachedTotal > 0 {
		cached := cachedTotal
		usage.CachedTokens = &cached
	}
	if uncachedTotal > 0 {
		uncached := uncachedTotal
		usage.PromptTokensUncached = &uncached
	}
	if usage.InputTokens > 0 {
		promptTotal := usage.InputTokens
		usage.PromptTokensTotal = &promptTotal
	}
	return usage
}
