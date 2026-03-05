package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// TelegramSection manages Telegram bot configuration (token only).
type TelegramSection struct {
	tokenInput textinput.Model
	loaded     bool
	statusMsg  string
}

func NewTelegramSection() TelegramSection {
	token := textinput.New()
	token.CharLimit = 200
	token.Prompt = "› "
	token.Placeholder = "Bot Token"
	token.EchoMode = textinput.EchoPassword
	token.EchoCharacter = '•'

	return TelegramSection{
		tokenInput: token,
	}
}

func (ts *TelegramSection) HandleConfig(msg TelegramConfigMsg) tea.Cmd {
	if msg.Err != nil {
		ts.statusMsg = "載入失敗: " + msg.Err.Error()
		return nil
	}
	ts.tokenInput.SetValue(msg.Config.BotToken)
	ts.loaded = true
	ts.tokenInput.Focus()
	return nil
}

func (ts *TelegramSection) HandleSave(msg TelegramSaveMsg) tea.Cmd {
	if msg.Err != nil {
		ts.statusMsg = "儲存失敗: " + msg.Err.Error()
		return nil
	}
	ts.statusMsg = "已儲存（需重啟生效）"
	return nil
}

func (ts *TelegramSection) HasActiveInput() bool { return true }

func (ts *TelegramSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	k := msg.String()

	if k == "enter" {
		ts.statusMsg = ""
		return ts.save(apiClient)
	}

	var cmd tea.Cmd
	ts.tokenInput, cmd = ts.tokenInput.Update(msg)
	return cmd
}

func (ts *TelegramSection) save(apiClient *client.APIClient) tea.Cmd {
	token := strings.TrimSpace(ts.tokenInput.Value())
	return saveTelegramConfigCmd(apiClient, core.TelegramConfig{
		BotToken: token,
	})
}

func (ts TelegramSection) View(width, height int) string {
	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Telegram Bot"))
	lines = append(lines, "")

	if !ts.loaded {
		lines = append(lines, theme.HintStyle.Render("  載入中..."))
		return strings.Join(lines, "\n")
	}

	lines = append(lines, theme.SectionStyle.Render("› Bot Token"))
	lines = append(lines, "  "+ts.tokenInput.View())
	lines = append(lines, "")

	if ts.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render("  "+sanitizeDisplayText(ts.statusMsg)))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}
