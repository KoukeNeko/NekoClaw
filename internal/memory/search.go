package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/logger"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

var logSearch = logger.New("search", logger.Cyan)

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
	Path      string    `json:"path,omitempty"`
	StartLine int       `json:"start_line,omitempty"`
	EndLine   int       `json:"end_line,omitempty"`
	Content   string    `json:"content"`
	Role      string    `json:"role"`
	Source    string    `json:"source,omitempty"`
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
			path TEXT DEFAULT '',
			start_line INTEGER DEFAULT 0,
			end_line INTEGER DEFAULT 0,
			content TEXT NOT NULL,
			role TEXT,
			source TEXT DEFAULT 'sessions',
			timestamp DATETIME
		);

		CREATE TABLE IF NOT EXISTS indexed_files (
			path TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			size INTEGER DEFAULT 0,
			hash TEXT DEFAULT '',
			indexed_at DATETIME
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

	// Migrate existing schema: add new columns if missing.
	migrations := []string{
		"ALTER TABLE chunks ADD COLUMN path TEXT DEFAULT ''",
		"ALTER TABLE chunks ADD COLUMN start_line INTEGER DEFAULT 0",
		"ALTER TABLE chunks ADD COLUMN end_line INTEGER DEFAULT 0",
		"ALTER TABLE chunks ADD COLUMN source TEXT DEFAULT 'sessions'",
	}
	for _, m := range migrations {
		_, _ = db.Exec(m) // ignore "duplicate column" errors
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
		SELECT c.session_id, c.entry_id, c.path, c.start_line, c.end_line,
		       c.content, c.role, c.source, c.timestamp, rank
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
		var path, source sql.NullString
		if err := rows.Scan(&r.SessionID, &r.EntryID, &path, &r.StartLine, &r.EndLine,
			&r.Content, &r.Role, &source, &ts, &r.Score); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		if ts.Valid {
			r.Timestamp = ts.Time
		}
		if path.Valid {
			r.Path = path.String
		}
		if source.Valid {
			r.Source = source.String
		}
		// FTS5 rank is negative (lower is better); invert for a positive score.
		r.Score = -r.Score
		results = append(results, r)
	}
	return results, rows.Err()
}

// IndexMemoryFiles scans memoryDir for MEMORY.md and memory/*.md files,
// chunking and indexing their content. Uses indexed_files to skip unchanged files.
func (idx *SearchIndex) IndexMemoryFiles(memoryDir string) error {
	if memoryDir == "" {
		return nil
	}
	var files []string
	// MEMORY.md at root
	memoryMD := filepath.Join(memoryDir, "MEMORY.md")
	if _, err := os.Stat(memoryMD); err == nil {
		files = append(files, memoryMD)
	}
	// memory/*.md subdirectory
	subdir := filepath.Join(memoryDir, "memory")
	if entries, err := os.ReadDir(subdir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			files = append(files, filepath.Join(subdir, entry.Name()))
		}
	}
	// Daily logs (YYYY-MM-DD.md) at root
	if entries, err := os.ReadDir(memoryDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".md") || name == "MEMORY.md" {
				continue
			}
			files = append(files, filepath.Join(memoryDir, name))
		}
	}

	for _, path := range files {
		if err := idx.indexFile(path, memoryDir, "memory"); err != nil {
			logSearch.Warnf("index memory file: path=%s error=%v", path, err)
		}
	}
	return nil
}

// IndexSessionFiles scans sessionsDir for .jsonl transcript files and indexes
// their content. Uses indexed_files to skip unchanged files.
func (idx *SearchIndex) IndexSessionFiles(sessionsDir string) error {
	if sessionsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil // silently skip if dir doesn't exist
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(sessionsDir, entry.Name())
		if err := idx.indexSessionFile(path); err != nil {
			logSearch.Warnf("index session file: path=%s error=%v", path, err)
		}
	}
	return nil
}

// indexFile indexes a single markdown file into chunks.
func (idx *SearchIndex) indexFile(path, baseDir, source string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hash := fileHash(data)
	relPath, _ := filepath.Rel(baseDir, path)

	// Check if already indexed with same hash.
	var existingHash string
	err = idx.db.QueryRow(`SELECT hash FROM indexed_files WHERE path = ?`, relPath).Scan(&existingHash)
	if err == nil && existingHash == hash {
		return nil // unchanged
	}

	// Remove old chunks for this file, then re-index.
	_, _ = idx.db.Exec(`DELETE FROM chunks WHERE path = ? AND source = ?`, relPath, source)

	tx, err := idx.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO chunks (session_id, entry_id, path, start_line, end_line, content, role, source, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	lines := strings.Split(string(data), "\n")
	chunks := chunkLines(lines, chunkTargetTokens, chunkOverlapTokens)
	for _, chunk := range chunks {
		if _, err := stmt.Exec("", relPath, relPath, chunk.startLine, chunk.endLine, chunk.text, "", source, info.ModTime()); err != nil {
			return err
		}
	}

	// Update indexed_files tracking.
	_, _ = tx.Exec(`INSERT OR REPLACE INTO indexed_files (path, source, size, hash, indexed_at) VALUES (?, ?, ?, ?, ?)`,
		relPath, source, info.Size(), hash, time.Now())

	logSearch.Logf("indexed memory file: path=%s chunks=%d", relPath, len(chunks))
	return tx.Commit()
}

// indexSessionFile indexes a JSONL session transcript file.
func (idx *SearchIndex) indexSessionFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hash := fileHash(data)

	// Check if already indexed with same hash.
	var existingHash string
	err = idx.db.QueryRow(`SELECT hash FROM indexed_files WHERE path = ?`, path).Scan(&existingHash)
	if err == nil && existingHash == hash {
		return nil // unchanged
	}

	// Parse JSONL entries and index via existing Index method.
	entries := core.ParseSessionEntries(data)
	if len(entries) == 0 {
		return nil
	}

	// Extract session ID from filename.
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")

	// Remove old chunks for this session file.
	_, _ = idx.db.Exec(`DELETE FROM chunks WHERE session_id = ? AND source = 'sessions'`, sessionID)

	// Re-index using the standard Index method.
	if err := idx.Index(sessionID, entries); err != nil {
		return err
	}

	// Update indexed_files tracking.
	_, _ = idx.db.Exec(`INSERT OR REPLACE INTO indexed_files (path, source, size, hash, indexed_at) VALUES (?, ?, ?, ?, ?)`,
		path, "sessions", info.Size(), hash, time.Now())

	logSearch.Logf("indexed session file: path=%s entries=%d", filepath.Base(path), len(entries))
	return nil
}

// lineChunk represents a chunk of text with line range metadata.
type lineChunk struct {
	text      string
	startLine int // 1-based
	endLine   int // 1-based, inclusive
}

// chunkLines splits lines into overlapping chunks and tracks line ranges.
func chunkLines(lines []string, targetTokens, overlapTokens int) []lineChunk {
	targetChars := targetTokens * charsPerToken
	overlapChars := overlapTokens * charsPerToken
	if targetChars <= 0 {
		targetChars = 1600
	}

	fullText := strings.Join(lines, "\n")
	runes := []rune(strings.TrimSpace(fullText))
	if len(runes) == 0 {
		return nil
	}
	if len(runes) <= targetChars {
		return []lineChunk{{text: string(runes), startLine: 1, endLine: len(lines)}}
	}

	// Build a cumulative character count per line for mapping char offset → line number.
	lineCumLen := make([]int, len(lines)+1) // lineCumLen[i] = total chars before line i (including \n)
	for i, line := range lines {
		lineCumLen[i+1] = lineCumLen[i] + utf8.RuneCountInString(line) + 1 // +1 for \n
	}

	var chunks []lineChunk
	start := 0
	step := targetChars - overlapChars
	if step < 1 {
		step = 1
	}
	for start < len(runes) {
		end := start + targetChars
		if end > len(runes) {
			end = len(runes)
		}
		startLine := charOffsetToLine(lineCumLen, start) + 1
		endLine := charOffsetToLine(lineCumLen, end-1) + 1
		chunks = append(chunks, lineChunk{
			text:      string(runes[start:end]),
			startLine: startLine,
			endLine:   endLine,
		})
		start += step
	}
	return chunks
}

// charOffsetToLine maps a rune offset to a 0-based line index.
func charOffsetToLine(cumLen []int, offset int) int {
	lo, hi := 0, len(cumLen)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if cumLen[mid] <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo > 0 {
		return lo - 1
	}
	return 0
}

func fileHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8]) // first 8 bytes is sufficient
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
