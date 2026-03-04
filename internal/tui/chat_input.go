package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var terminalProbeResponsePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\]1[01];rgb:[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}$`),
	regexp.MustCompile(`^\[\d+;\d+R$`),
}

// SlashCommand defines a local slash command available in chat.
type SlashCommand struct {
	Name        string
	Description string
}

// SubmitMsg is emitted when the user presses Enter in the chat input.
type SubmitMsg struct {
	Text string
}

// ChatInput wraps a textarea.Model with slash command support and input history.
type ChatInput struct {
	textarea textarea.Model
	commands []SlashCommand
	width    int

	// Slash command suggestions
	showSuggestions bool
	suggestions     []SlashCommand
	selectedIdx     int

	// Input history
	history      []string
	historyIndex int
	historyDraft string
}

func NewChatInput(width, termHeight int) ChatInput {
	ta := textarea.New()
	ta.Placeholder = "輸入訊息... (Shift+Enter 換行)"
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetWidth(width - 3) // account for "> " prefix
	ta.SetHeight(3)        // start at 3 lines — visually multi-line
	ta.MaxHeight = responsiveMaxHeight(termHeight)

	// Strip all border/padding styling for a clean look
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(theme.Muted)
	ta.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(theme.Muted)
	ta.Prompt = ""

	// Override: Enter is NOT newline. We handle it in Update.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "alt+enter"),
	)

	// Focus during construction so the textarea accepts input immediately.
	// Init() uses a value receiver — focus set there is lost on the copy.
	ta.Focus()

	ci := ChatInput{
		textarea:     ta,
		width:        width,
		historyIndex: -1,
		commands: []SlashCommand{
			{Name: "/help", Description: "Show available commands"},
			{Name: "/new", Description: "Start new conversation"},
			{Name: "/clear", Description: "Clear chat history"},
			{Name: "/image", Description: "Attach image file"},
			{Name: "/paste", Description: "Paste image from clipboard"},
			{Name: "/config", Description: "Open settings"},
			{Name: "/model", Description: "Switch model"},
			{Name: "/session", Description: "Switch session"},
			{Name: "/memory", Description: "Search memory"},
			{Name: "/mcp", Description: "Show MCP servers"},
		},
	}
	return ci
}

func (ci *ChatInput) Focus() tea.Cmd {
	return ci.textarea.Focus()
}

func (ci *ChatInput) Blur() {
	ci.textarea.Blur()
}

func (ci *ChatInput) SetWidth(width int) {
	ci.width = width
	ci.textarea.SetWidth(width - 3) // account for "> " prefix
}

// SetSize updates width and responsive max height.
func (ci *ChatInput) SetSize(width, termHeight int) {
	ci.width = width
	ci.textarea.SetWidth(width - 3)
	ci.textarea.MaxHeight = responsiveMaxHeight(termHeight)
}

func responsiveMaxHeight(termHeight int) int {
	maxH := termHeight / 3
	if maxH > 10 {
		maxH = 10
	}
	if maxH < 3 {
		maxH = 3
	}
	return maxH
}

func (ci *ChatInput) Value() string {
	return ci.textarea.Value()
}

func (ci *ChatInput) SetValue(v string) {
	ci.textarea.SetValue(v)
}

func (ci *ChatInput) Reset() {
	ci.textarea.Reset()
	ci.showSuggestions = false
	ci.selectedIdx = 0
}

// Update processes messages for the chat input component.
func (ci *ChatInput) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyRunes {
			raw := strings.TrimSpace(string(msg.Runes))
			if isTerminalProbeResponse(raw) {
				return nil
			}
		}

		// Handle slash command suggestion navigation
		if ci.showSuggestions {
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
				ci.selectedIdx = (ci.selectedIdx + 1) % len(ci.suggestions)
				return nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
				ci.selectedIdx--
				if ci.selectedIdx < 0 {
					ci.selectedIdx = len(ci.suggestions) - 1
				}
				return nil
			case key.Matches(msg, chatKeys.Submit) && !key.Matches(msg, chatKeys.NewLine):
				if len(ci.suggestions) > 0 && ci.selectedIdx < len(ci.suggestions) {
					cmd := ci.suggestions[ci.selectedIdx]
					ci.textarea.SetValue(cmd.Name + " ")
					ci.showSuggestions = false
					return nil
				}
			}
		}

		// Handle Enter as submit (exclude Shift+Enter / Alt+Enter which insert newlines)
		if key.Matches(msg, chatKeys.Submit) && !key.Matches(msg, chatKeys.NewLine) {
			text := strings.TrimSpace(ci.textarea.Value())
			if text == "" {
				return nil
			}
			ci.appendHistory(text)
			ci.textarea.Reset()
			ci.showSuggestions = false
			return func() tea.Msg { return SubmitMsg{Text: text} }
		}

		// Handle input history navigation
		if key.Matches(msg, chatKeys.HistoryPrev) {
			ci.historyMoveUp()
			return nil
		}
		if key.Matches(msg, chatKeys.HistoryNext) {
			ci.historyMoveDown()
			return nil
		}
	}

	// Pass to textarea
	var cmd tea.Cmd
	ci.textarea, cmd = ci.textarea.Update(msg)

	// Clean out any leaking terminal probe responses from the accumulated text
	val := ci.textarea.Value()
	if strings.Contains(val, "]11;rgb:") || strings.Contains(val, "]10;rgb:") {
		// Pattern matches e.g. ]11;rgb:158e/193a/1e75\ (with or without trailing \)
		reOSC := regexp.MustCompile(`\]1[01];rgb:[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}\\?`)
		val = reOSC.ReplaceAllString(val, "")
	}
	if strings.Contains(val, "R") && strings.Contains(val, "[") {
		reCursor := regexp.MustCompile(`\[\d+;\d+R`)
		val = reCursor.ReplaceAllString(val, "")
	}
	if val != ci.textarea.Value() {
		ci.textarea.SetValue(val)
	}

	// Update slash suggestions
	ci.updateSuggestions()

	return cmd
}

func isTerminalProbeResponse(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, pattern := range terminalProbeResponsePatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	if strings.Contains(text, "rgb:") &&
		(strings.HasPrefix(text, "]10;") || strings.HasPrefix(text, "]11;")) {
		return true
	}
	return false
}

func (ci ChatInput) View() string {
	var sb strings.Builder

	// Visual separator between messages and input
	sb.WriteString(theme.SystemStyle.Render(strings.Repeat("─", ci.width)))
	sb.WriteString("\n")

	// Slash command suggestions above the input
	if ci.showSuggestions && len(ci.suggestions) > 0 {
		for i, cmd := range ci.suggestions {
			prefix := "  "
			style := theme.NormalStyle
			if i == ci.selectedIdx {
				prefix = "› "
				style = theme.SelectedStyle
			}
			sb.WriteString(style.Render(prefix+cmd.Name) + "  " + theme.HintStyle.Render(cmd.Description))
			sb.WriteString("\n")
		}
	}

	// Prompt + textarea
	sb.WriteString(theme.PromptStyle.Render("> "))
	sb.WriteString(ci.textarea.View())
	sb.WriteString("\n")

	// Keybinding hint line
	sb.WriteString(theme.HintStyle.Render("  Shift+Enter 換行 · Enter 送出 · Ctrl+V 貼圖 · Ctrl+N 新對話 · Esc 設定"))

	return sb.String()
}

// InputHeight returns the current rendered height of the input component
// including separator line, suggestions, textarea, and hint line.
func (ci ChatInput) InputHeight() int {
	h := ci.textarea.Height()
	h += 2 // separator line + hint line
	if ci.showSuggestions && len(ci.suggestions) > 0 {
		h += len(ci.suggestions)
	}
	return h
}

func (ci *ChatInput) updateSuggestions() {
	text := ci.textarea.Value()
	if !strings.HasPrefix(text, "/") || strings.Contains(text, " ") || strings.Contains(text, "\n") {
		ci.showSuggestions = false
		ci.selectedIdx = 0
		return
	}
	prefix := strings.ToLower(text)
	var matches []SlashCommand
	for _, cmd := range ci.commands {
		if strings.HasPrefix(cmd.Name, prefix) {
			matches = append(matches, cmd)
		}
	}
	ci.suggestions = matches
	ci.showSuggestions = len(matches) > 0
	if ci.selectedIdx >= len(matches) {
		ci.selectedIdx = 0
	}
}

func (ci *ChatInput) appendHistory(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(ci.history) == 0 || ci.history[len(ci.history)-1] != text {
		ci.history = append(ci.history, text)
	}
	ci.historyIndex = -1
	ci.historyDraft = ""
}

func (ci *ChatInput) historyMoveUp() {
	if len(ci.history) == 0 {
		return
	}
	if ci.historyIndex == -1 {
		ci.historyDraft = ci.textarea.Value()
		ci.historyIndex = len(ci.history) - 1
	} else if ci.historyIndex > 0 {
		ci.historyIndex--
	}
	if ci.historyIndex >= 0 && ci.historyIndex < len(ci.history) {
		ci.textarea.SetValue(ci.history[ci.historyIndex])
	}
}

func (ci *ChatInput) historyMoveDown() {
	if len(ci.history) == 0 || ci.historyIndex == -1 {
		return
	}
	if ci.historyIndex < len(ci.history)-1 {
		ci.historyIndex++
		ci.textarea.SetValue(ci.history[ci.historyIndex])
		return
	}
	ci.historyIndex = -1
	ci.textarea.SetValue(ci.historyDraft)
	ci.historyDraft = ""
}
