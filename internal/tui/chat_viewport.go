package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/mcp"
)

// ChatMessage represents a single message in the chat history.
type ChatMessage struct {
	Role      string // "user", "assistant", "system", "error", "thinking"
	Content   string
	Images    []string // file names of attached images (display only)
	Timestamp time.Time

	// Token usage stats (populated for assistant messages)
	Provider     string // provider ID used for this response
	Model        string // model ID used for this response
	InputTokens  int
	OutputTokens int
	ElapsedMs    int64 // response time in milliseconds

	// Tool events (populated for assistant messages when tools were used)
	ToolEvents []core.ToolEvent

	// Cache for rendered output; invalidated when width changes.
	renderedCache string
	renderedWidth int
}

// ChatViewport wraps a bubbles viewport.Model and a glamour renderer to
// display a scrollable chat history with markdown rendering.
type ChatViewport struct {
	viewport viewport.Model
	renderer *glamour.TermRenderer
	width    int
	height   int
	messages []ChatMessage

	// atBottom tracks whether the user was at the bottom before an update.
	atBottom bool
}

func NewChatViewport(width, height int) ChatViewport {
	vp := viewport.New(width, height)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	cv := ChatViewport{
		viewport: vp,
		width:    width,
		height:   height,
		atBottom: true,
	}
	cv.renderer = cv.newRenderer(width)
	return cv
}

func (cv *ChatViewport) SetSize(width, height int) {
	if width != cv.width {
		cv.renderer = cv.newRenderer(width)
		cv.invalidateCache()
	}
	cv.width = width
	cv.height = height
	cv.viewport.Width = width
	cv.viewport.Height = height
	cv.rebuildContent()
}

func (cv *ChatViewport) Update(msg tea.Msg) (*ChatViewport, tea.Cmd) {
	var cmd tea.Cmd
	cv.viewport, cmd = cv.viewport.Update(msg)
	return cv, cmd
}

func (cv ChatViewport) View() string {
	return cv.viewport.View()
}

// SetMessages replaces the entire message list and re-renders.
func (cv *ChatViewport) SetMessages(messages []ChatMessage) {
	cv.messages = messages
	cv.rebuildContent()
	cv.viewport.GotoBottom()
	cv.atBottom = true
}

// AppendMessage adds a single message and scrolls to bottom if appropriate.
func (cv *ChatViewport) AppendMessage(msg ChatMessage) {
	cv.atBottom = cv.viewport.AtBottom()
	cv.messages = append(cv.messages, msg)
	cv.rebuildContent()
	if cv.atBottom {
		cv.viewport.GotoBottom()
	}
}

// RemoveLastMessage removes the last message and re-renders.
func (cv *ChatViewport) RemoveLastMessage() {
	if len(cv.messages) == 0 {
		return
	}
	cv.messages = cv.messages[:len(cv.messages)-1]
	cv.rebuildContent()
	if cv.atBottom {
		cv.viewport.GotoBottom()
	}
}

// UpdateLastMessage replaces the content of the last message (used for streaming).
func (cv *ChatViewport) UpdateLastMessage(content string) {
	if len(cv.messages) == 0 {
		return
	}
	cv.atBottom = cv.viewport.AtBottom()
	last := &cv.messages[len(cv.messages)-1]
	last.Content = content
	last.renderedCache = ""
	last.renderedWidth = 0
	cv.rebuildContent()
	if cv.atBottom {
		cv.viewport.GotoBottom()
	}
}

// AppendToLastMessage appends text to the last message's Content and re-renders.
func (cv *ChatViewport) AppendToLastMessage(text string) {
	if len(cv.messages) == 0 {
		return
	}
	cv.atBottom = cv.viewport.AtBottom()
	last := &cv.messages[len(cv.messages)-1]
	last.Content += text
	last.renderedCache = ""
	last.renderedWidth = 0
	cv.rebuildContent()
	if cv.atBottom {
		cv.viewport.GotoBottom()
	}
}

// UpdateLastMessageMeta copies metadata fields from meta to the last message
// without changing its Content. Updates Provider, Model, token counts,
// ElapsedMs, and ToolEvents, then re-renders.
func (cv *ChatViewport) UpdateLastMessageMeta(meta ChatMessage) {
	if len(cv.messages) == 0 {
		return
	}
	cv.atBottom = cv.viewport.AtBottom()
	last := &cv.messages[len(cv.messages)-1]
	last.Provider = meta.Provider
	last.Model = meta.Model
	last.InputTokens = meta.InputTokens
	last.OutputTokens = meta.OutputTokens
	last.ElapsedMs = meta.ElapsedMs
	last.ToolEvents = meta.ToolEvents
	last.renderedCache = ""
	last.renderedWidth = 0
	cv.rebuildContent()
	if cv.atBottom {
		cv.viewport.GotoBottom()
	}
}

// ScrollPercent returns the current scroll percentage.
func (cv ChatViewport) ScrollPercent() float64 {
	return cv.viewport.ScrollPercent()
}

// AtBottom returns true if the viewport is scrolled to the bottom.
func (cv ChatViewport) AtBottom() bool {
	return cv.viewport.AtBottom()
}

// MessageCount returns the number of messages in the viewport.
func (cv ChatViewport) MessageCount() int {
	return len(cv.messages)
}

func (cv *ChatViewport) rebuildContent() {
	var sb strings.Builder
	for i := range cv.messages {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(cv.renderMessage(&cv.messages[i]))
	}
	cv.viewport.SetContent(sb.String())
}

func (cv *ChatViewport) renderMessage(msg *ChatMessage) string {
	if msg.renderedCache != "" && msg.renderedWidth == cv.width {
		return msg.renderedCache
	}

	content := stripTerminalControlSequences(msg.Content)

	// Prepend image attachment labels for user messages
	var imagePrefix string
	if len(msg.Images) > 0 {
		for _, name := range msg.Images {
			imagePrefix += theme.HintStyle.Render("  📎 "+name) + "\n"
		}
	}

	var rendered string
	switch msg.Role {
	case "assistant":
		rendered = cv.renderMarkdown(content)
		if stats := formatUsageStats(msg); stats != "" {
			rendered += "\n" + theme.HintStyle.Render("  "+stats)
		}
		if summary := formatToolSummary(msg.ToolEvents); summary != "" {
			// Summary is multi-line; style each line individually.
			for _, line := range strings.Split(summary, "\n") {
				rendered += "\n" + theme.HintStyle.Render("  "+line)
			}
		}
	case "user":
		rendered = imagePrefix + theme.PromptStyle.Render("> ") + theme.UserStyle.Render(content)
	case "system":
		rendered = theme.SystemStyle.Render("  " + content)
	case "error":
		rendered = theme.ErrorStyle.Render("  \u2715 " + content)
	case "thinking":
		rendered = "  " + msg.Content // preserve spinner ANSI colors
	default:
		rendered = content
	}

	// Ensure all messages fit within the viewport width.
	// Glamour's word wrap only breaks at whitespace, which doesn't work for
	// CJK text (no spaces between characters). fitToTerminalWidth uses
	// lipgloss.MaxWidth which handles CJK character widths correctly.
	rendered = fitToTerminalWidth(rendered, cv.width)

	msg.renderedCache = rendered
	msg.renderedWidth = cv.width
	return rendered
}

func (cv *ChatViewport) renderMarkdown(content string) string {
	if cv.renderer == nil {
		return theme.AssistantStyle.Render(content)
	}
	rendered, err := cv.renderer.Render(content)
	if err != nil {
		return theme.AssistantStyle.Render(content)
	}
	return strings.TrimRight(rendered, "\n")
}

// formatUsageStats builds a concise stats line for assistant messages.
// Example: "⏱ 2.3s · ↑1.2K ↓567 · 245 tok/s · google-gemini-cli/gemini-2.0-flash"
func formatUsageStats(msg *ChatMessage) string {
	hasTokens := msg.InputTokens > 0 || msg.OutputTokens > 0
	hasElapsed := msg.ElapsedMs > 0
	if !hasTokens && !hasElapsed {
		return ""
	}

	var parts []string

	// Elapsed time
	if hasElapsed {
		sec := float64(msg.ElapsedMs) / 1000.0
		if sec >= 60 {
			parts = append(parts, fmt.Sprintf("⏱ %.0fm%.0fs", sec/60, float64(int64(sec)%60)))
		} else if sec >= 10 {
			parts = append(parts, fmt.Sprintf("⏱ %.1fs", sec))
		} else {
			parts = append(parts, fmt.Sprintf("⏱ %.2fs", sec))
		}
	}

	// Token counts: ↑input ↓output (total)
	if hasTokens {
		total := msg.InputTokens + msg.OutputTokens
		parts = append(parts, fmt.Sprintf("↑%s ↓%s (%s)",
			formatTokenCount(msg.InputTokens),
			formatTokenCount(msg.OutputTokens),
			formatTokenCount(total),
		))
	}

	// Throughput: tok/s (output tokens / elapsed seconds)
	if hasTokens && hasElapsed && msg.OutputTokens > 0 && msg.ElapsedMs > 0 {
		tokPerSec := float64(msg.OutputTokens) / (float64(msg.ElapsedMs) / 1000.0)
		parts = append(parts, fmt.Sprintf("%.0f tok/s", tokPerSec))
	}

	// Provider/model tag (useful when fallback occurs).
	if msg.Model != "" {
		tag := msg.Model
		if msg.Provider != "" {
			tag = msg.Provider + "/" + tag
		}
		parts = append(parts, tag)
	}

	return strings.Join(parts, " · ")
}

// formatToolSummary builds a numbered list of tools used during a response.
// Groups by unique tool name (preserving first-seen order) with call counts.
// MCP tools are displayed with friendly "server/tool" names.
func formatToolSummary(events []core.ToolEvent) string {
	if len(events) == 0 {
		return ""
	}
	// Count calls per tool, preserving first-seen order.
	type toolEntry struct {
		displayName string
		count       int
	}
	seen := map[string]int{} // raw name -> index in entries
	var entries []toolEntry
	for _, evt := range events {
		if evt.Phase != "executed" && evt.Phase != "failed" {
			continue
		}
		raw := evt.ToolName
		if idx, ok := seen[raw]; ok {
			entries[idx].count++
			continue
		}
		display := raw
		if serverName, toolName, isMCP := mcp.ParseNamespacedTool(raw); isMCP {
			display = serverName + "/" + toolName
		}
		seen[raw] = len(entries)
		entries = append(entries, toolEntry{displayName: display, count: 1})
	}
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("🔧 使用的工具：")
	for i, e := range entries {
		b.WriteString(fmt.Sprintf("\n   %d. %s", i+1, e.displayName))
		if e.count > 1 {
			b.WriteString(fmt.Sprintf(" (×%d)", e.count))
		}
	}
	return b.String()
}

func (cv *ChatViewport) invalidateCache() {
	for i := range cv.messages {
		cv.messages[i].renderedCache = ""
		cv.messages[i].renderedWidth = 0
	}
}

func (cv *ChatViewport) newRenderer(width int) *glamour.TermRenderer {
	renderWidth := width - 4
	if renderWidth < 20 {
		renderWidth = 20
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(renderWidth),
	)
	if err != nil {
		return nil
	}
	return r
}
