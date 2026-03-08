package binding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/logger"
)

var logBinding = logger.New("binding", logger.Cyan)

// Entry represents a persisted channel-to-session mapping.
type Entry struct {
	SessionID     string     `json:"session_id"`
	HistoryCutoff *time.Time `json:"history_cutoff,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// persistedPayload is the on-disk JSON structure.
type persistedPayload struct {
	Version  int              `json:"version"`
	Bindings map[string]Entry `json:"bindings"`
}

// Store persists channel → session ID mappings to a JSON file so that
// Discord/Telegram bots can recover their active session after restart.
type Store struct {
	path     string
	mu       sync.RWMutex
	bindings map[string]Entry
}

// Load reads a binding store from the given file path. If the file does
// not exist, an empty store is returned (no error). The parent directory
// is created automatically.
func Load(path string) (*Store, error) {
	s := &Store{
		path:     path,
		bindings: make(map[string]Entry),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}

	var payload persistedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		logBinding.Warnf("corrupt binding file %s, starting fresh: %v", path, err)
		return s, nil
	}

	if payload.Bindings != nil {
		s.bindings = payload.Bindings
	}
	logBinding.Logf("loaded %d bindings from %s", len(s.bindings), filepath.Base(path))
	return s, nil
}

// Get returns the binding entry for the given key.
func (s *Store) Get(key string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.bindings[key]
	return e, ok
}

// Set writes a binding entry and flushes to disk.
func (s *Store) Set(key string, entry Entry) {
	entry.UpdatedAt = time.Now()
	s.mu.Lock()
	s.bindings[key] = entry
	s.mu.Unlock()
	s.flush()
}

// Delete removes a binding entry and flushes to disk.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	delete(s.bindings, key)
	s.mu.Unlock()
	s.flush()
}

// All returns a snapshot of all bindings.
func (s *Store) All() map[string]Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]Entry, len(s.bindings))
	for k, v := range s.bindings {
		cp[k] = v
	}
	return cp
}

// flush writes the current bindings to disk atomically (write tmp → rename).
func (s *Store) flush() {
	s.mu.RLock()
	payload := persistedPayload{
		Version:  1,
		Bindings: s.bindings,
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		logBinding.Errorf("marshal bindings: %v", err)
		return
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logBinding.Errorf("create state dir %s: %v", dir, err)
		return
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logBinding.Errorf("write tmp binding file: %v", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		logBinding.Errorf("rename binding file: %v", err)
	}
}
