package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// ConfigDocument exposes an MCP config file for editing and diagnostics.
type ConfigDocument struct {
	FileName        string        `json:"file_name"`
	RawJSON         string        `json:"raw_json"`
	Config          *ServerConfig `json:"config,omitempty"`
	ParseError      string        `json:"parse_error,omitempty"`
	ValidationError string        `json:"validation_error,omitempty"`
}

var (
	ErrInvalidConfigDocument = errors.New("invalid mcp config document")
	ErrConfigNotFound        = errors.New("mcp config not found")
)

// validNamePattern allows alphanumeric, hyphens, and single underscores.
// Double underscores are reserved for namespace separation.
var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// LoadConfigs reads all *.json files from configDir and returns parsed configs.
// Invalid files are skipped with errors logged to the returned slice.
func LoadConfigs(configDir string) ([]ServerConfig, []error) {
	docs, err := LoadConfigDocuments(configDir)
	if err != nil {
		return nil, []error{err}
	}

	var configs []ServerConfig
	var errs []error
	for _, doc := range docs {
		if doc.ParseError != "" {
			errs = append(errs, fmt.Errorf("%s: %s", doc.FileName, doc.ParseError))
			continue
		}
		if doc.ValidationError != "" {
			errs = append(errs, fmt.Errorf("%s: %s", doc.FileName, doc.ValidationError))
			continue
		}
		if doc.Config != nil {
			configs = append(configs, *doc.Config)
		}
	}
	return configs, errs
}

// LoadConfigDocuments reads all *.json files from configDir, preserving invalid
// content for editing while attaching parse/validation diagnostics.
func LoadConfigDocuments(configDir string) ([]ConfigDocument, error) {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read mcp config dir: %w", err)
	}

	var docs []ConfigDocument
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(configDir, entry.Name())
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			docs = append(docs, ConfigDocument{
				FileName:   entry.Name(),
				ParseError: readErr.Error(),
			})
			continue
		}

		doc := ConfigDocument{
			FileName: entry.Name(),
			RawJSON:  string(raw),
		}
		cfg, parseErr := decodeConfigJSON(raw)
		if parseErr != nil {
			doc.ParseError = parseErr.Error()
			docs = append(docs, doc)
			continue
		}

		doc.Config = &cfg
		if validateErr := ValidateConfig(cfg); validateErr != nil {
			doc.ValidationError = validateErr.Error()
		}
		docs = append(docs, doc)
	}

	slices.SortFunc(docs, func(left, right ConfigDocument) int {
		return strings.Compare(left.FileName, right.FileName)
	})
	annotateDocumentConflicts(docs)
	return docs, nil
}

func loadConfigFile(path string) (ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ServerConfig{}, err
	}
	return decodeConfigJSON(data)
}

func decodeConfigJSON(data []byte) (ServerConfig, error) {
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

// SaveConfigDocument validates and writes a custom MCP config. When fileName is
// empty, a new file name is generated from cfg.Name.
func SaveConfigDocument(configDir, fileName string, cfg *ServerConfig, rawJSON string) (string, error) {
	trimmedFileName := strings.TrimSpace(fileName)
	hasConfig := cfg != nil
	hasRawJSON := strings.TrimSpace(rawJSON) != ""

	switch {
	case hasConfig && hasRawJSON:
		return "", fmt.Errorf("%w: provide either config or raw_json", ErrInvalidConfigDocument)
	case !hasConfig && !hasRawJSON:
		return "", fmt.Errorf("%w: config or raw_json is required", ErrInvalidConfigDocument)
	}

	var (
		resolvedCfg ServerConfig
		data        []byte
		err         error
	)

	if hasConfig {
		resolvedCfg = *cfg
		if err := ValidateConfig(resolvedCfg); err != nil {
			return "", fmt.Errorf("%w: %s", ErrInvalidConfigDocument, err)
		}
		data, err = marshalConfigJSON(resolvedCfg)
		if err != nil {
			return "", err
		}
	} else {
		resolvedCfg, err = decodeConfigJSON([]byte(rawJSON))
		if err != nil {
			return "", fmt.Errorf("%w: %s", ErrInvalidConfigDocument, err)
		}
		if err := ValidateConfig(resolvedCfg); err != nil {
			return "", fmt.Errorf("%w: %s", ErrInvalidConfigDocument, err)
		}
		data = []byte(rawJSON)
	}

	if isBuiltinName(resolvedCfg.Name) {
		return "", fmt.Errorf("%w: duplicate builtin server name %q", ErrInvalidConfigDocument, resolvedCfg.Name)
	}

	docs, err := LoadConfigDocuments(configDir)
	if err != nil {
		return "", err
	}

	if trimmedFileName != "" {
		trimmedFileName, err = sanitizeConfigFileName(trimmedFileName)
		if err != nil {
			return "", fmt.Errorf("%w: %s", ErrInvalidConfigDocument, err)
		}
		if _, statErr := os.Stat(filepath.Join(configDir, trimmedFileName)); statErr != nil {
			if os.IsNotExist(statErr) {
				return "", ErrConfigNotFound
			}
			return "", statErr
		}
	} else {
		trimmedFileName = generateUniqueConfigFileName(resolvedCfg.Name, docs)
	}

	for _, doc := range docs {
		if doc.FileName == trimmedFileName || doc.Config == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(doc.Config.Name), strings.TrimSpace(resolvedCfg.Name)) {
			return "", fmt.Errorf("%w: duplicate server name %q", ErrInvalidConfigDocument, resolvedCfg.Name)
		}
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("create mcp config dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, trimmedFileName), normalizeConfigJSON(data), 0o644); err != nil {
		return "", fmt.Errorf("write mcp config: %w", err)
	}
	return trimmedFileName, nil
}

// DeleteConfigDocument removes a custom MCP config file.
func DeleteConfigDocument(configDir, fileName string) error {
	safeName, err := sanitizeConfigFileName(fileName)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidConfigDocument, err)
	}
	path := filepath.Join(configDir, safeName)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrConfigNotFound
		}
		return fmt.Errorf("delete mcp config: %w", err)
	}
	return nil
}

func annotateDocumentConflicts(docs []ConfigDocument) {
	nameCounts := map[string]int{}
	for _, doc := range docs {
		if doc.Config == nil || doc.ParseError != "" || doc.ValidationError != "" {
			continue
		}
		nameCounts[strings.TrimSpace(doc.Config.Name)]++
	}
	for i := range docs {
		doc := &docs[i]
		if doc.Config == nil || doc.ParseError != "" || doc.ValidationError != "" {
			continue
		}
		name := strings.TrimSpace(doc.Config.Name)
		switch {
		case isBuiltinName(name):
			doc.ValidationError = fmt.Sprintf("duplicate builtin server name %q", name)
		case nameCounts[name] > 1:
			doc.ValidationError = fmt.Sprintf("duplicate server name %q", name)
		}
	}
}

func marshalConfigJSON(cfg ServerConfig) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal mcp config: %w", err)
	}
	return append(data, '\n'), nil
}

func normalizeConfigJSON(data []byte) []byte {
	trimmed := bytes.TrimRight(data, "\n")
	return append(trimmed, '\n')
}

func sanitizeConfigFileName(fileName string) (string, error) {
	name := strings.TrimSpace(fileName)
	if name == "" {
		return "", fmt.Errorf("file_name is required")
	}
	if filepath.Base(name) != name || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("file_name must not contain path separators")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		return "", fmt.Errorf("file_name must end with .json")
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("invalid file_name")
	}
	return name, nil
}

func generateUniqueConfigFileName(serverName string, docs []ConfigDocument) string {
	base := sanitizeConfigFileBase(serverName)
	if base == "" {
		base = "server"
	}
	used := map[string]struct{}{}
	for _, doc := range docs {
		used[doc.FileName] = struct{}{}
	}
	fileName := base + ".json"
	if _, exists := used[fileName]; !exists {
		return fileName
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d.json", base, i)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func sanitizeConfigFileBase(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-', r == '_':
			if prevDash {
				continue
			}
			b.WriteByte('-')
			prevDash = true
		default:
			if prevDash {
				continue
			}
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func isBuiltinName(name string) bool {
	trimmed := strings.TrimSpace(name)
	for _, def := range BuiltinDefs() {
		if def.Name == trimmed {
			return true
		}
	}
	return false
}
