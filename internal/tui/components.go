package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// StatusBarComponent — Claude Code-style rich status line
// ---------------------------------------------------------------------------

// StatusBarComponent renders a single-line status bar showing provider, model,
// session, context usage, cost, and message count.
type StatusBarComponent struct {
	provider     string
	modelID      string
	sessionID    string
	contextPct   int     // 0-100
	cost         float64 // accumulated cost in USD
	messageCount int
	width        int
}

// NewStatusBar creates a status bar pre-populated with the given info.
func NewStatusBar(provider, modelID, sessionID string) StatusBarComponent {
	return StatusBarComponent{
		provider:  provider,
		modelID:   modelID,
		sessionID: sessionID,
	}
}

func (s *StatusBarComponent) SetSize(width int)           { s.width = width }
func (s *StatusBarComponent) SetProvider(p string)         { s.provider = p }
func (s *StatusBarComponent) SetModel(m string)            { s.modelID = m }
func (s *StatusBarComponent) SetSession(sess string)       { s.sessionID = sess }
func (s *StatusBarComponent) SetContextPercent(pct int)    { s.contextPct = pct }
func (s *StatusBarComponent) SetCost(c float64)            { s.cost = c }
func (s *StatusBarComponent) SetMessageCount(n int)        { s.messageCount = n }

func (s StatusBarComponent) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}

	// Left: provider · model
	left := theme.StatusModelStyle.Render(
		fmt.Sprintf(" %s · %s", s.provider, s.modelID),
	)

	// Right: session  context%  $cost  N msgs
	var parts []string
	parts = append(parts, s.sessionID)
	if s.contextPct > 0 {
		parts = append(parts, fmt.Sprintf("%d%%", s.contextPct))
	}
	if s.cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", s.cost))
	}
	if s.messageCount > 0 {
		parts = append(parts, fmt.Sprintf("%d msgs", s.messageCount))
	}
	right := theme.StatusMetaStyle.Render(strings.Join(parts, "   "))

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := w - leftW - rightW
	if gap < 1 {
		gap = 1
	}

	// Fill the gap with the status bar background
	filler := theme.StatusBarStyle.Copy().Width(gap).Render("")
	return left + filler + right
}

// StatusBarHeight returns the number of terminal rows consumed by the status bar.
func StatusBarHeight() int { return 1 }

// ---------------------------------------------------------------------------
// DialogComponent — modal confirmation / input dialog
// ---------------------------------------------------------------------------

type DialogComponent struct {
	Title   string
	Message string
	Visible bool
	width   int
}

func (d *DialogComponent) SetSize(width int) { d.width = width }
func (d *DialogComponent) Show(title, message string) {
	d.Title = title
	d.Message = message
	d.Visible = true
}
func (d *DialogComponent) Hide() { d.Visible = false }

func (d DialogComponent) View() string {
	if !d.Visible {
		return ""
	}
	w := d.width
	if w <= 0 {
		w = 60
	}
	boxWidth := w * 60 / 100
	if boxWidth < 30 {
		boxWidth = 30
	}
	if boxWidth > w-4 {
		boxWidth = w - 4
	}

	title := theme.HeaderStyle.Render(d.Title)
	msg := theme.SystemStyle.Render(d.Message)
	hint := theme.HintStyle.Render("Enter 確認 · Esc 取消")

	box := theme.DialogBoxStyle.Copy().Width(boxWidth).Render(
		strings.Join([]string{title, "", msg, "", hint}, "\n"),
	)
	return lipgloss.Place(w, 0, lipgloss.Center, lipgloss.Center, box)
}
