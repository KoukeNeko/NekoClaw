package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultConfigDirName = ".nekoclaw"
	configFileName       = "config.json"
	maxFallbackSlots     = 5
)

// DiscordConfig holds Discord bot settings.
type DiscordConfig struct {
	BotToken        string `json:"bot_token,omitempty"`
	ReplyToOriginal *bool  `json:"reply_to_original,omitempty"` // nil = default true
	ConsoleChannel  string `json:"console_channel,omitempty"`   // channel ID for log output
}

// ShouldReplyToOriginal returns whether the bot should reply to the original message.
// Defaults to true when not explicitly configured.
func (c DiscordConfig) ShouldReplyToOriginal() bool {
	if c.ReplyToOriginal == nil {
		return true
	}
	return *c.ReplyToOriginal
}

// TelegramConfig holds Telegram bot settings.
type TelegramConfig struct {
	BotToken string `json:"bot_token,omitempty"`
}

// GeneralConfig holds global application preferences.
type GeneralConfig struct {
	Timezone string `json:"timezone,omitempty"`
}

// CompactionConfig holds deterministic/background compaction settings.
type CompactionConfig struct {
	Enabled               bool    `json:"enabled"`
	BackgroundEnabled     bool    `json:"background_enabled"`
	LLMSummaryEnabled     bool    `json:"llm_summary_enabled"`
	TriggerThresholdRatio float64 `json:"trigger_threshold_ratio"`
	KeepRecentTokens      int     `json:"keep_recent_tokens"`
}

// UnmarshalJSON applies defaults while still allowing explicit false values.
func (c *CompactionConfig) UnmarshalJSON(data []byte) error {
	type rawCompactionConfig struct {
		Enabled               *bool    `json:"enabled"`
		BackgroundEnabled     *bool    `json:"background_enabled"`
		LLMSummaryEnabled     *bool    `json:"llm_summary_enabled"`
		TriggerThresholdRatio *float64 `json:"trigger_threshold_ratio"`
		KeepRecentTokens      *int     `json:"keep_recent_tokens"`
	}
	cfg := DefaultCompactionConfig()
	if len(strings.TrimSpace(string(data))) == 0 || strings.TrimSpace(string(data)) == "null" {
		*c = cfg
		return nil
	}
	var raw rawCompactionConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Enabled != nil {
		cfg.Enabled = *raw.Enabled
	}
	if raw.BackgroundEnabled != nil {
		cfg.BackgroundEnabled = *raw.BackgroundEnabled
	}
	if raw.LLMSummaryEnabled != nil {
		cfg.LLMSummaryEnabled = *raw.LLMSummaryEnabled
	}
	if raw.TriggerThresholdRatio != nil {
		cfg.TriggerThresholdRatio = *raw.TriggerThresholdRatio
	}
	if raw.KeepRecentTokens != nil {
		cfg.KeepRecentTokens = *raw.KeepRecentTokens
	}
	*c = sanitizeCompactionConfig(cfg)
	return nil
}

// SecurityConfig holds browser admin auth settings.
type SecurityConfig struct {
	AuthEnabled          bool `json:"auth_enabled"`
	SessionIdleMinutes   int  `json:"session_idle_minutes"`
	SessionAbsoluteHours int  `json:"session_absolute_hours"`
	LoginMaxAttempts     int  `json:"login_max_attempts"`
	LoginBlockMinutes    int  `json:"login_block_minutes"`
}

// ToolsConfig holds settings for built-in AI assistant tools.
type ToolsConfig struct {
	BraveSearchAPIKey string `json:"brave_search_api_key,omitempty"`
}

// AppConfig holds user-configurable settings persisted to config.json.
type AppConfig struct {
	DefaultProvider     string           `json:"default_provider,omitempty"`
	DefaultModel        string           `json:"default_model,omitempty"`
	DefaultThinkingMode ThinkingMode     `json:"default_thinking_mode,omitempty"`
	Fallbacks           []FallbackEntry  `json:"fallbacks,omitempty"`
	ModelRoles          ModelRolesConfig `json:"model_roles,omitempty"`
	General             GeneralConfig    `json:"general,omitempty"`
	Compaction          CompactionConfig `json:"compaction,omitempty"`
	Playbooks           PlaybookConfig   `json:"playbooks,omitempty"`
	Automations         AutomationConfig `json:"automations,omitempty"`
	Permissions         PermissionConfig `json:"permissions,omitempty"`
	Security            SecurityConfig   `json:"security,omitempty"`
	Discord             DiscordConfig    `json:"discord,omitempty"`
	Telegram            TelegramConfig   `json:"telegram,omitempty"`
	Tools               ToolsConfig      `json:"tools,omitempty"`
}

// LoadConfig reads config.json from configDir.
// Returns a zero AppConfig (no error) when the file does not exist.
func LoadConfig(configDir string) (AppConfig, error) {
	configDir = resolveConfigDir(configDir)
	path := filepath.Join(configDir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AppConfig{
				Security:            DefaultSecurityConfig(),
				Compaction:          DefaultCompactionConfig(),
				DefaultThinkingMode: ThinkingModeAuto,
			}, nil
		}
		return AppConfig{}, err
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, err
	}
	cfg.DefaultProvider, cfg.DefaultModel = sanitizeDefaultSelection(
		cfg.DefaultProvider,
		cfg.DefaultModel,
	)
	cfg.DefaultThinkingMode = sanitizeThinkingMode(cfg.DefaultThinkingMode)
	cfg.Fallbacks = sanitizeFallbacks(cfg.Fallbacks)
	cfg.ModelRoles = NormalizeConfiguredModelRolesConfig(cfg.ModelRoles)
	cfg.General = sanitizeGeneralConfig(cfg.General)
	cfg.Compaction = sanitizeCompactionConfig(cfg.Compaction)
	cfg.Security = sanitizeSecurityConfig(cfg.Security)
	return cfg, nil
}

// SaveConfig writes config.json to configDir, creating the directory if needed.
func SaveConfig(configDir string, cfg AppConfig) error {
	configDir = resolveConfigDir(configDir)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	cfg.DefaultProvider, cfg.DefaultModel = sanitizeDefaultSelection(
		cfg.DefaultProvider,
		cfg.DefaultModel,
	)
	cfg.DefaultThinkingMode = sanitizeThinkingMode(cfg.DefaultThinkingMode)
	cfg.Fallbacks = sanitizeFallbacks(cfg.Fallbacks)
	cfg.ModelRoles = NormalizeConfiguredModelRolesConfig(cfg.ModelRoles)
	cfg.General = sanitizeGeneralConfig(cfg.General)
	cfg.Compaction = sanitizeCompactionConfig(cfg.Compaction)
	cfg.Security = sanitizeSecurityConfig(cfg.Security)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, configFileName), data, 0o600)
}

// resolveConfigDir returns configDir if non-empty, otherwise ~/.nekoclaw.
func resolveConfigDir(configDir string) string {
	configDir = strings.TrimSpace(configDir)
	if configDir != "" {
		return configDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultConfigDirName
	}
	return filepath.Join(home, defaultConfigDirName)
}

func sanitizeDefaultSelection(provider string, model string) (string, string) {
	return NormalizeConfiguredProviderModelSelection(provider, model)
}

// sanitizeFallbacks trims whitespace, removes entries with empty provider,
// and caps the list at maxFallbackSlots.
func sanitizeFallbacks(entries []FallbackEntry) []FallbackEntry {
	result := make([]FallbackEntry, 0, maxFallbackSlots)
	for _, entry := range entries {
		entry.Provider, entry.Model = NormalizeConfiguredProviderModelSelection(
			entry.Provider,
			entry.Model,
		)
		entry.ThinkingMode = sanitizeThinkingMode(entry.ThinkingMode)
		if entry.Provider == "" {
			continue
		}
		result = append(result, entry)
		if len(result) >= maxFallbackSlots {
			break
		}
	}
	return result
}

func sanitizeThinkingMode(mode ThinkingMode) ThinkingMode {
	return NormalizeThinkingMode(string(mode))
}

func NormalizeProviderModelSelection(provider string, model string) (string, string) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		return "", ""
	}
	if model == "" {
		model = "default"
	}
	return provider, model
}

func NormalizeConfiguredProviderModelSelection(provider string, model string) (string, string) {
	provider, model = NormalizeProviderModelSelection(provider, model)
	if provider == "" {
		return "", ""
	}
	return NormalizeConfiguredProviderID(provider), model
}

func NormalizeConfiguredProviderID(provider string) string {
	switch strings.TrimSpace(provider) {
	case "anthropic", "google-gemini-cli", "openai", "openai-codex":
		return "opencode"
	default:
		return strings.TrimSpace(provider)
	}
}

func sanitizeGeneralConfig(cfg GeneralConfig) GeneralConfig {
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	return cfg
}

func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		Enabled:               true,
		BackgroundEnabled:     true,
		LLMSummaryEnabled:     true,
		TriggerThresholdRatio: 0.80,
		KeepRecentTokens:      20_000,
	}
}

func (defaults CompactionConfig) ApplyDefaults(cfg CompactionConfig) CompactionConfig {
	if cfg == (CompactionConfig{}) {
		return defaults
	}
	if cfg.TriggerThresholdRatio <= 0 {
		cfg.TriggerThresholdRatio = defaults.TriggerThresholdRatio
	}
	if cfg.KeepRecentTokens <= 0 {
		cfg.KeepRecentTokens = defaults.KeepRecentTokens
	}
	return cfg
}

func sanitizeCompactionConfig(cfg CompactionConfig) CompactionConfig {
	return DefaultCompactionConfig().ApplyDefaults(cfg)
}

func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		AuthEnabled:          true,
		SessionIdleMinutes:   8 * 60,
		SessionAbsoluteHours: 7 * 24,
		LoginMaxAttempts:     5,
		LoginBlockMinutes:    15,
	}
}

func sanitizeSecurityConfig(cfg SecurityConfig) SecurityConfig {
	defaults := DefaultSecurityConfig()
	if cfg == (SecurityConfig{}) {
		return defaults
	}
	if cfg.SessionIdleMinutes <= 0 {
		cfg.SessionIdleMinutes = defaults.SessionIdleMinutes
	}
	if cfg.SessionAbsoluteHours <= 0 {
		cfg.SessionAbsoluteHours = defaults.SessionAbsoluteHours
	}
	if cfg.LoginMaxAttempts <= 0 {
		cfg.LoginMaxAttempts = defaults.LoginMaxAttempts
	}
	if cfg.LoginBlockMinutes <= 0 {
		cfg.LoginBlockMinutes = defaults.LoginBlockMinutes
	}
	return cfg
}
