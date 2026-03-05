package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BuiltinServerDef declares a pre-configured MCP server shipped with NekoClaw.
type BuiltinServerDef struct {
	Name        string
	Description string
	Config      ServerConfig
}

// builtinRegistry holds all builtin server definitions.
// Add new entries here to ship additional builtin servers.
var builtinRegistry = []BuiltinServerDef{
	{
		Name:        "playwright",
		Description: "Playwright 瀏覽器自動化（需要 Node.js）",
		Config: ServerConfig{
			Name:      "playwright",
			Transport: TransportStdio,
			Command:   "npx",
			Args: []string{
				"-y", "@playwright/mcp@latest",
				"--headless",
				"--vision",
			},
			Env: map[string]string{
				// Skip automatic Chromium download that triggers sudo password prompts.
				// Users should install browsers separately: npx playwright install
				"PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD": "1",
			},
			Trust:   TrustTrusted,
			Builtin: true,
		},
	},
}

// BuiltinDefs returns a copy of all builtin server definitions.
func BuiltinDefs() []BuiltinServerDef {
	out := make([]BuiltinServerDef, len(builtinRegistry))
	copy(out, builtinRegistry)
	return out
}

// BuiltinServerInfo carries builtin server status for display.
type BuiltinServerInfo struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Enabled     bool             `json:"enabled"`
	Status      ConnectionStatus `json:"status"`
	Error       string           `json:"error,omitempty"`
	ToolCount   int              `json:"tool_count"`
}

// builtinStatePath returns the path to the builtin state file.
// The file lives alongside the config directory (e.g. ~/.nekoclaw/mcp-builtin.json).
func builtinStatePath(configDir string) string {
	return filepath.Join(filepath.Dir(configDir), "mcp-builtin.json")
}

// loadBuiltinState reads the enabled/disabled map from disk.
// Returns an empty map if the file does not exist.
func loadBuiltinState(configDir string) (map[string]bool, error) {
	path := builtinStatePath(configDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("read builtin state: %w", err)
	}
	var state map[string]bool
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse builtin state: %w", err)
	}
	return state, nil
}

// saveBuiltinState writes the enabled/disabled map to disk.
func saveBuiltinState(configDir string, state map[string]bool) error {
	path := builtinStatePath(configDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal builtin state: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// isBuiltinEnabled checks if a builtin server is enabled.
// Servers default to enabled when no explicit state has been saved.
func isBuiltinEnabled(state map[string]bool, name string) bool {
	enabled, exists := state[name]
	if !exists {
		return true
	}
	return enabled
}
