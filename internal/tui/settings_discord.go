package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// DiscordSection manages Discord bot configuration (token only).
type DiscordSection struct {
	tokenInput textinput.Model
	loaded     bool
	statusMsg  string
}

func NewDiscordSection() DiscordSection {
	token := textinput.New()
	token.CharLimit = 200
	token.Prompt = "› "
	token.Placeholder = "Bot Token"
	token.EchoMode = textinput.EchoPassword
	token.EchoCharacter = '•'

	return DiscordSection{
		tokenInput: token,
	}
}

func (ds *DiscordSection) HandleConfig(msg DiscordConfigMsg) tea.Cmd {
	if msg.Err != nil {
		ds.statusMsg = "載入失敗: " + msg.Err.Error()
		return nil
	}
	ds.tokenInput.SetValue(msg.Config.BotToken)
	ds.loaded = true
	ds.tokenInput.Focus()
	return nil
}

func (ds *DiscordSection) HandleSave(msg DiscordSaveMsg) tea.Cmd {
	if msg.Err != nil {
		ds.statusMsg = "儲存失敗: " + msg.Err.Error()
		return nil
	}
	ds.statusMsg = "已儲存（需重啟生效）"
	return nil
}

func (ds *DiscordSection) HasActiveInput() bool { return true }

func (ds *DiscordSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	k := msg.String()

	if k == "enter" {
		ds.statusMsg = ""
		return ds.save(apiClient)
	}

	var cmd tea.Cmd
	ds.tokenInput, cmd = ds.tokenInput.Update(msg)
	return cmd
}

func (ds *DiscordSection) save(apiClient *client.APIClient) tea.Cmd {
	token := strings.TrimSpace(ds.tokenInput.Value())
	return saveDiscordConfigCmd(apiClient, core.DiscordConfig{
		BotToken: token,
	})
}

func (ds DiscordSection) View(width, height int) string {
	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Discord Bot"))
	lines = append(lines, "")

	if !ds.loaded {
		lines = append(lines, theme.HintStyle.Render("  載入中..."))
		return strings.Join(lines, "\n")
	}

	lines = append(lines, theme.SectionStyle.Render("› Bot Token"))
	lines = append(lines, "  "+ds.tokenInput.View())
	lines = append(lines, "")

	// Status
	if ds.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render("  "+sanitizeDisplayText(ds.statusMsg)))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}
