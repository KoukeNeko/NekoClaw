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

func TestCompactionConfigEndpoint_PersistsConfig(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	handler := NewServer(svc).Handler()

	getResp := performJSONRequest(t, handler, http.MethodGet, "/v1/compaction/config", "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("unexpected GET status: %d body=%s", getResp.Code, getResp.Body.String())
	}

	var initial core.CompactionConfig
	if err := json.Unmarshal(getResp.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial compaction config: %v", err)
	}
	if initial != core.DefaultCompactionConfig() {
		t.Fatalf("unexpected initial compaction config: %#v", initial)
	}

	putResp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/compaction/config",
		`{"enabled":true,"background_enabled":false,"llm_summary_enabled":false,"trigger_threshold_ratio":0.9,"keep_recent_tokens":4096}`,
	)
	if putResp.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %d body=%s", putResp.Code, putResp.Body.String())
	}

	var saved core.CompactionConfig
	if err := json.Unmarshal(putResp.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved compaction config: %v", err)
	}
	if saved.TriggerThresholdRatio != 0.9 || saved.KeepRecentTokens != 4096 {
		t.Fatalf("saved compaction config = %#v", saved)
	}
	if saved.BackgroundEnabled || saved.LLMSummaryEnabled {
		t.Fatalf("expected background/llm summary to be disabled, got %#v", saved)
	}

	config, err := core.LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if config.Compaction != saved {
		t.Fatalf("persisted compaction config = %#v, want %#v", config.Compaction, saved)
	}
}

func TestCompactionConfigEndpoint_RejectsInvalidValues(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.SetConfigDir(t.TempDir())

	handler := NewServer(svc).Handler()
	resp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/compaction/config",
		`{"trigger_threshold_ratio":0.2,"keep_recent_tokens":512}`,
	)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "trigger_threshold_ratio") {
		t.Fatalf("expected validation error, got %s", resp.Body.String())
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
	if saved.Provider != "opencode" {
		t.Fatalf("saved provider = %q, want %q", saved.Provider, "opencode")
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
	if config.DefaultProvider != "opencode" {
		t.Fatalf("config default_provider = %q, want %q", config.DefaultProvider, "opencode")
	}
	if config.DefaultModel != "gemini-2.5-pro" {
		t.Fatalf("config default_model = %q, want %q", config.DefaultModel, "gemini-2.5-pro")
	}
	if config.DefaultThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("config default_thinking_mode = %q, want %q", config.DefaultThinkingMode, core.ThinkingModeHigh)
	}
	if config.ModelRoles.Action.Provider != "opencode" {
		t.Fatalf("config model_roles.action.provider = %q, want %q", config.ModelRoles.Action.Provider, "opencode")
	}
	if config.ModelRoles.Action.Model != "gemini-2.5-pro" {
		t.Fatalf("config model_roles.action.model = %q, want %q", config.ModelRoles.Action.Model, "gemini-2.5-pro")
	}
	if config.ModelRoles.Action.ThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("config model_roles.action.thinking_mode = %q, want %q", config.ModelRoles.Action.ThinkingMode, core.ThinkingModeHigh)
	}
}

func TestDefaultProviderEndpoint_PreservesGeminiCLIModel(t *testing.T) {
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
	if saved.Model != "gemini-3-pro-preview" {
		t.Fatalf("saved model = %q, want %q", saved.Model, "gemini-3-pro-preview")
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
	if saved.Fallbacks[1].Provider != "opencode" {
		t.Fatalf("fallback[1].provider = %q, want %q", saved.Fallbacks[1].Provider, "opencode")
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
	if config.Fallbacks[1].Provider != "opencode" {
		t.Fatalf("config fallback[1].provider = %q, want %q", config.Fallbacks[1].Provider, "opencode")
	}
	if config.Fallbacks[1].ThinkingMode != core.ThinkingModeHigh {
		t.Fatalf("config fallback[1].thinking_mode = %q, want %q", config.Fallbacks[1].ThinkingMode, core.ThinkingModeHigh)
	}
}

func TestModelRolesConfigEndpoint_PersistsSelection(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	handler := NewServer(svc).Handler()
	resp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/model-roles/config",
		`{
			"action":{"provider":"google-ai-studio","model":"gemini-2.5-pro","thinking_mode":"high"},
			"planner":{"provider":"google-gemini-cli","model":"gemini-3.1-pro-preview","thinking_mode":"medium"},
			"compaction":{"thinking_mode":"low"},
			"title":{"provider":"openai","model":"gpt-5-mini"}
		}`,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %d body=%s", resp.Code, resp.Body.String())
	}

	var saved core.ModelRolesConfig
	if err := json.Unmarshal(resp.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved model roles: %v", err)
	}
	if saved.Action.Provider != "google-ai-studio" {
		t.Fatalf("action provider = %q, want google-ai-studio", saved.Action.Provider)
	}
	if saved.Planner.Provider != "opencode" {
		t.Fatalf("planner provider = %q, want opencode", saved.Planner.Provider)
	}
	if saved.Compaction.ThinkingMode != core.ThinkingModeLow {
		t.Fatalf("compaction thinking = %q, want %q", saved.Compaction.ThinkingMode, core.ThinkingModeLow)
	}

	config, err := core.LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if config.DefaultProvider != "google-ai-studio" {
		t.Fatalf("config default_provider = %q, want google-ai-studio", config.DefaultProvider)
	}
	if config.ModelRoles.Title.Provider != "opencode" {
		t.Fatalf("config model_roles.title.provider = %q, want opencode", config.ModelRoles.Title.Provider)
	}
	if config.ModelRoles.Title.Model != "gpt-5-mini" {
		t.Fatalf("config model_roles.title.model = %q, want gpt-5-mini", config.ModelRoles.Title.Model)
	}

	getResp := performJSONRequest(t, handler, http.MethodGet, "/v1/model-roles/config", "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("unexpected GET status: %d body=%s", getResp.Code, getResp.Body.String())
	}

	var fetched core.ModelRolesConfig
	if err := json.Unmarshal(getResp.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("decode fetched model roles: %v", err)
	}
	if fetched.Planner.Model != "gemini-3.1-pro-preview" {
		t.Fatalf("fetched planner model = %q, want gemini-3.1-pro-preview", fetched.Planner.Model)
	}
}

func TestFallbacksEndpoint_PreservesGeminiCLIModel(t *testing.T) {
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
	if saved.Fallbacks[0].Provider != "opencode" {
		t.Fatalf("fallback provider = %q, want %q", saved.Fallbacks[0].Provider, "opencode")
	}
	if saved.Fallbacks[0].Model != "gemini-3-pro-preview" {
		t.Fatalf("fallback model = %q, want %q", saved.Fallbacks[0].Model, "gemini-3-pro-preview")
	}
}
