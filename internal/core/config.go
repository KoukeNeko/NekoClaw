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

// ToolsConfig holds settings for built-in AI assistant tools.
type ToolsConfig struct {
	BraveSearchAPIKey string `json:"brave_search_api_key,omitempty"`
}

// AppConfig holds user-configurable settings persisted to config.json.
type AppConfig struct {
	Fallbacks []FallbackEntry `json:"fallbacks,omitempty"`
	Discord   DiscordConfig   `json:"discord,omitempty"`
	Telegram  TelegramConfig  `json:"telegram,omitempty"`
	Tools     ToolsConfig     `json:"tools,omitempty"`
}

// LoadConfig reads config.json from configDir.
// Returns a zero AppConfig (no error) when the file does not exist.
func LoadConfig(configDir string) (AppConfig, error) {
	configDir = resolveConfigDir(configDir)
	path := filepath.Join(configDir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AppConfig{}, nil
		}
		return AppConfig{}, err
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, err
	}
	cfg.Fallbacks = sanitizeFallbacks(cfg.Fallbacks)
	return cfg, nil
}

// SaveConfig writes config.json to configDir, creating the directory if needed.
func SaveConfig(configDir string, cfg AppConfig) error {
	configDir = resolveConfigDir(configDir)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	cfg.Fallbacks = sanitizeFallbacks(cfg.Fallbacks)
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

// sanitizeFallbacks trims whitespace, removes entries with empty provider,
// and caps the list at maxFallbackSlots.
func sanitizeFallbacks(entries []FallbackEntry) []FallbackEntry {
	result := make([]FallbackEntry, 0, maxFallbackSlots)
	for _, entry := range entries {
		entry.Provider = strings.TrimSpace(entry.Provider)
		entry.Model = strings.TrimSpace(entry.Model)
		if entry.Provider == "" {
			continue
		}
		if entry.Model == "" {
			entry.Model = "default"
		}
		result = append(result, entry)
		if len(result) >= maxFallbackSlots {
			break
		}
	}
	return result
}
