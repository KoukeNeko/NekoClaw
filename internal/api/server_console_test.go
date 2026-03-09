package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/logger"
	"github.com/doeshing/nekoclaw/internal/provider"
	"github.com/doeshing/nekoclaw/internal/security"
)

func TestConsoleStreamSendsSnapshotAndLiveAppend(t *testing.T) {
	t.Cleanup(func() {
		logger.SetOutput(os.Stderr)
	})
	logger.SetOutput(io.Discard)

	logPath := filepath.Join(t.TempDir(), "nekoclaw.log")
	if err := os.WriteFile(logPath, []byte(strings.Join([]string{
		"12:00:00 system    | boot",
		"12:00:01 mcp       | ready",
		"12:00:02 provider  | online",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write console log: %v", err)
	}

	server := NewServer(app.NewService(app.ServiceOptions{}))
	server.SetConsoleLogPath(logPath)

	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	resp, err := testServer.Client().Get(testServer.URL + "/v1/console/stream?tail=2")
	if err != nil {
		t.Fatalf("connect console stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("console stream status = %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	eventName, raw := readSSEEvent(t, reader)
	if eventName != "snapshot" {
		t.Fatalf("first event = %q, want snapshot", eventName)
	}

	var snapshot consoleSnapshotEvent
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snapshot.Type != "snapshot" {
		t.Fatalf("snapshot type = %q", snapshot.Type)
	}
	if snapshot.LogPath != logPath {
		t.Fatalf("snapshot log path = %q, want %q", snapshot.LogPath, logPath)
	}
	if len(snapshot.Lines) != 2 {
		t.Fatalf("snapshot lines = %d, want 2", len(snapshot.Lines))
	}
	if snapshot.Lines[0] != "12:00:01 mcp       | ready" {
		t.Fatalf("snapshot first line = %q", snapshot.Lines[0])
	}
	if snapshot.Lines[1] != "12:00:02 provider  | online" {
		t.Fatalf("snapshot second line = %q", snapshot.Lines[1])
	}

	logModule := logger.New("console", logger.White)
	logModule.Logf("stream append test")

	eventName, raw = readSSEEvent(t, reader)
	if eventName != "append" {
		t.Fatalf("second event = %q, want append", eventName)
	}

	var appendEvent consoleAppendEvent
	if err := json.Unmarshal(raw, &appendEvent); err != nil {
		t.Fatalf("decode append: %v", err)
	}
	if appendEvent.Type != "append" {
		t.Fatalf("append type = %q", appendEvent.Type)
	}
	if !strings.Contains(appendEvent.Line, "console") || !strings.Contains(appendEvent.Line, "stream append test") {
		t.Fatalf("append line = %q", appendEvent.Line)
	}
}

func TestConsoleStreamUnavailableReturns503(t *testing.T) {
	server := NewServer(app.NewService(app.ServiceOptions{}))
	server.SetConsoleLogPath(filepath.Join(t.TempDir(), "missing.log"))

	resp := performJSONRequest(t, server.Handler(), http.MethodGet, "/v1/console/stream", "")
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	if code := errorCodeFromBody(t, resp); code != "console_unavailable" {
		t.Fatalf("error code = %q, want console_unavailable", code)
	}
}

func TestConsoleStreamRequiresBrowserAuth(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.SetConfigDir(t.TempDir())
	svc.SetSecurityConfig(core.DefaultSecurityConfig())

	mockProvider := provider.NewMockProvider()
	svc.RegisterProvider(mockProvider)
	svc.RegisterPool(core.NewAccountPool("mock", []core.Account{
		{ID: "mock-default", Provider: "mock", Type: core.AccountAPIKey, Token: "mock"},
	}, nil, core.DefaultCooldownConfig()))

	rt, err := security.NewRuntime(security.RuntimeOptions{
		BaseDir: t.TempDir(),
		Config:  svc.GetSecurityConfig(),
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "nekoclaw.log")
	if err := os.WriteFile(logPath, []byte("12:00:00 system    | boot\n"), 0o644); err != nil {
		t.Fatalf("write log path: %v", err)
	}

	server := NewServer(svc)
	server.SetSecurityRuntime(rt)
	server.SetConsoleLogPath(logPath)

	resp := performSecurityRequest(t, server.Handler(), http.MethodGet, "/v1/console/stream", "", nil, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	if code := errorCodeFromBody(t, resp); code != "setup_required" {
		t.Fatalf("error code = %q, want setup_required", code)
	}
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) (string, []byte) {
	t.Helper()

	eventName := ""
	var data strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse line: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if eventName != "" || data.Len() > 0 {
				return eventName, []byte(data.String())
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
}
