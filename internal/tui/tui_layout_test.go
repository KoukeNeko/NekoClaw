package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func maxRenderedLineWidth(text string) int {
	max := 0
	for _, line := range strings.Split(text, "\n") {
		if w := lipgloss.Width(line); w > max {
			max = w
		}
	}
	return max
}

func TestMenuPanelsFitTerminalWidth(t *testing.T) {
	m := model{
		width:        100,
		provider:     "google-gemini-cli",
		modelID:      "default",
		sessionID:    "main",
		menuIndex:    0,
		authRedirect: "",
		lines: []chatLine{
			{role: "system", text: strings.Repeat("x", 180)},
		},
	}

	menuWidth, summaryWidth, stacked := m.menuLayoutWidths()
	if stacked {
		t.Fatalf("expected side-by-side layout for width %d", m.width)
	}

	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10")).Padding(0, 1)
	normalItemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Padding(0, 1)
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	assistantStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	youStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	menuPanel := m.renderMenuPanel(selectedStyle, normalItemStyle, menuWidth)
	summaryPanel := m.renderSummaryPanel(statusStyle, errorStyle, assistantStyle, youStyle, summaryWidth)
	joined := lipgloss.JoinHorizontal(lipgloss.Top, menuPanel, " ", summaryPanel)

	if width := maxRenderedLineWidth(joined); width > m.width {
		t.Fatalf("joined panels overflow terminal width: got %d want <= %d", width, m.width)
	}
}

func TestMenuPanelsStackOnNarrowWidth(t *testing.T) {
	m := model{
		width:     80,
		provider:  "google-gemini-cli",
		modelID:   "default",
		sessionID: "main",
		menuIndex: 0,
	}

	menuWidth, summaryWidth, stacked := m.menuLayoutWidths()
	if !stacked {
		t.Fatalf("expected stacked layout for width %d", m.width)
	}

	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10")).Padding(0, 1)
	normalItemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Padding(0, 1)
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	assistantStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	youStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	menuPanel := m.renderMenuPanel(selectedStyle, normalItemStyle, menuWidth)
	summaryPanel := m.renderSummaryPanel(statusStyle, errorStyle, assistantStyle, youStyle, summaryWidth)

	if width := maxRenderedLineWidth(menuPanel); width > menuWidth {
		t.Fatalf("menu panel overflow: got %d want <= %d", width, menuWidth)
	}
	if width := maxRenderedLineWidth(summaryPanel); width > summaryWidth {
		t.Fatalf("summary panel overflow: got %d want <= %d", width, summaryWidth)
	}
}

func TestMenuNavigationSingleColumn(t *testing.T) {
	m := model{menuIndex: 0}

	m.menuMoveRight()
	if m.menuIndex != 1 {
		t.Fatalf("expected right key to move down in single-column menu, got %d", m.menuIndex)
	}

	m.menuMoveDown()
	if m.menuIndex != 2 {
		t.Fatalf("expected down key to move to next item, got %d", m.menuIndex)
	}

	m.menuMoveLeft()
	if m.menuIndex != 1 {
		t.Fatalf("expected left key to move up in single-column menu, got %d", m.menuIndex)
	}

	m.menuMoveUp()
	if m.menuIndex != 0 {
		t.Fatalf("expected up key to move to previous item, got %d", m.menuIndex)
	}

	m.menuMoveUp()
	if m.menuIndex != 0 {
		t.Fatalf("expected up key to stay at first item, got %d", m.menuIndex)
	}

	m.menuIndex = len(menuItems) - 1
	m.menuMoveDown()
	if m.menuIndex != len(menuItems)-1 {
		t.Fatalf("expected down key to stay at last item, got %d", m.menuIndex)
	}
	m.menuMoveRight()
	if m.menuIndex != len(menuItems)-1 {
		t.Fatalf("expected right key to stay at last item, got %d", m.menuIndex)
	}
}

func TestMenuPanelsFitVeryNarrowWidth(t *testing.T) {
	m := model{
		width:     24,
		provider:  "google-gemini-cli",
		modelID:   "default",
		sessionID: "main",
		menuIndex: 0,
	}

	menuWidth, summaryWidth, stacked := m.menuLayoutWidths()
	if !stacked {
		t.Fatalf("expected stacked layout for very narrow width %d", m.width)
	}

	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10")).Padding(0, 1)
	normalItemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Padding(0, 1)
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	assistantStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	youStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	menuPanel := m.renderMenuPanel(selectedStyle, normalItemStyle, menuWidth)
	summaryPanel := m.renderSummaryPanel(statusStyle, errorStyle, assistantStyle, youStyle, summaryWidth)

	if width := maxRenderedLineWidth(menuPanel); width > menuWidth {
		t.Fatalf("menu panel overflow on very narrow width: got %d want <= %d", width, menuWidth)
	}
	if width := maxRenderedLineWidth(summaryPanel); width > summaryWidth {
		t.Fatalf("summary panel overflow on very narrow width: got %d want <= %d", width, summaryWidth)
	}
}

func TestApplyWindowSizeAdjustsInputWidths(t *testing.T) {
	m := model{}
	m.applyWindowSize(100, 40)
	if m.chatInput.Width != 98 || m.promptInput.Width != 98 {
		t.Fatalf("expected input width 98, got chat=%d prompt=%d", m.chatInput.Width, m.promptInput.Width)
	}

	m.applyWindowSize(8, 10)
	if m.chatInput.Width != 10 || m.promptInput.Width != 10 {
		t.Fatalf("expected minimum input width 10, got chat=%d prompt=%d", m.chatInput.Width, m.promptInput.Width)
	}
}

func TestChatTranscriptTracksWindowSize(t *testing.T) {
	m := model{
		width:  160,
		height: 46,
		lines: []chatLine{
			{role: "system", text: "ready"},
		},
	}

	textWidth, maxLines := m.chatTranscriptLayout()
	if textWidth != 158 {
		t.Fatalf("expected text width 158, got %d", textWidth)
	}
	if maxLines != 44 {
		t.Fatalf("expected max transcript lines 44, got %d", maxLines)
	}

	m.applyWindowSize(120, 28)
	textWidth, maxLines = m.chatTranscriptLayout()
	if textWidth != 118 {
		t.Fatalf("expected resized text width 118, got %d", textWidth)
	}
	if maxLines != 26 {
		t.Fatalf("expected resized max transcript lines 26, got %d", maxLines)
	}
}

func TestRenderLogLinesWrapsLongContent(t *testing.T) {
	m := model{
		width: 42,
		lines: []chatLine{
			{role: "error", text: strings.Repeat("x", 220)},
		},
	}
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	assistantStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	youStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	lines := m.renderLogLines(statusStyle, errorStyle, assistantStyle, youStyle, 40)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output for long line, got %d line(s)", len(lines))
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("wrapped line overflow: got %d want <= 40", width)
		}
	}
}
