package tui

import (
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
