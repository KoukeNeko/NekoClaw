package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// Discord settings field indices.
const (
	discordFieldToken = iota
	discordFieldReply
	discordFieldConsole
	discordFieldCount
)

// DiscordSection manages Discord bot configuration.
type DiscordSection struct {
	tokenInput   textinput.Model
	consoleInput textinput.Model

	replyToOriginal bool // toggle field

	focusField int
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

	console := textinput.New()
	console.CharLimit = 30
	console.Prompt = "› "
	console.Placeholder = "Channel ID"

	return DiscordSection{
		tokenInput:      token,
		consoleInput:    console,
		replyToOriginal: true, // default: reply to original
	}
}

func (ds *DiscordSection) HandleConfig(msg DiscordConfigMsg) tea.Cmd {
	if msg.Err != nil {
		ds.statusMsg = "載入失敗: " + msg.Err.Error()
		return nil
	}
	ds.tokenInput.SetValue(msg.Config.BotToken)
	ds.replyToOriginal = msg.Config.ShouldReplyToOriginal()
	ds.consoleInput.SetValue(msg.Config.ConsoleChannel)
	ds.loaded = true
	ds.focusField = discordFieldToken
	ds.updateInputFocus()
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

func (ds *DiscordSection) HasActiveInput() bool {
	return ds.focusField == discordFieldToken || ds.focusField == discordFieldConsole
}

func (ds *DiscordSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	k := msg.String()

	switch k {
	case "up":
		if ds.focusField > 0 {
			ds.focusField--
			ds.updateInputFocus()
		}
		return nil
	case "down":
		if ds.focusField < discordFieldCount-1 {
			ds.focusField++
			ds.updateInputFocus()
		}
		return nil
	case "enter":
		if ds.focusField == discordFieldReply {
			// Toggle reply mode.
			ds.replyToOriginal = !ds.replyToOriginal
			ds.statusMsg = ""
			return ds.save(apiClient)
		}
		// Save on enter for text fields.
		ds.statusMsg = ""
		return ds.save(apiClient)
	}

	// Delegate to the active text input.
	switch ds.focusField {
	case discordFieldToken:
		var cmd tea.Cmd
		ds.tokenInput, cmd = ds.tokenInput.Update(msg)
		return cmd
	case discordFieldConsole:
		var cmd tea.Cmd
		ds.consoleInput, cmd = ds.consoleInput.Update(msg)
		return cmd
	}

	return nil
}

func (ds *DiscordSection) updateInputFocus() {
	ds.tokenInput.Blur()
	ds.consoleInput.Blur()

	switch ds.focusField {
	case discordFieldToken:
		ds.tokenInput.Focus()
	case discordFieldConsole:
		ds.consoleInput.Focus()
	}
}

func (ds *DiscordSection) save(apiClient *client.APIClient) tea.Cmd {
	token := strings.TrimSpace(ds.tokenInput.Value())
	console := strings.TrimSpace(ds.consoleInput.Value())
	reply := ds.replyToOriginal
	return saveDiscordConfigCmd(apiClient, core.DiscordConfig{
		BotToken:        token,
		ReplyToOriginal: &reply,
		ConsoleChannel:  console,
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

	// Field 0: Bot Token
	label := "Bot Token"
	if ds.focusField == discordFieldToken {
		label = "› " + label
	} else {
		label = "  " + label
	}
	lines = append(lines, theme.SectionStyle.Render(label))
	lines = append(lines, "  "+ds.tokenInput.View())
	lines = append(lines, "")

	// Field 1: Reply Mode (toggle)
	replyLabel := "回覆原始訊息"
	replyValue := "✅ 開啟"
	if !ds.replyToOriginal {
		replyValue = "❌ 關閉"
	}
	if ds.focusField == discordFieldReply {
		replyLabel = "› " + replyLabel
	} else {
		replyLabel = "  " + replyLabel
	}
	lines = append(lines, theme.SectionStyle.Render(replyLabel)+"  "+replyValue)
	lines = append(lines, "")

	// Field 2: Console Channel
	consoleLabel := "Console Channel"
	if ds.focusField == discordFieldConsole {
		consoleLabel = "› " + consoleLabel
	} else {
		consoleLabel = "  " + consoleLabel
	}
	lines = append(lines, theme.SectionStyle.Render(consoleLabel))
	lines = append(lines, "  "+ds.consoleInput.View())
	lines = append(lines, "")

	// Status
	if ds.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render("  "+sanitizeDisplayText(ds.statusMsg)))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}
