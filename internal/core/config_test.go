package core

import "testing"

func TestLoadAndSaveConfig_RoundTripsGeneralTimezone(t *testing.T) {
	configDir := t.TempDir()
	want := AppConfig{
		General: GeneralConfig{
			Timezone: "Asia/Taipei",
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
}
