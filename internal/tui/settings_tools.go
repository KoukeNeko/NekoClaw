package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// ToolsSection manages tool settings (Brave Search API key, etc.).
type ToolsSection struct {
	braveKeyInput textinput.Model
	loaded        bool
	statusMsg     string
}

func NewToolsSection() ToolsSection {
	braveKey := textinput.New()
	braveKey.CharLimit = 200
	braveKey.Prompt = "› "
	braveKey.Placeholder = "BSA..."
	braveKey.EchoMode = textinput.EchoPassword
	braveKey.EchoCharacter = '•'

	return ToolsSection{
		braveKeyInput: braveKey,
	}
}

func (ts *ToolsSection) HandleConfig(msg ToolsConfigMsg) tea.Cmd {
	if msg.Err != nil {
		ts.statusMsg = "載入失敗: " + msg.Err.Error()
		return nil
	}
	ts.braveKeyInput.SetValue(msg.Config.BraveSearchAPIKey)
	ts.loaded = true
	ts.braveKeyInput.Focus()
	return nil
}

func (ts *ToolsSection) HandleSave(msg ToolsSaveMsg) tea.Cmd {
	if msg.Err != nil {
		ts.statusMsg = "儲存失敗: " + msg.Err.Error()
		return nil
	}
	ts.statusMsg = "已儲存（需重啟生效）"
	return nil
}

func (ts *ToolsSection) HasActiveInput() bool { return true }

func (ts *ToolsSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	k := msg.String()

	if k == "enter" {
		ts.statusMsg = ""
		return ts.save(apiClient)
	}

	var cmd tea.Cmd
	ts.braveKeyInput, cmd = ts.braveKeyInput.Update(msg)
	return cmd
}

func (ts *ToolsSection) save(apiClient *client.APIClient) tea.Cmd {
	key := strings.TrimSpace(ts.braveKeyInput.Value())
	return saveToolsConfigCmd(apiClient, core.ToolsConfig{
		BraveSearchAPIKey: key,
	})
}

func (ts ToolsSection) View(width, height int) string {
	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Tools"))
	lines = append(lines, "")

	if !ts.loaded {
		lines = append(lines, theme.HintStyle.Render("  載入中..."))
		return strings.Join(lines, "\n")
	}

	lines = append(lines, theme.SectionStyle.Render("› Brave Search API Key"))
	lines = append(lines, "  "+ts.braveKeyInput.View())
	lines = append(lines, theme.HintStyle.Render("  啟用 web_search 工具（免費: brave.com/search/api）"))
	lines = append(lines, "")

	if ts.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render("  "+sanitizeDisplayText(ts.statusMsg)))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}
