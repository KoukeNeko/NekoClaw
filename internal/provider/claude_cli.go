package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

// Claude Code CLI provider — wraps the locally installed `claude` binary so
// users can chat against their existing Claude Code subscription without
// supplying an API key.
//
// Scope (v1): chat only. No tool calling, no streaming, no image input.
// The provider is intended for personal local use; per Anthropic's terms,
// Claude Code subscriptions cannot be resold or proxied to third parties.

const (
	defaultClaudeCLIBin           = "claude"
	defaultClaudeCLITimeout       = 120 * time.Second
	defaultClaudeCLIContextWindow = 200_000
	defaultClaudeCLIModel         = "sonnet"
	claudeCLIEndpoint             = "local://claude-cli"
)

type ClaudeCLIOptions struct {
	BinPath       string        // path to `claude` binary; default "claude" (PATH lookup)
	Timeout       time.Duration // hard ceiling for each invocation; default 120s
	ContextWindow int           // default 200_000
	DefaultModel  string        // default "sonnet"
	WorkDir       string        // sandbox cwd for the spawned CLI; default OS temp dir
}

type ClaudeCLIProvider struct {
	binPath       string
	timeout       time.Duration
	contextWindow int
	defaultModel  string
	workDir       string
}

func NewClaudeCLIProvider(opts ClaudeCLIOptions) *ClaudeCLIProvider {
	bin := strings.TrimSpace(opts.BinPath)
	if bin == "" {
		if env := strings.TrimSpace(os.Getenv("NEKOCLAW_CLAUDE_CLI_BIN")); env != "" {
			bin = env
		} else {
			bin = defaultClaudeCLIBin
		}
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultClaudeCLITimeout
	}
	cw := opts.ContextWindow
	if cw <= 0 {
		cw = defaultClaudeCLIContextWindow
	}
	model := strings.TrimSpace(opts.DefaultModel)
	if model == "" {
		model = defaultClaudeCLIModel
	}
	return &ClaudeCLIProvider{
		binPath:       bin,
		timeout:       timeout,
		contextWindow: cw,
		defaultModel:  model,
		workDir:       strings.TrimSpace(opts.WorkDir),
	}
}

func (p *ClaudeCLIProvider) ID() string { return "claude-cli" }

func (p *ClaudeCLIProvider) ContextWindow(_ string) int { return p.contextWindow }

func (p *ClaudeCLIProvider) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	prompt, systemPrompt := serializeClaudeCLIMessages(req.Messages)
	if strings.TrimSpace(prompt) == "" {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "claude-cli request has no user content",
			Endpoint: claudeCLIEndpoint,
		}
	}

	model := mapClaudeCLIModel(req.Model, p.defaultModel)

	workDir, cleanup, err := p.resolveWorkDir()
	if err != nil {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureUnknown,
			Message:  "claude-cli sandbox setup failed: " + err.Error(),
			Endpoint: claudeCLIEndpoint,
		}
	}
	if cleanup != nil {
		defer cleanup()
	}

	args := []string{
		"--print",
		"--model", model,
		"--output-format", "json",
		"--no-session-persistence",
		"--tools", "",
		"--permission-mode", "bypassPermissions",
		"--setting-sources", "",
		"--strict-mcp-config",
		"--disable-slash-commands",
	}
	if systemPrompt != "" {
		args = append(args, "--append-system-prompt", systemPrompt)
	}

	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, p.binPath, args...)
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureTimeout,
			Message:  fmt.Sprintf("claude-cli timed out after %s", p.timeout),
			Endpoint: claudeCLIEndpoint,
		}
	}
	if runErr != nil {
		// Process failed to start, was killed, or exited non-zero.
		// Surface stderr for diagnosis but classify as unknown by default.
		var exitErr *exec.ExitError
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		reason := core.FailureUnknown
		if errors.As(runErr, &exitErr) {
			// CLI ran and exited non-zero with no parseable JSON.
			reason = classifyClaudeCLIMessage(msg)
		}
		return GenerateResponse{}, &FailureError{
			Reason:   reason,
			Message:  truncateClaudeCLIMessage(msg, 400),
			Endpoint: claudeCLIEndpoint,
		}
	}

	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) == 0 {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "claude-cli returned empty stdout",
			Endpoint: claudeCLIEndpoint,
		}
	}

	var parsed claudeCLIResult
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "claude-cli returned malformed JSON: " + truncateClaudeCLIMessage(string(raw), 280),
			Endpoint: claudeCLIEndpoint,
		}
	}

	if parsed.IsError {
		return GenerateResponse{}, &FailureError{
			Reason:   classifyClaudeCLIMessage(parsed.Result),
			Message:  truncateClaudeCLIMessage(parsed.Result, 400),
			Endpoint: claudeCLIEndpoint,
		}
	}

	if strings.TrimSpace(parsed.Result) == "" {
		return GenerateResponse{}, &FailureError{
			Reason:   core.FailureFormat,
			Message:  "claude-cli response missing .result",
			Endpoint: claudeCLIEndpoint,
		}
	}

	return GenerateResponse{
		Text:     parsed.Result,
		Endpoint: claudeCLIEndpoint,
		Raw:      append([]byte(nil), raw...),
		Usage:    parsed.usageInfo(),
	}, nil
}

// resolveWorkDir returns a sandbox cwd for the CLI process. When WorkDir is
// configured the caller owns its lifecycle; otherwise we create a temp dir
// per call and return a cleanup func.
func (p *ClaudeCLIProvider) resolveWorkDir() (string, func(), error) {
	if p.workDir != "" {
		return p.workDir, nil, nil
	}
	dir, err := os.MkdirTemp("", "nekoclaw-claude-cli-*")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// claudeCLIResult mirrors the JSON document produced by `claude -p
// --output-format json`. Fields not consumed here are deliberately omitted.
type claudeCLIResult struct {
	Type      string          `json:"type"`
	Subtype   string           `json:"subtype"`
	IsError   bool             `json:"is_error"`
	Result    string           `json:"result"`
	SessionID string           `json:"session_id"`
	Usage     claudeCLIUsage   `json:"usage"`
	CostUSD   float64          `json:"total_cost_usd"`
}

type claudeCLIUsage struct {
	InputTokens             int `json:"input_tokens"`
	OutputTokens            int `json:"output_tokens"`
	CacheReadInputTokens    int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

func (u claudeCLIUsage) toUsageInfo() core.UsageInfo {
	cached := u.CacheReadInputTokens
	promptTotal := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	promptUncached := u.InputTokens + u.CacheCreationInputTokens
	return core.UsageInfo{
		InputTokens:          u.InputTokens,
		OutputTokens:         u.OutputTokens,
		TotalTokens:          u.InputTokens + u.OutputTokens,
		CachedTokens:         intPtr(cached),
		PromptTokensTotal:    intPtr(promptTotal),
		PromptTokensUncached: intPtr(promptUncached),
	}
}

func (r claudeCLIResult) usageInfo() core.UsageInfo {
	return r.Usage.toUsageInfo()
}

// serializeClaudeCLIMessages converts NekoClaw's structured message history
// into a single prompt string suitable for `claude -p`. System messages are
// concatenated and returned separately so they can be passed via
// --append-system-prompt; user/assistant turns are emitted with explicit role
// tags so the model preserves multi-turn context.
//
// Image messages are silently skipped — v1 does not support multimodal input.
func serializeClaudeCLIMessages(messages []core.Message) (prompt string, system string) {
	var systemParts []string
	var convoParts []string

	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch msg.Role {
		case core.RoleSystem:
			systemParts = append(systemParts, content)
		case core.RoleUser:
			convoParts = append(convoParts, "User: "+content)
		case core.RoleAssistant:
			convoParts = append(convoParts, "Assistant: "+content)
		case core.RoleTool:
			// Tool messages have no clean place in a pure-chat CLI flow;
			// surface them as a tool note so the model has context.
			convoParts = append(convoParts, "Tool ("+msg.ToolName+"): "+content)
		}
	}

	system = strings.Join(systemParts, "\n\n")

	switch len(convoParts) {
	case 0:
		prompt = ""
	case 1:
		// Single user turn: send the bare content (no role tag noise) to
		// match how a person would invoke `claude -p`.
		prompt = strings.TrimPrefix(convoParts[0], "User: ")
	default:
		// Multi-turn: emit the full transcript and prompt the model to
		// continue as the assistant on the next turn.
		convoParts = append(convoParts, "Assistant:")
		prompt = strings.Join(convoParts, "\n\n")
	}
	return prompt, system
}

// mapClaudeCLIModel passes through both model aliases ("sonnet", "opus",
// "haiku") and full Anthropic model IDs ("claude-sonnet-4-6"). Empty input
// falls back to the provider's default model.
func mapClaudeCLIModel(requested, fallback string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return fallback
	}
	return requested
}

// classifyClaudeCLIMessage maps known CLI error strings onto NekoClaw failure
// reasons. Falls back to the generic classifier for anything unrecognized.
func classifyClaudeCLIMessage(msg string) core.FailureReason {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "not logged in"),
		strings.Contains(lower, "please run /login"),
		strings.Contains(lower, "subscription"):
		return core.FailureAuthPermanent
	case strings.Contains(lower, "no such file") && strings.Contains(lower, "claude"):
		return core.FailureAuthPermanent
	}
	return core.ClassifyFailure(msg)
}

func truncateClaudeCLIMessage(msg string, maxLen int) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen] + "…"
}

// Convenience helper used by tests/setup to validate a provided binary path
// resolves to an executable file. Returns the resolved absolute path.
func ResolveClaudeCLIBin(bin string) (string, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		bin = defaultClaudeCLIBin
	}
	if strings.ContainsRune(bin, filepath.Separator) {
		abs, err := filepath.Abs(bin)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", abs)
		}
		return abs, nil
	}
	return exec.LookPath(bin)
}
