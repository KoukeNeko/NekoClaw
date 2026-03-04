package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

// PersonaSection displays persona selection in the settings overlay.
type PersonaSection struct {
	personas      []client.PersonaInfo
	activeDirName string
	cursor        int
	loading       bool
	err           error
}

// NewPersonaSection creates an empty persona section.
func NewPersonaSection() PersonaSection {
	return PersonaSection{}
}

// HasActiveInput returns false — this section has no text inputs.
func (ps *PersonaSection) HasActiveInput() bool { return false }

// Update handles key events within the persona section.
func (ps *PersonaSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	total := len(ps.personas)

	switch msg.String() {
	case "up", "k":
		if ps.cursor > 0 {
			ps.cursor--
		}
	case "down", "j":
		if ps.cursor < total-1 {
			ps.cursor++
		}
	case "enter":
		if ps.cursor < total {
			selected := ps.personas[ps.cursor]
			if selected.DirName == ps.activeDirName {
				// Already active — deactivate.
				return clearPersonaCmd(apiClient)
			}
			return usePersonaCmd(apiClient, selected.DirName)
		}
	case "d":
		// Deactivate current persona.
		if ps.activeDirName != "" {
			return clearPersonaCmd(apiClient)
		}
	case "r":
		ps.loading = true
		return reloadPersonasCmd(apiClient)
	case "o":
		return openPersonasDirCmd()
	}
	return nil
}

// HandlePersonasList processes the API response for persona list.
func (ps *PersonaSection) HandlePersonasList(msg PersonasListMsg) tea.Cmd {
	ps.loading = false
	ps.err = msg.Err
	if msg.Err == nil {
		ps.personas = msg.Personas
	}
	if ps.cursor >= len(ps.personas) {
		ps.cursor = max(0, len(ps.personas)-1)
	}
	return nil
}

// HandlePersonaActive processes the active persona response.
func (ps *PersonaSection) HandlePersonaActive(msg PersonaActiveMsg) tea.Cmd {
	if msg.Err != nil {
		ps.err = msg.Err
		return nil
	}
	if msg.Persona != nil {
		ps.activeDirName = msg.Persona.DirName
	} else {
		ps.activeDirName = ""
	}
	return nil
}

// HandlePersonaUse processes the result of activating a persona.
func (ps *PersonaSection) HandlePersonaUse(msg PersonaUseMsg) tea.Cmd {
	if msg.Err != nil {
		ps.err = msg.Err
		return nil
	}
	ps.activeDirName = msg.DirName
	return nil
}

// HandlePersonaClear processes the result of deactivating a persona.
func (ps *PersonaSection) HandlePersonaClear(msg PersonaClearMsg) tea.Cmd {
	if msg.Err != nil {
		ps.err = msg.Err
		return nil
	}
	ps.activeDirName = ""
	return nil
}

// View renders the persona section content.
func (ps *PersonaSection) View(width int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Personas"))
	lines = append(lines, "")

	if ps.loading {
		lines = append(lines, theme.HintStyle.Render("  載入中..."))
		return strings.Join(lines, "\n")
	}

	if ps.err != nil {
		lines = append(lines, theme.ErrorStyle.Render("  錯誤: "+ps.err.Error()))
		lines = append(lines, "")
	}

	// Active persona indicator.
	if ps.activeDirName != "" {
		activeName := ps.activeDirName
		for _, p := range ps.personas {
			if p.DirName == ps.activeDirName {
				activeName = p.Name
				break
			}
		}
		lines = append(lines, theme.HighlightStyle.Render(fmt.Sprintf("  目前角色: %s", activeName)))
		lines = append(lines, "")
	}

	if len(ps.personas) == 0 {
		ps.renderEmptyState(&lines)
		return strings.Join(lines, "\n")
	}

	// Persona list.
	for i, p := range ps.personas {
		prefix := "  "
		if i == ps.cursor {
			prefix = "▸ "
		}

		suffix := ""
		if p.DirName == ps.activeDirName {
			suffix = " ✓"
		}

		label := fmt.Sprintf("%s%s%s", prefix, p.Name, suffix)
		label = clampLine(label, textW)

		if i == ps.cursor {
			lines = append(lines, theme.SelectedStyle.Render(label))
			// Show description for selected persona.
			if p.Description != "" {
				descLine := fmt.Sprintf("    %s", p.Description)
				lines = append(lines, theme.HintStyle.Render(clampLine(descLine, textW)))
			}
			dirLine := fmt.Sprintf("    目錄: %s", p.DirName)
			lines = append(lines, theme.SubtleStyle.Render(clampLine(dirLine, textW)))
		} else if p.DirName == ps.activeDirName {
			lines = append(lines, theme.HighlightStyle.Render(label))
		} else {
			lines = append(lines, theme.NormalStyle.Render(label))
		}
	}

	lines = append(lines, "")
	lines = append(lines, theme.HintStyle.Render("↑↓ 選擇  ·  Enter 啟用/停用  ·  d 停用  ·  r 重新載入  ·  o 開啟資料夾  ·  Esc 返回"))

	return strings.Join(lines, "\n")
}

// renderEmptyState renders help text when no personas are configured.
func (ps *PersonaSection) renderEmptyState(lines *[]string) {
	*lines = append(*lines, theme.HintStyle.Render("  尚未設定任何角色。"))
	*lines = append(*lines, "")
	*lines = append(*lines, theme.HintStyle.Render("  角色目錄：~/.nekoclaw/personas/"))
	*lines = append(*lines, theme.HintStyle.Render("  每個角色一個子資料夾，包含："))
	*lines = append(*lines, theme.HintStyle.Render("    config.yaml  — 設定與 system template"))
	*lines = append(*lines, theme.HintStyle.Render("    anchors.yaml — few-shot 範例 (選用)"))
	*lines = append(*lines, theme.HintStyle.Render("    lore.md      — 角色知識庫 (選用)"))
	*lines = append(*lines, "")
	*lines = append(*lines, theme.HintStyle.Render("r 重新載入  ·  o 開啟資料夾  ·  Esc 返回"))
}

// openPersonasDirCmd opens the personas directory in the system file manager.
func openPersonasDirCmd() tea.Cmd {
	return func() tea.Msg {
		dir := resolvePersonasDir()
		// Ensure the directory exists so the file manager doesn't complain.
		_ = os.MkdirAll(dir, 0o755)
		_ = openExternalURL(dir)
		return nil
	}
}

// resolvePersonasDir returns the personas directory path.
func resolvePersonasDir() string {
	if envDir := strings.TrimSpace(os.Getenv("NEKOCLAW_PERSONAS_DIR")); envDir != "" {
		return envDir
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".nekoclaw", "personas")
	}
	return "./personas"
}
