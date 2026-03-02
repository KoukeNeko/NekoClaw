package contextwindow

import (
	"strings"

	"github.com/doeshing/nekoclaw/internal/core"
)

const charsPerToken = 4

type Policy struct {
	MaxContextTokens     int
	ReserveTokens        int
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
		MaxContextTokens:     contextWindow,
		ReserveTokens:        2048,
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

	working := append([]core.Message(nil), messages...)
	originalTokens := estimateMessagesTokens(working)
	maxChars := policy.MaxContextTokens * charsPerToken
	if maxChars <= 0 {
		return working, core.CompressionMeta{OriginalTokens: originalTokens, CompressedTokens: originalTokens}, false
	}

	totalChars := estimateMessagesChars(working)
	softTrimmed := 0
	hardCleared := 0
	if float64(totalChars) >= policy.SoftTrimRatio*float64(maxChars) {
		working, softTrimmed = softTrimToolResults(working, policy)
		totalChars = estimateMessagesChars(working)
	}
	if float64(totalChars) >= policy.HardClearRatio*float64(maxChars) {
		working, hardCleared = hardClearToolResults(working, policy)
		totalChars = estimateMessagesChars(working)
	}

	budgetTokens := policy.MaxContextTokens - policy.ReserveTokens
	if budgetTokens < 256 {
		budgetTokens = 256
	}
	sliding, dropped := slidingWindow(working, budgetTokens)
	compressedTokens := estimateMessagesTokens(sliding)
	meta := core.CompressionMeta{
		OriginalTokens:   originalTokens,
		CompressedTokens: compressedTokens,
		DroppedMessages:  dropped,
		SoftTrimmed:      softTrimmed,
		HardCleared:      hardCleared,
	}
	compressed := dropped > 0 || softTrimmed > 0 || hardCleared > 0
	return sliding, meta, compressed
}

func slidingWindow(messages []core.Message, budgetTokens int) ([]core.Message, int) {
	if budgetTokens <= 0 {
		return []core.Message{}, len(messages)
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
	dropped := len(messages) - len(result)
	if dropped <= 0 {
		return result, 0
	}
	prefix := core.Message{
		Role:    core.RoleSystem,
		Content: "[Older history omitted by sliding-window compaction to stay within context limits.]",
	}
	return append([]core.Message{prefix}, result...), dropped
}

func softTrimToolResults(messages []core.Message, policy Policy) ([]core.Message, int) {
	cutoff := assistantCutoff(messages, policy.KeepLastAssistants)
	if cutoff <= 0 {
		return messages, 0
	}
	count := 0
	out := append([]core.Message(nil), messages...)
	for i := 0; i < cutoff; i++ {
		msg := out[i]
		if msg.Role != core.RoleTool {
			continue
		}
		if len(msg.Content) <= policy.ToolSoftTrimMax {
			continue
		}
		head := takeHead(msg.Content, policy.ToolSoftTrimHead)
		tail := takeTail(msg.Content, policy.ToolSoftTrimTail)
		out[i].Content = strings.TrimSpace(head + "\n...\n" + tail + "\n\n[tool result trimmed]")
		count++
	}
	return out, count
}

func hardClearToolResults(messages []core.Message, policy Policy) ([]core.Message, int) {
	cutoff := assistantCutoff(messages, policy.KeepLastAssistants)
	if cutoff <= 0 {
		return messages, 0
	}
	count := 0
	out := append([]core.Message(nil), messages...)
	for i := 0; i < cutoff; i++ {
		if out[i].Role != core.RoleTool {
			continue
		}
		out[i].Content = policy.HardClearPlaceholder
		count++
	}
	return out, count
}

func assistantCutoff(messages []core.Message, keepLast int) int {
	if keepLast <= 0 {
		return len(messages)
	}
	remaining := keepLast
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != core.RoleAssistant {
			continue
		}
		remaining--
		if remaining == 0 {
			return i
		}
	}
	return 0
}

func estimateMessagesChars(messages []core.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)
	}
	return total
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
	t := len(msg.Content) / charsPerToken
	if t < 1 {
		return 1
	}
	return t
}

func takeHead(text string, n int) string {
	if n <= 0 || len(text) <= n {
		return text
	}
	return text[:n]
}

func takeTail(text string, n int) string {
	if n <= 0 || len(text) <= n {
		return text
	}
	return text[len(text)-n:]
}
