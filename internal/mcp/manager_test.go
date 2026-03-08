package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerApplyCustomConfigsReconcilesFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manager := NewManager(dir)

	fileName, err := SaveConfigDocument(dir, "", &ServerConfig{
		Name:      "alpha",
		Transport: TransportStdio,
		Command:   "missing-binary",
		Trust:     TrustTrusted,
	}, "")
	if err != nil {
		t.Fatalf("save alpha config: %v", err)
	}

	if err := manager.ApplyCustomConfigs(ctx); err != nil {
		t.Fatalf("apply alpha: %v", err)
	}
	assertServerNames(t, manager.Servers(), "alpha")

	if _, err := SaveConfigDocument(dir, fileName, &ServerConfig{
		Name:      "beta",
		Transport: TransportStdio,
		Command:   "missing-binary",
		Trust:     TrustTrusted,
	}, ""); err != nil {
		t.Fatalf("update beta config: %v", err)
	}
	if err := manager.ApplyCustomConfigs(ctx); err != nil {
		t.Fatalf("apply beta: %v", err)
	}
	assertServerNames(t, manager.Servers(), "beta")

	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{"), 0o644); err != nil {
		t.Fatalf("write invalid json: %v", err)
	}
	if err := manager.ApplyCustomConfigs(ctx); err != nil {
		t.Fatalf("apply invalid json: %v", err)
	}
	assertServerNames(t, manager.Servers())

	if _, err := SaveConfigDocument(dir, fileName, &ServerConfig{
		Name:      "gamma",
		Transport: TransportStdio,
		Command:   "missing-binary",
		Trust:     TrustUntrusted,
	}, ""); err != nil {
		t.Fatalf("save gamma config: %v", err)
	}
	if err := manager.ApplyCustomConfigs(ctx); err != nil {
		t.Fatalf("apply gamma: %v", err)
	}
	assertServerNames(t, manager.Servers(), "gamma")

	if err := DeleteConfigDocument(dir, fileName); err != nil {
		t.Fatalf("delete gamma config: %v", err)
	}
	if err := manager.ApplyCustomConfigs(ctx); err != nil {
		t.Fatalf("apply delete: %v", err)
	}
	assertServerNames(t, manager.Servers())
}

func TestManagerApplyCustomConfigsKeepsBuiltinState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manager := NewManager(dir)

	defs := BuiltinDefs()
	if len(defs) == 0 {
		t.Fatal("expected at least one builtin definition")
	}
	builtinName := defs[0].Name

	if err := manager.SetBuiltinEnabled(ctx, builtinName, false); err != nil {
		t.Fatalf("disable builtin: %v", err)
	}
	if _, err := SaveConfigDocument(dir, "", &ServerConfig{
		Name:      "alpha",
		Transport: TransportStdio,
		Command:   "missing-binary",
		Trust:     TrustTrusted,
	}, ""); err != nil {
		t.Fatalf("save alpha config: %v", err)
	}
	if err := manager.ApplyCustomConfigs(ctx); err != nil {
		t.Fatalf("apply custom configs: %v", err)
	}

	builtinStates := manager.BuiltinServers()
	var matched bool
	for _, builtin := range builtinStates {
		if builtin.Name == builtinName {
			matched = true
			if builtin.Enabled {
				t.Fatalf("expected builtin %s to remain disabled after apply", builtinName)
			}
		}
	}
	if !matched {
		t.Fatalf("builtin %s not found", builtinName)
	}

	assertServerNames(t, manager.Servers(), "alpha")
}

func assertServerNames(t *testing.T, servers []ServerInfo, want ...string) {
	t.Helper()
	if len(servers) != len(want) {
		t.Fatalf("unexpected server count: got=%d want=%d (%v)", len(servers), len(want), servers)
	}
	for index := range servers {
		if servers[index].Name != want[index] {
			t.Fatalf("unexpected server order: got=%v want=%v", servers, want)
		}
	}
}
