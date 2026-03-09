package core

import "testing"

func TestLoadAndSaveConfig_RoundTripsGeneralTimezone(t *testing.T) {
	configDir := t.TempDir()
	want := AppConfig{
		General: GeneralConfig{
			Timezone: "Asia/Taipei",
		},
		Security: SecurityConfig{
			AuthEnabled:          false,
			SessionIdleMinutes:   120,
			SessionAbsoluteHours: 48,
			LoginMaxAttempts:     3,
			LoginBlockMinutes:    10,
		},
	}

	if err := SaveConfig(configDir, want); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	got, err := LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if got.General.Timezone != want.General.Timezone {
		t.Fatalf("general.timezone = %q, want %q", got.General.Timezone, want.General.Timezone)
	}
	if got.Security != want.Security {
		t.Fatalf("security = %#v, want %#v", got.Security, want.Security)
	}
}

func TestLoadConfig_DefaultsSecurityConfigWhenMissing(t *testing.T) {
	configDir := t.TempDir()

	got, err := LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if got.Security != DefaultSecurityConfig() {
		t.Fatalf("security defaults = %#v, want %#v", got.Security, DefaultSecurityConfig())
	}
}
