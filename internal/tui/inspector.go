package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type InspectorMode int

const (
	InspectorChat InspectorMode = iota
	InspectorSettings
)

// Inspector is the right-pane component providing context and controls.
type Inspector struct {
	// Chat mode data
	provider       string
	model          string
	session        string
	contextPercent int
	cost           float64
	messageCount   int

	width, height int
}

func NewInspector(provider, model, session string) Inspector {
	return Inspector{
		provider: provider,
		model:    model,
		session:  session,
	}
}

func (i *Inspector) SetSize(width, height int) {
	i.width = width
	i.height = height
}

func (i *Inspector) SetProvider(p string) { i.provider = p }
func (i *Inspector) SetModel(m string)    { i.model = m }
func (i *Inspector) SetSession(s string)  { i.session = s }

func (i *Inspector) SetContextPercent(p int)   { i.contextPercent = p }
func (i *Inspector) SetCost(c float64)         { i.cost = c }
func (i *Inspector) SetMessageCount(count int) { i.messageCount = count }

func (i Inspector) Init() tea.Cmd { return nil }

func (i *Inspector) Update(msg tea.Msg) tea.Cmd {
	return nil
}

func (i Inspector) View() string {
	if i.width <= 0 || i.height <= 0 {
		return ""
	}

	content := i.renderChatInspector()

	// Make sure the exact height is filled
	lines := strings.Split(content, "\n")
	for len(lines) < i.height {
		lines = append(lines, "")
	}
	if len(lines) > i.height {
		lines = lines[:i.height]
	}

	// Add left border (separator)
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(theme.Border).
		PaddingLeft(1)

	return borderStyle.Width(i.width - 1).Render(strings.Join(lines, "\n"))
}

func (i Inspector) renderChatInspector() string {
	var sb strings.Builder

	// Title
	sb.WriteString(theme.SectionStyle.Render("INSPECTOR") + "\n\n")

	// Context / Connection
	sb.WriteString(theme.SubtleStyle.Render("PROVIDER") + "\n")
	sb.WriteString(theme.NormalStyle.Render(i.provider) + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("MODEL") + "\n")
	sb.WriteString(theme.NormalStyle.Render(i.model) + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("SESSION") + "\n")
	sb.WriteString(theme.NormalStyle.Render(i.session) + "\n\n")

	// Stats
	sb.WriteString(theme.SectionStyle.Render("STATS") + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("CONTEXT USAGE") + "\n")
	sb.WriteString(renderProgressBar(i.contextPercent, i.width-3) + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("COST (USD)") + "\n")
	sb.WriteString(theme.HighlightStyle.Render(fmt.Sprintf("$%.4f", i.cost)) + "\n\n")

	sb.WriteString(theme.SubtleStyle.Render("MESSAGES") + "\n")
	sb.WriteString(theme.NormalStyle.Render(fmt.Sprintf("%d", i.messageCount)) + "\n\n")

	// Shortcuts (pinned to bottom if we had layout positioning, for now just append)
	// Add padding to push shortcuts to the bottom
	usedLines := 20
	padLines := i.height - usedLines - 5
	if padLines > 0 {
		sb.WriteString(strings.Repeat("\n", padLines))
	}

	sb.WriteString(theme.SectionStyle.Render("SHORTCUTS") + "\n\n")
	sb.WriteString(theme.HintStyle.Render("Esc    Settings\n"))
	sb.WriteString(theme.HintStyle.Render("↑/↓    History\n"))
	sb.WriteString(theme.HintStyle.Render("/help  Commands\n"))

	return sb.String()
}

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
