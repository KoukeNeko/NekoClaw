package main

import (
	"path/filepath"
	"testing"
)

func TestResolveDefaultLogFilePath_UsesHomeDefaultWhenAuthDirEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := resolveDefaultLogFilePath("")
	want := filepath.Join(home, ".nekoclaw", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(empty) = %q, want %q", got, want)
	}
}

func TestResolveDefaultLogFilePath_AuthDirParentRoot(t *testing.T) {
	got := resolveDefaultLogFilePath("/tmp/custom/auth")
	want := filepath.Join("/tmp/custom", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(auth) = %q, want %q", got, want)
	}
}

func TestResolveDefaultLogFilePath_NonAuthDirRoot(t *testing.T) {
	got := resolveDefaultLogFilePath("/tmp/custom-state")
	want := filepath.Join("/tmp/custom-state", "logs", "nekoclaw.log")
	if got != want {
		t.Fatalf("resolveDefaultLogFilePath(non-auth) = %q, want %q", got, want)
	}
}
