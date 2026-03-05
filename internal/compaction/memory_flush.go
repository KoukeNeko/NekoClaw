package compaction

import (
	"context"
	"strings"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/memory"
	"github.com/doeshing/nekoclaw/internal/provider"
)

const (
	noReplyMarker           = "NO_REPLY"
	defaultSoftThresholdGap = 24000
)

// MemoryFlusher performs a "silent agent turn" before compaction to extract
// durable notes from the conversation into daily logs.
type MemoryFlusher struct {
	prov      provider.Provider
	model     string
	account   core.Account
	memoryDir string
}

func NewMemoryFlusher(prov provider.Provider, model string, account core.Account, memoryDir string) *MemoryFlusher {
	return &MemoryFlusher{
		prov:      prov,
		model:     model,
		account:   account,
		memoryDir: memoryDir,
	}
}

// ShouldFlush returns true when contextTokens is close to the compaction
// threshold, triggering an early flush before messages get dropped.
// softThresholdGap is the gap before the compaction point (default 24000).
func (f *MemoryFlusher) ShouldFlush(contextTokens, contextWindow, reserveTokens int) bool {
	if f.memoryDir == "" {
		return false
	}
	compactionPoint := contextWindow - reserveTokens
	softThreshold := compactionPoint - defaultSoftThresholdGap
	return contextTokens > softThreshold && contextTokens <= compactionPoint
}

// Flush executes a silent LLM turn that extracts durable notes, then
// appends them to the daily log. Returns the extracted content, or
// empty string if nothing worth recording.
func (f *MemoryFlusher) Flush(ctx context.Context, entries []core.SessionEntry) (string, error) {
	if f.memoryDir == "" {
		return "", nil
	}

	prompt := buildFlushPrompt(entries)
	resp, err := f.prov.Generate(ctx, provider.GenerateRequest{
		Model: f.model,
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: flushSystemPrompt},
			{Role: core.RoleUser, Content: prompt},
		},
		Account: f.account,
	})
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(resp.Text)
	if content == "" || content == noReplyMarker {
		logCompact.Logf("memory flush skip: reason=no_content")
		return "", nil
	}

	if err := memory.AppendDailyLog(f.memoryDir, content); err != nil {
		return content, err
	}

	logCompact.Logf("memory flush complete: len=%d", len(content))
	return content, nil
}

const flushSystemPrompt = `在這段對話即將被壓縮之前，請提取值得長期記住的資訊。
格式：簡潔的要點列表。包含：
- 使用者明確要求記住的事項
- 關鍵技術決策和架構選擇
- 專案結構發現
- 偏好設定和工作流程

如果沒有值得記錄的內容，只回覆 NO_REPLY（不要加其他文字）。
用繁體中文回覆。`

func buildFlushPrompt(entries []core.SessionEntry) string {
	var sb strings.Builder
	sb.WriteString("以下是目前的對話內容。請提取值得長期記憶的要點：\n\n")
	for _, e := range entries {
		if e.Type != core.EntryMessage {
			continue
		}
		sb.WriteString("[")
		sb.WriteString(string(e.Role))
		sb.WriteString("] ")
		sb.WriteString(truncateForPrompt(e.Content, 1500))
		sb.WriteString("\n")
	}
	return sb.String()
}
