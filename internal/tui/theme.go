package tui

import "github.com/charmbracelet/lipgloss"

// Theme holds all lipgloss styles for the TUI, matching Claude Code's
// muted terminal-native aesthetic.
type Theme struct {
	// Colors
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Error     lipgloss.Color
	Success   lipgloss.Color
	Muted     lipgloss.Color
	Normal    lipgloss.Color
	Border    lipgloss.Color

	// Prompt
	PromptStyle lipgloss.Style // cyan bold ">" character

	// Chat roles
	UserStyle      lipgloss.Style // bold white for user messages
	AssistantStyle lipgloss.Style // default foreground for assistant
	SystemStyle    lipgloss.Style // dim gray for system messages
	ErrorStyle     lipgloss.Style // red for errors

	// Status bar / Inspector
	StatusBarStyle   lipgloss.Style // overall status bar
	StatusModelStyle lipgloss.Style // bold white model/provider text
	StatusMetaStyle  lipgloss.Style // gray metadata (session, context, cost)
	SubtleStyle      lipgloss.Style
	HighlightStyle   lipgloss.Style

	// Menu / Picker
	SelectedStyle lipgloss.Style
	NormalStyle   lipgloss.Style
	SectionStyle  lipgloss.Style
	HeaderStyle   lipgloss.Style
	HintStyle     lipgloss.Style

	// Panels
	PanelStyle  lipgloss.Style
	BorderStyle lipgloss.Style

	// Input
	PlaceholderStyle lipgloss.Style
	CursorStyle      lipgloss.Style

	// Dialog / Overlay
	DialogOverlayStyle lipgloss.Style
	DialogBoxStyle     lipgloss.Style
	DialogButtonStyle  lipgloss.Style
	DimStyle           lipgloss.Style // for dimming background content

	// Settings tabs
	TabActiveStyle   lipgloss.Style
	TabInactiveStyle lipgloss.Style
}

// DefaultTheme returns the default NekoClaw theme inspired by Claude Code.
func DefaultTheme() Theme {
	primary := lipgloss.Color("6")    // cyan
	secondary := lipgloss.Color("8")  // gray
	accent := lipgloss.Color("15")    // bright white
	errorColor := lipgloss.Color("9") // red
	success := lipgloss.Color("10")   // green
	muted := lipgloss.Color("8")      // gray
	normal := lipgloss.Color("7")
	border := lipgloss.Color("236")

	return Theme{
		Primary:   primary,
		Secondary: secondary,
		Accent:    accent,
		Error:     errorColor,
		Success:   success,
		Muted:     muted,
		Normal:    normal,
		Border:    border,

		PromptStyle: lipgloss.NewStyle().Bold(true).Foreground(primary),

		UserStyle:      lipgloss.NewStyle().Bold(true).Foreground(accent),
		AssistantStyle: lipgloss.NewStyle(), // default terminal foreground
		SystemStyle:    lipgloss.NewStyle().Foreground(muted),
		ErrorStyle:     lipgloss.NewStyle().Foreground(errorColor),

		StatusBarStyle:   lipgloss.NewStyle().Foreground(muted).Background(lipgloss.Color("235")).Padding(0, 1),
		StatusModelStyle: lipgloss.NewStyle().Bold(true).Foreground(accent).Background(lipgloss.Color("235")),
		StatusMetaStyle:  lipgloss.NewStyle().Foreground(muted).Background(lipgloss.Color("235")),
		SubtleStyle:      lipgloss.NewStyle().Foreground(muted),
		HighlightStyle:   lipgloss.NewStyle().Foreground(primary).Bold(true),

		SelectedStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(primary).Padding(0, 1),
		NormalStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Padding(0, 1),
		SectionStyle:  lipgloss.NewStyle().Bold(true).Foreground(primary),
		HeaderStyle:   lipgloss.NewStyle().Bold(true).Foreground(primary).Padding(0, 1),
		HintStyle:     lipgloss.NewStyle().Foreground(muted).Italic(true),

		PanelStyle:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(secondary).Padding(0, 1),
		BorderStyle: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(secondary),

		PlaceholderStyle: lipgloss.NewStyle().Foreground(muted),
		CursorStyle:      lipgloss.NewStyle().Foreground(primary),

		DialogOverlayStyle: lipgloss.NewStyle(),
		DialogBoxStyle:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(primary).Padding(1, 2),
		DialogButtonStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(primary).Padding(0, 2),
		DimStyle:           lipgloss.NewStyle().Foreground(lipgloss.Color("237")),

		TabActiveStyle:   lipgloss.NewStyle().Bold(true).Underline(true).Foreground(primary),
		TabInactiveStyle: lipgloss.NewStyle().Foreground(muted),
	}
}

var theme = DefaultTheme()
