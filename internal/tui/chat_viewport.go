package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

// ChatMessage represents a single message in the chat history.
type ChatMessage struct {
	Role      string // "user", "assistant", "system", "error", "thinking"
	Content   string
	Images    []string // file names of attached images (display only)
	Timestamp time.Time

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
