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

func TestLoadAndSaveConfig_RoundTripsDefaultSelection(t *testing.T) {
	configDir := t.TempDir()
	want := AppConfig{
		DefaultProvider:     "google-gemini-cli",
		DefaultModel:        "gemini-2.5-pro",
		DefaultThinkingMode: ThinkingModeHigh,
		Fallbacks: []FallbackEntry{
			{
				Provider:     "google-ai-studio",
				Model:        "gemini-2.5-flash",
				ThinkingMode: ThinkingModeMedium,
			},
		},
	}

	if err := SaveConfig(configDir, want); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	got, err := LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if got.DefaultProvider != want.DefaultProvider {
		t.Fatalf("default_provider = %q, want %q", got.DefaultProvider, want.DefaultProvider)
	}
	if got.DefaultModel != want.DefaultModel {
		t.Fatalf("default_model = %q, want %q", got.DefaultModel, want.DefaultModel)
	}
	if got.DefaultThinkingMode != want.DefaultThinkingMode {
		t.Fatalf("default_thinking_mode = %q, want %q", got.DefaultThinkingMode, want.DefaultThinkingMode)
	}
	if len(got.Fallbacks) != 1 {
		t.Fatalf("fallbacks len = %d, want 1", len(got.Fallbacks))
	}
	if got.Fallbacks[0].ThinkingMode != ThinkingModeMedium {
		t.Fatalf("fallback thinking_mode = %q, want %q", got.Fallbacks[0].ThinkingMode, ThinkingModeMedium)
	}
}

func TestLoadConfig_SanitizesInvalidThinkingModeToAuto(t *testing.T) {
	configDir := t.TempDir()
	if err := SaveConfig(configDir, AppConfig{
		DefaultProvider:     "google-gemini-cli",
		DefaultModel:        "gemini-2.5-pro",
		DefaultThinkingMode: ThinkingMode("LOUD"),
		Fallbacks: []FallbackEntry{
			{
				Provider:     "google-ai-studio",
				Model:        "gemini-2.5-flash",
				ThinkingMode: ThinkingMode("turbo"),
			},
		},
	}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	got, err := LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if got.DefaultThinkingMode != ThinkingModeAuto {
		t.Fatalf("default_thinking_mode = %q, want %q", got.DefaultThinkingMode, ThinkingModeAuto)
	}
	if len(got.Fallbacks) != 1 {
		t.Fatalf("fallbacks len = %d, want 1", len(got.Fallbacks))
	}
	if got.Fallbacks[0].ThinkingMode != ThinkingModeAuto {
		t.Fatalf("fallback thinking_mode = %q, want %q", got.Fallbacks[0].ThinkingMode, ThinkingModeAuto)
	}
}
