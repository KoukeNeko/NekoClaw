package memory

import (
	"os"
	"path/filepath"
	"strings"
)

const memoryFileName = "MEMORY.md"

// MemoryContext holds the loaded memory content to be injected into chat.
// Only MEMORY.md is injected into the system prompt (passive).
// Daily logs and memory/*.md files are accessed on-demand via memory_search
// and memory_get tools, keeping per-request token cost low.
type MemoryContext struct {
	MemoryMD string // Content of MEMORY.md (injected every turn)
}

// IsEmpty returns true when there is no memory content to inject.
func (mc MemoryContext) IsEmpty() bool {
	return mc.MemoryMD == ""
}

// LoadMemoryContext reads MEMORY.md from the memory directory.
// Daily logs are NOT loaded here — they are accessed via tools on demand.
func LoadMemoryContext(memoryDir string) (MemoryContext, error) {
	if memoryDir == "" {
		return MemoryContext{}, nil
	}

	var ctx MemoryContext

	memoryPath := filepath.Join(memoryDir, memoryFileName)
	if content, err := os.ReadFile(memoryPath); err == nil {
		ctx.MemoryMD = strings.TrimSpace(string(content))
	}

	return ctx, nil
}

// BuildSystemPrompt formats the memory context as a system message.
// Only MEMORY.md is included. Daily memory files are available via
// memory_search and memory_get tools.
func BuildSystemPrompt(mc MemoryContext) string {
	if mc.IsEmpty() {
		return ""
	}
	return "# Long-term Memory\n\n" + mc.MemoryMD
}

// WriteMemoryMD writes or overwrites the MEMORY.md file.
func WriteMemoryMD(memoryDir, content string) error {
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(memoryDir, memoryFileName)
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}
