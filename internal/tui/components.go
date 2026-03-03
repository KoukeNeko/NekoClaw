package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
