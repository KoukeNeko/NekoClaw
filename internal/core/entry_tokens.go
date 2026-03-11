package core

import (
	"strings"

	"github.com/doeshing/nekoclaw/internal/tokenutil"
)

// EstimateSessionEntryTokens approximates the token cost of a transcript entry.
func EstimateSessionEntryTokens(e SessionEntry) int {
	combined := strings.TrimSpace(e.Content) + " " +
		string(e.Role) + " " +
		strings.TrimSpace(e.ToolName) + " " +
		strings.TrimSpace(e.Summary)
	return tokenutil.EstimateStringWithOverhead(combined)
}

// EstimateSessionEntriesTokens approximates the total token cost of transcript entries.
func EstimateSessionEntriesTokens(entries []SessionEntry) int {
	total := 0
	for _, e := range entries {
		total += EstimateSessionEntryTokens(e)
	}
	if total == 0 {
		return 1
	}
	return total
}
