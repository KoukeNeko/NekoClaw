package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type metadataFile struct {
	Sessions map[string]SessionMetadata `json:"sessions"`
}

type SessionStore struct {
	mu       sync.Mutex
	dataDir  string // empty string = in-memory only
	cache    map[string][]SessionEntry
	metadata map[string]SessionMetadata
	loaded   map[string]bool
}

// NewSessionStore creates a purely in-memory session store (backward compatible).
func NewSessionStore() *SessionStore {
	return &SessionStore{
		cache:    map[string][]SessionEntry{},
		metadata: map[string]SessionMetadata{},
		loaded:   map[string]bool{},
	}
}

// NewPersistentSessionStore creates a session store backed by JSONL transcript
// files and a metadata index. Only metadata is read at init time; individual
// session transcripts are loaded lazily on first access.
func NewPersistentSessionStore(dataDir string) (*SessionStore, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return NewSessionStore(), nil
	}
	transcriptDir := filepath.Join(dataDir, "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		return nil, err
	}
	store := &SessionStore{
		dataDir:  dataDir,
		cache:    map[string][]SessionEntry{},
		metadata: map[string]SessionMetadata{},
		loaded:   map[string]bool{},
	}
	if err := store.loadMetadata(); err != nil {
		return nil, err
	}
	return store, nil
}

// History returns a copy of all entries for the given session.
func (s *SessionStore) History(sessionID string) []SessionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked(sessionID)
	return append([]SessionEntry(nil), s.cache[sessionID]...)
}

// HistoryAsMessages returns only message-type entries converted to Message,
// preserving compaction summaries as system messages so the chat pipeline
// receives a coherent conversation view.
func (s *SessionStore) HistoryAsMessages(sessionID string) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked(sessionID)

	entries := s.cache[sessionID]
	messages := make([]Message, 0, len(entries))
	for _, e := range entries {
		switch e.Type {
		case EntryMessage:
			messages = append(messages, e.ToMessage())
		case EntryCompaction:
			// Inject compaction summary as a system message so the LLM
			// retains context from compacted history.
			if e.Summary != "" {
				messages = append(messages, Message{
					Role:      RoleSystem,
					Content:   e.Summary,
					CreatedAt: e.Timestamp,
				})
			}
		}
	}
	return messages
}

// Append adds entries to the session and persists them to disk.
func (s *SessionStore) Append(sessionID string, entries ...SessionEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked(sessionID)

	// Auto-insert session header for brand new sessions.
	if len(s.cache[sessionID]) == 0 {
		header := NewSessionHeader("", "")
		s.cache[sessionID] = append(s.cache[sessionID], header)
		if s.dataDir != "" {
			s.appendToTranscriptLocked(sessionID, []SessionEntry{header})
		}
	}

	s.cache[sessionID] = append(s.cache[sessionID], entries...)

	// Always update in-memory metadata (needed for ListSessions/lifecycle).
	s.updateMetadataLocked(sessionID)

	if s.dataDir == "" {
		return
	}
	s.appendToTranscriptLocked(sessionID, entries)
}

// AppendMessage is a convenience wrapper that converts Messages to entries.
func (s *SessionStore) AppendMessage(sessionID string, msgs ...Message) {
	entries := make([]SessionEntry, len(msgs))
	for i, msg := range msgs {
		entries[i] = MessageToEntry(msg)
	}
	s.Append(sessionID, entries...)
}

// ListSessions returns metadata for all known sessions sorted by UpdatedAt
// descending (most recently active first).
func (s *SessionStore) ListSessions() []SessionMetadata {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions := make([]SessionMetadata, 0, len(s.metadata))
	for _, meta := range s.metadata {
		sessions = append(sessions, meta)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions
}

// DeleteSession removes a session's transcript file, metadata entry, and
// in-memory cache.
func (s *SessionStore) DeleteSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.cache, sessionID)
	delete(s.metadata, sessionID)
	delete(s.loaded, sessionID)

	if s.dataDir == "" {
		return nil
	}

	path := s.transcriptPath(sessionID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.writeMetadataLocked()
	return nil
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

func (s *SessionStore) loadMetadata() error {
	path := filepath.Join(s.dataDir, "metadata.json")
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return nil
	}
	var file metadataFile
	if err := json.Unmarshal(content, &file); err != nil {
		return err
	}
	if file.Sessions != nil {
		s.metadata = file.Sessions
	}
	return nil
}

// ensureLoadedLocked reads a transcript JSONL from disk into cache.
// It supports both the new typed-entry format and the legacy plain-Message
// format for backward compatibility.
func (s *SessionStore) ensureLoadedLocked(sessionID string) {
	if s.loaded[sessionID] || s.dataDir == "" {
		return
	}
	s.loaded[sessionID] = true

	path := s.transcriptPath(sessionID)
	file, err := os.Open(path)
	if err != nil {
		return // file does not exist yet
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var entries []SessionEntry
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		// Try typed entry first.
		var entry SessionEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			log.Printf("event=session_line_corrupt session_id=%s error=%q", sessionID, err)
			continue
		}

		// Detect legacy format: no "type" field means it's a plain Message.
		if entry.Type == "" {
			entry = migrateLineToEntry(line)
			if entry.Type == "" {
				continue
			}
		}

		entries = append(entries, entry)
	}
	if len(entries) > 0 {
		s.cache[sessionID] = entries
	}
}

// migrateLineToEntry attempts to parse a JSONL line as a legacy Message
// and convert it to a SessionEntry.
func migrateLineToEntry(line []byte) SessionEntry {
	var msg Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return SessionEntry{}
	}
	if msg.Role == "" {
		return SessionEntry{}
	}
	return MessageToEntry(msg)
}

func (s *SessionStore) appendToTranscriptLocked(sessionID string, entries []SessionEntry) {
	path := s.transcriptPath(sessionID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("event=session_append_error session_id=%s error=%q", sessionID, err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			log.Printf("event=session_encode_error session_id=%s error=%q", sessionID, err)
		}
	}
}

func (s *SessionStore) updateMetadataLocked(sessionID string) {
	now := time.Now()
	meta, exists := s.metadata[sessionID]
	if !exists {
		meta = SessionMetadata{
			SessionID: sessionID,
			CreatedAt: now,
		}
	}
	meta.MessageCount = s.countMessagesLocked(sessionID)
	meta.UpdatedAt = now
	s.metadata[sessionID] = meta
	if s.dataDir != "" {
		s.writeMetadataLocked()
	}
}

// SetTitle updates the session title in metadata. Safe for concurrent use.
func (s *SessionStore) SetTitle(sessionID, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, exists := s.metadata[sessionID]
	if !exists {
		return
	}
	meta.Title = title
	s.metadata[sessionID] = meta
	if s.dataDir != "" {
		s.writeMetadataLocked()
	}
}

// GetMetadata returns the metadata for a single session.
func (s *SessionStore) GetMetadata(sessionID string) (SessionMetadata, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.metadata[sessionID]
	return meta, ok
}

// countMessagesLocked counts only message-type entries (excludes headers,
// compaction entries, etc.) for an accurate message count.
func (s *SessionStore) countMessagesLocked(sessionID string) int {
	count := 0
	for _, e := range s.cache[sessionID] {
		if e.Type == EntryMessage {
			count++
		}
	}
	return count
}

func (s *SessionStore) writeMetadataLocked() {
	file := metadataFile{Sessions: s.metadata}
	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		log.Printf("event=metadata_marshal_error error=%q", err)
		return
	}
	metaPath := filepath.Join(s.dataDir, "metadata.json")
	tmp := metaPath + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		log.Printf("event=metadata_write_error error=%q", err)
		return
	}
	if err := os.Rename(tmp, metaPath); err != nil {
		log.Printf("event=metadata_rename_error error=%q", err)
	}
}

func (s *SessionStore) transcriptPath(sessionID string) string {
	return filepath.Join(s.dataDir, "transcripts", sanitizeSessionID(sessionID)+".jsonl")
}

func sanitizeSessionID(id string) string {
	r := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return r.Replace(strings.TrimSpace(id))
}
