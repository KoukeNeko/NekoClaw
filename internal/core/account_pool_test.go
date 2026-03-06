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

func TestAuthFailureSetsCooldownBackoff(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
	}, nil, DefaultCooldownConfig())

	pool.MarkFailure("a1", FailureAuth)
	stats := pool.Snapshot()[0].Usage
	if stats == nil {
		t.Fatalf("expected usage stats")
	}
	if stats.CooldownUntil.IsZero() {
		t.Fatalf("expected cooldown backoff for auth failure")
	}
	if !stats.DisabledUntil.IsZero() {
		t.Fatalf("did not expect disabled window for auth failure")
	}
	if stats.DisabledReason != "" {
		t.Fatalf("expected no disabled reason for auth cooldown, got %q", stats.DisabledReason)
	}
}

func TestSetCredentialDoesNotForceExplicitOrderPath(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
	}, nil, DefaultCooldownConfig())

	pool.SetCredential("a2", Account{
		ID:       "a2",
		Provider: "google-gemini-cli",
		Type:     AccountOAuth,
		Token:    "t2",
	})
	pool.MarkFailure("a2", FailureRateLimit)

	account, ok := pool.Acquire("")
	if !ok {
		t.Fatalf("expected fallback to a1 even when a2 is in cooldown")
	}
	if account.ID != "a1" {
		t.Fatalf("expected a1, got %s", account.ID)
	}
}

func TestTimeoutDoesNotSetCooldown(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
	}, nil, DefaultCooldownConfig())

	pool.MarkFailure("a1", FailureTimeout)
	stats := pool.Snapshot()[0].Usage
	if stats == nil {
		t.Fatalf("expected usage stats to track error count")
	}
	if !stats.CooldownUntil.IsZero() {
		t.Fatalf("timeout should NOT set cooldown, got %s", stats.CooldownUntil)
	}
	if stats.ErrorCount != 1 {
		t.Fatalf("expected error count 1, got %d", stats.ErrorCount)
	}

	account, ok := pool.Acquire("")
	if !ok {
		t.Fatalf("account should remain available after timeout")
	}
	if account.ID != "a1" {
		t.Fatalf("expected a1, got %s", account.ID)
	}
}

func TestCircuitBreakerExtendsGlobalCooldown(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
		{ID: "a2", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t2"},
	}, nil, DefaultCooldownConfig())

	// First two capacity failures: normal global cooldown (90s).
	pool.MarkFailure("a1", FailureModelCapacity)
	pool.MarkFailure("a2", FailureModelCapacity)

	soonest := pool.SoonestAvailableAt()
	if soonest.IsZero() {
		t.Fatalf("expected global cooldown after capacity failures")
	}

	// Third capacity failure trips the circuit breaker (threshold = 3).
	pool.MarkFailure("a1", FailureModelCapacity)
	soonestAfter := pool.SoonestAvailableAt()
	if soonestAfter.IsZero() {
		t.Fatalf("expected extended global cooldown after circuit breaker trips")
	}
	// Circuit breaker cooldown (5min) should be longer than normal (90s).
	if !soonestAfter.After(soonest) {
		t.Fatalf("circuit breaker should extend cooldown beyond normal: got %v, baseline %v", soonestAfter, soonest)
	}
}

func TestCircuitBreakerResetsOnSuccess(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
	}, nil, DefaultCooldownConfig())

	// Accumulate capacity failures near threshold.
	pool.MarkFailure("a1", FailureModelCapacity)
	pool.MarkFailure("a1", FailureModelCapacity)

	// Success resets the circuit breaker counter.
	pool.MarkUsed("a1")

	// Next capacity failure should NOT trip the breaker (counter was reset).
	pool.MarkFailure("a1", FailureModelCapacity)
	soonest := pool.SoonestAvailableAt()
	if soonest.IsZero() {
		t.Fatalf("expected global cooldown")
	}
	// Should be near normal cooldown (90s), not extended (5min).
	// The global cooldown should be within 2 minutes of now.
	remaining := soonest.Sub(pool.globalCooldownUntilForTest())
	if remaining != 0 {
		t.Fatalf("unexpected: soonest != globalCooldownUntil")
	}
}

func TestCircuitBreakerResetsOnNonCapacityFailure(t *testing.T) {
	pool := NewAccountPool("google-gemini-cli", []Account{
		{ID: "a1", Provider: "google-gemini-cli", Type: AccountOAuth, Token: "t1"},
	}, nil, DefaultCooldownConfig())

	pool.MarkFailure("a1", FailureModelCapacity)
	pool.MarkFailure("a1", FailureModelCapacity)

	// A non-capacity failure breaks the consecutive streak.
	pool.MarkFailure("a1", FailureRateLimit)

	// Next capacity failure should NOT trip the breaker.
	pool.MarkFailure("a1", FailureModelCapacity)
	// Counter is at 1 (reset to 0 by rate_limit, then +1), not ≥ 3.
}

func TestOpenRouterBypassesCooldownTracking(t *testing.T) {
	pool := NewAccountPool("openrouter", []Account{
		{ID: "or1", Provider: "openrouter", Type: AccountAPIKey, Token: "sk-or"},
	}, nil, DefaultCooldownConfig())

	pool.MarkFailure("or1", FailureRateLimit)

	account, ok := pool.Acquire("")
	if !ok {
		t.Fatalf("expected account to stay available for openrouter")
	}
	if account.ID != "or1" {
		t.Fatalf("expected or1, got %s", account.ID)
	}
	snapshots := pool.Snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Usage != nil {
		t.Fatalf("expected no cooldown usage stats for openrouter")
	}
}
