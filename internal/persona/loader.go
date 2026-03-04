package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	configFileName  = "config.yaml"
	anchorsFileName = "anchors.yaml"
	loreFileName    = "lore.md"
)

// LoadPersona reads a single persona from the given directory.
// config.yaml is required; anchors.yaml and lore.md are optional.
func LoadPersona(dir string) (*Persona, error) {
	configPath := filepath.Join(dir, configFileName)
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read persona config: %w", err)
	}

	var cfg PersonaConfig
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		return nil, fmt.Errorf("parse persona config %s: %w", configPath, err)
	}

	if strings.TrimSpace(cfg.Meta.ID) == "" {
		return nil, fmt.Errorf("persona config %s: meta.id is required", configPath)
	}

	p := &Persona{
		Config:  cfg,
		DirName: filepath.Base(dir),
	}

	// Anchors (optional)
	anchorsPath := filepath.Join(dir, anchorsFileName)
	if data, err := os.ReadFile(anchorsPath); err == nil {
		var af AnchorsFile
		if err := yaml.Unmarshal(data, &af); err != nil {
			return nil, fmt.Errorf("parse anchors %s: %w", anchorsPath, err)
		}
		p.Anchors = af.Anchors
	}

	// Lore (optional)
	lorePath := filepath.Join(dir, loreFileName)
	if data, err := os.ReadFile(lorePath); err == nil {
		p.Lore = strings.TrimSpace(string(data))
	}

	return p, nil
}

// ListPersonas scans baseDir for persona subdirectories.
// Each subdirectory must contain a valid config.yaml to be included.
// Directories that fail to load are silently skipped.
func ListPersonas(baseDir string) ([]PersonaInfo, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read personas directory: %w", err)
	}

	var infos []PersonaInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(baseDir, entry.Name())
		p, err := LoadPersona(dir)
		if err != nil {
			continue // skip invalid personas
		}
		infos = append(infos, p.Info())
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}
