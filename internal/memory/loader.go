package memory

import (
	"os"
	"path/filepath"
	"strings"
)

const memoryFileName = "MEMORY.md"

// MemoryContext holds the loaded memory content to be injected into chat.
type MemoryContext struct {
	MemoryMD  string // Content of MEMORY.md
	DailyLogs string // Recent daily logs (today + yesterday)
}

// IsEmpty returns true when there is no memory content to inject.
func (mc MemoryContext) IsEmpty() bool {
	return mc.MemoryMD == "" && mc.DailyLogs == ""
}

// LoadMemoryContext reads MEMORY.md and recent daily logs from the memory
// directory. Missing files are silently skipped.
func LoadMemoryContext(memoryDir string) (MemoryContext, error) {
	if memoryDir == "" {
		return MemoryContext{}, nil
	}

	var ctx MemoryContext

	memoryPath := filepath.Join(memoryDir, memoryFileName)
	if content, err := os.ReadFile(memoryPath); err == nil {
		ctx.MemoryMD = strings.TrimSpace(string(content))
	}

	logs, err := LoadRecentLogs(memoryDir, 2)
	if err != nil {
		return ctx, err
	}
	ctx.DailyLogs = logs

	return ctx, nil
}

// BuildSystemPrompt formats the memory context as a system message.
func BuildSystemPrompt(mc MemoryContext) string {
	if mc.IsEmpty() {
		return ""
	}

	var parts []string
	if mc.MemoryMD != "" {
		parts = append(parts, "# Long-term Memory\n\n"+mc.MemoryMD)
	}
	if mc.DailyLogs != "" {
		parts = append(parts, "# Recent Activity\n\n"+mc.DailyLogs)
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// WriteMemoryMD writes or overwrites the MEMORY.md file.
func WriteMemoryMD(memoryDir, content string) error {
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(memoryDir, memoryFileName)
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}
