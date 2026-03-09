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
	DefaultProvider     string          `json:"default_provider,omitempty"`
	DefaultModel        string          `json:"default_model,omitempty"`
	DefaultThinkingMode ThinkingMode    `json:"default_thinking_mode,omitempty"`
	Fallbacks           []FallbackEntry `json:"fallbacks,omitempty"`
	General             GeneralConfig   `json:"general,omitempty"`
	Security            SecurityConfig  `json:"security,omitempty"`
	Discord             DiscordConfig   `json:"discord,omitempty"`
	Telegram            TelegramConfig  `json:"telegram,omitempty"`
	Tools               ToolsConfig     `json:"tools,omitempty"`
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
	cfg.General = sanitizeGeneralConfig(cfg.General)
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
	cfg.General = sanitizeGeneralConfig(cfg.General)
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
	return NormalizeProviderModelSelection(provider, model)
}

// sanitizeFallbacks trims whitespace, removes entries with empty provider,
// and caps the list at maxFallbackSlots.
func sanitizeFallbacks(entries []FallbackEntry) []FallbackEntry {
	result := make([]FallbackEntry, 0, maxFallbackSlots)
	for _, entry := range entries {
		entry.Provider, entry.Model = NormalizeProviderModelSelection(
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
	if provider == "google-gemini-cli" && strings.EqualFold(model, "gemini-3-pro-preview") {
		model = "gemini-3.1-pro-preview"
	}
	return provider, model
}

func sanitizeGeneralConfig(cfg GeneralConfig) GeneralConfig {
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	return cfg
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
