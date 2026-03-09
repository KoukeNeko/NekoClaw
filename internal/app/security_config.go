package app

import (
	"errors"
	"fmt"

	"github.com/doeshing/nekoclaw/internal/core"
)

var ErrInvalidSecurityConfig = errors.New("invalid security config")

// GetSecurityConfig returns a copy of the current browser security configuration.
func (s *Service) GetSecurityConfig() core.SecurityConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sanitizeSecurityConfig(s.securityConfig)
}

// SaveSecurityConfig persists browser security settings to config.json and updates in-memory state.
func (s *Service) SaveSecurityConfig(cfg core.SecurityConfig) error {
	cfg = sanitizeSecurityConfig(cfg)
	if err := validateSecurityConfig(cfg); err != nil {
		return err
	}

	s.mu.Lock()
	configDir := s.configDir
	s.securityConfig = cfg
	s.mu.Unlock()

	appCfg, _ := core.LoadConfig(configDir)
	appCfg.Security = cfg
	return core.SaveConfig(configDir, appCfg)
}

// SetSecurityConfig sets the in-memory browser security config (used during startup).
func (s *Service) SetSecurityConfig(cfg core.SecurityConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.securityConfig = sanitizeSecurityConfig(cfg)
}

func sanitizeSecurityConfig(cfg core.SecurityConfig) core.SecurityConfig {
	defaults := core.DefaultSecurityConfig()
	if cfg == (core.SecurityConfig{}) {
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

func validateSecurityConfig(cfg core.SecurityConfig) error {
	if cfg.SessionIdleMinutes <= 0 {
		return fmt.Errorf("%w: session_idle_minutes must be > 0", ErrInvalidSecurityConfig)
	}
	if cfg.SessionAbsoluteHours <= 0 {
		return fmt.Errorf("%w: session_absolute_hours must be > 0", ErrInvalidSecurityConfig)
	}
	if cfg.LoginMaxAttempts <= 0 {
		return fmt.Errorf("%w: login_max_attempts must be > 0", ErrInvalidSecurityConfig)
	}
	if cfg.LoginBlockMinutes <= 0 {
		return fmt.Errorf("%w: login_block_minutes must be > 0", ErrInvalidSecurityConfig)
	}
	return nil
}
