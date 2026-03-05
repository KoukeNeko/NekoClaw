package core

import (
	"fmt"
	"os"
	"time"

	"github.com/doeshing/nekoclaw/internal/logger"
)

var logSession = logger.New("session", logger.Blue)

// SessionLifecycle manages automatic session resets, rotation, and housekeeping.
type SessionLifecycle struct {
	store  *SessionStore
	config SessionLifecycleConfig
}

func NewSessionLifecycle(store *SessionStore, config SessionLifecycleConfig) *SessionLifecycle {
	// DailyResetHour = -1 disables daily reset.
	if config.DailyResetHour < -1 || config.DailyResetHour > 23 {
		config.DailyResetHour = 4
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 60 * time.Minute
	}
	if config.RetentionDays <= 0 {
		config.RetentionDays = 30
	}
	if config.MaxEntries <= 0 {
		config.MaxEntries = 500
	}
	if config.MaxFileSize <= 0 {
		config.MaxFileSize = 10 * 1024 * 1024
	}
	return &SessionLifecycle{store: store, config: config}
}

// ShouldReset returns true if the session should be reset due to daily reset
// policy or idle timeout.
func (l *SessionLifecycle) ShouldReset(sessionID string) bool {
	sessions := l.store.ListSessions()
	var meta *SessionMetadata
	for i := range sessions {
		if sessions[i].SessionID == sessionID {
			meta = &sessions[i]
			break
		}
	}
	if meta == nil {
		return false
	}

	// Daily reset: if the session was last updated before today's reset hour.
	if l.pastDailyResetBoundary(meta.UpdatedAt) {
		return true
	}

	// Idle timeout: if the session has been idle longer than the configured timeout.
	if time.Since(meta.UpdatedAt) > l.config.IdleTimeout {
		return true
	}

	return false
}

// pastDailyResetBoundary returns true if lastUpdate is before the most recent
// daily reset boundary (today at DailyResetHour, or yesterday's if it hasn't
// occurred yet today).
func (l *SessionLifecycle) pastDailyResetBoundary(lastUpdate time.Time) bool {
	if l.config.DailyResetHour < 0 {
		return false // daily reset disabled
	}
	now := time.Now()
	resetToday := time.Date(now.Year(), now.Month(), now.Day(), l.config.DailyResetHour, 0, 0, 0, now.Location())

	var boundary time.Time
	if now.Before(resetToday) {
		// Reset hour hasn't passed yet today; use yesterday's boundary.
		boundary = resetToday.AddDate(0, 0, -1)
	} else {
		boundary = resetToday
	}

	return lastUpdate.Before(boundary)
}

// RotateSession archives the current session (by renaming its ID) and removes
// its cache so a new session can start fresh.
func (l *SessionLifecycle) RotateSession(sessionID string) error {
	archivedID := fmt.Sprintf("%s_archived_%s", sessionID, time.Now().Format("20060102_150405"))
	logSession.Logf("rotate: session_id=%s archived_as=%s", sessionID, archivedID)

	l.store.mu.Lock()
	defer l.store.mu.Unlock()

	// Move entries to archived session.
	l.store.ensureLoadedLocked(sessionID)
	entries := l.store.cache[sessionID]
	if len(entries) > 0 {
		l.store.cache[archivedID] = entries
		l.store.loaded[archivedID] = true

		// Copy metadata under archived ID.
		if meta, ok := l.store.metadata[sessionID]; ok {
			archivedMeta := meta
			archivedMeta.SessionID = archivedID
			l.store.metadata[archivedID] = archivedMeta
		}

		// Persist archived transcript.
		if l.store.dataDir != "" {
			l.store.appendToTranscriptLocked(archivedID, entries)
			l.store.writeMetadataLocked()
		}
	}

	// Clear the original session.
	delete(l.store.cache, sessionID)
	delete(l.store.metadata, sessionID)
	delete(l.store.loaded, sessionID)

	// Remove original transcript file.
	if l.store.dataDir != "" {
		path := l.store.transcriptPath(sessionID)
		_ = os.Remove(path)
		l.store.writeMetadataLocked()
	}

	return nil
}

// RunHousekeeping performs retention cleanup and rotation for oversized sessions.
func (l *SessionLifecycle) RunHousekeeping() error {
	sessions := l.store.ListSessions()
	cutoff := time.Now().AddDate(0, 0, -l.config.RetentionDays)

	for _, meta := range sessions {
		// Retention: delete sessions older than RetentionDays.
		if meta.UpdatedAt.Before(cutoff) {
			logSession.Logf("housekeeping delete: session_id=%s reason=retention updated_at=%s",
				meta.SessionID, meta.UpdatedAt.Format(time.RFC3339))
			if err := l.store.DeleteSession(meta.SessionID); err != nil {
				logSession.Errorf("housekeeping delete error: session_id=%s error=%v", meta.SessionID, err)
			}
			continue
		}

		// Entry cap: rotate sessions exceeding MaxEntries.
		if meta.MessageCount > l.config.MaxEntries {
			logSession.Logf("housekeeping rotate: session_id=%s reason=max_entries count=%d",
				meta.SessionID, meta.MessageCount)
			if err := l.RotateSession(meta.SessionID); err != nil {
				logSession.Errorf("housekeeping rotate error: session_id=%s error=%v", meta.SessionID, err)
			}
			continue
		}

		// File size: rotate sessions exceeding MaxFileSize.
		if l.store.dataDir != "" {
			path := l.store.transcriptPath(meta.SessionID)
			if info, err := os.Stat(path); err == nil && info.Size() > l.config.MaxFileSize {
				logSession.Logf("housekeeping rotate: session_id=%s reason=max_file_size size=%d",
					meta.SessionID, info.Size())
				if err := l.RotateSession(meta.SessionID); err != nil {
					logSession.Errorf("housekeeping rotate error: session_id=%s error=%v", meta.SessionID, err)
				}
			}
		}
	}
	return nil
}
