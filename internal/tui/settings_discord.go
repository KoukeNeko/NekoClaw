package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// discordFieldCount is the number of editable fields in the Discord section.
const discordFieldCount = 2

// DiscordSection manages Discord bot configuration (token + active channels).
type DiscordSection struct {
	tokenInput    textinput.Model
	channelsInput textinput.Model
	focusField    int // 0 = token, 1 = channels
	loaded        bool
	statusMsg     string
}

func NewDiscordSection() DiscordSection {
	token := textinput.New()
	token.CharLimit = 200
	token.Prompt = "› "
	token.Placeholder = "Bot Token"
	token.EchoMode = textinput.EchoPassword
	token.EchoCharacter = '•'

	channels := textinput.New()
	channels.CharLimit = 500
	channels.Prompt = "› "
	channels.Placeholder = "Channel ID（以逗號分隔）"

	return DiscordSection{
		tokenInput:    token,
		channelsInput: channels,
	}
}

func (ds *DiscordSection) HandleConfig(msg DiscordConfigMsg) tea.Cmd {
	if msg.Err != nil {
		ds.statusMsg = "載入失敗: " + msg.Err.Error()
		return nil
	}
	ds.tokenInput.SetValue(msg.Config.BotToken)
	ds.channelsInput.SetValue(strings.Join(msg.Config.ActiveChannels, ", "))
	ds.loaded = true
	ds.focusField = 0
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

	switch {
	case key.Matches(msg, settingsKeys.Up):
		if ds.focusField > 0 {
			ds.focusField--
			ds.syncFocus()
		}
		return nil

	case key.Matches(msg, settingsKeys.Down):
		if ds.focusField < discordFieldCount-1 {
			ds.focusField++
			ds.syncFocus()
		}
		return nil

	case k == "enter":
		ds.statusMsg = ""
		return ds.save(apiClient)
	}

	// Delegate to focused input.
	var cmd tea.Cmd
	switch ds.focusField {
	case 0:
		ds.tokenInput, cmd = ds.tokenInput.Update(msg)
	case 1:
		ds.channelsInput, cmd = ds.channelsInput.Update(msg)
	}
	return cmd
}

func (ds *DiscordSection) syncFocus() {
	ds.tokenInput.Blur()
	ds.channelsInput.Blur()
	switch ds.focusField {
	case 0:
		ds.tokenInput.Focus()
	case 1:
		ds.channelsInput.Focus()
	}
}

func (ds *DiscordSection) save(apiClient *client.APIClient) tea.Cmd {
	token := strings.TrimSpace(ds.tokenInput.Value())
	raw := strings.TrimSpace(ds.channelsInput.Value())
	var channels []string
	if raw != "" {
		for _, ch := range strings.Split(raw, ",") {
			if id := strings.TrimSpace(ch); id != "" {
				channels = append(channels, id)
			}
		}
	}
	return saveDiscordConfigCmd(apiClient, core.DiscordConfig{
		BotToken:       token,
		ActiveChannels: channels,
	})
}

func (ds DiscordSection) View(width, height int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Discord Bot"))
	lines = append(lines, "")

	if !ds.loaded {
		lines = append(lines, theme.HintStyle.Render("  載入中..."))
		return strings.Join(lines, "\n")
	}

	// Token field
	tokenLabel := "  Bot Token"
	if ds.focusField == 0 {
		tokenLabel = "› Bot Token"
	}
	lines = append(lines, theme.SectionStyle.Render(tokenLabel))
	lines = append(lines, "  "+ds.tokenInput.View())
	lines = append(lines, "")

	// Channels field
	channelsLabel := "  Active Channels（以逗號分隔）"
	if ds.focusField == 1 {
		channelsLabel = "› Active Channels（以逗號分隔）"
	}
	lines = append(lines, theme.SectionStyle.Render(channelsLabel))
	lines = append(lines, "  "+ds.channelsInput.View())
	lines = append(lines, "")

	// Status
	if ds.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render("  "+sanitizeDisplayText(ds.statusMsg)))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}
