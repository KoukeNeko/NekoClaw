package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

func TestSnapshotsUndoRestoresWorkspaceAndPersistsGrant(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "tmp.txt"), []byte("before"), 0o644); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	permStore, err := core.NewPermissionStore(filepath.Join(t.TempDir(), "permissions"))
	if err != nil {
		t.Fatalf("permission store: %v", err)
	}
	snapshotStore, err := core.NewSnapshotStore(filepath.Join(t.TempDir(), "snapshots"))
	if err != nil {
		t.Fatalf("snapshot store: %v", err)
	}
	svc := app.NewService(app.ServiceOptions{
		WorkspaceRoot:   workspace,
		PermissionStore: permStore,
		SnapshotStore:   snapshotStore,
	})
	svc.RegisterProvider(&fakeToolChatProvider{})
	svc.RegisterPool(core.NewAccountPool("anthropic", []core.Account{{
		ID:       "anthropic-main",
		Provider: "anthropic",
		Type:     core.AccountToken,
		Token:    provider.AnthropicSetupTokenPrefix + strings.Repeat("a", provider.AnthropicSetupTokenMinLength),
	}}, nil, core.DefaultCooldownConfig()))

	handler := NewServer(svc).Handler()

	firstReq := `{"session_id":"tool-1","surface":"web","provider":"anthropic","model":"default","message":"write file","enable_tools":true}`
	firstResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", firstResp.Code, firstResp.Body.String())
	}
	var firstPayload core.ChatResponse
	if err := json.Unmarshal(firstResp.Body.Bytes(), &firstPayload); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	secondReq := `{"session_id":"tool-1","surface":"web","provider":"anthropic","model":"default","enable_tools":true,"run_id":"` + firstPayload.RunID + `","tool_approvals":[{"approval_id":"call-1","decision":"allow_for_workspace"}]}`
	secondResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", secondReq)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", secondResp.Code, secondResp.Body.String())
	}
	if got := string(mustReadFile(t, filepath.Join(workspace, "tmp.txt"))); got != "hello" {
		t.Fatalf("workspace file = %q, want hello", got)
	}
	if got := len(svc.ListPermissions()); got != 1 {
		t.Fatalf("permission grants = %d, want 1", got)
	}

	snapshotsResp := performJSONRequest(t, handler, http.MethodGet, "/v1/snapshots?session_id=tool-1", "")
	if snapshotsResp.Code != http.StatusOK {
		t.Fatalf("list snapshots status=%d body=%s", snapshotsResp.Code, snapshotsResp.Body.String())
	}
	var listPayload struct {
		Snapshots []core.SnapshotRecord `json:"snapshots"`
	}
	if err := json.Unmarshal(snapshotsResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode snapshots: %v", err)
	}
	if len(listPayload.Snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(listPayload.Snapshots))
	}

	undoResp := performJSONRequest(t, handler, http.MethodPost, "/v1/snapshots/"+listPayload.Snapshots[0].ID+"/undo", "")
	if undoResp.Code != http.StatusOK {
		t.Fatalf("undo status=%d body=%s", undoResp.Code, undoResp.Body.String())
	}
	if got := string(mustReadFile(t, filepath.Join(workspace, "tmp.txt"))); got != "before" {
		t.Fatalf("workspace file after undo = %q, want before", got)
	}

	transcriptResp := performJSONRequest(t, handler, http.MethodGet, "/v1/sessions/transcript?session_id=tool-1", "")
	if transcriptResp.Code != http.StatusOK || !strings.Contains(transcriptResp.Body.String(), "已還原 snapshot") {
		t.Fatalf("transcript missing undo note: code=%d body=%s", transcriptResp.Code, transcriptResp.Body.String())
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
