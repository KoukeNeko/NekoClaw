package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/memory"

	_ "modernc.org/sqlite"
)

func TestMemorySearchEndpoint_LegacyTimestampReturns200(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "search.db")
	idx, err := memory.NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("create search index: %v", err)
	}
	defer idx.Close()

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	legacyTimestamp := "2026-03-05 17:20:16.276647 +0800 CST m=+32.558701501"
	if _, err := rawDB.Exec(
		`INSERT INTO chunks (session_id, entry_id, path, start_line, end_line, content, role, source, timestamp) VALUES (?, ?, '', 0, 0, ?, 'user', 'sessions', ?)`,
		"legacy-api-session",
		"legacy-api-entry",
		"legacyapi token",
		legacyTimestamp,
	); err != nil {
		t.Fatalf("seed legacy chunk: %v", err)
	}

	svc := app.NewService(app.ServiceOptions{SearchIndex: idx})
	handler := NewServer(svc).Handler()

	resp := performJSONRequest(t, handler, http.MethodPost, "/v1/memory/search", `{"query":"legacyapi","limit":10}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Results []memory.SearchResult `json:"results"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(payload.Results))
	}
	if got := payload.Results[0].Timestamp.Format(time.RFC3339Nano); got != "2026-03-05T17:20:16.276647+08:00" {
		t.Fatalf("timestamp = %q, want %q", got, "2026-03-05T17:20:16.276647+08:00")
	}
}
