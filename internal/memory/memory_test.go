package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDailyLogPath(t *testing.T) {
	date := time.Date(2026, 3, 2, 14, 30, 0, 0, time.UTC)
	path := DailyLogPath("/tmp/memory", date)
	expected := "/tmp/memory/2026-03-02.md"
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}
}

func TestAppendDailyLog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")

	if err := AppendDailyLog(dir, "First entry"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := AppendDailyLog(dir, "Second entry"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := DailyLogPath(dir, time.Now())
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read daily log: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "First entry") {
		t.Fatalf("expected 'First entry' in log, got: %s", s)
	}
	if !strings.Contains(s, "Second entry") {
		t.Fatalf("expected 'Second entry' in log, got: %s", s)
	}
}

func TestAppendDailyLog_EmptyContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")

	if err := AppendDailyLog(dir, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := AppendDailyLog(dir, "   "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should not exist since nothing was written.
	path := DailyLogPath(dir, time.Now())
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no file for empty content")
	}
}

func TestLoadRecentLogs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write today's log.
	todayPath := DailyLogPath(dir, time.Now())
	if err := os.WriteFile(todayPath, []byte("Today's notes"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write yesterday's log.
	yesterdayPath := DailyLogPath(dir, time.Now().AddDate(0, 0, -1))
	if err := os.WriteFile(yesterdayPath, []byte("Yesterday's notes"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logs, err := LoadRecentLogs(dir, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(logs, "Today's notes") {
		t.Fatalf("expected today's notes in logs")
	}
	if !strings.Contains(logs, "Yesterday's notes") {
		t.Fatalf("expected yesterday's notes in logs")
	}
}

func TestLoadRecentLogs_Empty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")

	logs, err := LoadRecentLogs(dir, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logs != "" {
		t.Fatalf("expected empty logs from empty dir, got: %q", logs)
	}
}

func TestLoadMemoryContext_Empty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")

	ctx, err := LoadMemoryContext(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ctx.IsEmpty() {
		t.Fatalf("expected empty context")
	}
}

func TestLoadMemoryContext_WithFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write MEMORY.md
	memPath := filepath.Join(dir, "MEMORY.md")
	if err := os.WriteFile(memPath, []byte("Long-term memory content"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write today's daily log.
	todayPath := DailyLogPath(dir, time.Now())
	if err := os.WriteFile(todayPath, []byte("Today's activity"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, err := LoadMemoryContext(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.IsEmpty() {
		t.Fatalf("expected non-empty context")
	}
	if ctx.MemoryMD != "Long-term memory content" {
		t.Fatalf("expected MEMORY.md content, got %q", ctx.MemoryMD)
	}
	// Daily logs are NOT injected into context (accessed via tools instead).
}

func TestLoadMemoryContext_EmptyDir(t *testing.T) {
	ctx, err := LoadMemoryContext("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ctx.IsEmpty() {
		t.Fatalf("expected empty context for empty dir")
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	mc := MemoryContext{
		MemoryMD: "Remember: user prefers Go",
	}
	prompt := BuildSystemPrompt(mc)
	if !strings.Contains(prompt, "Long-term Memory") {
		t.Fatalf("expected Long-term Memory header")
	}
	if !strings.Contains(prompt, "user prefers Go") {
		t.Fatalf("expected MEMORY.md content in prompt")
	}
	// Daily logs should NOT be in the system prompt (tool-based access only).
	if strings.Contains(prompt, "Recent Activity") {
		t.Fatalf("daily logs should not be injected into system prompt")
	}
}

func TestBuildSystemPrompt_Empty(t *testing.T) {
	prompt := BuildSystemPrompt(MemoryContext{})
	if prompt != "" {
		t.Fatalf("expected empty prompt for empty context, got %q", prompt)
	}
}

func TestWriteMemoryMD(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")

	if err := WriteMemoryMD(dir, "Test memory content"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := filepath.Join(dir, "MEMORY.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read MEMORY.md: %v", err)
	}
	if strings.TrimSpace(string(content)) != "Test memory content" {
		t.Fatalf("expected 'Test memory content', got %q", string(content))
	}
}
