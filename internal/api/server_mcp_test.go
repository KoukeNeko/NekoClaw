package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/mcp"
)

func TestMCPConfigsEndpointListsValidAndInvalidDocuments(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "valid.json"), `{
  "name": "alpha",
  "transport": "stdio",
  "command": "missing-binary",
  "trust": "trusted"
}`)
	writeTestFile(t, filepath.Join(dir, "invalid.json"), `{
  "name": "beta",
  "transport": "stdio",
  "trust": "trusted"
}`)
	writeTestFile(t, filepath.Join(dir, "broken.json"), `{`)

	svc := app.NewService(app.ServiceOptions{MCPConfigDir: dir})
	handler := NewServer(svc).Handler()

	resp := performJSONRequest(t, handler, http.MethodGet, "/v1/mcp/configs", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		ConfigDir string               `json:"config_dir"`
		Documents []mcp.ConfigDocument `json:"documents"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ConfigDir != dir {
		t.Fatalf("unexpected config_dir: %q", payload.ConfigDir)
	}
	if len(payload.Documents) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(payload.Documents))
	}

	byFile := map[string]mcp.ConfigDocument{}
	for _, doc := range payload.Documents {
		byFile[doc.FileName] = doc
	}
	if byFile["valid.json"].Config == nil || byFile["valid.json"].ValidationError != "" || byFile["valid.json"].ParseError != "" {
		t.Fatalf("expected valid.json to be valid, got %#v", byFile["valid.json"])
	}
	if got := byFile["invalid.json"].ValidationError; got == "" {
		t.Fatalf("expected invalid.json validation_error, got %#v", byFile["invalid.json"])
	}
	if got := byFile["broken.json"].ParseError; got == "" {
		t.Fatalf("expected broken.json parse_error, got %#v", byFile["broken.json"])
	}
}

func TestMCPConfigSaveDeleteAndApplyEndpoints(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "alpha.json"), `{`)

	svc := app.NewService(app.ServiceOptions{MCPConfigDir: dir})
	handler := NewServer(svc).Handler()

	invalidSave := performJSONRequest(t, handler, http.MethodPost, "/v1/mcp/configs/save", `{
  "raw_json": "{\"name\":\"oops\",\"transport\":\"stdio\"}"
}`)
	if invalidSave.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid save to return 400, got %d body=%s", invalidSave.Code, invalidSave.Body.String())
	}

	createResp := performJSONRequest(t, handler, http.MethodPost, "/v1/mcp/configs/save", `{
  "config": {
    "name": "alpha",
    "transport": "stdio",
    "command": "missing-binary",
    "trust": "trusted"
  }
}`)
	if createResp.Code != http.StatusOK {
		t.Fatalf("unexpected create status: %d body=%s", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.FileName != "alpha-2.json" {
		t.Fatalf("expected generated file_name alpha-2.json, got %q", createPayload.FileName)
	}

	applyResp := performJSONRequest(t, handler, http.MethodPost, "/v1/mcp/configs/apply", `{}`)
	if applyResp.Code != http.StatusOK {
		t.Fatalf("unexpected apply status: %d body=%s", applyResp.Code, applyResp.Body.String())
	}
	assertCustomServerNames(t, handler, "alpha")

	updateResp := performJSONRequest(t, handler, http.MethodPost, "/v1/mcp/configs/save", `{
  "file_name": "alpha-2.json",
  "raw_json": "{\"name\":\"beta\",\"transport\":\"stdio\",\"command\":\"missing-binary\",\"trust\":\"trusted\"}"
}`)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("unexpected update status: %d body=%s", updateResp.Code, updateResp.Body.String())
	}
	applyResp = performJSONRequest(t, handler, http.MethodPost, "/v1/mcp/configs/apply", `{}`)
	if applyResp.Code != http.StatusOK {
		t.Fatalf("unexpected apply status after update: %d body=%s", applyResp.Code, applyResp.Body.String())
	}
	assertCustomServerNames(t, handler, "beta")

	deleteResp := performJSONRequest(t, handler, http.MethodPost, "/v1/mcp/configs/delete", `{
  "file_name": "alpha-2.json"
}`)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: %d body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	assertCustomServerNames(t, handler, "beta")

	applyResp = performJSONRequest(t, handler, http.MethodPost, "/v1/mcp/configs/apply", `{}`)
	if applyResp.Code != http.StatusOK {
		t.Fatalf("unexpected apply status after delete: %d body=%s", applyResp.Code, applyResp.Body.String())
	}
	assertCustomServerNames(t, handler)
}

func assertCustomServerNames(t *testing.T, handler http.Handler, want ...string) {
	t.Helper()
	resp := performJSONRequest(t, handler, http.MethodGet, "/v1/mcp/servers", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected list servers status: %d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Servers []mcp.ServerInfo `json:"servers"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list servers response: %v", err)
	}

	var got []string
	for _, server := range payload.Servers {
		if server.Builtin {
			continue
		}
		got = append(got, server.Name)
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected custom server count: got=%v want=%v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("unexpected custom servers: got=%v want=%v", got, want)
		}
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
