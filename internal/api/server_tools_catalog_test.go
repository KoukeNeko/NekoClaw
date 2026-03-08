package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/tooling"
)

func TestToolsCatalogEndpointReturnsBuiltins(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	server := NewServer(svc)
	handler := server.Handler()

	resp := performJSONRequest(t, handler, http.MethodGet, "/v1/tools/catalog", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Tools []tooling.ToolCatalogEntry `json:"tools"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Tools) == 0 {
		t.Fatalf("expected builtin tools in catalog")
	}

	webSearch := findToolCatalogEntry(payload.Tools, "web_search")
	if webSearch == nil {
		t.Fatalf("expected web_search in tools catalog")
	}
	if !webSearch.Configurable {
		t.Fatalf("expected web_search configurable")
	}
	if webSearch.Configured {
		t.Fatalf("expected web_search unconfigured without Brave key")
	}

	fileWrite := findToolCatalogEntry(payload.Tools, "file_write")
	if fileWrite == nil {
		t.Fatalf("expected file_write in tools catalog")
	}
	if !fileWrite.Mutating {
		t.Fatalf("expected file_write mutating")
	}
}

func findToolCatalogEntry(
	tools []tooling.ToolCatalogEntry,
	name string,
) *tooling.ToolCatalogEntry {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}
