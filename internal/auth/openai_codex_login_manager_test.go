package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeOpenAICodexRunner struct {
	availableErr error
	token        string
	runErr       error
	blockUntil   <-chan struct{}
	emitMessages []string
}

func (f fakeOpenAICodexRunner) Available(context.Context) error {
	return f.availableErr
}

func (f fakeOpenAICodexRunner) RunLogin(ctx context.Context, emit func(message string)) (string, error) {
	if emit != nil {
		emit("starting openai codex login")
		for _, message := range f.emitMessages {
			emit(message)
		}
	}
	if f.blockUntil != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-f.blockUntil:
		}
	}
	if f.runErr != nil {
		return "", f.runErr
	}
	return f.token, nil
}

func TestOpenAICodexLoginManagerStartAutoBridgeSuccess(t *testing.T) {
	manager := NewOpenAICodexLoginManager(OpenAICodexLoginManagerOptions{
		Runner: fakeOpenAICodexRunner{
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2NvdW50IjoiYWJjMTIzIn0.signaturepayload",
		},
		IsRemote: func() bool { return false },
	})

	started, err := manager.Start(context.Background(), OpenAICodexLoginStartRequest{
		DisplayName: "codex-main",
		OnToken: func(_ context.Context, _ string, _ string, _ string, _ bool) (OpenAICodexPersistResult, error) {
			return OpenAICodexPersistResult{
				ProfileID:   "openai-codex:oauth_main_abcd12",
				DisplayName: "codex-main",
				KeyHint:     "****cd12",
				Preferred:   true,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Mode != string(OpenAICodexLoginModeCLIBridge) {
		t.Fatalf("expected cli_bridge, got %s", started.Mode)
	}

	var final OpenAICodexLoginSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		final, err = manager.Get(started.JobID)
		if err == nil && final.Status == string(OpenAICodexLoginStatusCompleted) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final.Status != string(OpenAICodexLoginStatusCompleted) {
		t.Fatalf("expected completed status, got %s err=%v", final.Status, err)
	}
	if final.ProfileID != "openai-codex:oauth_main_abcd12" {
		t.Fatalf("profile id mismatch: %s", final.ProfileID)
	}
}

func TestOpenAICodexLoginManagerStartRemoteRequiresManual(t *testing.T) {
	manager := NewOpenAICodexLoginManager(OpenAICodexLoginManagerOptions{
		Runner:   fakeOpenAICodexRunner{},
		IsRemote: func() bool { return true },
	})
	started, err := manager.Start(context.Background(), OpenAICodexLoginStartRequest{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Status != string(OpenAICodexLoginStatusManualRequired) {
		t.Fatalf("expected manual_required, got %s", started.Status)
	}
	if started.Mode != string(OpenAICodexLoginModeManual) {
		t.Fatalf("expected manual mode, got %s", started.Mode)
	}
}

func TestOpenAICodexLoginManagerExpiry(t *testing.T) {
	now := time.Date(2026, 3, 3, 3, 0, 0, 0, time.UTC)
	manager := NewOpenAICodexLoginManager(OpenAICodexLoginManagerOptions{
		Runner: fakeOpenAICodexRunner{},
		Now: func() time.Time {
			return now
		},
		JobTTL: 2 * time.Second,
	})
	started, err := manager.Start(context.Background(), OpenAICodexLoginStartRequest{Mode: "remote"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	now = now.Add(3 * time.Second)
	_, err = manager.Get(started.JobID)
	if !errors.Is(err, ErrOpenAICodexLoginJobExpired) {
		t.Fatalf("expected job expired, got %v", err)
	}
}

func TestOpenAICodexLoginManagerCancelRunning(t *testing.T) {
	block := make(chan struct{})
	manager := NewOpenAICodexLoginManager(OpenAICodexLoginManagerOptions{
		Runner: fakeOpenAICodexRunner{
			token:      "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2NvdW50IjoiYWJjMTIzIn0.signaturepayload",
			blockUntil: block,
		},
		IsRemote: func() bool { return false },
	})
	started, err := manager.Start(context.Background(), OpenAICodexLoginStartRequest{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	cancelled, err := manager.Cancel(started.JobID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != string(OpenAICodexLoginStatusCancelled) {
		t.Fatalf("cancel status mismatch: %s", cancelled.Status)
	}
	close(block)
}

func TestOpenAICodexLoginManagerSanitizesAndRedactsEvents(t *testing.T) {
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2NvdW50IjoiYWJjMTIzIn0.signaturepayload"
	manager := NewOpenAICodexLoginManager(OpenAICodexLoginManagerOptions{
		Runner: fakeOpenAICodexRunner{
			token: token,
			emitMessages: []string{
				"\x1b]11;rgb:11/22/33\x07",
				"\x1b[31mline with token " + token + "\x1b[0m",
			},
		},
		IsRemote: func() bool { return false },
	})

	started, err := manager.Start(context.Background(), OpenAICodexLoginStartRequest{
		OnToken: func(_ context.Context, _ string, _ string, _ string, _ bool) (OpenAICodexPersistResult, error) {
			return OpenAICodexPersistResult{ProfileID: "openai-codex:test", KeyHint: "****load"}, nil
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	var final OpenAICodexLoginSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		final, err = manager.Get(started.JobID)
		if err == nil && final.Status == string(OpenAICodexLoginStatusCompleted) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final.Status != string(OpenAICodexLoginStatusCompleted) {
		t.Fatalf("expected completed status, got %s", final.Status)
	}

	for _, event := range final.Events {
		if strings.Contains(event.Message, "\x1b") {
			t.Fatalf("event contains escape sequence: %q", event.Message)
		}
		if strings.Contains(event.Message, "rgb:") {
			t.Fatalf("event contains OSC payload: %q", event.Message)
		}
		if strings.Contains(event.Message, "eyJ") {
			t.Fatalf("event leaked raw token: %q", event.Message)
		}
	}
}
