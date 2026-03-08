package tooling

import (
	"fmt"
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

// trimPreview returns a head-only preview of input, suitable for short
// display strings (ToolEvent previews, error summaries).
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

// truncateHeadTail preserves the beginning and end of a long string,
// replacing the middle with a truncation marker. This keeps both the
// initial context (e.g. headers, setup) and the final result (e.g.
// output summary, error messages) which are typically most useful.
//
// Split ratio: 40% head, 40% tail, ~20% reserved for the marker.
func truncateHeadTail(input string, maxLen int) string {
	if maxLen <= 0 || len(input) <= maxLen {
		return input
	}
	// Reserve space for the truncation marker.
	markerBudget := 80
	usable := maxLen - markerBudget
	if usable < 200 {
		// Too small for head+tail — fall back to head-only.
		return input[:maxLen-4] + "..."
	}
	headLen := usable * 2 / 5
	tailLen := usable - headLen
	dropped := len(input) - headLen - tailLen
	marker := fmt.Sprintf("\n\n[...truncated %d characters...]\n\n", dropped)
	return input[:headLen] + marker + input[len(input)-tailLen:]
}
