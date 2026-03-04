package tooling

import (
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

func newToolEvent(toolCallID, toolName, phase string, mutating bool) core.ToolEvent {
	return core.ToolEvent{
		At:         time.Now(),
		ToolCallID: strings.TrimSpace(toolCallID),
		ToolName:   strings.TrimSpace(toolName),
		Phase:      strings.TrimSpace(phase),
		Mutating:   mutating,
	}
}

func trimPreview(input string, max int) string {
	if max <= 0 {
		max = 200
	}
	trimmed := strings.TrimSpace(input)
	if len(trimmed) <= max {
		return trimmed
	}
	return trimmed[:max] + "..."
}
