package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
)

func TestGeneralConfigEndpoint_PersistsTimezone(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	handler := NewServer(svc).Handler()

	getResp := performJSONRequest(t, handler, http.MethodGet, "/v1/general/config", "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("unexpected GET status: %d body=%s", getResp.Code, getResp.Body.String())
	}

	var initial core.GeneralConfig
	if err := json.Unmarshal(getResp.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial general config: %v", err)
	}
	if initial.Timezone != "" {
		t.Fatalf("expected empty initial timezone, got %q", initial.Timezone)
	}

	putResp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/general/config",
		`{"timezone":"Asia/Taipei"}`,
	)
	if putResp.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %d body=%s", putResp.Code, putResp.Body.String())
	}

	var saved core.GeneralConfig
	if err := json.Unmarshal(putResp.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved general config: %v", err)
	}
	if saved.Timezone != "Asia/Taipei" {
		t.Fatalf("saved timezone = %q, want %q", saved.Timezone, "Asia/Taipei")
	}

	config, err := core.LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if config.General.Timezone != "Asia/Taipei" {
		t.Fatalf("config general.timezone = %q, want %q", config.General.Timezone, "Asia/Taipei")
	}
}

func TestGeneralConfigEndpoint_RejectsInvalidTimezone(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.SetConfigDir(t.TempDir())

	handler := NewServer(svc).Handler()
	resp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/general/config",
		`{"timezone":"Invalid/Timezone"}`,
	)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid timezone") {
		t.Fatalf("expected invalid timezone error, got %s", resp.Body.String())
	}
}

func TestDefaultProviderEndpoint_PersistsSelection(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	handler := NewServer(svc).Handler()
	resp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/default-provider",
		`{"provider":"google-gemini-cli","model":"gemini-2.5-pro","thinking_mode":"high"}`,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %d body=%s", resp.Code, resp.Body.String())
	}

	var saved struct {
		Provider     string            `json:"provider"`
		Model        string            `json:"model"`
		ThinkingMode core.ThinkingMode `json:"thinking_mode"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved default provider: %v", err)
	}
	if saved.Provider != "google-gemini-cli" {
		t.Fatalf("saved provider = %q, want %q", saved.Provider, "google-gemini-cli")
	}
	if saved.Model != "gemini-2.5-pro" {
		t.Fatalf("saved model = %q, want %q", saved.Model, "gemini-2.5-pro")
	}
	if saved.ThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("saved thinking_mode = %q, want %q", saved.ThinkingMode, core.ThinkingModeHigh)
	}

	config, err := core.LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if config.DefaultProvider != "google-gemini-cli" {
		t.Fatalf("config default_provider = %q, want %q", config.DefaultProvider, "google-gemini-cli")
	}
	if config.DefaultModel != "gemini-2.5-pro" {
		t.Fatalf("config default_model = %q, want %q", config.DefaultModel, "gemini-2.5-pro")
	}
	if config.DefaultThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("config default_thinking_mode = %q, want %q", config.DefaultThinkingMode, core.ThinkingModeHigh)
	}
}

func TestDefaultProviderEndpoint_UpgradesLegacyGeminiCLIModel(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	handler := NewServer(svc).Handler()
	resp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/default-provider",
		`{"provider":"google-gemini-cli","model":"gemini-3-pro-preview"}`,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %d body=%s", resp.Code, resp.Body.String())
	}

	var saved struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved default provider: %v", err)
	}
	if saved.Model != "gemini-3.1-pro-preview" {
		t.Fatalf("saved model = %q, want %q", saved.Model, "gemini-3.1-pro-preview")
	}
}

func TestFallbacksEndpoint_PersistsThinkingMode(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	handler := NewServer(svc).Handler()
	resp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/fallbacks",
		`{"fallbacks":[{"provider":"google-ai-studio","model":"gemini-2.5-flash","thinking_mode":"medium"},{"provider":"openai","model":"gpt-5","thinking_mode":"high"}]}`,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %d body=%s", resp.Code, resp.Body.String())
	}

	var saved struct {
		Fallbacks []core.FallbackEntry `json:"fallbacks"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved fallbacks: %v", err)
	}
	if len(saved.Fallbacks) != 2 {
		t.Fatalf("fallbacks len = %d, want 2", len(saved.Fallbacks))
	}
	if saved.Fallbacks[0].ThinkingMode != core.ThinkingModeMedium {
		t.Fatalf("fallback[0].thinking_mode = %q, want %q", saved.Fallbacks[0].ThinkingMode, core.ThinkingModeMedium)
	}
	if saved.Fallbacks[1].ThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("fallback[1].thinking_mode = %q, want %q", saved.Fallbacks[1].ThinkingMode, core.ThinkingModeHigh)
	}

	config, err := core.LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(config.Fallbacks) != 2 {
		t.Fatalf("config fallbacks len = %d, want 2", len(config.Fallbacks))
	}
	if config.Fallbacks[0].ThinkingMode != core.ThinkingModeMedium {
		t.Fatalf("config fallback[0].thinking_mode = %q, want %q", config.Fallbacks[0].ThinkingMode, core.ThinkingModeMedium)
	}
	if config.Fallbacks[1].ThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("config fallback[1].thinking_mode = %q, want %q", config.Fallbacks[1].ThinkingMode, core.ThinkingModeHigh)
	}
}

func TestFallbacksEndpoint_UpgradesLegacyGeminiCLIModel(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	handler := NewServer(svc).Handler()
	resp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/fallbacks",
		`{"fallbacks":[{"provider":"google-gemini-cli","model":"gemini-3-pro-preview"}]}`,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %d body=%s", resp.Code, resp.Body.String())
	}

	var saved struct {
		Fallbacks []core.FallbackEntry `json:"fallbacks"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved fallbacks: %v", err)
	}
	if len(saved.Fallbacks) != 1 {
		t.Fatalf("fallbacks len = %d, want 1", len(saved.Fallbacks))
	}
	if saved.Fallbacks[0].Model != "gemini-3.1-pro-preview" {
		t.Fatalf("fallback model = %q, want %q", saved.Fallbacks[0].Model, "gemini-3.1-pro-preview")
	}
}
