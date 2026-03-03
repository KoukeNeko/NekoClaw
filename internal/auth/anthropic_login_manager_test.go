package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeAnthropicRunner struct {
	availableErr error
	token        string
	runErr       error
	blockUntil   <-chan struct{}
}

func (f fakeAnthropicRunner) Available(context.Context) error {
	return f.availableErr
}

func (f fakeAnthropicRunner) RunSetupToken(ctx context.Context, emit func(message string)) (string, error) {
	if emit != nil {
		emit("starting setup-token")
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

func TestAnthropicLoginManagerStartAutoBridgeSuccess(t *testing.T) {
	manager := NewAnthropicLoginManager(AnthropicLoginManagerOptions{
		Runner: fakeAnthropicRunner{
			token: "sk-ant-oat01-" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefgh",
		},
		IsRemote: func() bool { return false },
	})

	started, err := manager.Start(context.Background(), AnthropicLoginStartRequest{
		DisplayName: "sub-main",
		OnToken: func(_ context.Context, _ string, _ string, _ string, _ bool) (AnthropicPersistResult, error) {
			return AnthropicPersistResult{
				ProfileID:   "anthropic:sub_main_abcdef",
				DisplayName: "sub-main",
				KeyHint:     "****abcdef",
				Preferred:   true,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Mode != string(AnthropicLoginModeCLIBridge) {
		t.Fatalf("expected cli_bridge, got %s", started.Mode)
	}

	var final AnthropicLoginSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		final, err = manager.Get(started.JobID)
		if err == nil && final.Status == string(AnthropicLoginStatusCompleted) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final.Status != string(AnthropicLoginStatusCompleted) {
		t.Fatalf("expected completed status, got %s err=%v", final.Status, err)
	}
	if final.ProfileID != "anthropic:sub_main_abcdef" {
		t.Fatalf("profile id mismatch: %s", final.ProfileID)
	}
	if final.KeyHint != "****abcdef" {
		t.Fatalf("key hint mismatch: %s", final.KeyHint)
	}
}

func TestAnthropicLoginManagerStartRemoteRequiresManual(t *testing.T) {
	manager := NewAnthropicLoginManager(AnthropicLoginManagerOptions{
		Runner:   fakeAnthropicRunner{},
		IsRemote: func() bool { return true },
	})
	started, err := manager.Start(context.Background(), AnthropicLoginStartRequest{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Status != string(AnthropicLoginStatusManualRequired) {
		t.Fatalf("expected manual_required, got %s", started.Status)
	}
	if started.Mode != string(AnthropicLoginModeManual) {
		t.Fatalf("expected manual mode, got %s", started.Mode)
	}
}

func TestAnthropicLoginManagerExpiry(t *testing.T) {
	now := time.Date(2026, 3, 3, 3, 0, 0, 0, time.UTC)
	manager := NewAnthropicLoginManager(AnthropicLoginManagerOptions{
		Runner: fakeAnthropicRunner{},
		Now: func() time.Time {
			return now
		},
		JobTTL: 2 * time.Second,
	})
	started, err := manager.Start(context.Background(), AnthropicLoginStartRequest{Mode: "remote"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	now = now.Add(3 * time.Second)
	_, err = manager.Get(started.JobID)
	if !errors.Is(err, ErrAnthropicLoginJobExpired) {
		t.Fatalf("expected job expired, got %v", err)
	}
}

func TestAnthropicLoginManagerCancelRunning(t *testing.T) {
	block := make(chan struct{})
	manager := NewAnthropicLoginManager(AnthropicLoginManagerOptions{
		Runner: fakeAnthropicRunner{
			token:      "sk-ant-oat01-" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefgh",
			blockUntil: block,
		},
		IsRemote: func() bool { return false },
	})
	started, err := manager.Start(context.Background(), AnthropicLoginStartRequest{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	cancelled, err := manager.Cancel(started.JobID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != string(AnthropicLoginStatusCancelled) {
		t.Fatalf("cancel status mismatch: %s", cancelled.Status)
	}
	close(block)
}
