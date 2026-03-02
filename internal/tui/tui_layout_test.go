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
	if m.chatInput.Width != 94 || m.promptInput.Width != 94 {
		t.Fatalf("expected input width 94, got chat=%d prompt=%d", m.chatInput.Width, m.promptInput.Width)
	}

	m.applyWindowSize(8, 10)
	if m.chatInput.Width != 12 || m.promptInput.Width != 12 {
		t.Fatalf("expected minimum input width 12, got chat=%d prompt=%d", m.chatInput.Width, m.promptInput.Width)
	}
}
