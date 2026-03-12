package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/auth"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

func writeGeminiTestScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-gemini.sh")
	script := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func seedLocalGeminiProjects(t *testing.T, projects map[string]string) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	geminiDir := filepath.Join(homeDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	raw, err := json.MarshalIndent(geminiCLIProjectsFile{Projects: projects}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(geminiDir, geminiCLIProjectsFilename), raw, 0o600); err != nil {
		t.Fatalf("WriteFile(projects.json) error = %v", err)
	}
	return homeDir
}

func newGeminiCLIRequest(t *testing.T) geminiCLIHeadlessRequest {
	t.Helper()
	workspaceRoot := t.TempDir()
	authBaseDir := t.TempDir()
	seedLocalGeminiProjects(t, map[string]string{
		workspaceRoot:          "nekoclaw",
		"/Users/example/other": "ignored",
	})
	return geminiCLIHeadlessRequest{
		ProfileID:     "profile:main",
		Model:         geminiCLI31ProModelID,
		Prompt:        "hello from test",
		WorkspaceRoot: workspaceRoot,
		AuthBaseDir:   authBaseDir,
		Credential: auth.Credential{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Unix(1_700_000_000, 0).UTC(),
		},
	}
}

func TestGeminiCLIProcessRunnerBuildCommandWritesOAuthCredsAndProjects(t *testing.T) {
	runner := &geminiCLIProcessRunner{binary: writeGeminiTestScript(t, `exit 0`)}
	req := newGeminiCLIRequest(t)

	cmd, err := runner.buildCommand(context.Background(), req, geminiCLIOutputJSON)
	if err != nil {
		t.Fatalf("buildCommand() error = %v", err)
	}

	if cmd.Dir != req.WorkspaceRoot {
		t.Fatalf("cmd.Dir = %q, want %q", cmd.Dir, req.WorkspaceRoot)
	}
	if len(cmd.Args) != 7 {
		t.Fatalf("len(cmd.Args) = %d, want 7 (%v)", len(cmd.Args), cmd.Args)
	}
	if cmd.Args[1] != "-m" || cmd.Args[2] != req.Model {
		t.Fatalf("model args = %v", cmd.Args)
	}
	if cmd.Args[3] != "-p" || cmd.Args[4] != req.Prompt {
		t.Fatalf("prompt args = %v", cmd.Args)
	}
	if cmd.Args[5] != "--output-format" || cmd.Args[6] != geminiCLIOutputJSON {
		t.Fatalf("output args = %v", cmd.Args)
	}

	expectedHome := geminiCLIHomeDir(req.AuthBaseDir, req.ProfileID)
	foundHome := false
	for _, env := range cmd.Env {
		if env == "GEMINI_CLI_HOME="+expectedHome {
			foundHome = true
			break
		}
	}
	if !foundHome {
		t.Fatalf("GEMINI_CLI_HOME missing from env: %v", cmd.Env)
	}

	rawCreds, err := os.ReadFile(filepath.Join(expectedHome, ".gemini", geminiCLIOAuthFilename))
	if err != nil {
		t.Fatalf("ReadFile(oauth_creds.json) error = %v", err)
	}
	var creds map[string]any
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		t.Fatalf("json.Unmarshal(oauth_creds.json) error = %v", err)
	}
	if creds["access_token"] != "access-token" {
		t.Fatalf("access_token = %#v, want access-token", creds["access_token"])
	}
	if creds["refresh_token"] != "refresh-token" {
		t.Fatalf("refresh_token = %#v, want refresh-token", creds["refresh_token"])
	}
	if creds["token_type"] != "Bearer" {
		t.Fatalf("token_type = %#v, want Bearer", creds["token_type"])
	}
	if got, ok := creds["expiry_date"].(float64); !ok || int64(got) != req.Credential.ExpiresAt.UnixMilli() {
		t.Fatalf("expiry_date = %#v, want %d", creds["expiry_date"], req.Credential.ExpiresAt.UnixMilli())
	}

	rawProjects, err := os.ReadFile(filepath.Join(expectedHome, ".gemini", geminiCLIProjectsFilename))
	if err != nil {
		t.Fatalf("ReadFile(projects.json) error = %v", err)
	}
	var projects geminiCLIProjectsFile
	if err := json.Unmarshal(rawProjects, &projects); err != nil {
		t.Fatalf("json.Unmarshal(projects.json) error = %v", err)
	}
	if len(projects.Projects) != 1 {
		t.Fatalf("len(projects.Projects) = %d, want 1", len(projects.Projects))
	}
	if got := projects.Projects[req.WorkspaceRoot]; got != "nekoclaw" {
		t.Fatalf("projects[%q] = %q, want nekoclaw", req.WorkspaceRoot, got)
	}
}

func TestGeminiCLIProcessRunnerGenerateStreamParsesMessageAndStats(t *testing.T) {
	runner := &geminiCLIProcessRunner{binary: writeGeminiTestScript(t, `
cat <<'EOF'
{"type":"init","session_id":"s1"}
{"type":"message","role":"assistant","content":"hello "}
{"type":"message","role":"assistant","content":"world"}
{"type":"result","status":"success","stats":{"input_tokens":7,"output_tokens":4,"total_tokens":11,"cached":2,"input":5}}
EOF
`)}
	req := newGeminiCLIRequest(t)

	var streamed strings.Builder
	resp, err := runner.GenerateStream(context.Background(), req, func(text string) {
		streamed.WriteString(text)
	})
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if resp.Text != "hello world" {
		t.Fatalf("Text = %q, want hello world", resp.Text)
	}
	if streamed.String() != "hello world" {
		t.Fatalf("streamed text = %q, want hello world", streamed.String())
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 4 || resp.Usage.TotalTokens != 11 {
		t.Fatalf("usage = %+v, want input=7 output=4 total=11", resp.Usage)
	}
	if resp.Usage.CachedTokens == nil || *resp.Usage.CachedTokens != 2 {
		t.Fatalf("cached tokens = %+v, want 2", resp.Usage.CachedTokens)
	}
	if resp.Usage.PromptTokensUncached == nil || *resp.Usage.PromptTokensUncached != 5 {
		t.Fatalf("prompt_tokens_uncached = %+v, want 5", resp.Usage.PromptTokensUncached)
	}
}

func TestGeminiCLIProcessRunnerGenerateMapsExitCodeAndPayloadErrors(t *testing.T) {
	t.Run("exit code 41 maps to auth permanent", func(t *testing.T) {
		runner := &geminiCLIProcessRunner{binary: writeGeminiTestScript(t, `
echo "oauth expired" >&2
exit 41
`)}
		_, err := runner.Generate(context.Background(), newGeminiCLIRequest(t))
		if err == nil {
			t.Fatal("expected Generate() error")
		}
		var failureErr *provider.FailureError
		if !errors.As(err, &failureErr) {
			t.Fatalf("error = %T, want *provider.FailureError", err)
		}
		if failureErr.Reason != core.FailureAuthPermanent {
			t.Fatalf("Reason = %q, want %q", failureErr.Reason, core.FailureAuthPermanent)
		}
		if !strings.Contains(failureErr.Message, "oauth expired") {
			t.Fatalf("Message = %q, want stderr text", failureErr.Message)
		}
	})

	t.Run("json payload error is classified", func(t *testing.T) {
		runner := &geminiCLIProcessRunner{binary: writeGeminiTestScript(t, `
cat <<'EOF'
{"error":{"message":"rate limit hit","code":429}}
EOF
`)}
		_, err := runner.Generate(context.Background(), newGeminiCLIRequest(t))
		if err == nil {
			t.Fatal("expected Generate() error")
		}
		var failureErr *provider.FailureError
		if !errors.As(err, &failureErr) {
			t.Fatalf("error = %T, want *provider.FailureError", err)
		}
		if failureErr.Reason != core.FailureRateLimit {
			t.Fatalf("Reason = %q, want %q", failureErr.Reason, core.FailureRateLimit)
		}
		if failureErr.Message != "rate limit hit" {
			t.Fatalf("Message = %q, want %q", failureErr.Message, "rate limit hit")
		}
	})
}

func TestGeminiCLIProcessRunnerBuildCommandWithoutWorkspaceProjectReturnsRouteUnavailable(t *testing.T) {
	runner := &geminiCLIProcessRunner{binary: writeGeminiTestScript(t, `exit 0`)}
	req := newGeminiCLIRequest(t)
	seedLocalGeminiProjects(t, map[string]string{
		"/Users/example/other": "other-project",
	})

	_, err := runner.buildCommand(context.Background(), req, geminiCLIOutputJSON)
	if err == nil {
		t.Fatal("expected buildCommand() error")
	}
	routeErr, ok := asGeminiCLIRouteUnavailable(err)
	if !ok {
		t.Fatalf("error = %T, want route unavailable", err)
	}
	if routeErr.Reason != "project_mapping_missing" {
		t.Fatalf("Reason = %q, want project_mapping_missing", routeErr.Reason)
	}
}

func TestReadGeminiCLIWorkspaceProjectInvalidJSONReturnsRouteUnavailable(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	geminiDir := filepath.Join(homeDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(geminiDir, geminiCLIProjectsFilename), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile(projects.json) error = %v", err)
	}

	_, err := readGeminiCLIWorkspaceProject(t.TempDir())
	if err == nil {
		t.Fatal("expected readGeminiCLIWorkspaceProject() error")
	}
	routeErr, ok := asGeminiCLIRouteUnavailable(err)
	if !ok {
		t.Fatalf("error = %T, want route unavailable", err)
	}
	if routeErr.Reason != "project_mapping_invalid" {
		t.Fatalf("Reason = %q, want project_mapping_invalid", routeErr.Reason)
	}
}
