package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestGeminiOAuthManagerStartAndManualConsume(t *testing.T) {
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	manager := NewGeminiOAuthManager(ManagerOptions{
		StateTTL: 5 * time.Minute,
		Now: func() time.Time {
			return now
		},
		IsRemote: func() bool { return true },
	})

	started, err := manager.Start(context.Background(), StartRequest{
		ProfileID: "p1",
	}, func(challenge, state, redirectURI string) (string, error) {
		if challenge == "" || state == "" || redirectURI == "" {
			return "", fmt.Errorf("missing oauth params")
		}
		return "https://accounts.google.com/mock?state=" + state, nil
	})
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}
	if started.Mode != OAuthFlowManual {
		t.Fatalf("expected manual mode, got %s", started.Mode)
	}

	callbackURL := fmt.Sprintf(
		"http://localhost:8085/oauth2callback?code=auth-code&state=%s",
		started.State,
	)
	pending, code, err := manager.ConsumeFromManual(started.State, callbackURL)
	if err != nil {
		t.Fatalf("consume manual: %v", err)
	}
	if code != "auth-code" {
		t.Fatalf("expected auth-code, got %q", code)
	}
	if pending.ProfileID != "p1" {
		t.Fatalf("expected profile id to survive pending state")
	}
}

func TestGeminiOAuthManagerConsumeExpiredState(t *testing.T) {
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	manager := NewGeminiOAuthManager(ManagerOptions{
		StateTTL: 2 * time.Second,
		Now: func() time.Time {
			return now
		},
		IsRemote: func() bool { return true },
	})

	started, err := manager.Start(context.Background(), StartRequest{}, func(_ string, state string, _ string) (string, error) {
		return "https://accounts.google.com/mock?state=" + state, nil
	})
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}

	now = now.Add(3 * time.Second)
	_, err = manager.ConsumeFromCallback(started.State, "code")
	if !errors.Is(err, ErrStateExpired) {
		t.Fatalf("expected ErrStateExpired, got %v", err)
	}
}

func TestGeminiOAuthManagerStateMismatch(t *testing.T) {
	manager := NewGeminiOAuthManager(ManagerOptions{
		IsRemote: func() bool { return true },
	})
	started, err := manager.Start(context.Background(), StartRequest{}, func(_ string, state string, _ string) (string, error) {
		return "https://accounts.google.com/mock?state=" + state, nil
	})
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}

	_, _, err = manager.ConsumeFromManual(started.State, "http://localhost:8085/oauth2callback?code=abc&state=wrong")
	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("expected ErrStateMismatch, got %v", err)
	}
}

func TestGeminiOAuthManagerLoopbackModeWhenListenerAlive(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen loopback: %v", err)
	}
	defer ln.Close()
	_, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	manager := NewGeminiOAuthManager(ManagerOptions{
		Host:     "127.0.0.1",
		Port:     port,
		IsRemote: func() bool { return false },
	})
	started, err := manager.Start(context.Background(), StartRequest{}, func(_ string, state string, _ string) (string, error) {
		return "https://accounts.google.com/mock?state=" + state, nil
	})
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}
	if started.Mode != OAuthFlowLoopback {
		t.Fatalf("expected loopback mode, got %s", started.Mode)
	}
	if started.OAuthMode != "auto" {
		t.Fatalf("expected oauth_mode auto, got %q", started.OAuthMode)
	}
}

func TestGeminiOAuthManagerRemoteModeForcesManual(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen loopback: %v", err)
	}
	defer ln.Close()
	_, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	manager := NewGeminiOAuthManager(ManagerOptions{
		Host:     "127.0.0.1",
		Port:     port,
		IsRemote: func() bool { return false },
	})
	started, err := manager.Start(context.Background(), StartRequest{
		Mode: "remote",
	}, func(_ string, state string, _ string) (string, error) {
		return "https://accounts.google.com/mock?state=" + state, nil
	})
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}
	if started.Mode != OAuthFlowManual {
		t.Fatalf("expected manual mode for remote, got %s", started.Mode)
	}
	if started.OAuthMode != "remote" {
		t.Fatalf("expected oauth_mode remote, got %q", started.OAuthMode)
	}
}

func TestGeminiOAuthManagerLocalModeUsesCustomRedirect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen loopback: %v", err)
	}
	defer ln.Close()
	_, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	redirectURI := "http://127.0.0.1:" + portText + "/oauth2callback"

	manager := NewGeminiOAuthManager(ManagerOptions{
		IsRemote: func() bool { return true },
	})
	started, err := manager.Start(context.Background(), StartRequest{
		Mode:        "local",
		RedirectURI: redirectURI,
	}, func(_ string, state string, redirect string) (string, error) {
		if redirect != redirectURI {
			return "", fmt.Errorf("redirect mismatch: %s", redirect)
		}
		return "https://accounts.google.com/mock?state=" + state, nil
	})
	if err != nil {
		t.Fatalf("start oauth: %v", err)
	}
	if started.Mode != OAuthFlowLoopback {
		t.Fatalf("expected loopback mode for local redirect, got %s", started.Mode)
	}
	if started.OAuthMode != "local" {
		t.Fatalf("expected oauth_mode local, got %q", started.OAuthMode)
	}
	if started.RedirectURI != redirectURI {
		t.Fatalf("redirect uri mismatch: got %s", started.RedirectURI)
	}
}
