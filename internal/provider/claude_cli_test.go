package provider

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

// ---------------------------------------------------------------------------
// serializeClaudeCLIMessages — pure helper, table-driven.
// ---------------------------------------------------------------------------

func TestSerializeClaudeCLIMessages_SingleUserTurn(t *testing.T) {
	prompt, system := serializeClaudeCLIMessages([]core.Message{
		{Role: core.RoleUser, Content: "Hello world"},
	})
	if prompt != "Hello world" {
		t.Fatalf("prompt = %q, want %q", prompt, "Hello world")
	}
	if system != "" {
		t.Fatalf("system = %q, want empty", system)
	}
}

func TestSerializeClaudeCLIMessages_SystemPlusUser(t *testing.T) {
	prompt, system := serializeClaudeCLIMessages([]core.Message{
		{Role: core.RoleSystem, Content: "Be terse"},
		{Role: core.RoleSystem, Content: "Reply in English"},
		{Role: core.RoleUser, Content: "ping"},
	})
	if prompt != "ping" {
		t.Fatalf("prompt = %q, want %q", prompt, "ping")
	}
	wantSystem := "Be terse\n\nReply in English"
	if system != wantSystem {
		t.Fatalf("system = %q, want %q", system, wantSystem)
	}
}

func TestSerializeClaudeCLIMessages_MultiTurn(t *testing.T) {
	prompt, system := serializeClaudeCLIMessages([]core.Message{
		{Role: core.RoleUser, Content: "first"},
		{Role: core.RoleAssistant, Content: "ack"},
		{Role: core.RoleUser, Content: "second"},
	})
	if system != "" {
		t.Fatalf("system = %q, want empty", system)
	}
	want := "User: first\n\nAssistant: ack\n\nUser: second\n\nAssistant:"
	if prompt != want {
		t.Fatalf("prompt = %q, want %q", prompt, want)
	}
}

func TestSerializeClaudeCLIMessages_SkipsEmptyAndImageOnly(t *testing.T) {
	prompt, _ := serializeClaudeCLIMessages([]core.Message{
		{Role: core.RoleUser, Content: "  "},
		{Role: core.RoleSystem, Content: ""},
		{Role: core.RoleUser, Content: "hi"},
	})
	if prompt != "hi" {
		t.Fatalf("prompt = %q, want %q", prompt, "hi")
	}
}

func TestSerializeClaudeCLIMessages_EmptyHistory(t *testing.T) {
	prompt, system := serializeClaudeCLIMessages(nil)
	if prompt != "" || system != "" {
		t.Fatalf("expected empty, got prompt=%q system=%q", prompt, system)
	}
}

func TestSerializeClaudeCLIMessages_ToolMessageEmittedAsNote(t *testing.T) {
	prompt, _ := serializeClaudeCLIMessages([]core.Message{
		{Role: core.RoleUser, Content: "search the docs"},
		{Role: core.RoleAssistant, Content: "calling tool"},
		{Role: core.RoleTool, ToolName: "search", Content: "found 3 results"},
		{Role: core.RoleUser, Content: "summarize them"},
	})
	if !strings.Contains(prompt, "Tool (search): found 3 results") {
		t.Fatalf("expected tool note in prompt, got %q", prompt)
	}
	if !strings.HasSuffix(prompt, "Assistant:") {
		t.Fatalf("expected trailing Assistant: cue, got %q", prompt)
	}
}

// ---------------------------------------------------------------------------
// mapClaudeCLIModel — pass-through with default fallback.
// ---------------------------------------------------------------------------

func TestMapClaudeCLIModel(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{"empty falls back", "", "sonnet", "sonnet"},
		{"whitespace falls back", "  \t", "opus", "opus"},
		{"alias preserved", "haiku", "sonnet", "haiku"},
		{"full id preserved", "claude-opus-4-7", "sonnet", "claude-opus-4-7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapClaudeCLIModel(tc.input, tc.fallback)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// classifyClaudeCLIMessage — known CLI errors map to specific failure reasons.
// ---------------------------------------------------------------------------

func TestClassifyClaudeCLIMessage(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want core.FailureReason
	}{
		{"not logged in", "Not logged in · Please run /login", core.FailureAuthPermanent},
		{"rate limit", "429 rate limit exceeded", core.FailureRateLimit},
		{"timeout", "context deadline exceeded", core.FailureTimeout},
		{"unknown text", "something exploded", core.FailureUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyClaudeCLIMessage(tc.msg)
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Generate() — covered with a fake binary so tests stay hermetic.
// ---------------------------------------------------------------------------

// writeFakeClaudeBinary writes a shell script that emits the given stdout
// and exits with the given code. The script ignores its arguments and stdin
// so each test gets a deterministic response.
func writeFakeClaudeBinary(t *testing.T, stdoutPayload string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake binary uses POSIX shell; skipping on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	// Quoted heredoc keeps the payload byte-for-byte; cat consumes stdin so
	// the parent process does not see a broken pipe when we write to it.
	script := "#!/usr/bin/env bash\n" +
		"cat >/dev/null\n" +
		"cat <<'NEKOCLAW_FAKE_EOF'\n" +
		stdoutPayload + "\n" +
		"NEKOCLAW_FAKE_EOF\n" +
		"exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return path
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestGenerate_FakeBinary_Success(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"type":           "result",
		"subtype":        "success",
		"is_error":       false,
		"result":         "hello back",
		"session_id":     "fake-session",
		"total_cost_usd": 0.001,
		"usage": map[string]int{
			"input_tokens":                3,
			"output_tokens":               5,
			"cache_read_input_tokens":     1000,
			"cache_creation_input_tokens": 200,
		},
	})
	bin := writeFakeClaudeBinary(t, string(payload), 0)

	prov := NewClaudeCLIProvider(ClaudeCLIOptions{BinPath: bin, Timeout: 10 * time.Second})
	resp, err := prov.Generate(context.Background(), GenerateRequest{
		Model:    "sonnet",
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "hello back" {
		t.Fatalf("Text = %q, want %q", resp.Text, "hello back")
	}
	if resp.Endpoint != claudeCLIEndpoint {
		t.Fatalf("Endpoint = %q, want %q", resp.Endpoint, claudeCLIEndpoint)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("Usage tokens = %+v", resp.Usage)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Fatalf("TotalTokens = %d, want 8", resp.Usage.TotalTokens)
	}
	if resp.Usage.CachedTokens == nil || *resp.Usage.CachedTokens != 1000 {
		t.Fatalf("CachedTokens = %+v", resp.Usage.CachedTokens)
	}
	if resp.Usage.PromptTokensTotal == nil || *resp.Usage.PromptTokensTotal != 1203 {
		t.Fatalf("PromptTokensTotal = %+v", resp.Usage.PromptTokensTotal)
	}
	if resp.Usage.PromptTokensUncached == nil || *resp.Usage.PromptTokensUncached != 203 {
		t.Fatalf("PromptTokensUncached = %+v", resp.Usage.PromptTokensUncached)
	}
	if len(resp.Raw) == 0 {
		t.Fatalf("Raw should be populated")
	}
}

func TestGenerate_FakeBinary_NotLoggedIn(t *testing.T) {
	payload := `{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login","session_id":"x","usage":{"input_tokens":0,"output_tokens":0}}`
	bin := writeFakeClaudeBinary(t, payload, 0)

	prov := NewClaudeCLIProvider(ClaudeCLIOptions{BinPath: bin, Timeout: 10 * time.Second})
	_, err := prov.Generate(context.Background(), GenerateRequest{
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T (%v)", err, err)
	}
	if fe.Reason != core.FailureAuthPermanent {
		t.Fatalf("Reason = %s, want %s", fe.Reason, core.FailureAuthPermanent)
	}
}

func TestGenerate_FakeBinary_MalformedJSON(t *testing.T) {
	bin := writeFakeClaudeBinary(t, "not-json-at-all", 0)
	prov := NewClaudeCLIProvider(ClaudeCLIOptions{BinPath: bin, Timeout: 10 * time.Second})
	_, err := prov.Generate(context.Background(), GenerateRequest{
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T (%v)", err, err)
	}
	if fe.Reason != core.FailureFormat {
		t.Fatalf("Reason = %s, want %s", fe.Reason, core.FailureFormat)
	}
}

func TestGenerate_FakeBinary_NonZeroExit(t *testing.T) {
	bin := writeFakeClaudeBinary(t, "boom", 7)
	prov := NewClaudeCLIProvider(ClaudeCLIOptions{BinPath: bin, Timeout: 10 * time.Second})
	_, err := prov.Generate(context.Background(), GenerateRequest{
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T (%v)", err, err)
	}
}

func TestGenerate_FakeBinary_EmptyPrompt(t *testing.T) {
	// No fake binary needed — should fail before exec.
	prov := NewClaudeCLIProvider(ClaudeCLIOptions{BinPath: "/does/not/exist"})
	_, err := prov.Generate(context.Background(), GenerateRequest{
		Messages: []core.Message{{Role: core.RoleSystem, Content: "system only"}},
	})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T (%v)", err, err)
	}
	if fe.Reason != core.FailureFormat {
		t.Fatalf("Reason = %s, want %s", fe.Reason, core.FailureFormat)
	}
}

func TestGenerate_FakeBinary_BinaryMissing(t *testing.T) {
	prov := NewClaudeCLIProvider(ClaudeCLIOptions{
		BinPath: "/nonexistent/path/to/claude-binary",
		Timeout: 5 * time.Second,
	})
	_, err := prov.Generate(context.Background(), GenerateRequest{
		Messages: []core.Message{{Role: core.RoleUser, Content: "hi"}},
	})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T (%v)", err, err)
	}
}

// ---------------------------------------------------------------------------
// Provider interface conformance & basic accessors.
// ---------------------------------------------------------------------------

func TestClaudeCLIProvider_ID(t *testing.T) {
	prov := NewClaudeCLIProvider(ClaudeCLIOptions{})
	if prov.ID() != "claude-cli" {
		t.Fatalf("ID = %q, want claude-cli", prov.ID())
	}
}

func TestClaudeCLIProvider_ContextWindowDefault(t *testing.T) {
	prov := NewClaudeCLIProvider(ClaudeCLIOptions{})
	if prov.ContextWindow("anything") != defaultClaudeCLIContextWindow {
		t.Fatalf("ContextWindow default mismatch")
	}
}

func TestClaudeCLIProvider_DefaultsApplied(t *testing.T) {
	prov := NewClaudeCLIProvider(ClaudeCLIOptions{})
	if prov.binPath != defaultClaudeCLIBin {
		t.Fatalf("binPath = %q, want %q", prov.binPath, defaultClaudeCLIBin)
	}
	if prov.timeout != defaultClaudeCLITimeout {
		t.Fatalf("timeout = %s, want %s", prov.timeout, defaultClaudeCLITimeout)
	}
	if prov.defaultModel != defaultClaudeCLIModel {
		t.Fatalf("defaultModel = %q, want %q", prov.defaultModel, defaultClaudeCLIModel)
	}
}

func TestClaudeCLIProvider_BinPathFromEnv(t *testing.T) {
	t.Setenv("NEKOCLAW_CLAUDE_CLI_BIN", "/some/custom/path")
	prov := NewClaudeCLIProvider(ClaudeCLIOptions{})
	if prov.binPath != "/some/custom/path" {
		t.Fatalf("binPath = %q, want /some/custom/path", prov.binPath)
	}
}

// Compile-time assertion that ClaudeCLIProvider satisfies the Provider
// interface. Not actually invoked.
var _ Provider = (*ClaudeCLIProvider)(nil)
