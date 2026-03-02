package core

import "sync"

type SessionStore struct {
	mu       sync.Mutex
	sessions map[string][]Message
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: map[string][]Message{}}
}

func (s *SessionStore) History(sessionID string) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.sessions[sessionID]...)
}

func (s *SessionStore) Append(sessionID string, msgs ...Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = append(s.sessions[sessionID], msgs...)
}
