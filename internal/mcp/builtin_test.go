package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBuiltinDefsUsesNpxFallbackWhenPlaywrightMCPMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	defs := BuiltinDefs()
	if len(defs) == 0 {
		t.Fatal("expected builtin definitions")
	}

	cfg := defs[0].Config
	if cfg.Command != "npx" {
		t.Fatalf("expected npx fallback, got %q", cfg.Command)
	}
	if len(cfg.Args) < 2 || cfg.Args[0] != "-y" || cfg.Args[1] != "@playwright/mcp@latest" {
		t.Fatalf("unexpected fallback args: %#v", cfg.Args)
	}
}

func TestBuiltinDefsPrefersInstalledPlaywrightMCPBinary(t *testing.T) {
	tempDir := t.TempDir()
	binName := "playwright-mcp"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(tempDir, binName)

	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	t.Setenv("PATH", tempDir)

	defs := BuiltinDefs()
	if len(defs) == 0 {
		t.Fatal("expected builtin definitions")
	}

	cfg := defs[0].Config
	if cfg.Command != binPath {
		t.Fatalf("expected installed binary %q, got %q", binPath, cfg.Command)
	}
	if len(cfg.Args) != 2 || cfg.Args[0] != "--headless" || cfg.Args[1] != "--vision" {
		t.Fatalf("unexpected installed binary args: %#v", cfg.Args)
	}
}
