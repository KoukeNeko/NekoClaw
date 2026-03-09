package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestSetupCreatesPasswordHashAndConsumesToken(t *testing.T) {
	now := time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)
	rt, err := NewRuntime(RuntimeOptions{
		BaseDir: t.TempDir(),
		Config:  core.DefaultSecurityConfig(),
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}

	token, err := rt.EnsureSetupToken()
	if err != nil {
		t.Fatalf("EnsureSetupToken failed: %v", err)
	}

	session, err := rt.Setup(token, "correct horse battery")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if strings.TrimSpace(session.ID) == "" {
		t.Fatalf("expected session id after setup")
	}

	stateData, err := os.ReadFile(filepath.Join(rt.baseDir, stateFileName))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if strings.Contains(string(stateData), "correct horse battery") {
		t.Fatalf("security state leaked raw password: %s", string(stateData))
	}

	if _, err := rt.Setup(token, "another password"); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second setup error = %v, want %v", err, ErrAlreadyInitialized)
	}
}

func TestSessionExpiryAndLogoutAll(t *testing.T) {
	now := time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)
	rt, err := NewRuntime(RuntimeOptions{
		BaseDir: t.TempDir(),
		Config: core.SecurityConfig{
			AuthEnabled:          true,
			SessionIdleMinutes:   10,
			SessionAbsoluteHours: 1,
			LoginMaxAttempts:     5,
			LoginBlockMinutes:    15,
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}

	token, _ := rt.EnsureSetupToken()
	session, err := rt.Setup(token, "correct horse battery")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	now = now.Add(9 * time.Minute)
	if _, err := rt.Authenticate(session.ID); err != nil {
		t.Fatalf("Authenticate before idle expiry failed: %v", err)
	}

	now = now.Add(11 * time.Minute)
	if _, err := rt.Authenticate(session.ID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Authenticate after idle expiry error = %v, want %v", err, ErrSessionExpired)
	}

	login, err := rt.Login("client-1", "correct horse battery")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	now = login.AbsoluteExpiresAt.Add(time.Second)
	if _, err := rt.Authenticate(login.ID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Authenticate after absolute expiry error = %v, want %v", err, ErrSessionExpired)
	}

	now = time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	login, err = rt.Login("client-1", "correct horse battery")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if status := rt.Status(login.ID, "client-1"); status.SessionCount != 1 {
		t.Fatalf("SessionCount = %d, want 1", status.SessionCount)
	}
	rt.LogoutAll()
	if status := rt.Status(login.ID, "client-1"); status.SessionCount != 0 || status.Authenticated {
		t.Fatalf("unexpected status after LogoutAll: %#v", status)
	}
}

func TestLoginRateLimitingAndRecovery(t *testing.T) {
	now := time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)
	rt, err := NewRuntime(RuntimeOptions{
		BaseDir: t.TempDir(),
		Config: core.SecurityConfig{
			AuthEnabled:          true,
			SessionIdleMinutes:   480,
			SessionAbsoluteHours: 168,
			LoginMaxAttempts:     3,
			LoginBlockMinutes:    15,
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}

	token, _ := rt.EnsureSetupToken()
	if _, err := rt.Setup(token, "correct horse battery"); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	rt.LogoutAll()

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := rt.Login("client-2", "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d error = %v, want %v", attempt, err, ErrInvalidCredentials)
		}
	}
	if _, err := rt.Login("client-2", "wrong password"); !errors.Is(err, ErrLoginRateLimited) {
		t.Fatalf("blocked attempt error = %v, want %v", err, ErrLoginRateLimited)
	}

	status := rt.Status("", "client-2")
	if status.LoginStatus.BlockedUntil.IsZero() {
		t.Fatalf("expected blocked_until after too many failures")
	}

	now = now.Add(16 * time.Minute)
	login, err := rt.Login("client-2", "correct horse battery")
	if err != nil {
		t.Fatalf("Login after block expiry failed: %v", err)
	}
	if strings.TrimSpace(login.ID) == "" {
		t.Fatalf("expected session id after successful login")
	}
}
