package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/doeshing/nekoclaw/internal/client"
)

// Sidebar is the left-pane component showing a session list and inspector footer.
type Sidebar struct {
	// Session list
	sessions       []client.SessionInfo
	selectedIdx    int
	currentSession string
	loaded         bool
	scrollOffset   int
	focused        bool

	// Inspector data
	provider       string
	model          string
	contextPercent int
	cost           float64
	messageCount   int

	width, height int
}

func NewSidebar(provider, model, session string) Sidebar {
	return Sidebar{
		provider:       provider,
		model:          model,
		currentSession: session,
	}
}

// ---------------------------------------------------------------------------
// Setters
// ---------------------------------------------------------------------------

func (s *Sidebar) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func (s *Sidebar) SetFocus(focused bool) { s.focused = focused }
func (s *Sidebar) IsFocused() bool       { return s.focused }

func (s *Sidebar) SetProvider(p string)        { s.provider = p }
func (s *Sidebar) SetModel(m string)           { s.model = m }
func (s *Sidebar) SetCurrentSession(session string) { s.currentSession = session }
func (s *Sidebar) SetContextPercent(p int)     { s.contextPercent = p }
func (s *Sidebar) SetCost(c float64)           { s.cost = c }
func (s *Sidebar) SetMessageCount(count int)   { s.messageCount = count }

// ---------------------------------------------------------------------------
// Session list handling
// ---------------------------------------------------------------------------

func (s *Sidebar) HandleSessionsList(msg SessionsListMsg) tea.Cmd {
	if msg.Err != nil {
		return nil
	}
	s.sessions = msg.Sessions
	s.loaded = true

	// Sync selectedIdx to current session
	for i, sess := range s.sessions {
		if sess.SessionID == s.currentSession {
			s.selectedIdx = i
			break
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Update (key handling when focused)
// ---------------------------------------------------------------------------

func (s *Sidebar) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch {
	case key.Matches(keyMsg, sidebarKeys.Up):
		if s.selectedIdx > 0 {
			s.selectedIdx--
			s.ensureVisible()
		}
	case key.Matches(keyMsg, sidebarKeys.Down):
		if s.selectedIdx < len(s.sessions)-1 {
			s.selectedIdx++
			s.ensureVisible()
		}
	case key.Matches(keyMsg, sidebarKeys.Select):
		if s.selectedIdx < len(s.sessions) {
			selected := s.sessions[s.selectedIdx].SessionID
			s.currentSession = selected
			// Select session and return focus to chat
			return tea.Batch(
				func() tea.Msg { return SessionChangedMsg{SessionID: selected} },
				func() tea.Msg { return SidebarToggleFocusMsg{} },
			)
		}
	case key.Matches(keyMsg, sidebarKeys.Back):
		// Return focus to chat
		return func() tea.Msg { return SidebarToggleFocusMsg{} }
	}
	return nil
}

// ensureVisible adjusts scrollOffset so selectedIdx is within the visible window.
func (s *Sidebar) ensureVisible() {
	footerHeight := s.inspectorFooterHeight()
	separatorHeight := 1
	headerHeight := 2 // "SESSIONS" + blank line
	visibleCount := s.height - footerHeight - separatorHeight - headerHeight
	if visibleCount < 1 {
		visibleCount = 1
	}

	if s.selectedIdx < s.scrollOffset {
		s.scrollOffset = s.selectedIdx
	}
	if s.selectedIdx >= s.scrollOffset+visibleCount {
		s.scrollOffset = s.selectedIdx - visibleCount + 1
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (s Sidebar) View() string {
	if s.width <= 0 || s.height <= 0 {
		return ""
	}

	// Content width after border (right border = 1) and padding (1 left)
	contentW := s.width - 3
	if contentW < 5 {
		contentW = 5
	}

	footerContent := s.renderInspectorFooter(contentW)
	footerLines := strings.Split(footerContent, "\n")
	footerHeight := len(footerLines)

	// Session list gets remaining height (minus footer, separator line)
	sessionHeight := s.height - footerHeight - 1
	if sessionHeight < 3 {
		sessionHeight = 3
	}
	sessionContent := s.renderSessionList(contentW, sessionHeight)

	// Combine: session list + separator + inspector footer
	var lines []string
	sessionLines := strings.Split(sessionContent, "\n")

	// Pad session area to exact height
	for len(sessionLines) < sessionHeight {
		sessionLines = append(sessionLines, "")
	}
	if len(sessionLines) > sessionHeight {
		sessionLines = sessionLines[:sessionHeight]
	}
	lines = append(lines, sessionLines...)

	// Separator
	lines = append(lines, theme.SystemStyle.Render(strings.Repeat("─", contentW)))

	// Inspector footer
	lines = append(lines, footerLines...)

	// Pad/clamp to exact height
	for len(lines) < s.height {
		lines = append(lines, "")
	}
	if len(lines) > s.height {
		lines = lines[:s.height]
	}

	// Right border (separator between sidebar and chat)
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(theme.Border).
		PaddingLeft(1)

	return borderStyle.Width(s.width - 1).Render(strings.Join(lines, "\n"))
}

func (s Sidebar) renderSessionList(width, maxHeight int) string {
	var lines []string

	// Header
	headerStyle := theme.SectionStyle
	if s.focused {
		headerStyle = theme.HighlightStyle
	}
	lines = append(lines, headerStyle.Render("SESSIONS"))
	lines = append(lines, "")

	if !s.loaded {
		lines = append(lines, theme.HintStyle.Render("  Loading..."))
		return strings.Join(lines, "\n")
	}

	if len(s.sessions) == 0 {
		lines = append(lines, theme.HintStyle.Render("  No sessions"))
		return strings.Join(lines, "\n")
	}

	// Calculate visible window
	headerLines := 2 // header + blank line
	availableLines := maxHeight - headerLines
	if availableLines < 1 {
		availableLines = 1
	}

	// Ensure scrollOffset is valid
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
	endIdx := s.scrollOffset + availableLines
	if endIdx > len(s.sessions) {
		endIdx = len(s.sessions)
	}

	// Scroll indicator (top)
	if s.scrollOffset > 0 {
		lines = append(lines, theme.HintStyle.Render("  ↑ more"))
		availableLines--
		endIdx = s.scrollOffset + availableLines
		if endIdx > len(s.sessions) {
			endIdx = len(s.sessions)
		}
	}

	// Render visible sessions
	for i := s.scrollOffset; i < endIdx; i++ {
		sess := s.sessions[i]
		isCurrent := sess.SessionID == s.currentSession
		isSelected := s.focused && i == s.selectedIdx

		prefix := "  "
		if isSelected {
			prefix = "› "
		}

		suffix := ""
		if isCurrent {
			suffix = " ✓"
		}

		age := formatTimeAgo(sess.UpdatedAt)
		displayName := sess.SessionID
		if sess.Title != "" {
			displayName = sess.Title
		}
		label := fmt.Sprintf("%s%s", displayName, suffix)
		if age != "" {
			label = fmt.Sprintf("%s · %s", label, age)
		}
		label = clampLine(prefix+label, width)

		if isSelected {
			lines = append(lines, theme.SelectedStyle.Render(label))
		} else if isCurrent {
			lines = append(lines, theme.HighlightStyle.Render(label))
		} else {
			lines = append(lines, theme.NormalStyle.Render(label))
		}
	}

	// Scroll indicator (bottom)
	if endIdx < len(s.sessions) {
		lines = append(lines, theme.HintStyle.Render("  ↓ more"))
	}

	return strings.Join(lines, "\n")
}

func (s Sidebar) renderInspectorFooter(width int) string {
	var sb strings.Builder

	sb.WriteString(theme.SectionStyle.Render("INSPECTOR") + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("PROVIDER") + "\n")
	sb.WriteString(theme.NormalStyle.Render(clampLine(s.provider+" · "+s.model, width)) + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("CONTEXT USAGE") + "\n")
	sb.WriteString(renderProgressBar(s.contextPercent, width) + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("COST (USD)") + "\n")
	sb.WriteString(theme.HighlightStyle.Render(fmt.Sprintf("$%.4f", s.cost)) + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("MESSAGES") + "\n")
	sb.WriteString(theme.NormalStyle.Render(fmt.Sprintf("%d", s.messageCount)) + "\n\n")

	sb.WriteString(theme.SectionStyle.Render("SHORTCUTS") + "\n")
	sb.WriteString(theme.HintStyle.Render("Ctrl+N New Chat") + "\n")
	sb.WriteString(theme.HintStyle.Render("Ctrl+B Sidebar") + "\n")
	sb.WriteString(theme.HintStyle.Render("Esc    Settings") + "\n")
	sb.WriteString(theme.HintStyle.Render("/help  Commands"))

	return sb.String()
}

// inspectorFooterHeight returns the number of lines the inspector footer occupies.
func (s Sidebar) inspectorFooterHeight() int {
	// INSPECTOR(1) + blank(1)
	// + PROVIDER(1) + value(1) + blank(1)
	// + CONTEXT USAGE(1) + bar(1) + blank(1)
	// + COST(1) + value(1) + blank(1)
	// + MESSAGES(1) + value(1) + blank(1)
	// + SHORTCUTS(1) + Ctrl+N(1) + Ctrl+B(1) + Esc(1) + /help(1) = 19
	return 19
}

// renderProgressBar renders a text-based progress bar (moved from inspector.go).
func renderProgressBar(percent int, width int) string {
	if width < 10 {
		return fmt.Sprintf("%d%%", percent)
	}
	if percent > 100 {
		percent = 100
	} else if percent < 0 {
		percent = 0
	}

	barWidth := width - 6
	if barWidth < 1 {
		barWidth = 1
	}

	filled := (percent * barWidth) / 100
	empty := barWidth - filled

	fillStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("░", empty)

	return fmt.Sprintf("%s%s %3d%%", fillStr, emptyStr, percent)
}
