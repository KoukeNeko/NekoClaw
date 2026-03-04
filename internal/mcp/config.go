package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TrustLevel controls the approval policy for a server's tools.
type TrustLevel string

const (
	TrustTrusted   TrustLevel = "trusted"
	TrustUntrusted TrustLevel = "untrusted"
)

// TransportType identifies the MCP transport protocol.
type TransportType string

const (
	TransportStdio          TransportType = "stdio"
	TransportSSE            TransportType = "sse"
	TransportStreamableHTTP TransportType = "streamable-http"
)

// ServerConfig defines one MCP server loaded from a JSON file.
type ServerConfig struct {
	Name      string            `json:"name"`
	Transport TransportType     `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Trust     TrustLevel        `json:"trust"`
	Builtin   bool              `json:"-"` // set programmatically for builtin servers
}

// validNamePattern allows alphanumeric, hyphens, and single underscores.
// Double underscores are reserved for namespace separation.
var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// LoadConfigs reads all *.json files from configDir and returns parsed configs.
// Invalid files are skipped with errors logged to the returned slice.
func LoadConfigs(configDir string) ([]ServerConfig, []error) {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("read mcp config dir: %w", err)}
	}

	var configs []ServerConfig
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(configDir, entry.Name())
		cfg, err := loadConfigFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}
		if err := ValidateConfig(cfg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}
		configs = append(configs, cfg)
	}
	return configs, errs
}

func loadConfigFile(path string) (ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ServerConfig{}, err
	}
	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ServerConfig{}, fmt.Errorf("parse json: %w", err)
	}
	// Default trust level to untrusted if omitted.
	if cfg.Trust == "" {
		cfg.Trust = TrustUntrusted
	}
	return cfg, nil
}

// ValidateConfig checks a ServerConfig for required fields and valid values.
func ValidateConfig(cfg ServerConfig) error {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if !validNamePattern.MatchString(name) {
		return fmt.Errorf("name %q contains invalid characters (use alphanumeric, hyphens, single underscores)", name)
	}
	if strings.Contains(name, "__") {
		return fmt.Errorf("name %q must not contain double underscores (reserved for namespacing)", name)
	}

	switch cfg.Transport {
	case TransportStdio:
		if strings.TrimSpace(cfg.Command) == "" {
			return fmt.Errorf("command is required for stdio transport")
		}
	case TransportSSE, TransportStreamableHTTP:
		if strings.TrimSpace(cfg.URL) == "" {
			return fmt.Errorf("url is required for %s transport", cfg.Transport)
		}
	default:
		return fmt.Errorf("unsupported transport %q (use stdio, sse, or streamable-http)", cfg.Transport)
	}

	switch cfg.Trust {
	case TrustTrusted, TrustUntrusted:
		// valid
	default:
		return fmt.Errorf("unsupported trust level %q (use trusted or untrusted)", cfg.Trust)
	}

	return nil
}
