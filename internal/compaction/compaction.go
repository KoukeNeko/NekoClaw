package compaction

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/logger"
	"github.com/doeshing/nekoclaw/internal/provider"
)

var logCompact = logger.New("compact", logger.Cyan)

const (
	DefaultReserveTokens    = 16384
	defaultKeepRecentTokens = 20000
	charsPerToken           = 4
	entryEnvelopeOverhead   = 24
)

// Compactor performs LLM-based compaction of session history.
// When the context window is nearly full, it summarizes older messages
// using the LLM and produces a compaction entry that replaces them.
type Compactor struct {
	prov    provider.Provider
	model   string
	account core.Account
}

// CompactionRequest describes what to compact.
type CompactionRequest struct {
	Entries          []core.SessionEntry
	ContextWindow    int
	ReserveTokens    int
	KeepRecentTokens int
}

// CompactionResult is the output of a successful compaction.
type CompactionResult struct {
	KeptEntries     []core.SessionEntry
	CompactionEntry core.SessionEntry
	DroppedCount    int
	DroppedTokens   int
	SummaryTokens   int
}

func NewCompactor(prov provider.Provider, model string, account core.Account) *Compactor {
	return &Compactor{
		prov:    prov,
		model:   model,
		account: account,
	}
}

// ShouldCompact returns true when contextTokens exceeds the available budget.
func (c *Compactor) ShouldCompact(entries []core.SessionEntry, contextWindow, reserveTokens int) bool {
	if reserveTokens <= 0 {
		reserveTokens = DefaultReserveTokens
	}
	budget := contextWindow - reserveTokens
	if budget <= 0 {
		return false
	}
	current := EstimateEntriesTokens(entries)
	return current > budget
}

// Compact summarizes older entries using the LLM and returns the kept entries
// plus a new compaction entry.
func (c *Compactor) Compact(ctx context.Context, req CompactionRequest) (CompactionResult, error) {
	if req.ReserveTokens <= 0 {
		req.ReserveTokens = DefaultReserveTokens
	}
	if req.KeepRecentTokens <= 0 {
		req.KeepRecentTokens = defaultKeepRecentTokens
	}

	// Split entries into "to keep" (recent) and "to drop" (older).
	kept, dropped := splitByTokenBudget(req.Entries, req.KeepRecentTokens)
	if len(dropped) == 0 {
		return CompactionResult{KeptEntries: req.Entries}, nil
	}

	droppedTokens := EstimateEntriesTokens(dropped)

	// Generate LLM summary of dropped messages.
	summary, err := c.summarizeDropped(ctx, dropped)
	if err != nil {
		return CompactionResult{}, fmt.Errorf("compaction summarize failed: %w", err)
	}

	firstKeptID := ""
	if len(kept) > 0 {
		firstKeptID = kept[0].ID
	}

	compactionEntry := core.NewCompactionEntry(summary, len(dropped), droppedTokens, firstKeptID)

	return CompactionResult{
		KeptEntries:     kept,
		CompactionEntry: compactionEntry,
		DroppedCount:    len(dropped),
		DroppedTokens:   droppedTokens,
		SummaryTokens:   estimateStringTokens(summary),
	}, nil
}

// splitByTokenBudget keeps the newest entries that fit within keepTokens,
// returning (kept, dropped). Entries are processed from newest to oldest.
func splitByTokenBudget(entries []core.SessionEntry, keepTokens int) (kept, dropped []core.SessionEntry) {
	if keepTokens <= 0 {
		return nil, append([]core.SessionEntry(nil), entries...)
	}

	total := 0
	splitIdx := len(entries)
	for i := len(entries) - 1; i >= 0; i-- {
		t := EstimateEntryTokens(entries[i])
		if total+t > keepTokens && i < len(entries)-1 {
			splitIdx = i + 1
			break
		}
		total += t
		if i == 0 {
			splitIdx = 0
		}
	}

	if splitIdx == 0 {
		return append([]core.SessionEntry(nil), entries...), nil
	}

	dropped = append([]core.SessionEntry(nil), entries[:splitIdx]...)
	kept = append([]core.SessionEntry(nil), entries[splitIdx:]...)
	return kept, dropped
}

// ---------------------------------------------------------------------------
// Token estimation for SessionEntry
// ---------------------------------------------------------------------------

// EstimateEntryTokens estimates the token count for a single entry.
func EstimateEntryTokens(e core.SessionEntry) int {
	contentRunes := utf8.RuneCountInString(strings.TrimSpace(e.Content))
	roleRunes := utf8.RuneCountInString(string(e.Role))
	toolRunes := utf8.RuneCountInString(strings.TrimSpace(e.ToolName))
	summaryRunes := utf8.RuneCountInString(strings.TrimSpace(e.Summary))

	runeBudget := contentRunes + roleRunes + toolRunes + summaryRunes + entryEnvelopeOverhead
	t := (runeBudget + (charsPerToken - 1)) / charsPerToken
	if t < 1 {
		return 1
	}
	return t
}

// EstimateEntriesTokens estimates the total token count for a slice of entries.
func EstimateEntriesTokens(entries []core.SessionEntry) int {
	total := 0
	for _, e := range entries {
		total += EstimateEntryTokens(e)
	}
	if total == 0 {
		return 1
	}
	return total
}

func estimateStringTokens(s string) int {
	runes := utf8.RuneCountInString(strings.TrimSpace(s))
	t := (runes + entryEnvelopeOverhead + charsPerToken - 1) / charsPerToken
	if t < 1 {
		return 1
	}
	return t
}

// ---------------------------------------------------------------------------
// LLM summarization
// ---------------------------------------------------------------------------

func (c *Compactor) summarizeDropped(ctx context.Context, dropped []core.SessionEntry) (string, error) {
	prompt := buildSummarizationPrompt(dropped)

	resp, err := c.prov.Generate(ctx, provider.GenerateRequest{
		Model: c.model,
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: summarizationSystemPrompt},
			{Role: core.RoleUser, Content: prompt},
		},
		Account: c.account,
	})
	if err != nil {
		return "", err
	}

	summary := strings.TrimSpace(resp.Text)
	if summary == "" {
		return "History compacted (no summary generated).", nil
	}

	logCompact.Logf("summary: dropped=%d dropped_tokens=%d summary_len=%d",
		len(dropped), EstimateEntriesTokens(dropped), len(summary))

	return summary, nil
}

const summarizationSystemPrompt = `你是一個對話摘要助手。你的任務是摘要被壓縮移除的對話歷史。
請生成一個簡潔但完整的摘要，保留：
- 關鍵決策和結論
- 程式碼修改摘要（檔案名、函數名）
- 使用者的偏好和指令
- 未完成的任務
- 重要的技術細節

使用要點列表格式。用繁體中文回覆。`

func buildSummarizationPrompt(dropped []core.SessionEntry) string {
	var sb strings.Builder
	sb.WriteString("以下是即將被移除的對話歷史。請生成摘要：\n\n")
	for _, e := range dropped {
		if e.Type != core.EntryMessage {
			continue
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n", e.Role, truncateForPrompt(e.Content, 2000)))
	}
	return sb.String()
}

func truncateForPrompt(s string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes-1]) + "…"
}
