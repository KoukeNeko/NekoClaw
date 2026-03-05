package tui

import (
	"errors"
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
	authFlowManualCallback
	authFlowAddKey
	authFlowAddKeyName
	authFlowAddAnthropicToken
	authFlowAddAnthropicTokenName
	authFlowAddAnthropicAPIKey
	authFlowAddAnthropicAPIKeyName
	authFlowAddOpenAIKey
	authFlowAddOpenAIKeyName
	authFlowAddOpenAICodexToken
	authFlowAddOpenAICodexTokenName
	authFlowAnthropicBrowserManualComplete
	authFlowOpenAICodexBrowserManualComplete
)

// AuthSection handles Gemini OAuth, AI Studio key, and Anthropic credential management.
type AuthSection struct {
	geminiProfiles    []client.GeminiAuthProfile
	aiStudioProfiles  []client.AIStudioProfile
	anthropicProfiles []client.AnthropicProfile
	openAIProfiles    []client.OpenAIProfile
	openAICodex       []client.OpenAIProfile

	focusArea int // 0=gemini, 1=ai studio, 2=anthropic, 3=openai
	geminiIdx int
	studioIdx int
	anthroIdx int
	openAIIdx int

	// Wizard state
	flow                      AuthFlow
	input                     textinput.Model
	oauthState                string
	aiStudioKeyDraft          string
	anthropicTokenDraft       string
	anthropicKeyDraft         string
	openAIKeyDraft            string
	openAICodexDraft          string
	statusMsg                 string
	browserJobID              string
	browserJobMode            string
	browserJobStatus          string
	browserJobExpiresAt       time.Time
	browserJobEvents          []client.AnthropicBrowserJobEvent
	openAIBrowserJobID        string
	openAIBrowserJobMode      string
	openAIBrowserJobStatus    string
	openAIBrowserJobExpiresAt time.Time
	openAIBrowserJobEvents    []client.OpenAICodexBrowserJobEvent

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

func (as *AuthSection) HandleAnthropicProfiles(msg AnthropicProfilesMsg) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "載入 Anthropic profiles 失敗: " + msg.Err.Error()
		return nil
	}
	as.anthropicProfiles = sortedAnthropicProfiles(msg.Profiles)
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
	as.aiStudioKeyDraft = ""
	as.input.Blur()
	if msg.Err != nil {
		as.statusMsg = "新增 API key 失敗: " + msg.Err.Error()
		return nil
	}
	as.statusMsg = fmt.Sprintf("AI Studio API key 已新增: %s (%s)", msg.Response.ProfileID, msg.Response.KeyHint)
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

func (as *AuthSection) HandleAnthropicAdd(msg AnthropicAddMsg) tea.Cmd {
	as.flow = authFlowNone
	as.anthropicTokenDraft = ""
	as.anthropicKeyDraft = ""
	as.input.Blur()
	if msg.Err != nil {
		as.statusMsg = "新增 Anthropic credential 失敗: " + msg.Err.Error()
		return nil
	}
	if as.browserJobID != "" {
		as.browserJobStatus = "completed"
	}
	as.statusMsg = fmt.Sprintf("Anthropic credential 已新增: %s (%s)", msg.Response.ProfileID, msg.Response.KeyHint)
	return func() tea.Msg {
		return ProviderChangedMsg{Provider: "anthropic"}
	}
}

func (as *AuthSection) HandleAnthropicAction(msg AnthropicProfileActionMsg) tea.Cmd {
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

func (as *AuthSection) HandleOpenAIProfiles(msg OpenAIProfilesMsg) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "載入 OpenAI profiles 失敗: " + msg.Err.Error()
		return nil
	}
	switch strings.TrimSpace(msg.Provider) {
	case "openai-codex":
		as.openAICodex = sortedOpenAIProfiles(msg.Profiles)
	default:
		as.openAIProfiles = sortedOpenAIProfiles(msg.Profiles)
	}
	as.loaded = true
	return nil
}

func (as *AuthSection) HandleOpenAIAdd(msg OpenAIAddMsg) tea.Cmd {
	as.flow = authFlowNone
	as.openAIKeyDraft = ""
	as.openAICodexDraft = ""
	as.input.Blur()
	if msg.Err != nil {
		as.statusMsg = "新增 OpenAI credential 失敗: " + msg.Err.Error()
		return nil
	}
	if strings.TrimSpace(msg.Response.Provider) == "openai-codex" && as.openAIBrowserJobID != "" {
		as.openAIBrowserJobStatus = "completed"
	}
	as.statusMsg = fmt.Sprintf("OpenAI credential 已新增: %s (%s)", msg.Response.ProfileID, msg.Response.KeyHint)
	return func() tea.Msg {
		return ProviderChangedMsg{Provider: strings.TrimSpace(msg.Response.Provider)}
	}
}

func (as *AuthSection) HandleOpenAIAction(msg OpenAIProfileActionMsg) tea.Cmd {
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

func (as *AuthSection) HandleAnthropicBrowserStart(msg AnthropicBrowserStartMsg, apiClient *client.APIClient) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "Anthropic Browser Login 啟動失敗: " + msg.Err.Error()
		return nil
	}
	as.browserJobID = strings.TrimSpace(msg.Response.JobID)
	as.browserJobMode = strings.TrimSpace(msg.Response.Mode)
	as.browserJobStatus = strings.TrimSpace(msg.Response.Status)
	as.browserJobExpiresAt = msg.Response.ExpiresAt
	as.browserJobEvents = nil

	message := strings.TrimSpace(msg.Response.Message)
	if message == "" {
		message = "Anthropic Browser Login 已啟動。"
	}
	as.statusMsg = message

	switch as.browserJobStatus {
	case "running":
		if as.browserJobID == "" {
			return nil
		}
		return pollAnthropicBrowserLoginJobCmd(apiClient, as.browserJobID, time.Second)
	case "manual_required":
		as.startAnthropicBrowserManualFlow()
		return nil
	case "completed":
		return tea.Batch(
			listAnthropicProfilesCmd(apiClient),
			func() tea.Msg { return ProviderChangedMsg{Provider: "anthropic"} },
		)
	default:
		return nil
	}
}

func (as *AuthSection) HandleAnthropicBrowserJob(msg AnthropicBrowserJobMsg, apiClient *client.APIClient) tea.Cmd {
	if msg.Err != nil {
		if isAnthropicBrowserJobTerminalError(msg.Err) {
			as.clearAnthropicBrowserJob()
			as.flow = authFlowNone
			as.input.Blur()
			as.statusMsg = "Browser Login 工作已結束或不存在，已清除本地狀態。"
			return nil
		}
		as.statusMsg = "Anthropic Browser Login 狀態查詢失敗: " + msg.Err.Error()
		return nil
	}
	if as.browserJobID != "" && strings.TrimSpace(msg.Response.JobID) != "" && strings.TrimSpace(msg.Response.JobID) != as.browserJobID {
		return nil
	}

	as.browserJobID = strings.TrimSpace(msg.Response.JobID)
	as.browserJobMode = strings.TrimSpace(msg.Response.Mode)
	as.browserJobStatus = strings.TrimSpace(msg.Response.Status)
	as.browserJobExpiresAt = msg.Response.ExpiresAt
	as.browserJobEvents = make([]client.AnthropicBrowserJobEvent, 0, len(msg.Response.Events))
	for _, event := range msg.Response.Events {
		as.browserJobEvents = append(as.browserJobEvents, client.AnthropicBrowserJobEvent{
			At:      event.At,
			Message: sanitizeDisplayText(event.Message),
		})
	}

	statusText := strings.TrimSpace(msg.Response.Message)
	if statusText == "" {
		statusText = "Anthropic Browser Login 狀態更新。"
	}
	if hint := strings.TrimSpace(msg.Response.ManualHint); hint != "" && (as.browserJobStatus == "failed" || as.browserJobStatus == "manual_required") {
		statusText = statusText + " " + hint
	}
	as.statusMsg = statusText

	switch as.browserJobStatus {
	case "running":
		if as.browserJobID == "" {
			return nil
		}
		return pollAnthropicBrowserLoginJobCmd(apiClient, as.browserJobID, time.Second)
	case "manual_required":
		as.startAnthropicBrowserManualFlow()
		return nil
	case "completed":
		as.flow = authFlowNone
		as.input.Blur()
		return tea.Batch(
			listAnthropicProfilesCmd(apiClient),
			func() tea.Msg { return ProviderChangedMsg{Provider: "anthropic"} },
		)
	case "failed":
		if strings.TrimSpace(msg.Response.ManualHint) != "" {
			as.startAnthropicBrowserManualFlow()
		}
		return nil
	default:
		return nil
	}
}

func (as *AuthSection) HandleAnthropicBrowserCancel(msg AnthropicBrowserCancelMsg) tea.Cmd {
	if msg.Err != nil {
		if isAnthropicBrowserJobTerminalError(msg.Err) {
			as.clearAnthropicBrowserJob()
			as.statusMsg = "Browser Login 工作已結束，已清除本地狀態。"
			return nil
		}
		as.statusMsg = "取消 Browser Login 失敗: " + msg.Err.Error()
		return nil
	}
	as.flow = authFlowNone
	as.input.Blur()
	as.clearAnthropicBrowserJob()
	as.statusMsg = "Anthropic Browser Login 已取消。"
	return nil
}

func (as *AuthSection) HandleOpenAICodexBrowserStart(msg OpenAICodexBrowserStartMsg, apiClient *client.APIClient) tea.Cmd {
	if msg.Err != nil {
		as.statusMsg = "OpenAI Codex Browser Login 啟動失敗: " + msg.Err.Error()
		return nil
	}
	as.openAIBrowserJobID = strings.TrimSpace(msg.Response.JobID)
	as.openAIBrowserJobMode = strings.TrimSpace(msg.Response.Mode)
	as.openAIBrowserJobStatus = strings.TrimSpace(msg.Response.Status)
	as.openAIBrowserJobExpiresAt = msg.Response.ExpiresAt
	as.openAIBrowserJobEvents = nil

	message := strings.TrimSpace(msg.Response.Message)
	if message == "" {
		message = "OpenAI Codex Browser Login 已啟動。"
	}
	as.statusMsg = message

	switch as.openAIBrowserJobStatus {
	case "running":
		if as.openAIBrowserJobID == "" {
			return nil
		}
		return pollOpenAICodexBrowserLoginJobCmd(apiClient, as.openAIBrowserJobID, time.Second)
	case "manual_required":
		as.startOpenAICodexBrowserManualFlow()
		return nil
	case "completed":
		return tea.Batch(
			listOpenAIProfilesCmd(apiClient, "openai-codex"),
			func() tea.Msg { return ProviderChangedMsg{Provider: "openai-codex"} },
		)
	default:
		return nil
	}
}

func (as *AuthSection) HandleOpenAICodexBrowserJob(msg OpenAICodexBrowserJobMsg, apiClient *client.APIClient) tea.Cmd {
	if msg.Err != nil {
		if isOpenAICodexBrowserJobTerminalError(msg.Err) {
			as.clearOpenAICodexBrowserJob()
			as.flow = authFlowNone
			as.input.Blur()
			as.statusMsg = "OpenAI Codex Browser Login 工作已結束或不存在，已清除本地狀態。"
			return nil
		}
		as.statusMsg = "OpenAI Codex Browser Login 狀態查詢失敗: " + msg.Err.Error()
		return nil
	}
	if as.openAIBrowserJobID != "" && strings.TrimSpace(msg.Response.JobID) != "" &&
		strings.TrimSpace(msg.Response.JobID) != as.openAIBrowserJobID {
		return nil
	}

	as.openAIBrowserJobID = strings.TrimSpace(msg.Response.JobID)
	as.openAIBrowserJobMode = strings.TrimSpace(msg.Response.Mode)
	as.openAIBrowserJobStatus = strings.TrimSpace(msg.Response.Status)
	as.openAIBrowserJobExpiresAt = msg.Response.ExpiresAt
	as.openAIBrowserJobEvents = make([]client.OpenAICodexBrowserJobEvent, 0, len(msg.Response.Events))
	for _, event := range msg.Response.Events {
		as.openAIBrowserJobEvents = append(as.openAIBrowserJobEvents, client.OpenAICodexBrowserJobEvent{
			At:      event.At,
			Message: sanitizeDisplayText(event.Message),
		})
	}

	statusText := strings.TrimSpace(msg.Response.Message)
	if statusText == "" {
		statusText = "OpenAI Codex Browser Login 狀態更新。"
	}
	if hint := strings.TrimSpace(msg.Response.ManualHint); hint != "" &&
		(as.openAIBrowserJobStatus == "failed" || as.openAIBrowserJobStatus == "manual_required") {
		statusText = statusText + " " + hint
	}
	as.statusMsg = statusText

	switch as.openAIBrowserJobStatus {
	case "running":
		if as.openAIBrowserJobID == "" {
			return nil
		}
		return pollOpenAICodexBrowserLoginJobCmd(apiClient, as.openAIBrowserJobID, time.Second)
	case "manual_required":
		as.startOpenAICodexBrowserManualFlow()
		return nil
	case "completed":
		as.flow = authFlowNone
		as.input.Blur()
		return tea.Batch(
			listOpenAIProfilesCmd(apiClient, "openai-codex"),
			func() tea.Msg { return ProviderChangedMsg{Provider: "openai-codex"} },
		)
	case "failed":
		if strings.TrimSpace(msg.Response.ManualHint) != "" {
			as.startOpenAICodexBrowserManualFlow()
		}
		return nil
	default:
		return nil
	}
}

func (as *AuthSection) HandleOpenAICodexBrowserCancel(msg OpenAICodexBrowserCancelMsg) tea.Cmd {
	if msg.Err != nil {
		if isOpenAICodexBrowserJobTerminalError(msg.Err) {
			as.clearOpenAICodexBrowserJob()
			as.statusMsg = "OpenAI Codex Browser Login 工作已結束，已清除本地狀態。"
			return nil
		}
		as.statusMsg = "取消 OpenAI Codex Browser Login 失敗: " + msg.Err.Error()
		return nil
	}
	as.flow = authFlowNone
	as.input.Blur()
	as.clearOpenAICodexBrowserJob()
	as.statusMsg = "OpenAI Codex Browser Login 已取消。"
	return nil
}

func (as *AuthSection) HasActiveInput() bool {
	return as.flow != authFlowNone
}

func (as *AuthSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	if as.flow != authFlowNone {
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
	case key.Matches(msg, key.NewBinding(key.WithKeys("o"))):
		return startGeminiOAuthCmd(apiClient, client.GeminiAuthStartRequest{Mode: "auto"})
	case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
		as.startMaskedFlow(authFlowAddKey, "貼上 AI Studio API key")
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("t"))):
		as.startMaskedFlow(authFlowAddAnthropicToken, "貼上 Anthropic setup-token")
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("k"))):
		as.startMaskedFlow(authFlowAddAnthropicAPIKey, "貼上 Anthropic API key")
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("p"))):
		as.startMaskedFlow(authFlowAddOpenAIKey, "貼上 OpenAI API key")
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("x"))):
		as.startMaskedFlow(authFlowAddOpenAICodexToken, "貼上 OpenAI Codex OAuth token")
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("w"))):
		if as.browserJobID != "" && as.browserJobStatus == "running" {
			as.statusMsg = "已有進行中的 Anthropic Browser Login，按 c 可取消。"
			return nil
		}
		if as.openAIBrowserJobID != "" && as.openAIBrowserJobStatus == "running" {
			as.statusMsg = "已有進行中的 OpenAI Codex Browser Login，按 c 可取消。"
			return nil
		}
		return startOpenAICodexBrowserLoginCmd(apiClient, "auto")
	case key.Matches(msg, key.NewBinding(key.WithKeys("b"))):
		if as.browserJobID != "" && as.browserJobStatus == "running" {
			as.statusMsg = "已有進行中的 Browser Login，按 c 可取消。"
			return nil
		}
		return startAnthropicBrowserLoginCmd(apiClient, "auto")
	case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
		if as.browserJobID != "" {
			return cancelAnthropicBrowserLoginCmd(apiClient, as.browserJobID)
		}
		if as.openAIBrowserJobID != "" {
			return cancelOpenAICodexBrowserLoginCmd(apiClient, as.openAIBrowserJobID)
		}
	}
	return nil
}

func (as *AuthSection) startMaskedFlow(flow AuthFlow, placeholder string) {
	as.flow = flow
	as.input.Placeholder = placeholder
	as.input.EchoMode = textinput.EchoPassword
	as.input.EchoCharacter = '•'
	as.input.SetValue("")
	as.input.Focus()
}

func (as *AuthSection) startAnthropicBrowserManualFlow() {
	as.flow = authFlowAnthropicBrowserManualComplete
	as.input.Placeholder = "貼上 Anthropic setup-token"
	as.input.EchoMode = textinput.EchoPassword
	as.input.EchoCharacter = '•'
	as.input.SetValue("")
	as.input.Focus()
}

func (as *AuthSection) clearAnthropicBrowserJob() {
	as.browserJobID = ""
	as.browserJobMode = ""
	as.browserJobStatus = ""
	as.browserJobExpiresAt = time.Time{}
	as.browserJobEvents = nil
}

func (as *AuthSection) startOpenAICodexBrowserManualFlow() {
	as.flow = authFlowOpenAICodexBrowserManualComplete
	as.input.Placeholder = "貼上 OpenAI Codex OAuth token"
	as.input.EchoMode = textinput.EchoPassword
	as.input.EchoCharacter = '•'
	as.input.SetValue("")
	as.input.Focus()
}

func (as *AuthSection) clearOpenAICodexBrowserJob() {
	as.openAIBrowserJobID = ""
	as.openAIBrowserJobMode = ""
	as.openAIBrowserJobStatus = ""
	as.openAIBrowserJobExpiresAt = time.Time{}
	as.openAIBrowserJobEvents = nil
}

func (as *AuthSection) openAICombinedProfiles() []client.OpenAIProfile {
	combined := make([]client.OpenAIProfile, 0, len(as.openAIProfiles)+len(as.openAICodex))
	combined = append(combined, as.openAIProfiles...)
	combined = append(combined, as.openAICodex...)
	return sortedOpenAIProfiles(combined)
}

func isAnthropicBrowserJobTerminalError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch strings.TrimSpace(apiErr.Code) {
	case "job_not_found", "job_expired", "job_cancelled", "job_completed":
		return true
	default:
		return false
	}
}

func isOpenAICodexBrowserJobTerminalError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch strings.TrimSpace(apiErr.Code) {
	case "job_not_found", "job_expired", "job_cancelled", "job_completed":
		return true
	default:
		return false
	}
}

func (as *AuthSection) handleWizardInput(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	k := msg.String()
	if k == "esc" {
		as.flow = authFlowNone
		as.input.Blur()
		as.input.EchoMode = textinput.EchoNormal
		as.aiStudioKeyDraft = ""
		as.anthropicTokenDraft = ""
		as.anthropicKeyDraft = ""
		as.openAIKeyDraft = ""
		as.openAICodexDraft = ""
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
			as.aiStudioKeyDraft = value
			as.flow = authFlowAddKeyName
			as.input.EchoMode = textinput.EchoNormal
			as.input.Placeholder = "顯示名稱（可選）"
			as.input.SetValue("")
			return nil
		case authFlowAddKeyName:
			as.flow = authFlowNone
			as.input.Blur()
			return addAIStudioKeyCmd(apiClient, as.aiStudioKeyDraft, value)
		case authFlowAddAnthropicToken:
			if value == "" {
				return nil
			}
			as.anthropicTokenDraft = value
			as.flow = authFlowAddAnthropicTokenName
			as.input.EchoMode = textinput.EchoNormal
			as.input.Placeholder = "顯示名稱（可選）"
			as.input.SetValue("")
			return nil
		case authFlowAddAnthropicTokenName:
			as.flow = authFlowNone
			as.input.Blur()
			return addAnthropicTokenCmd(apiClient, as.anthropicTokenDraft, value)
		case authFlowAddAnthropicAPIKey:
			if value == "" {
				return nil
			}
			as.anthropicKeyDraft = value
			as.flow = authFlowAddAnthropicAPIKeyName
			as.input.EchoMode = textinput.EchoNormal
			as.input.Placeholder = "顯示名稱（可選）"
			as.input.SetValue("")
			return nil
		case authFlowAddAnthropicAPIKeyName:
			as.flow = authFlowNone
			as.input.Blur()
			return addAnthropicAPIKeyCmd(apiClient, as.anthropicKeyDraft, value)
		case authFlowAddOpenAIKey:
			if value == "" {
				return nil
			}
			as.openAIKeyDraft = value
			as.flow = authFlowAddOpenAIKeyName
			as.input.EchoMode = textinput.EchoNormal
			as.input.Placeholder = "顯示名稱（可選）"
			as.input.SetValue("")
			return nil
		case authFlowAddOpenAIKeyName:
			as.flow = authFlowNone
			as.input.Blur()
			return addOpenAIKeyCmd(apiClient, as.openAIKeyDraft, value)
		case authFlowAddOpenAICodexToken:
			if value == "" {
				return nil
			}
			as.openAICodexDraft = value
			as.flow = authFlowAddOpenAICodexTokenName
			as.input.EchoMode = textinput.EchoNormal
			as.input.Placeholder = "顯示名稱（可選）"
			as.input.SetValue("")
			return nil
		case authFlowAddOpenAICodexTokenName:
			as.flow = authFlowNone
			as.input.Blur()
			return addOpenAICodexTokenCmd(apiClient, as.openAICodexDraft, value)
		case authFlowAnthropicBrowserManualComplete:
			if value == "" {
				return nil
			}
			as.flow = authFlowNone
			as.input.Blur()
			jobID := strings.TrimSpace(as.browserJobID)
			if jobID == "" {
				return addAnthropicTokenCmd(apiClient, value, "")
			}
			return completeAnthropicBrowserManualCmd(apiClient, jobID, value)
		case authFlowOpenAICodexBrowserManualComplete:
			if value == "" {
				return nil
			}
			as.flow = authFlowNone
			as.input.Blur()
			jobID := strings.TrimSpace(as.openAIBrowserJobID)
			if jobID == "" {
				return addOpenAICodexTokenCmd(apiClient, value, "")
			}
			return completeOpenAICodexBrowserManualCmd(apiClient, jobID, value)
		}
	}

	var cmd tea.Cmd
	as.input, cmd = as.input.Update(msg)
	return cmd
}

func (as *AuthSection) handleSelect(apiClient *client.APIClient) tea.Cmd {
	switch as.focusArea {
	case 0:
		if as.geminiIdx < len(as.geminiProfiles) {
			profileID := as.geminiProfiles[as.geminiIdx].ProfileID
			return useGeminiProfileCmd(apiClient, profileID)
		}
	case 1:
		if as.studioIdx < len(as.aiStudioProfiles) {
			profileID := as.aiStudioProfiles[as.studioIdx].ProfileID
			return useAIStudioProfileCmd(apiClient, profileID)
		}
	case 2:
		if as.anthroIdx < len(as.anthropicProfiles) {
			profileID := as.anthropicProfiles[as.anthroIdx].ProfileID
			return useAnthropicProfileCmd(apiClient, profileID)
		}
	case 3:
		profiles := as.openAICombinedProfiles()
		if as.openAIIdx < len(profiles) {
			profile := profiles[as.openAIIdx]
			return useOpenAIProfileCmd(apiClient, profile.Provider, profile.ProfileID)
		}
	}
	return nil
}

func (as *AuthSection) handleDelete(apiClient *client.APIClient) tea.Cmd {
	switch as.focusArea {
	case 1:
		if as.studioIdx < len(as.aiStudioProfiles) {
			profileID := as.aiStudioProfiles[as.studioIdx].ProfileID
			return deleteAIStudioProfileCmd(apiClient, profileID)
		}
	case 2:
		if as.anthroIdx < len(as.anthropicProfiles) {
			profileID := as.anthropicProfiles[as.anthroIdx].ProfileID
			return deleteAnthropicProfileCmd(apiClient, profileID)
		}
	case 3:
		profiles := as.openAICombinedProfiles()
		if as.openAIIdx < len(profiles) {
			profile := profiles[as.openAIIdx]
			return deleteOpenAIProfileCmd(apiClient, profile.Provider, profile.ProfileID)
		}
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
			as.focusArea = 0
			as.geminiIdx = len(as.geminiProfiles) - 1
		}
	case 2:
		if as.anthroIdx > 0 {
			as.anthroIdx--
		} else if len(as.aiStudioProfiles) > 0 {
			as.focusArea = 1
			as.studioIdx = len(as.aiStudioProfiles) - 1
		} else if len(as.geminiProfiles) > 0 {
			as.focusArea = 0
			as.geminiIdx = len(as.geminiProfiles) - 1
		}
	case 3:
		if as.openAIIdx > 0 {
			as.openAIIdx--
		} else if len(as.anthropicProfiles) > 0 {
			as.focusArea = 2
			as.anthroIdx = len(as.anthropicProfiles) - 1
		} else if len(as.aiStudioProfiles) > 0 {
			as.focusArea = 1
			as.studioIdx = len(as.aiStudioProfiles) - 1
		} else if len(as.geminiProfiles) > 0 {
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
			as.focusArea = 1
			as.studioIdx = 0
		} else if len(as.anthropicProfiles) > 0 {
			as.focusArea = 2
			as.anthroIdx = 0
		} else if len(as.openAICombinedProfiles()) > 0 {
			as.focusArea = 3
			as.openAIIdx = 0
		}
	case 1:
		if as.studioIdx < len(as.aiStudioProfiles)-1 {
			as.studioIdx++
		} else if len(as.anthropicProfiles) > 0 {
			as.focusArea = 2
			as.anthroIdx = 0
		} else if len(as.openAICombinedProfiles()) > 0 {
			as.focusArea = 3
			as.openAIIdx = 0
		}
	case 2:
		if as.anthroIdx < len(as.anthropicProfiles)-1 {
			as.anthroIdx++
		} else if len(as.openAICombinedProfiles()) > 0 {
			as.focusArea = 3
			as.openAIIdx = 0
		}
	case 3:
		openAIProfiles := as.openAICombinedProfiles()
		if as.openAIIdx < len(openAIProfiles)-1 {
			as.openAIIdx++
		}
	}
}

func (as AuthSection) View(width, height int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Auth"))
	lines = append(lines, "")

	if as.flow != authFlowNone {
		var title string
		switch as.flow {
		case authFlowManualCallback:
			title = "OAuth Manual Complete"
		case authFlowAddKey:
			title = "新增 AI Studio API Key"
		case authFlowAddKeyName:
			title = "AI Studio Key 顯示名稱"
		case authFlowAddAnthropicToken:
			title = "新增 Anthropic setup-token"
		case authFlowAddAnthropicTokenName:
			title = "Anthropic Token 顯示名稱"
		case authFlowAddAnthropicAPIKey:
			title = "新增 Anthropic API key"
		case authFlowAddAnthropicAPIKeyName:
			title = "Anthropic API key 顯示名稱"
		case authFlowAddOpenAIKey:
			title = "新增 OpenAI API key"
		case authFlowAddOpenAIKeyName:
			title = "OpenAI API key 顯示名稱"
		case authFlowAddOpenAICodexToken:
			title = "新增 OpenAI Codex OAuth token"
		case authFlowAddOpenAICodexTokenName:
			title = "OpenAI Codex token 顯示名稱"
		case authFlowAnthropicBrowserManualComplete:
			title = "Anthropic Browser Manual Complete"
		case authFlowOpenAICodexBrowserManualComplete:
			title = "OpenAI Codex Browser Manual Complete"
		}
		lines = append(lines, theme.SectionStyle.Render(title))
		if as.statusMsg != "" {
			lines = append(lines, theme.SystemStyle.Render(sanitizeDisplayText(as.statusMsg)))
		}
		lines = append(lines, as.input.View())
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render("Enter 確認  ·  Esc 取消"))
		return strings.Join(lines, "\n")
	}

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

	anthropicHeader := "Anthropic"
	if as.focusArea == 2 {
		anthropicHeader = "› Anthropic"
	}
	lines = append(lines, theme.SectionStyle.Render(anthropicHeader))
	if len(as.anthropicProfiles) == 0 {
		lines = append(lines, theme.HintStyle.Render("  尚無 credentials。按 b 啟動 browser login，或按 t/k 手動新增。"))
	} else {
		for i, p := range as.anthropicProfiles {
			prefix := "  "
			style := theme.NormalStyle
			if i == as.anthroIdx && as.focusArea == 2 {
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
			name := fallback(p.DisplayName, p.ProfileID)
			label := fmt.Sprintf("%s [%s] (%s) %s%s", name, fallback(p.Type, "-"), fallback(p.KeyHint, "-"), state, star)
			lines = append(lines, style.Render(clampLine(prefix+label, textW)))
		}
	}

	lines = append(lines, "")

	openAIHeader := "OpenAI / Codex"
	if as.focusArea == 3 {
		openAIHeader = "› OpenAI / Codex"
	}
	lines = append(lines, theme.SectionStyle.Render(openAIHeader))
	openAIProfiles := as.openAICombinedProfiles()
	if len(openAIProfiles) == 0 {
		lines = append(lines, theme.HintStyle.Render("  尚無 credentials。按 w 啟動 OpenAI Codex browser login，按 p 新增 OpenAI API key，按 x 新增 OpenAI Codex token。"))
	} else {
		for i, p := range openAIProfiles {
			prefix := "  "
			style := theme.NormalStyle
			if i == as.openAIIdx && as.focusArea == 3 {
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
			name := fallback(p.DisplayName, p.ProfileID)
			label := fmt.Sprintf("%s {%s} [%s] (%s) %s%s", name, fallback(p.Provider, "-"), fallback(p.Type, "-"), fallback(p.KeyHint, "-"), state, star)
			lines = append(lines, style.Render(clampLine(prefix+label, textW)))
		}
	}

	if as.browserJobID != "" {
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render(clampLine("  [Anthropic] job="+as.browserJobID+" · mode="+fallback(as.browserJobMode, "-")+" · status="+fallback(as.browserJobStatus, "-"), textW)))
		if !as.browserJobExpiresAt.IsZero() {
			lines = append(lines, theme.HintStyle.Render(clampLine("  expires="+as.browserJobExpiresAt.Format(time.RFC3339), textW)))
		}
		if len(as.browserJobEvents) > 0 {
			lines = append(lines, theme.HintStyle.Render("  Recent job events:"))
			start := len(as.browserJobEvents) - 3
			if start < 0 {
				start = 0
			}
			for _, event := range as.browserJobEvents[start:] {
				item := fmt.Sprintf("    %s %s", event.At.Format("15:04:05"), event.Message)
				lines = append(lines, theme.HintStyle.Render(clampLine(item, textW)))
			}
		}
	}

	if as.openAIBrowserJobID != "" {
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render(clampLine("  [OpenAI Codex] job="+as.openAIBrowserJobID+" · mode="+fallback(as.openAIBrowserJobMode, "-")+" · status="+fallback(as.openAIBrowserJobStatus, "-"), textW)))
		if !as.openAIBrowserJobExpiresAt.IsZero() {
			lines = append(lines, theme.HintStyle.Render(clampLine("  expires="+as.openAIBrowserJobExpiresAt.Format(time.RFC3339), textW)))
		}
		if len(as.openAIBrowserJobEvents) > 0 {
			lines = append(lines, theme.HintStyle.Render("  Recent OpenAI job events:"))
			start := len(as.openAIBrowserJobEvents) - 3
			if start < 0 {
				start = 0
			}
			for _, event := range as.openAIBrowserJobEvents[start:] {
				item := fmt.Sprintf("    %s %s", event.At.Format("15:04:05"), event.Message)
				lines = append(lines, theme.HintStyle.Render(clampLine(item, textW)))
			}
		}
	}

	lines = append(lines, "")
	if as.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render(sanitizeDisplayText(as.statusMsg)))
		lines = append(lines, "")
	}

	lines = append(lines, theme.HintStyle.Render("o Gemini OAuth  ·  a AI Studio key  ·  b Anthropic browser login  ·  t/k Anthropic key  ·  w OpenAI Codex browser login  ·  p OpenAI key  ·  x OpenAI Codex token  ·  c 取消 browser job  ·  Enter 選用  ·  d 刪除"))
	return strings.Join(lines, "\n")
}
