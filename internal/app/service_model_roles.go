package app

import "github.com/doeshing/nekoclaw/internal/core"

// GetModelRolesConfig returns the persisted workflow model roles, with action
// normalized to the current default provider selection for compatibility.
func (s *Service) GetModelRolesConfig() core.ModelRolesConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg := core.NormalizeModelRolesConfig(s.modelRoles)
	cfg.Action = core.MergeModelRoleConfigs(cfg.Action, s.defaultRoleConfigLocked())
	return cfg
}

// SaveModelRolesConfig persists workflow model roles to config.json and keeps
// the action role synchronized with the legacy default provider fields.
func (s *Service) SaveModelRolesConfig(cfg core.ModelRolesConfig) error {
	cfg = core.NormalizeModelRolesConfig(cfg)

	s.mu.Lock()
	configDir := s.configDir
	action := core.MergeModelRoleConfigs(cfg.Action, s.defaultRoleConfigLocked())
	cfg.Action = action
	s.modelRoles = cfg
	if action.Provider != "" {
		s.defaultProvider = action.Provider
		s.defaultModel = action.Model
		if action.ThinkingMode != "" {
			s.defaultThinking = core.NormalizeThinkingMode(string(action.ThinkingMode))
		}
	}
	s.mu.Unlock()

	appCfg, _ := core.LoadConfig(configDir)
	appCfg.ModelRoles = cfg
	appCfg.DefaultProvider = action.Provider
	appCfg.DefaultModel = action.Model
	appCfg.DefaultThinkingMode = core.NormalizeThinkingMode(string(action.ThinkingMode))
	return core.SaveConfig(configDir, appCfg)
}

// SetModelRolesConfig updates the in-memory workflow role config.
func (s *Service) SetModelRolesConfig(cfg core.ModelRolesConfig) {
	cfg = core.NormalizeModelRolesConfig(cfg)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelRoles = cfg
	if cfg.Action.Provider != "" {
		s.defaultProvider = cfg.Action.Provider
		s.defaultModel = cfg.Action.Model
		if cfg.Action.ThinkingMode != "" {
			s.defaultThinking = core.NormalizeThinkingMode(string(cfg.Action.ThinkingMode))
		}
	}
}

// ResolveModelRole returns the effective config for a workflow role. The role
// is resolved against the action role and then the legacy default provider
// state. Optional fallbacks are applied between the requested role and action.
func (s *Service) ResolveModelRole(role core.ModelRole, fallbacks ...core.ModelRoleConfig) core.ModelRoleConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.resolveModelRoleLocked(role, fallbacks...)
}

func (s *Service) resolveRequestRuntime(toolMode core.ToolMode, providerID string, modelID string) (string, string, core.ThinkingMode) {
	requestConfig := core.NormalizeModelRoleConfig(core.ModelRoleConfig{
		Provider: providerID,
		Model:    modelID,
	})

	switch toolMode {
	case core.ToolModePlanner:
		resolved := s.ResolveModelRole(core.ModelRolePlanner, requestConfig)
		if resolved.Provider == "" {
			resolved.Provider = "mock"
		}
		if resolved.Model == "" {
			resolved.Model = "default"
		}
		thinking := core.NormalizeThinkingMode(string(resolved.ThinkingMode))
		if thinking == "" {
			thinking = core.ThinkingModeAuto
		}
		return resolved.Provider, resolved.Model, thinking
	default:
		action := s.ResolveModelRole(core.ModelRoleAction)
		resolved := core.MergeModelRoleConfigs(requestConfig, action)
		if resolved.Provider == "" {
			resolved.Provider = "mock"
		}
		if resolved.Model == "" {
			resolved.Model = "default"
		}
		thinking := core.NormalizeThinkingMode(string(action.ThinkingMode))
		if thinking == "" {
			thinking = core.ThinkingModeAuto
		}
		return resolved.Provider, resolved.Model, thinking
	}
}

func (s *Service) defaultRoleConfigLocked() core.ModelRoleConfig {
	return core.NormalizeModelRoleConfig(core.ModelRoleConfig{
		Provider:     s.defaultProvider,
		Model:        s.defaultModel,
		ThinkingMode: core.NormalizeThinkingMode(string(s.defaultThinking)),
	})
}

func (s *Service) resolveModelRoleLocked(role core.ModelRole, fallbacks ...core.ModelRoleConfig) core.ModelRoleConfig {
	cfg := core.NormalizeModelRolesConfig(s.modelRoles)
	action := core.MergeModelRoleConfigs(cfg.Action, s.defaultRoleConfigLocked())
	if role == core.ModelRoleAction {
		return action
	}
	parts := make([]core.ModelRoleConfig, 0, 2+len(fallbacks))
	parts = append(parts, cfg.Get(role))
	parts = append(parts, fallbacks...)
	parts = append(parts, action)
	return core.MergeModelRoleConfigs(parts...)
}
