package persona

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Manager loads, caches, and manages the active persona.
type Manager struct {
	mu        sync.RWMutex
	baseDir   string
	personas  map[string]*Persona // dirName → Persona
	activeID  string              // active persona's dirName (empty = none)
	statePath string              // persisted active persona state
}

// persistedState is the JSON structure written to persona-state.json.
type persistedState struct {
	Active string `json:"active"`
}

// NewManager creates a Manager that reads personas from baseDir.
func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir:   baseDir,
		personas:  map[string]*Persona{},
		statePath: filepath.Join(filepath.Dir(baseDir), "persona-state.json"),
	}
}

// Start loads all personas from disk and restores the active selection.
func (m *Manager) Start() error {
	if err := m.loadAll(); err != nil {
		return err
	}
	m.restoreState()
	return nil
}

// List returns lightweight info for every loaded persona.
func (m *Manager) List() []PersonaInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]PersonaInfo, 0, len(m.personas))
	for _, p := range m.personas {
		infos = append(infos, p.Info())
	}
	return infos
}

// Active returns the currently active Persona, or nil if none is active.
func (m *Manager) Active() *Persona {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.activeID == "" {
		return nil
	}
	return m.personas[m.activeID]
}

// ActiveInfo returns lightweight info for the active persona, or nil.
func (m *Manager) ActiveInfo() *PersonaInfo {
	p := m.Active()
	if p == nil {
		return nil
	}
	info := p.Info()
	return &info
}

// SetActive switches the active persona to the given dirName.
func (m *Manager) SetActive(dirName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.personas[dirName]; !ok {
		return fmt.Errorf("persona %q not found", dirName)
	}
	m.activeID = dirName
	m.saveStateLocked()
	log.Printf("event=persona_activated persona=%s", dirName)
	return nil
}

// ClearActive deactivates the current persona.
func (m *Manager) ClearActive() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.activeID = ""
	m.saveStateLocked()
	log.Printf("event=persona_cleared")
	return nil
}

// Reload re-scans the personas directory and reloads all definitions.
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.activeID
	if err := m.loadAllLocked(); err != nil {
		return err
	}
	// Keep active selection if the persona still exists after reload.
	if _, ok := m.personas[old]; ok {
		m.activeID = old
	} else {
		m.activeID = ""
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (m *Manager) loadAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadAllLocked()
}

func (m *Manager) loadAllLocked() error {
	m.personas = map[string]*Persona{}

	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no personas directory is fine
		}
		return fmt.Errorf("read personas directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(m.baseDir, entry.Name())
		p, loadErr := LoadPersona(dir)
		if loadErr != nil {
			log.Printf("event=persona_load_skip dir=%s error=%q", entry.Name(), loadErr)
			continue
		}
		m.personas[p.DirName] = p
		log.Printf("event=persona_loaded name=%s dir=%s", p.Config.Meta.Name, p.DirName)
	}
	return nil
}

func (m *Manager) restoreState() {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.statePath)
	if err != nil {
		return // no state file is fine
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	// Only restore if the persona still exists.
	if _, ok := m.personas[state.Active]; ok {
		m.activeID = state.Active
		log.Printf("event=persona_state_restored persona=%s", state.Active)
	}
}

func (m *Manager) saveStateLocked() {
	state := persistedState{Active: m.activeID}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		log.Printf("event=persona_state_save_error error=%q", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o755); err != nil {
		log.Printf("event=persona_state_dir_error error=%q", err)
		return
	}
	if err := os.WriteFile(m.statePath, data, 0o644); err != nil {
		log.Printf("event=persona_state_write_error error=%q", err)
	}
}
