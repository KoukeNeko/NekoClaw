package auth

import (
	"strings"

	"github.com/doeshing/nekoclaw/internal/termclean"
)

func extractDisplayLinesFromCLIChunk(carry, chunk string) ([]string, string) {
	combined := carry + chunk
	if combined == "" {
		return nil, ""
	}
	lines := make([]string, 0, 4)
	start := 0
	for idx := 0; idx < len(combined); idx++ {
		b := combined[idx]
		if b != '\n' && b != '\r' {
			continue
		}
		if normalized := normalizeCLIEventLine(combined[start:idx]); normalized != "" {
			lines = append(lines, normalized)
		}
		start = idx + 1
	}
	remainder := combined[start:]
	// Keep carry bounded in case the child process writes very long lines.
	if len(remainder) > 8192 {
		remainder = remainder[len(remainder)-8192:]
	}
	return lines, remainder
}

func flushDisplayCLIEventCarry(carry string) string {
	return normalizeCLIEventLine(carry)
}

func normalizeCLIEventLine(line string) string {
	clean := termclean.StripTerminalControlSequences(line)
	clean = strings.TrimSpace(clean)
	return clean
}
