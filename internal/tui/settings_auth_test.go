package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/client"
)

func TestHandleAnthropicBrowserCancelJobNotFoundClearsState(t *testing.T) {
	as := NewAuthSection()
	as.browserJobID = "job-1"
	as.browserJobStatus = "running"
	as.browserJobEvents = []client.AnthropicBrowserJobEvent{{At: time.Now(), Message: "running"}}

	_ = as.HandleAnthropicBrowserCancel(AnthropicBrowserCancelMsg{
		JobID: "job-1",
		Err: &client.APIError{
			StatusCode: 404,
			Code:       "job_not_found",
			Message:    "not found",
		},
	})

	if as.browserJobID != "" {
		t.Fatalf("expected browser job id cleared, got %q", as.browserJobID)
	}
	if as.browserJobStatus != "" {
		t.Fatalf("expected browser job status cleared, got %q", as.browserJobStatus)
	}
	if len(as.browserJobEvents) != 0 {
		t.Fatalf("expected browser events cleared, got %d", len(as.browserJobEvents))
	}
}

func TestAuthViewStripsTerminalControlSequences(t *testing.T) {
	as := NewAuthSection()
	as.browserJobID = "job-1"
	as.browserJobMode = "cli_bridge"
	as.browserJobStatus = "running"
	as.browserJobEvents = []client.AnthropicBrowserJobEvent{
		{At: time.Now(), Message: "\x1b]11;rgb:11/22/33\x07"},
		{At: time.Now(), Message: "\x1b[31mPaste code here\x1b[0m"},
	}
	as.statusMsg = "bad:\x1b]11;rgb:11/22/33\x07done"

	view := as.View(80)
	if strings.Contains(view, "\x1b]11;") {
		t.Fatalf("view should not contain raw OSC sequence: %q", view)
	}
	if strings.Contains(view, "rgb:11/22/33") {
		t.Fatalf("view should not contain OSC payload: %q", view)
	}
	if strings.Contains(view, "\x1b[31m") {
		t.Fatalf("view should not contain ANSI sequence: %q", view)
	}
}
