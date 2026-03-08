package contextwindow

import (
	"fmt"
	"strings"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/tokenutil"
)

const (
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
	originalTokens := EstimateMessagesTokens(working)
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
	effectiveBudget := ApplySafetyMargin(budgetTokens)
	if effectiveBudget < 1 {
		effectiveBudget = 1
	}

	var extraDropped []core.Message
	working, extraDropped = KeepNewest(working, effectiveBudget)
	if len(extraDropped) > 0 {
		droppedMessagesList = append(droppedMessagesList, extraDropped...)
		droppedMessages += len(extraDropped)
	}

	if droppedMessages > 0 {
		summaryMessage := core.Message{
			Role:    core.RoleSystem,
			Content: buildDroppedHistorySummary(droppedMessagesList, droppedMessages),
		}
		remainingBudget := effectiveBudget - EstimateMessageTokens(summaryMessage)
		if remainingBudget < 1 {
			working = []core.Message{}
		} else {
			var fitDropped []core.Message
			working, fitDropped = KeepNewest(working, remainingBudget)
			if len(fitDropped) > 0 {
				droppedMessagesList = append(droppedMessagesList, fitDropped...)
				droppedMessages += len(fitDropped)
				summaryMessage.Content = buildDroppedHistorySummary(droppedMessagesList, droppedMessages)
			}
		}
		working = append([]core.Message{summaryMessage}, working...)
	}

	compressedTokens := EstimateMessagesTokens(working)
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

// ApplySafetyMargin reduces a token budget by the standard safety factor
// (1.2x divisor) to provide headroom for estimation inaccuracies.
func ApplySafetyMargin(tokens int) int {
	if tokens <= 0 {
		return 1
	}
	effective := int(float64(tokens) / tokenSafetyMargin)
	if effective < 1 {
		return 1
	}
	return effective
}

// KeepNewest retains the most recent messages that fit within the given
// token budget, dropping older messages from the front. It returns the
// kept messages and the dropped messages separately.
func KeepNewest(messages []core.Message, budgetTokens int) ([]core.Message, []core.Message) {
	if budgetTokens <= 0 {
		return []core.Message{}, append([]core.Message(nil), messages...)
	}

	total := 0
	result := make([]core.Message, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := EstimateMessageTokens(messages[i])
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

	totalTokens := EstimateMessagesTokens(messages)
	targetTokens := float64(totalTokens) / float64(parts)
	chunks := make([][]core.Message, 0, parts)
	current := make([]core.Message, 0)
	currentTokens := 0

	for _, message := range messages {
		messageTokens := EstimateMessageTokens(message)
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

	for len(keptMessages) > 0 && EstimateMessagesTokens(keptMessages) > budgetTokens {
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
		droppedTokens += EstimateMessagesTokens(droppedChunk)
		allDropped = append(allDropped, droppedChunk...)
		keptMessages = kept
	}

	return pruneResult{
		Messages:            keptMessages,
		DroppedMessagesList: allDropped,
		DroppedChunks:       droppedChunks,
		DroppedMessages:     droppedMessages,
		DroppedTokens:       droppedTokens,
		KeptTokens:          EstimateMessagesTokens(keptMessages),
		BudgetTokens:        budgetTokens,
	}
}

func buildDroppedHistorySummary(dropped []core.Message, droppedMessages int) string {
	if droppedMessages <= 0 {
		return "History compacted."
	}
	droppedTokens := EstimateMessagesTokens(dropped)
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

// EstimateMessagesTokens estimates total tokens for a slice of messages
// using CJK-aware counting (delegated to tokenutil).
func EstimateMessagesTokens(messages []core.Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateMessageTokens(msg)
	}
	if total == 0 {
		return 1
	}
	return total
}

// EstimateMessageTokens estimates tokens for a single message using
// CJK-aware counting for the content, plus image and tool overhead.
func EstimateMessageTokens(msg core.Message) int {
	// Combine all text fields for CJK-aware estimation with envelope overhead.
	combined := strings.TrimSpace(msg.Content) + " " + string(msg.Role) + " " + strings.TrimSpace(msg.ToolName)
	t := tokenutil.EstimateStringWithOverhead(combined)

	// Account for image data: base64 is ~1.33× the raw bytes, and typical vision
	// models charge ~85 tokens per 512×512 tile. A rough heuristic: treat each
	// image's base64 length ÷ 750 as the estimated token cost.
	for _, img := range msg.Images {
		imageTokens := len(img.Data) / 750
		if imageTokens < 85 {
			imageTokens = 85 // minimum cost for any image
		}
		t += imageTokens
	}

	if msg.Role == core.RoleTool {
		t += 4
	}
	if t < 1 {
		return 1
	}
	return t
}
