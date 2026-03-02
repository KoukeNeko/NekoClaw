package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doeshing/nekoclaw/internal/core"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

const (
	chunkTargetTokens  = 400
	chunkOverlapTokens = 80
	charsPerToken      = 4
	searchDriverName   = "sqlite"
)

// SearchResult represents a single search hit from the FTS5 index.
type SearchResult struct {
	SessionID string    `json:"session_id"`
	EntryID   string    `json:"entry_id"`
	Content   string    `json:"content"`
	Role      string    `json:"role"`
	Score     float64   `json:"score"`
	Timestamp time.Time `json:"timestamp"`
}

// SearchIndex manages a SQLite FTS5 full-text search index over session entries.
type SearchIndex struct {
	db *sql.DB
}

// NewSearchIndex opens or creates a SQLite database at dbPath with FTS5 tables.
func NewSearchIndex(dbPath string) (*SearchIndex, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create search index dir: %w", err)
	}

	db, err := sql.Open(searchDriverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open search db: %w", err)
	}

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SearchIndex{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			entry_id TEXT NOT NULL,
			content TEXT NOT NULL,
			role TEXT,
			timestamp DATETIME
		);

		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts
			USING fts5(content, content=chunks, content_rowid=id);

		CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
			INSERT INTO chunks_fts(rowid, content) VALUES (new.id, new.content);
		END;

		CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, content) VALUES('delete', old.id, old.content);
		END;

		CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
			INSERT INTO chunks_fts(chunks_fts, rowid, content) VALUES('delete', old.id, old.content);
			INSERT INTO chunks_fts(rowid, content) VALUES (new.id, new.content);
		END;
	`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("init search schema: %w", err)
	}
	return nil
}

// Index inserts or updates chunks for the given session entries.
func (idx *SearchIndex) Index(sessionID string, entries []core.SessionEntry) error {
	tx, err := idx.db.Begin()
	if err != nil {
		return fmt.Errorf("begin index tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO chunks (session_id, entry_id, content, role, timestamp) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		if e.Type != core.EntryMessage || strings.TrimSpace(e.Content) == "" {
			continue
		}

		// Check if entry already indexed.
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM chunks WHERE entry_id = ?`, e.ID).Scan(&count); err != nil {
			return fmt.Errorf("check existing: %w", err)
		}
		if count > 0 {
			continue
		}

		chunks := chunkText(e.Content, chunkTargetTokens, chunkOverlapTokens)
		for _, chunk := range chunks {
			if _, err := stmt.Exec(sessionID, e.ID, chunk, string(e.Role), e.Timestamp); err != nil {
				return fmt.Errorf("insert chunk: %w", err)
			}
		}
	}

	return tx.Commit()
}

// Search performs a full-text search and returns ranked results.
func (idx *SearchIndex) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	rows, err := idx.db.Query(`
		SELECT c.session_id, c.entry_id, c.content, c.role, c.timestamp,
		       rank
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		WHERE chunks_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var ts sql.NullTime
		if err := rows.Scan(&r.SessionID, &r.EntryID, &r.Content, &r.Role, &ts, &r.Score); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		if ts.Valid {
			r.Timestamp = ts.Time
		}
		// FTS5 rank is negative (lower is better); invert for a positive score.
		r.Score = -r.Score
		results = append(results, r)
	}
	return results, rows.Err()
}

// DeleteSession removes all indexed chunks for a session.
func (idx *SearchIndex) DeleteSession(sessionID string) error {
	_, err := idx.db.Exec(`DELETE FROM chunks WHERE session_id = ?`, sessionID)
	return err
}

// Close closes the underlying database connection.
func (idx *SearchIndex) Close() error {
	if idx.db != nil {
		return idx.db.Close()
	}
	return nil
}

// chunkText splits text into overlapping chunks of approximately targetTokens.
func chunkText(text string, targetTokens, overlapTokens int) []string {
	targetChars := targetTokens * charsPerToken
	overlapChars := overlapTokens * charsPerToken
	if targetChars <= 0 {
		targetChars = 1600
	}
	if overlapChars < 0 {
		overlapChars = 0
	}

	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= targetChars {
		return []string{string(runes)}
	}

	var chunks []string
	start := 0
	for start < len(runes) {
		end := start + targetChars
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		step := targetChars - overlapChars
		if step < 1 {
			step = 1
		}
		start += step
	}
	return chunks
}

// estimateStringTokens is a simple rune-based token estimator.
func estimateStringTokens(s string) int {
	runes := utf8.RuneCountInString(strings.TrimSpace(s))
	t := (runes + charsPerToken - 1) / charsPerToken
	if t < 1 {
		return 1
	}
	return t
}
