package core

import (
	"testing"
)

func TestAccountPoolSkipsCooldownAccount(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
		{ID: "a2", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t2"},
	}, []string{"a1", "a2"}, DefaultCooldownConfig())

	pool.MarkFailure("a1", FailureRateLimit)
	account, ok := pool.Acquire("")
	if !ok {
		t.Fatalf("expected available account")
	}
	if account.ID != "a2" {
		t.Fatalf("expected a2, got %s", account.ID)
	}
}

func TestBillingWindowNotExtendedWhileActive(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
	}, nil, DefaultCooldownConfig())

	pool.MarkFailure("a1", FailureBilling)
	first := pool.Snapshot()[0].Usage
	if first == nil || first.DisabledUntil.IsZero() {
		t.Fatalf("expected disabled window after first billing failure")
	}
	firstUntil := first.DisabledUntil

	pool.MarkFailure("a1", FailureBilling)
	second := pool.Snapshot()[0].Usage
	if second == nil {
		t.Fatalf("expected usage stats")
	}
	if !second.DisabledUntil.Equal(firstUntil) {
		t.Fatalf("disabled window should stay immutable while active")
	}
}

func TestRateLimitSetsCooldownNotDisabled(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
	}, nil, DefaultCooldownConfig())

	pool.MarkFailure("a1", FailureRateLimit)
	stats := pool.Snapshot()[0].Usage
	if stats == nil || stats.CooldownUntil.IsZero() {
		t.Fatalf("expected cooldown")
	}
	if stats.DisabledReason != "" {
		t.Fatalf("expected no disabled reason for rate-limit")
	}
}

func TestAuthFailureSetsDisabledBackoff(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
	}, nil, DefaultCooldownConfig())

	pool.MarkFailure("a1", FailureAuth)
	stats := pool.Snapshot()[0].Usage
	if stats == nil {
		t.Fatalf("expected usage stats")
	}
	if stats.DisabledUntil.IsZero() {
		t.Fatalf("expected disabled backoff for auth failure")
	}
	if stats.DisabledReason != FailureAuth {
		t.Fatalf("expected disabled reason auth, got %q", stats.DisabledReason)
	}
}
