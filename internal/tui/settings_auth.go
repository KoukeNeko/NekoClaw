package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

// AuthFlow tracks the current auth wizard step.
type AuthFlow int

const (
	authFlowNone AuthFlow = iota
	authFlowOAuthStarted
	authFlowManualState
	authFlowManualCallback
	authFlowAddKey
	authFlowAddKeyName
)

// AuthSection handles Gemini OAuth and AI Studio key management.
type AuthSection struct {
	geminiProfiles  []client.GeminiAuthProfile
	aiStudioProfiles []client.AIStudioProfile

	focusArea int // 0=gemini profiles, 1=ai studio profiles, 2=actions
	geminiIdx int
	studioIdx int

	// Wizard state
	flow     AuthFlow
	input    textinput.Model
	oauthState string
	apiKeyDraft string
	statusMsg   string

	loaded bool
}

func NewAuthSection() AuthSection {
	ti := textinput.New()
	ti.CharLimit = 0
	ti.Prompt = "› "
	return AuthSection{input: ti}
}

func (as *AuthSection) HandleProfiles(msg AuthProfilesMsg) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "載入 Gemini profiles 失敗: " + msg.Err.Error()
		return nil
	}
	as.geminiProfiles = sortedProfiles(msg.Profiles)
	as.loaded = true
	return nil
}

func (as *AuthSection) HandleAIStudioProfiles(msg AIStudioProfilesMsg) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "載入 AI Studio profiles 失敗: " + msg.Err.Error()
		return nil
	}
	as.aiStudioProfiles = sortedAIStudioProfiles(msg.Profiles)
	as.loaded = true
	return nil
}

func (as *AuthSection) HandleAuthStart(msg AuthStartMsg) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "OAuth 啟動失敗: " + msg.Err.Error()
		as.flow = authFlowNone
		return nil
	}
	as.oauthState = msg.Response.State
	if msg.Response.Mode == "loopback" {
		_ = openExternalURL(msg.Response.AuthURL)
		as.statusMsg = "已開啟瀏覽器，請完成授權。"
	} else {
		as.statusMsg = fmt.Sprintf("請手動開啟以下網址完成授權：\n%s\n\n授權後請貼上 callback URL 或 code。", msg.Response.AuthURL)
		as.flow = authFlowManualCallback
		as.input.Placeholder = "貼上 callback URL 或 code"
		as.input.SetValue("")
		as.input.Focus()
	}
	return nil
}

func (as *AuthSection) HandleAuthManualComplete(msg AuthManualCompleteMsg) tea.Cmd {
	as.flow = authFlowNone
	as.input.Blur()
	if msg.Err != nil {
		as.statusMsg = "OAuth 完成失敗: " + msg.Err.Error()
		return nil
	}
	as.statusMsg = fmt.Sprintf("OAuth 完成！profile=%s email=%s", msg.Response.ProfileID, fallback(msg.Response.Email, "-"))
	return func() tea.Msg {
		return ProviderChangedMsg{Provider: "google-gemini-cli"}
	}
}

func (as *AuthSection) HandleUseProfile(msg AuthUseMsg) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "切換 profile 失敗: " + msg.Err.Error()
		return nil
	}
	as.statusMsg = "已切換至 profile: " + msg.ProfileID
	return func() tea.Msg {
		return ProfileChangedMsg{ProfileID: msg.ProfileID}
	}
}

func (as *AuthSection) HandleAddKey(msg AIStudioAddKeyMsg) tea.Cmd {
	as.flow = authFlowNone
	as.apiKeyDraft = ""
	as.input.Blur()
	if msg.Err != nil {
		as.statusMsg = "新增 API key 失敗: " + msg.Err.Error()
		return nil
	}
	as.statusMsg = fmt.Sprintf("API key 已新增: %s (%s)", msg.Response.ProfileID, msg.Response.KeyHint)
	return func() tea.Msg {
		return ProviderChangedMsg{Provider: "google-ai-studio"}
	}
}

func (as *AuthSection) HandleAIStudioAction(msg AIStudioProfileActionMsg) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "操作失敗: " + msg.Err.Error()
		return nil
	}
	if msg.Deleted {
		as.statusMsg = "已刪除: " + msg.ProfileID
	} else {
		as.statusMsg = "已選用: " + msg.ProfileID
	}
	return nil
}

func (as *AuthSection) HasActiveInput() bool {
	return as.flow != authFlowNone
}

func (as *AuthSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	// If in wizard flow with text input
	if as.flow == authFlowManualCallback || as.flow == authFlowAddKey || as.flow == authFlowAddKeyName {
		return as.handleWizardInput(msg, apiClient)
	}

	switch {
	case key.Matches(msg, settingsKeys.Up):
		as.moveUp()
	case key.Matches(msg, settingsKeys.Down):
		as.moveDown()
	case key.Matches(msg, settingsKeys.Select):
		return as.handleSelect(apiClient)
	case key.Matches(msg, settingsKeys.Delete):
		return as.handleDelete(apiClient)

	// Quick action keys
	case key.Matches(msg, key.NewBinding(key.WithKeys("o"))):
		// Start OAuth auto
		return startGeminiOAuthCmd(apiClient, client.GeminiAuthStartRequest{Mode: "auto"})
	case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
		// Add AI Studio key
		as.flow = authFlowAddKey
		as.input.Placeholder = "貼上 API key"
		as.input.EchoMode = textinput.EchoPassword
		as.input.EchoCharacter = '•'
		as.input.SetValue("")
		as.input.Focus()
		return nil
	}
	return nil
}

func (as *AuthSection) handleWizardInput(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	k := msg.String()
	if k == "esc" {
		as.flow = authFlowNone
		as.input.Blur()
		as.input.EchoMode = textinput.EchoNormal
		return nil
	}
	if k == "enter" {
		value := strings.TrimSpace(as.input.Value())
		switch as.flow {
		case authFlowManualCallback:
			if value == "" {
				return nil
			}
			as.flow = authFlowNone
			as.input.Blur()
			return completeGeminiOAuthManualCmd(apiClient, as.oauthState, value)
		case authFlowAddKey:
			if value == "" {
				return nil
			}
			as.apiKeyDraft = value
			as.flow = authFlowAddKeyName
			as.input.EchoMode = textinput.EchoNormal
			as.input.Placeholder = "顯示名稱（可選）"
			as.input.SetValue("")
			return nil
		case authFlowAddKeyName:
			as.flow = authFlowNone
			as.input.Blur()
			return addAIStudioKeyCmd(apiClient, as.apiKeyDraft, value)
		}
	}

	// Pass to textinput
	var cmd tea.Cmd
	as.input, cmd = as.input.Update(msg)
	return cmd
}

func (as *AuthSection) handleSelect(apiClient *client.APIClient) tea.Cmd {
	switch as.focusArea {
	case 0: // Gemini profile
		if as.geminiIdx < len(as.geminiProfiles) {
			profileID := as.geminiProfiles[as.geminiIdx].ProfileID
			return useGeminiProfileCmd(apiClient, profileID)
		}
	case 1: // AI Studio profile
		if as.studioIdx < len(as.aiStudioProfiles) {
			profileID := as.aiStudioProfiles[as.studioIdx].ProfileID
			return useAIStudioProfileCmd(apiClient, profileID)
		}
	}
	return nil
}

func (as *AuthSection) handleDelete(apiClient *client.APIClient) tea.Cmd {
	if as.focusArea == 1 && as.studioIdx < len(as.aiStudioProfiles) {
		profileID := as.aiStudioProfiles[as.studioIdx].ProfileID
		return deleteAIStudioProfileCmd(apiClient, profileID)
	}
	return nil
}

func (as *AuthSection) moveUp() {
	switch as.focusArea {
	case 0:
		if as.geminiIdx > 0 {
			as.geminiIdx--
		}
	case 1:
		if as.studioIdx > 0 {
			as.studioIdx--
		} else if len(as.geminiProfiles) > 0 {
			// Jump to last gemini profile
			as.focusArea = 0
			as.geminiIdx = len(as.geminiProfiles) - 1
		}
	}
}

func (as *AuthSection) moveDown() {
	switch as.focusArea {
	case 0:
		if as.geminiIdx < len(as.geminiProfiles)-1 {
			as.geminiIdx++
		} else if len(as.aiStudioProfiles) > 0 {
			// Jump to first studio profile
			as.focusArea = 1
			as.studioIdx = 0
		}
	case 1:
		if as.studioIdx < len(as.aiStudioProfiles)-1 {
			as.studioIdx++
		}
	}
}

func (as AuthSection) View(width int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Auth"))
	lines = append(lines, "")

	// Wizard input mode
	if as.flow == authFlowManualCallback || as.flow == authFlowAddKey || as.flow == authFlowAddKeyName {
		var title string
		switch as.flow {
		case authFlowManualCallback:
			title = "OAuth Manual Complete"
		case authFlowAddKey:
			title = "新增 AI Studio API Key"
		case authFlowAddKeyName:
			title = "API Key 顯示名稱"
		}
		lines = append(lines, theme.SectionStyle.Render(title))
		if as.statusMsg != "" {
			lines = append(lines, theme.SystemStyle.Render(as.statusMsg))
		}
		lines = append(lines, as.input.View())
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render("Enter 確認  ·  Esc 取消"))
		return strings.Join(lines, "\n")
	}

	// Gemini OAuth Profiles
	geminiHeader := "Gemini OAuth"
	if as.focusArea == 0 {
		geminiHeader = "› Gemini OAuth"
	}
	lines = append(lines, theme.SectionStyle.Render(geminiHeader))

	if len(as.geminiProfiles) == 0 {
		lines = append(lines, theme.HintStyle.Render("  尚無 profiles。按 o 啟動 OAuth。"))
	} else {
		for i, p := range as.geminiProfiles {
			prefix := "  "
			style := theme.NormalStyle
			if i == as.geminiIdx && as.focusArea == 0 {
				prefix = "› "
				style = theme.SelectedStyle
			}
			star := ""
			if p.Preferred {
				star = " ★"
			}
			state := "✓"
			if !p.Available {
				state = "✗"
				if !p.CooldownUntil.IsZero() {
					state = fmt.Sprintf("冷卻至 %s", p.CooldownUntil.Format(time.Kitchen))
				}
			}
			label := fmt.Sprintf("%s (%s) %s%s", p.ProfileID, fallback(p.Email, "-"), state, star)
			lines = append(lines, style.Render(clampLine(prefix+label, textW)))
		}
	}

	lines = append(lines, "")

	// AI Studio Key Profiles
	studioHeader := "AI Studio Keys"
	if as.focusArea == 1 {
		studioHeader = "› AI Studio Keys"
	}
	lines = append(lines, theme.SectionStyle.Render(studioHeader))

	if len(as.aiStudioProfiles) == 0 {
		lines = append(lines, theme.HintStyle.Render("  尚無 keys。按 a 新增。"))
	} else {
		for i, p := range as.aiStudioProfiles {
			prefix := "  "
			style := theme.NormalStyle
			if i == as.studioIdx && as.focusArea == 1 {
				prefix = "› "
				style = theme.SelectedStyle
			}
			star := ""
			if p.Preferred {
				star = " ★"
			}
			state := "✓"
			if !p.Available {
				state = "✗"
			}
			label := fmt.Sprintf("%s (%s) %s%s", fallback(p.DisplayName, p.ProfileID), fallback(p.KeyHint, "-"), state, star)
			lines = append(lines, style.Render(clampLine(prefix+label, textW)))
		}
	}

	lines = append(lines, "")

	// Status
	if as.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render(as.statusMsg))
		lines = append(lines, "")
	}

	// Hints
	lines = append(lines, theme.HintStyle.Render("o OAuth  ·  a 新增 key  ·  Enter 選用  ·  d 刪除"))

	return strings.Join(lines, "\n")
}
