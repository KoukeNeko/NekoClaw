package contextwindow

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/doeshing/nekoclaw/internal/core"
)

const (
	charsPerToken          = 4
	tokenSafetyMargin      = 1.2
	defaultMaxHistoryShare = 0.5
	defaultPruneParts      = 2
	minSlidingBudgetTokens = 256
	maxDroppedPreviewLines = 3
	maxDroppedPreviewChars = 160
)

type Policy struct {
	MaxContextTokens int
	ReserveTokens    int
	MaxHistoryShare  float64
	PruneParts       int

	// Legacy fields kept for compatibility with prior configuration shape.
	// OpenClaw-aligned compaction no longer uses these soft/hard tool-trim stages.
	KeepLastAssistants   int
	SoftTrimRatio        float64
	HardClearRatio       float64
	ToolSoftTrimMax      int
	ToolSoftTrimHead     int
	ToolSoftTrimTail     int
	HardClearPlaceholder string
}

func DefaultPolicy(contextWindow int) Policy {
	if contextWindow <= 0 {
		contextWindow = 128000
	}
	return Policy{
		MaxContextTokens: contextWindow,
		ReserveTokens:    2048,
		MaxHistoryShare:  defaultMaxHistoryShare,
		PruneParts:       defaultPruneParts,

		KeepLastAssistants:   3,
		SoftTrimRatio:        0.30,
		HardClearRatio:       0.50,
		ToolSoftTrimMax:      4000,
		ToolSoftTrimHead:     1500,
		ToolSoftTrimTail:     1500,
		HardClearPlaceholder: "[Old tool result content cleared]",
	}
}

func Compress(messages []core.Message, policy Policy) ([]core.Message, core.CompressionMeta, bool) {
	if policy.MaxContextTokens <= 0 {
		policy = DefaultPolicy(128000)
	}
	if policy.ReserveTokens < 0 {
		policy.ReserveTokens = 0
	}
	if policy.MaxHistoryShare <= 0 || policy.MaxHistoryShare > 1 {
		policy.MaxHistoryShare = defaultMaxHistoryShare
	}
	if policy.PruneParts < 2 {
		policy.PruneParts = defaultPruneParts
	}

	working := append([]core.Message(nil), messages...)
	originalTokens := estimateMessagesTokens(working)
	if policy.MaxContextTokens <= 0 {
		return working, core.CompressionMeta{OriginalTokens: originalTokens, CompressedTokens: originalTokens}, false
	}
	if len(working) == 0 {
		return working, core.CompressionMeta{OriginalTokens: originalTokens, CompressedTokens: originalTokens}, false
	}

	pruned := pruneHistoryForContextShare(working, policy.MaxContextTokens, policy.MaxHistoryShare, policy.PruneParts)
	working = pruned.Messages
	droppedMessagesList := append([]core.Message(nil), pruned.DroppedMessagesList...)
	droppedMessages := pruned.DroppedMessages

	budgetTokens := policy.MaxContextTokens - policy.ReserveTokens
	if budgetTokens < minSlidingBudgetTokens {
		budgetTokens = minSlidingBudgetTokens
	}
	effectiveBudget := applySafetyMargin(budgetTokens)
	if effectiveBudget < 1 {
		effectiveBudget = 1
	}

	var extraDropped []core.Message
	working, extraDropped = keepNewestByTokenBudget(working, effectiveBudget)
	if len(extraDropped) > 0 {
		droppedMessagesList = append(droppedMessagesList, extraDropped...)
		droppedMessages += len(extraDropped)
	}

	if droppedMessages > 0 {
		summaryMessage := core.Message{
			Role:    core.RoleSystem,
			Content: buildDroppedHistorySummary(droppedMessagesList, droppedMessages),
		}
		remainingBudget := effectiveBudget - estimateMessageTokens(summaryMessage)
		if remainingBudget < 1 {
			working = []core.Message{}
		} else {
			var fitDropped []core.Message
			working, fitDropped = keepNewestByTokenBudget(working, remainingBudget)
			if len(fitDropped) > 0 {
				droppedMessagesList = append(droppedMessagesList, fitDropped...)
				droppedMessages += len(fitDropped)
				summaryMessage.Content = buildDroppedHistorySummary(droppedMessagesList, droppedMessages)
			}
		}
		working = append([]core.Message{summaryMessage}, working...)
	}

	compressedTokens := estimateMessagesTokens(working)
	meta := core.CompressionMeta{
		OriginalTokens:   originalTokens,
		CompressedTokens: compressedTokens,
		DroppedMessages:  droppedMessages,
		SoftTrimmed:      0,
		HardCleared:      0,
	}
	compressed := droppedMessages > 0
	return working, meta, compressed
}

type pruneResult struct {
	Messages            []core.Message
	DroppedMessagesList []core.Message
	DroppedChunks       int
	DroppedMessages     int
	DroppedTokens       int
	KeptTokens          int
	BudgetTokens        int
}

func applySafetyMargin(tokens int) int {
	if tokens <= 0 {
		return 1
	}
	effective := int(float64(tokens) / tokenSafetyMargin)
	if effective < 1 {
		return 1
	}
	return effective
}

func keepNewestByTokenBudget(messages []core.Message, budgetTokens int) ([]core.Message, []core.Message) {
	if budgetTokens <= 0 {
		return []core.Message{}, append([]core.Message(nil), messages...)
	}

	total := 0
	result := make([]core.Message, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := estimateMessageTokens(messages[i])
		if len(result) > 0 && total+msgTokens > budgetTokens {
			break
		}
		total += msgTokens
		result = append(result, messages[i])
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	droppedCount := len(messages) - len(result)
	if droppedCount <= 0 {
		return result, nil
	}
	dropped := append([]core.Message(nil), messages[:droppedCount]...)
	return result, dropped
}

func splitMessagesByTokenShare(messages []core.Message, parts int) [][]core.Message {
	if len(messages) == 0 {
		return nil
	}
	parts = normalizeParts(parts, len(messages))
	if parts <= 1 {
		return [][]core.Message{append([]core.Message(nil), messages...)}
	}

	totalTokens := estimateMessagesTokens(messages)
	targetTokens := float64(totalTokens) / float64(parts)
	chunks := make([][]core.Message, 0, parts)
	current := make([]core.Message, 0)
	currentTokens := 0

	for _, message := range messages {
		messageTokens := estimateMessageTokens(message)
		if len(chunks) < parts-1 && len(current) > 0 && float64(currentTokens+messageTokens) > targetTokens {
			chunks = append(chunks, current)
			current = make([]core.Message, 0)
			currentTokens = 0
		}
		current = append(current, message)
		currentTokens += messageTokens
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

func normalizeParts(parts int, messageCount int) int {
	if parts <= 1 {
		return 1
	}
	if parts > messageCount {
		return messageCount
	}
	return parts
}

func pruneHistoryForContextShare(
	messages []core.Message,
	maxContextTokens int,
	maxHistoryShare float64,
	parts int,
) pruneResult {
	if len(messages) == 0 {
		return pruneResult{Messages: []core.Message{}, BudgetTokens: 1}
	}
	if maxHistoryShare <= 0 || maxHistoryShare > 1 {
		maxHistoryShare = defaultMaxHistoryShare
	}
	if maxContextTokens <= 0 {
		maxContextTokens = 1
	}

	budgetTokens := int(float64(maxContextTokens) * maxHistoryShare)
	if budgetTokens < 1 {
		budgetTokens = 1
	}

	keptMessages := append([]core.Message(nil), messages...)
	allDropped := make([]core.Message, 0)
	droppedChunks := 0
	droppedMessages := 0
	droppedTokens := 0
	parts = normalizeParts(parts, len(keptMessages))

	for len(keptMessages) > 0 && estimateMessagesTokens(keptMessages) > budgetTokens {
		chunks := splitMessagesByTokenShare(keptMessages, parts)
		if len(chunks) <= 1 {
			break
		}
		droppedChunk := chunks[0]
		kept := make([]core.Message, 0, len(keptMessages)-len(droppedChunk))
		for _, chunk := range chunks[1:] {
			kept = append(kept, chunk...)
		}
		droppedChunks++
		droppedMessages += len(droppedChunk)
		droppedTokens += estimateMessagesTokens(droppedChunk)
		allDropped = append(allDropped, droppedChunk...)
		keptMessages = kept
	}

	return pruneResult{
		Messages:            keptMessages,
		DroppedMessagesList: allDropped,
		DroppedChunks:       droppedChunks,
		DroppedMessages:     droppedMessages,
		DroppedTokens:       droppedTokens,
		KeptTokens:          estimateMessagesTokens(keptMessages),
		BudgetTokens:        budgetTokens,
	}
}

func buildDroppedHistorySummary(dropped []core.Message, droppedMessages int) string {
	if droppedMessages <= 0 {
		return "History compacted."
	}
	droppedTokens := estimateMessagesTokens(dropped)
	lines := []string{
		fmt.Sprintf(
			"History compacted: dropped %d older messages (~%d tokens) to stay within context limits.",
			droppedMessages,
			droppedTokens,
		),
	}
	preview := buildDroppedPreviewLines(dropped, maxDroppedPreviewLines)
	if len(preview) > 0 {
		lines = append(lines, "Dropped preview:")
		lines = append(lines, preview...)
	}
	return strings.Join(lines, "\n")
}

func buildDroppedPreviewLines(dropped []core.Message, maxLines int) []string {
	if len(dropped) == 0 || maxLines <= 0 {
		return nil
	}
	start := len(dropped) - maxLines
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, len(dropped)-start)
	for _, msg := range dropped[start:] {
		snippet := inlineSnippet(msg.Content, maxDroppedPreviewChars)
		if snippet == "" {
			snippet = "<empty>"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", msg.Role, snippet))
	}
	return lines
}

func inlineSnippet(text string, maxChars int) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if maxChars <= 0 {
		return compact
	}
	runes := []rune(compact)
	if len(runes) <= maxChars {
		return compact
	}
	if maxChars == 1 {
		return "…"
	}
	return string(runes[:maxChars-1]) + "…"
}

func estimateMessagesTokens(messages []core.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateMessageTokens(msg)
	}
	if total == 0 {
		return 1
	}
	return total
}

func estimateMessageTokens(msg core.Message) int {
	contentRunes := utf8.RuneCountInString(strings.TrimSpace(msg.Content))
	roleRunes := utf8.RuneCountInString(string(msg.Role))
	toolRunes := utf8.RuneCountInString(strings.TrimSpace(msg.ToolName))

	// OpenClaw-style accounting includes message envelope overhead (role, wrapper
	// structure, and per-message framing), not only plain content length.
	runeBudget := contentRunes + roleRunes + toolRunes + 24
	t := (runeBudget + (charsPerToken - 1)) / charsPerToken
	if msg.Role == core.RoleTool {
		t += 4
	}
	if t < 1 {
		return 1
	}
	return t
}
