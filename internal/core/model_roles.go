package core

import "strings"

type ModelRole string

const (
	ModelRoleAction     ModelRole = "action"
	ModelRolePlanner    ModelRole = "planner"
	ModelRoleCompaction ModelRole = "compaction"
	ModelRoleTitle      ModelRole = "title"
)

type ModelRoleConfig struct {
	Provider     string       `json:"provider,omitempty"`
	Model        string       `json:"model,omitempty"`
	ThinkingMode ThinkingMode `json:"thinking_mode,omitempty"`
}

type ModelRolesConfig struct {
	Action     ModelRoleConfig `json:"action,omitempty"`
	Planner    ModelRoleConfig `json:"planner,omitempty"`
	Compaction ModelRoleConfig `json:"compaction,omitempty"`
	Title      ModelRoleConfig `json:"title,omitempty"`
}

func (cfg ModelRolesConfig) Get(role ModelRole) ModelRoleConfig {
	switch role {
	case ModelRolePlanner:
		return cfg.Planner
	case ModelRoleCompaction:
		return cfg.Compaction
	case ModelRoleTitle:
		return cfg.Title
	default:
		return cfg.Action
	}
}

func (cfg *ModelRolesConfig) Set(role ModelRole, value ModelRoleConfig) {
	switch role {
	case ModelRolePlanner:
		cfg.Planner = value
	case ModelRoleCompaction:
		cfg.Compaction = value
	case ModelRoleTitle:
		cfg.Title = value
	default:
		cfg.Action = value
	}
}

func NormalizeModelRoleConfig(cfg ModelRoleConfig) ModelRoleConfig {
	cfg.Provider = strings.TrimSpace(cfg.Provider)
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Provider == "" {
		cfg.Model = ""
	} else if cfg.Model == "" {
		cfg.Model = "default"
	}
	cfg.ThinkingMode = sanitizeOptionalThinkingMode(cfg.ThinkingMode)
	return cfg
}

func NormalizeModelRolesConfig(cfg ModelRolesConfig) ModelRolesConfig {
	cfg.Action = NormalizeModelRoleConfig(cfg.Action)
	cfg.Planner = NormalizeModelRoleConfig(cfg.Planner)
	cfg.Compaction = NormalizeModelRoleConfig(cfg.Compaction)
	cfg.Title = NormalizeModelRoleConfig(cfg.Title)
	return cfg
}

func MergeModelRoleConfigs(configs ...ModelRoleConfig) ModelRoleConfig {
	var merged ModelRoleConfig
	normalized := make([]ModelRoleConfig, 0, len(configs))
	for _, cfg := range configs {
		normalized = append(normalized, NormalizeModelRoleConfig(cfg))
	}
	for _, cfg := range normalized {
		if merged.Provider == "" && cfg.Provider != "" {
			merged.Provider = cfg.Provider
		}
		if merged.ThinkingMode == "" && cfg.ThinkingMode != "" {
			merged.ThinkingMode = cfg.ThinkingMode
		}
	}
	if merged.Provider != "" {
		for _, cfg := range normalized {
			if cfg.Provider == merged.Provider && cfg.Model != "" {
				merged.Model = cfg.Model
				break
			}
		}
		if merged.Model == "" {
			merged.Model = "default"
		}
	}
	return NormalizeModelRoleConfig(merged)
}

func sanitizeOptionalThinkingMode(mode ThinkingMode) ThinkingMode {
	if strings.TrimSpace(string(mode)) == "" {
		return ""
	}
	return NormalizeThinkingMode(string(mode))
}
