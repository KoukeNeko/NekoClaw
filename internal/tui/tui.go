package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

type uiView string

const (
	viewMenu   uiView = "menu"
	viewChat   uiView = "chat"
	viewPrompt uiView = "prompt"
	viewPicker uiView = "picker"
)

type pickerPurpose string

const (
	pickProvider          pickerPurpose = "provider"
	pickModel             pickerPurpose = "model"
	pickSession           pickerPurpose = "session"
	pickUseProfile        pickerPurpose = "use_profile"
	pickAuthRedirect      pickerPurpose = "auth_redirect"
	pickManualState       pickerPurpose = "manual_state"
	pickAuthStatus        pickerPurpose = "auth_status"
	pickAIStudioUseKey    pickerPurpose = "ai_studio_use_key"
	pickAIStudioDeleteKey pickerPurpose = "ai_studio_delete_key"
	pickAIStudioModel     pickerPurpose = "ai_studio_model"
)

type promptPurpose string

const (
	promptManualStateCustom promptPurpose = "manual_state_custom"
	promptManualCallbackURL promptPurpose = "manual_callback_url"
	promptAIStudioAPIKey    promptPurpose = "ai_studio_api_key"
	promptAIStudioName      promptPurpose = "ai_studio_name"
	promptNewSession        promptPurpose = "new_session"
)

type menuAction int

const (
	actionOpenChat menuAction = iota
	actionSetProvider
	actionSetModel
	actionSetSession
	actionAuthLoginAuto
	actionAuthLoginLocal
	actionAuthLoginRemote
	actionAuthStatus
	actionAuthUseProfile
	actionAuthManualComplete
	actionSetAuthRedirect
	actionAIStudioAddKey
	actionAIStudioProfiles
	actionAIStudioUseKey
	actionAIStudioDeleteKey
	actionQuit
)

type menuItem struct {
	Title  string
	Detail string
	Action menuAction
}

type menuSection struct {
	Title string
	Items []menuItem
}

var menuSections = []menuSection{
	{
		Title: "Chat",
		Items: []menuItem{
			{Title: "Chat", Detail: "Open conversation panel", Action: actionOpenChat},
			{Title: "Session", Detail: "Select session from list", Action: actionSetSession},
		},
	},
	{
		Title: "Runtime",
		Items: []menuItem{
			{Title: "Provider", Detail: "Select provider from list", Action: actionSetProvider},
			{Title: "Model", Detail: "Select model from list", Action: actionSetModel},
		},
	},
	{
		Title: "Gemini OAuth",
		Items: []menuItem{
			{Title: "OAuth Auto", Detail: "Start OAuth with auto mode", Action: actionAuthLoginAuto},
			{Title: "OAuth Local", Detail: "Force localhost callback mode", Action: actionAuthLoginLocal},
			{Title: "OAuth Remote", Detail: "Force remote/manual mode", Action: actionAuthLoginRemote},
			{Title: "Use Profile", Detail: "List pool profiles (runtime auto-select)", Action: actionAuthUseProfile},
			{Title: "Profiles", Detail: "Show Gemini profile status", Action: actionAuthStatus},
			{Title: "Manual Complete", Detail: "Complete OAuth with pasted callback/code", Action: actionAuthManualComplete},
			{Title: "Auth Redirect", Detail: "Select redirect_uri override", Action: actionSetAuthRedirect},
		},
	},
	{
		Title: "AI Studio Key",
		Items: []menuItem{
			{Title: "Add Key", Detail: "Add and validate API key", Action: actionAIStudioAddKey},
			{Title: "Profiles", Detail: "Show AI Studio key status", Action: actionAIStudioProfiles},
			{Title: "Use Key", Detail: "Set preferred key", Action: actionAIStudioUseKey},
			{Title: "Delete Key", Detail: "Delete API key profile", Action: actionAIStudioDeleteKey},
		},
	},
	{
		Title: "System",
		Items: []menuItem{
			{Title: "Quit", Detail: "Exit TUI", Action: actionQuit},
		},
	},
}

var menuItems = flattenMenuSections(menuSections)

func flattenMenuSections(sections []menuSection) []menuItem {
	out := make([]menuItem, 0, len(sections)*2)
	for _, section := range sections {
		out = append(out, section.Items...)
	}
	return out
}

type pickerOption struct {
	Label string
	Value string
}

type chatLine struct {
	role string
	text string
}

type chatResultMsg struct {
	response core.ChatResponse
	err      error
}

type providersMsg struct {
	providers []string
	err       error
}

type authStartMsg struct {
	response client.GeminiAuthStartResponse
	err      error
}

type authManualCompleteMsg struct {
	response client.GeminiAuthCompleteResponse
	err      error
}

type authProfilesMsg struct {
	profiles []client.GeminiAuthProfile
	purpose  pickerPurpose
	err      error
}

type authUseMsg struct {
	profileID string
	err       error
}

type aiStudioAddKeyMsg struct {
	response client.AIStudioAddKeyResponse
	err      error
}

type aiStudioProfilesMsg struct {
	profiles []client.AIStudioProfile
	purpose  pickerPurpose
	err      error
}

type aiStudioProfileActionMsg struct {
	profileID string
	deleted   bool
	err       error
}

type aiStudioModelsMsg struct {
	response client.AIStudioModelsResponse
	err      error
}

type sessionsListMsg struct {
	sessions []client.SessionInfo
	err      error
}

type sessionDeleteMsg struct {
	sessionID string
	err       error
}

type model struct {
	client *client.APIClient

	chatInput   textinput.Model
	promptInput textinput.Model
	spinner     spinner.Model

	view      uiView
	menuIndex int

	pickerPurpose pickerPurpose
	pickerTitle   string
	pickerHint    string
	pickerOptions []pickerOption
	pickerIndex   int

	promptPurpose promptPurpose
	promptTitle   string
	promptHint    string

	pending bool
	status  string
	width   int
	height  int

	sessionID string
	provider  string
	modelID   string

	authRedirect string

	manualState   string
	lastAuthState string
	activeProfile string
	defaultModel  string
	aiStudioKey   string

	knownProfiles []client.GeminiAuthProfile
	knownAIKeys   []client.AIStudioProfile
	knownSessions []string
	knownStates   []string
	lines         []chatLine

	inputHistory      []string
	inputHistoryIndex int
	inputDraft        string
}

func Run(apiBaseURL string, providerID string, modelID string, sessionID string) error {
	chatInput := textinput.New()
	chatInput.Placeholder = "Type message and press Enter"
	chatInput.CharLimit = 0
	chatInput.Prompt = "› "

	promptInput := textinput.New()
	promptInput.CharLimit = 0
	promptInput.Prompt = "› "

	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	initSession := fallback(sessionID, "main")
	m := model{
		client:        client.New(apiBaseURL),
		chatInput:     chatInput,
		promptInput:   promptInput,
		spinner:       s,
		view:          viewMenu,
		status:        "idle",
		sessionID:     initSession,
		provider:      fallback(providerID, "mock"),
		modelID:       fallback(modelID, "default"),
		authRedirect:  "",
		knownSessions: []string{initSession},
		knownStates:   []string{},
		knownProfiles: nil,
		pickerOptions: nil,
		lines: []chatLine{{
			role: "system",
			text: "Welcome to NekoClaw. Open Chat from menu to start.",
		}},
		inputHistoryIndex: -1,
	}

	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.applyWindowSize(msg.Width, msg.Height)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case chatResultMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: formatChatError(msg.err)})
			return m, nil
		}
		if m.provider == "google-gemini-cli" || m.provider == "google-ai-studio" {
			if strings.EqualFold(m.modelID, "default") {
				m.defaultModel = strings.TrimSpace(msg.response.Model)
			}
			m.activeProfile = strings.TrimSpace(msg.response.AccountID)
		}
		m.status = fmt.Sprintf("ok · account=%s", msg.response.AccountID)
		m.lines = append(m.lines, chatLine{role: "assistant", text: msg.response.Reply})
		m.addKnownSession(m.sessionID)
		m.view = viewChat
		m.chatInput.Focus()

		var output strings.Builder
		prefix := "● " // Claude style might omit prefix, but we add some text distinction
		styledReply := m.wrapAndStyle(prefix+msg.response.Reply, lipgloss.NewStyle().Foreground(lipgloss.Color("10")))
		output.WriteString(styledReply + "\n")
		return m, tea.Printf("%s", output.String())

	case providersMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		opts := providerOptions(msg.providers, m.provider)
		if len(opts) == 0 {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: "no providers available"})
			m.view = viewMenu
			return m, nil
		}
		m.openPicker(pickProvider, "Select Provider", "Choose provider", opts)
		return m, nil

	case authStartMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		m.status = fmt.Sprintf("oauth_start · mode=%s oauth_mode=%s", msg.response.Mode, fallback(msg.response.OAuthMode, "auto"))
		m.lastAuthState = strings.TrimSpace(msg.response.State)
		m.addKnownState(m.lastAuthState)
		m.lines = append(m.lines, chatLine{role: "system", text: fmt.Sprintf(
			"OAuth started: mode=%s oauth_mode=%s state=%s expires_at=%s redirect=%s",
			msg.response.Mode,
			fallback(msg.response.OAuthMode, "auto"),
			msg.response.State,
			msg.response.ExpiresAt.Format(time.RFC3339),
			msg.response.RedirectURI,
		)})
		if msg.response.Mode == "loopback" {
			if err := openExternalURL(msg.response.AuthURL); err != nil {
				m.lines = append(m.lines, chatLine{role: "system", text: "Cannot auto-open browser; open manually: " + msg.response.AuthURL})
			} else {
				m.lines = append(m.lines, chatLine{role: "system", text: "Browser opened. Complete consent there."})
			}
		} else {
			m.lines = append(m.lines, chatLine{role: "system", text: "Manual URL: " + msg.response.AuthURL})
			m.lines = append(m.lines, chatLine{role: "system", text: "Use menu Manual Complete to finish OAuth."})
		}
		m.view = viewMenu
		return m, nil

	case authManualCompleteMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		m.status = "oauth_complete"
		m.provider = "google-gemini-cli"
		m.activeProfile = strings.TrimSpace(msg.response.ProfileID)
		m.defaultModel = ""
		m.lines = append(m.lines, chatLine{role: "system", text: fmt.Sprintf(
			"OAuth complete: profile=%s email=%s project=%s endpoint=%s",
			msg.response.ProfileID,
			fallback(msg.response.Email, "-"),
			fallback(msg.response.ProjectID, "-"),
			fallback(msg.response.ActiveEndpoint, "-"),
		)})
		m.view = viewMenu
		return m, nil

	case authProfilesMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		m.knownProfiles = sortedProfiles(msg.profiles)
		m.refreshActiveProfileFromKnown()
		switch msg.purpose {
		case pickAuthStatus:
			m.renderProfileStatusLines(m.knownProfiles)
			m.status = fmt.Sprintf("profiles=%d", len(m.knownProfiles))
			m.view = viewMenu
			return m, nil
		case pickUseProfile:
			m.renderProfileStatusLines(m.knownProfiles)
			m.status = fmt.Sprintf("profiles=%d", len(m.knownProfiles))
			m.view = viewMenu
			return m, nil
		}
		m.view = viewMenu
		return m, nil

	case authUseMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		m.status = "profile_selected"
		m.provider = "google-gemini-cli"
		m.lines = append(m.lines, chatLine{role: "system", text: "Gemini profile selected: " + msg.profileID})
		m.view = viewMenu
		return m, nil

	case aiStudioAddKeyMsg:
		m.pending = false
		m.aiStudioKey = ""
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		m.provider = "google-ai-studio"
		m.activeProfile = strings.TrimSpace(msg.response.ProfileID)
		m.status = "key_added"
		m.lines = append(m.lines, chatLine{
			role: "system",
			text: fmt.Sprintf(
				"AI Studio key added: %s · %s · preferred=%t",
				msg.response.ProfileID,
				msg.response.KeyHint,
				msg.response.Preferred,
			),
		})
		m.view = viewMenu
		return m, nil

	case aiStudioProfilesMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		m.knownAIKeys = sortedAIStudioProfiles(msg.profiles)
		switch msg.purpose {
		case pickAuthStatus:
			m.renderAIStudioProfileLines(m.knownAIKeys)
			m.status = fmt.Sprintf("ai_studio_profiles=%d", len(m.knownAIKeys))
			m.view = viewMenu
			return m, nil
		case pickAIStudioUseKey:
			opts := aiStudioProfilePickerOptions(m.knownAIKeys)
			if len(opts) == 0 {
				m.lines = append(m.lines, chatLine{role: "system", text: "No AI Studio key profiles found."})
				m.view = viewMenu
				return m, nil
			}
			m.openPicker(pickAIStudioUseKey, "Use AI Studio Key", "Choose preferred key profile", opts)
			return m, nil
		case pickAIStudioDeleteKey:
			opts := aiStudioProfilePickerOptions(m.knownAIKeys)
			if len(opts) == 0 {
				m.lines = append(m.lines, chatLine{role: "system", text: "No AI Studio key profiles found."})
				m.view = viewMenu
				return m, nil
			}
			m.openPicker(pickAIStudioDeleteKey, "Delete AI Studio Key", "Choose key profile to delete", opts)
			return m, nil
		}
		m.view = viewMenu
		return m, nil

	case aiStudioProfileActionMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		if msg.deleted {
			if strings.TrimSpace(m.activeProfile) == strings.TrimSpace(msg.profileID) {
				m.activeProfile = ""
			}
			m.lines = append(m.lines, chatLine{role: "system", text: "AI Studio key deleted: " + msg.profileID})
			m.status = "key_deleted"
		} else {
			m.provider = "google-ai-studio"
			m.activeProfile = strings.TrimSpace(msg.profileID)
			m.lines = append(m.lines, chatLine{role: "system", text: "AI Studio key selected: " + msg.profileID})
			m.status = "key_selected"
		}
		m.view = viewMenu
		return m, nil

	case aiStudioModelsMsg:
		m.pending = false
		if msg.err != nil {
			opts := modelOptionsForProvider("google-ai-studio", m.modelID)
			m.lines = append(m.lines, chatLine{role: "system", text: "AI Studio model list fallback: " + msg.err.Error()})
			m.openPicker(pickModel, "Select Model", "Choose model", opts)
			return m, nil
		}
		opts := aiStudioModelOptions(msg.response.Models, m.modelID)
		m.openPicker(pickAIStudioModel, "Select Model", "Choose AI Studio model", opts)
		return m, nil

	case sessionsListMsg:
		m.pending = false
		if msg.err != nil {
			opts := sessionOptions(m.knownSessions, m.sessionID)
			m.openPicker(pickSession, "Select Session", "Choose active session", opts)
			return m, nil
		}
		for _, s := range msg.sessions {
			m.addKnownSession(s.SessionID)
		}
		opts := sessionOptionsFromInfo(msg.sessions, m.knownSessions, m.sessionID)
		m.openPicker(pickSession, "Select Session", "Choose session (d=delete)", opts)
		return m, nil

	case sessionDeleteMsg:
		m.pending = false
		if msg.err != nil {
			m.lines = append(m.lines, chatLine{role: "error", text: "delete failed: " + msg.err.Error()})
			m.view = viewMenu
			return m, nil
		}
		m.lines = append(m.lines, chatLine{role: "system", text: "deleted session: " + msg.sessionID})
		m.knownSessions = removeFromSlice(m.knownSessions, msg.sessionID)
		if m.sessionID == msg.sessionID {
			m.sessionID = "main"
		}
		m.pending = true
		m.status = "loading_sessions"
		return m, listSessionsCmd(m.client)
	}

	if m.view == viewChat {
		var cmd tea.Cmd
		m.chatInput, cmd = m.chatInput.Update(msg)
		return m, cmd
	}
	if m.view == viewPrompt {
		var cmd tea.Cmd
		m.promptInput, cmd = m.promptInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.pending {
		if key == "esc" {
			m.view = viewMenu
		}
		return m, nil
	}

	switch m.view {
	case viewMenu:
		switch key {
		case "up":
			m.menuMoveUp()
			return m, nil
		case "down":
			m.menuMoveDown()
			return m, nil
		case "left":
			m.menuMoveLeft()
			return m, nil
		case "right":
			m.menuMoveRight()
			return m, nil
		case "enter":
			return m.executeMenuAction()
		case "esc":
			return m, tea.Quit
		default:
			return m, nil
		}

	case viewChat:
		switch key {
		case "ctrl+o":
			m.chatInput.Blur()
			m.view = viewMenu
			m.status = "menu"
			return m, nil
		case "esc":
			m.chatInput.Blur()
			m.view = viewMenu
			return m, nil
		case "up":
			m.historyMoveUp()
			return m, nil
		case "down":
			m.historyMoveDown()
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.chatInput.Value())
			if text == "" {
				return m, nil
			}
			if handled := m.handleLocalChatCommand(text); handled {
				return m, nil
			}
			m.chatInput.SetValue("")
			m.appendInputHistory(text)
			m.lines = append(m.lines, chatLine{role: "you", text: text})
			m.pending = true
			m.status = "sending"
			req := core.ChatRequest{
				SessionID: m.sessionID,
				Surface:   core.SurfaceTUI,
				Provider:  m.provider,
				Model:     m.modelID,
				Message:   text,
			}
			styledPrompt := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("› " + text)
			return m, tea.Batch(tea.Printf("%s\n", styledPrompt), sendChatCmd(m.client, req), m.spinner.Tick)
		}
		var cmd tea.Cmd
		m.chatInput, cmd = m.chatInput.Update(msg)
		return m, cmd

	case viewPicker:
		switch key {
		case "esc":
			m.view = viewMenu
			return m, nil
		case "up", "left":
			if m.pickerIndex > 0 {
				m.pickerIndex--
			}
			return m, nil
		case "down", "right":
			if m.pickerIndex+1 < len(m.pickerOptions) {
				m.pickerIndex++
			}
			return m, nil
		case "enter":
			return m.applyPickerSelection()
		case "d":
			if m.pickerPurpose == pickSession && len(m.pickerOptions) > 0 {
				picked := m.pickerOptions[m.pickerIndex]
				if picked.Value != "__new__" && picked.Value != "" {
					m.pending = true
					m.status = "deleting_session"
					return m, deleteSessionCmd(m.client, picked.Value)
				}
			}
		}
		return m, nil

	case viewPrompt:
		switch key {
		case "esc":
			m.resetPromptInput()
			m.view = viewMenu
			m.status = "cancelled"
			return m, nil
		case "enter":
			return m.submitPrompt()
		}
		var cmd tea.Cmd
		m.promptInput, cmd = m.promptInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *model) menuMoveUp() {
	if m.menuIndex > 0 {
		m.menuIndex--
	}
}

func (m *model) menuMoveDown() {
	if m.menuIndex+1 < len(menuItems) {
		m.menuIndex++
	}
}

func (m *model) menuMoveLeft() {
	m.menuMoveUp()
}

func (m *model) menuMoveRight() {
	m.menuMoveDown()
}

func (m model) executeMenuAction() (tea.Model, tea.Cmd) {
	if m.menuIndex < 0 || m.menuIndex >= len(menuItems) {
		return m, nil
	}
	action := menuItems[m.menuIndex].Action
	switch action {
	case actionOpenChat:
		m.view = viewChat
		m.status = "chat"
		m.chatInput.Focus()
		return m, nil

	case actionSetProvider:
		m.pending = true
		m.status = "loading_providers"
		return m, loadProvidersCmd(m.client)

	case actionSetModel:
		if m.provider == "google-ai-studio" {
			m.pending = true
			m.status = "loading_models"
			return m, listAIStudioModelsCmd(m.client, m.activeProfile)
		}
		opts := modelOptionsForProvider(m.provider, m.modelID)
		m.openPicker(pickModel, "Select Model", "Choose model", opts)
		return m, nil

	case actionSetSession:
		m.pending = true
		m.status = "loading_sessions"
		return m, listSessionsCmd(m.client)

	case actionAuthLoginAuto:
		return m.startOAuthWithMode("auto")
	case actionAuthLoginLocal:
		return m.startOAuthWithMode("local")
	case actionAuthLoginRemote:
		return m.startOAuthWithMode("remote")

	case actionAuthStatus:
		m.pending = true
		m.status = "loading_profiles"
		return m, listGeminiProfilesCmd(m.client, pickAuthStatus)

	case actionAuthUseProfile:
		m.pending = true
		m.status = "loading_profiles"
		return m, listGeminiProfilesCmd(m.client, pickUseProfile)

	case actionAuthManualComplete:
		opts := manualStateOptions(m.knownStates, m.lastAuthState)
		if len(opts) == 0 {
			m.openPrompt(promptManualStateCustom, "Manual OAuth: State", "Paste state", m.lastAuthState)
			return m, nil
		}
		m.openPicker(pickManualState, "Select OAuth state", "Choose recent state or custom", opts)
		return m, nil

	case actionSetAuthRedirect:
		opts := redirectOptions(m.authRedirect)
		m.openPicker(pickAuthRedirect, "Select redirect_uri", "Choose redirect URL", opts)
		return m, nil

	case actionAIStudioAddKey:
		m.openPrompt(promptAIStudioAPIKey, "AI Studio API Key", "Paste API key", "")
		return m, nil

	case actionAIStudioProfiles:
		m.pending = true
		m.status = "loading_ai_studio_profiles"
		return m, listAIStudioProfilesCmd(m.client, pickAuthStatus)

	case actionAIStudioUseKey:
		m.pending = true
		m.status = "loading_ai_studio_profiles"
		return m, listAIStudioProfilesCmd(m.client, pickAIStudioUseKey)

	case actionAIStudioDeleteKey:
		m.pending = true
		m.status = "loading_ai_studio_profiles"
		return m, listAIStudioProfilesCmd(m.client, pickAIStudioDeleteKey)

	case actionQuit:
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) openPicker(purpose pickerPurpose, title, hint string, options []pickerOption) {
	m.pickerPurpose = purpose
	m.pickerTitle = title
	m.pickerHint = hint
	m.pickerOptions = options
	m.pickerIndex = 0
	m.view = viewPicker
	m.status = "select"
}

func (m *model) openPrompt(purpose promptPurpose, title, hint, initial string) {
	m.promptPurpose = purpose
	m.promptTitle = title
	m.promptHint = hint
	m.promptInput.EchoMode = textinput.EchoNormal
	if purpose == promptAIStudioAPIKey {
		m.promptInput.EchoMode = textinput.EchoPassword
		m.promptInput.EchoCharacter = '•'
	}
	m.promptInput.SetValue(strings.TrimSpace(initial))
	m.promptInput.Placeholder = hint
	m.promptInput.Focus()
	m.view = viewPrompt
	m.status = "input"
}

func (m *model) resetPromptInput() {
	m.promptInput.SetValue("")
	m.promptInput.EchoMode = textinput.EchoNormal
	m.promptInput.Blur()
}

func (m model) applyPickerSelection() (tea.Model, tea.Cmd) {
	if len(m.pickerOptions) == 0 || m.pickerIndex < 0 || m.pickerIndex >= len(m.pickerOptions) {
		m.view = viewMenu
		return m, nil
	}
	picked := m.pickerOptions[m.pickerIndex]

	switch m.pickerPurpose {
	case pickProvider:
		m.provider = picked.Value
		m.activeProfile = ""
		m.defaultModel = ""
		m.lines = append(m.lines, chatLine{role: "system", text: "provider set to " + m.provider})
		m.status = "updated"
		m.view = viewMenu
		return m, nil

	case pickModel:
		m.modelID = picked.Value
		m.lines = append(m.lines, chatLine{role: "system", text: "model set to " + m.modelID})
		m.status = "updated"
		m.view = viewMenu
		return m, nil

	case pickAIStudioModel:
		m.modelID = picked.Value
		m.lines = append(m.lines, chatLine{role: "system", text: "model set to " + m.modelID})
		m.status = "updated"
		m.view = viewMenu
		return m, nil

	case pickSession:
		if picked.Value == "__new__" {
			m.openPrompt(promptNewSession, "New Session", "Enter session name", "")
			return m, nil
		}
		m.sessionID = picked.Value
		m.addKnownSession(m.sessionID)
		m.lines = []chatLine{{role: "system", text: "session set to " + m.sessionID}}
		m.status = "updated"
		m.view = viewMenu
		return m, nil

	case pickUseProfile:
		profileID := strings.TrimSpace(picked.Value)
		if profileID == "" {
			m.view = viewMenu
			return m, nil
		}
		m.pending = true
		m.status = "switching_profile"
		m.view = viewMenu
		return m, useGeminiProfileCmd(m.client, profileID)

	case pickAuthRedirect:
		if picked.Value == "<default>" {
			m.authRedirect = ""
		} else {
			m.authRedirect = strings.TrimSpace(picked.Value)
		}
		m.lines = append(m.lines, chatLine{role: "system", text: "oauth redirect set to " + fallback(m.authRedirect, "<default localhost>")})
		m.status = "updated"
		m.view = viewMenu
		return m, nil

	case pickManualState:
		if picked.Value == "<custom>" {
			m.openPrompt(promptManualStateCustom, "Manual OAuth: State", "Paste state", m.lastAuthState)
			return m, nil
		}
		m.manualState = strings.TrimSpace(picked.Value)
		m.openPrompt(promptManualCallbackURL, "Manual OAuth: Callback URL or Code", "Paste callback URL or code", "")
		return m, nil

	case pickAIStudioUseKey:
		profileID := strings.TrimSpace(picked.Value)
		if profileID == "" {
			m.view = viewMenu
			return m, nil
		}
		m.pending = true
		m.status = "switching_key"
		m.view = viewMenu
		return m, useAIStudioProfileCmd(m.client, profileID)

	case pickAIStudioDeleteKey:
		profileID := strings.TrimSpace(picked.Value)
		if profileID == "" {
			m.view = viewMenu
			return m, nil
		}
		m.pending = true
		m.status = "deleting_key"
		m.view = viewMenu
		return m, deleteAIStudioProfileCmd(m.client, profileID)
	}

	m.view = viewMenu
	return m, nil
}

func (m model) submitPrompt() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.promptInput.Value())
	switch m.promptPurpose {
	case promptNewSession:
		if value == "" {
			m.status = "cancelled"
			m.resetPromptInput()
			m.view = viewMenu
			return m, nil
		}
		m.sessionID = value
		m.addKnownSession(value)
		m.lines = []chatLine{{role: "system", text: "new session: " + value}}
		m.status = "updated"
		m.resetPromptInput()
		m.view = viewMenu
		return m, nil

	case promptManualStateCustom:
		if value == "" {
			m.lines = append(m.lines, chatLine{role: "error", text: "state is required"})
			return m, nil
		}
		m.manualState = value
		m.addKnownState(value)
		m.openPrompt(promptManualCallbackURL, "Manual OAuth: Callback URL or Code", "Paste callback URL or code", "")
		return m, nil

	case promptManualCallbackURL:
		if value == "" {
			m.lines = append(m.lines, chatLine{role: "error", text: "callback URL or code is required"})
			return m, nil
		}
		m.pending = true
		m.status = "oauth_completing"
		m.resetPromptInput()
		m.view = viewMenu
		return m, completeGeminiOAuthManualCmd(m.client, m.manualState, value)

	case promptAIStudioAPIKey:
		if value == "" {
			m.lines = append(m.lines, chatLine{role: "error", text: "api key is required"})
			return m, nil
		}
		m.aiStudioKey = value
		m.openPrompt(promptAIStudioName, "AI Studio Display Name", "Optional display name", "")
		return m, nil

	case promptAIStudioName:
		if strings.TrimSpace(m.aiStudioKey) == "" {
			m.lines = append(m.lines, chatLine{role: "error", text: "missing API key draft"})
			m.view = viewMenu
			return m, nil
		}
		m.pending = true
		m.status = "adding_key"
		m.resetPromptInput()
		m.view = viewMenu
		return m, addAIStudioKeyCmd(m.client, m.aiStudioKey, value)
	}

	m.view = viewMenu
	return m, nil
}

func (m model) startOAuthWithMode(mode string) (tea.Model, tea.Cmd) {
	m.pending = true
	m.status = "oauth_starting"
	m.view = viewMenu
	return m, startGeminiOAuthCmd(m.client, client.GeminiAuthStartRequest{
		ProfileID:   "",
		Mode:        strings.TrimSpace(mode),
		RedirectURI: strings.TrimSpace(m.authRedirect),
	})
}

func (m model) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	assistantStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	youStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10")).Padding(0, 1)
	normalItemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Padding(0, 1)
	panelStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	header := fmt.Sprintf("NekoClaw TUI · view=%s · provider=%s · model=%s · session=%s", m.view, m.provider, m.modelID, m.sessionID)
	maxHeaderWidth := m.width
	if maxHeaderWidth <= 0 {
		maxHeaderWidth = 100
	}
	header = clampLine(header, maxHeaderWidth)
	parts := []string{headerStyle.Render(header)}

	switch m.view {
	case viewMenu:
		menuWidth, summaryWidth, stacked := m.menuLayoutWidths()
		menuPanel := m.renderMenuPanel(selectedStyle, normalItemStyle, menuWidth)
		summaryPanel := m.renderSummaryPanel(statusStyle, errorStyle, assistantStyle, youStyle, summaryWidth)
		if stacked {
			parts = append(parts, menuPanel)
			parts = append(parts, summaryPanel)
		} else {
			parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Top, menuPanel, " ", summaryPanel))
		}
		parts = append(parts, statusStyle.Render("↑↓←→ move · Enter execute · Esc quit"))

	case viewChat:
		parts = append(parts, m.renderChatView(statusStyle, errorStyle, assistantStyle, youStyle))

	case viewPicker:
		parts = append(parts, m.renderPicker(panelStyle, selectedStyle, normalItemStyle, statusStyle))
		parts = append(parts, statusStyle.Render("↑↓/←→ select · Enter confirm · Esc back"))

	case viewPrompt:
		body := []string{
			headerStyle.Render(m.promptTitle),
			statusStyle.Render(m.promptHint),
			m.promptInput.View(),
			statusStyle.Render("Enter confirm · Esc cancel"),
		}
		parts = append(parts, panelStyle.Render(strings.Join(body, "\n")))
	}

	if m.view != viewChat {
		parts = append(parts, statusStyle.Render("status: "+m.status))
	}
	rendered := strings.Join(parts, "\n")
	if m.width > 0 {
		return fitToTerminalWidth(rendered, m.width)
	}
	return rendered
}

func (m model) menuLayoutWidths() (menuWidth int, summaryWidth int, stacked bool) {
	total := m.width
	if total <= 0 {
		total = 100
	}
	// Always keep at least one visible character after borders/padding.
	minPanel := 12
	stackedWidth := total - 2
	if stackedWidth < minPanel {
		stackedWidth = minPanel
	}

	// Narrow screens should stack panels.
	if total < 90 {
		return stackedWidth, stackedWidth, true
	}

	gap := 1
	menuWidth = total * 45 / 100
	if menuWidth < minPanel {
		menuWidth = minPanel
	}
	if menuWidth > 60 {
		menuWidth = 60
	}
	summaryWidth = total - gap - menuWidth
	if summaryWidth < minPanel {
		return stackedWidth, stackedWidth, true
	}
	return menuWidth, summaryWidth, false
}

func (m model) renderMenuPanel(selectedStyle, normalStyle lipgloss.Style, width int) string {
	const columns = 1
	outerWidth := width
	if outerWidth <= 0 {
		outerWidth = 36
	}
	blockWidth := outerWidth - 2 // exclude border
	if blockWidth < 8 {
		blockWidth = 8
	}
	textWidth := blockWidth - 2 // exclude horizontal padding
	colWidth := textWidth
	if colWidth < 4 {
		colWidth = 4
	}
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	rows := make([]string, 0, len(menuItems)+len(menuSections)+2)
	globalIdx := 0
	for sectionIndex, section := range menuSections {
		if sectionIndex > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, sectionStyle.Render(clampLine("["+section.Title+"]", textWidth)))
		for row := 0; row*columns < len(section.Items); row++ {
			cells := make([]string, 0, columns)
			for col := 0; col < columns; col++ {
				localIdx := row*columns + col
				if localIdx >= len(section.Items) {
					break
				}
				item := section.Items[localIdx]
				label := item.Title
				if globalIdx+localIdx == m.menuIndex {
					cells = append(cells, selectedStyle.Width(colWidth).Render(label))
				} else {
					cells = append(cells, normalStyle.Width(colWidth).Render(label))
				}
			}
			rows = append(rows, strings.Join(cells, "  "))
		}
		globalIdx += len(section.Items)
	}

	detail := ""
	if m.menuIndex >= 0 && m.menuIndex < len(menuItems) {
		detail = menuItems[m.menuIndex].Detail
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(blockWidth)
	menuLines := append([]string{"Menu"}, rows...)
	menuLines = append(menuLines, "", "Tip: "+detail)
	return box.Render(strings.Join(menuLines, "\n"))
}

func (m model) renderSummaryPanel(statusStyle, errorStyle, assistantStyle, youStyle lipgloss.Style, width int) string {
	outerWidth := width
	if outerWidth <= 0 {
		outerWidth = 42
	}
	blockWidth := outerWidth - 2 // exclude border
	if blockWidth < 8 {
		blockWidth = 8
	}
	textWidth := blockWidth - 2 // exclude horizontal padding
	lines := []string{
		"Context",
		clampLine(fmt.Sprintf("provider: %s", m.provider), textWidth),
		clampLine(fmt.Sprintf("model: %s", m.modelID), textWidth),
		clampLine(fmt.Sprintf("default-model: %s", m.defaultModelDisplay()), textWidth),
		clampLine(fmt.Sprintf("session: %s", m.sessionID), textWidth),
	}
	switch m.provider {
	case "google-gemini-cli":
		lines = append(lines,
			"",
			"OAuth Draft",
			clampLine(fmt.Sprintf("profile_id: %s", m.profileDisplay()), textWidth),
			clampLine(fmt.Sprintf("project: %s", m.projectDisplay()), textWidth),
			clampLine("endpoint: <auto selection>", textWidth),
			clampLine(fmt.Sprintf("redirect: %s", fallback(m.authRedirect, "<default localhost>")), textWidth),
			clampLine(fmt.Sprintf("last_state: %s", fallback(m.lastAuthState, "<none>")), textWidth),
		)
	case "google-ai-studio":
		lines = append(lines,
			"",
			"AI Studio",
			clampLine(fmt.Sprintf("profile_id: %s", m.profileDisplay()), textWidth),
			clampLine(fmt.Sprintf("key_hint: %s", m.aiStudioKeyHintDisplay()), textWidth),
			clampLine("auth: api_key", textWidth),
		)
	}
	lines = append(lines,
		"Recent Events",
	)
	for _, line := range m.visibleLines(8) {
		text := clampLine(fmt.Sprintf("[%s] %s", line.role, line.text), textWidth)
		switch line.role {
		case "assistant":
			lines = append(lines, assistantStyle.Render(text))
		case "you":
			lines = append(lines, youStyle.Render(text))
		case "error":
			lines = append(lines, errorStyle.Render(text))
		default:
			lines = append(lines, statusStyle.Render(text))
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(blockWidth)
	return box.Render(strings.Join(lines, "\n"))
}

func (m *model) applyWindowSize(width, height int) {
	m.width = width
	m.height = height

	inputWidth := width - 2
	if inputWidth < 10 {
		inputWidth = 10
	}
	m.chatInput.Width = inputWidth
	m.promptInput.Width = inputWidth
}

func (m model) renderPicker(panelStyle, selectedStyle, normalStyle, statusStyle lipgloss.Style) string {
	lines := []string{m.pickerTitle, statusStyle.Render(m.pickerHint)}
	if len(m.pickerOptions) == 0 {
		lines = append(lines, statusStyle.Render("No options"))
		return panelStyle.Render(strings.Join(lines, "\n"))
	}
	for idx, opt := range m.pickerOptions {
		prefix := "  "
		if idx == m.pickerIndex {
			prefix = "→ "
		}
		text := prefix + opt.Label
		if idx == m.pickerIndex {
			lines = append(lines, selectedStyle.Render(text))
		} else {
			lines = append(lines, normalStyle.Render(text))
		}
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m model) renderChatTranscript(statusStyle, errorStyle, assistantStyle, youStyle lipgloss.Style) string {
	textWidth, maxLines := m.chatTranscriptLayout()
	return m.renderChatTranscriptWithLimit(statusStyle, errorStyle, assistantStyle, youStyle, textWidth, maxLines)
}

func (m model) renderChatTranscriptWithLimit(
	statusStyle,
	errorStyle,
	assistantStyle,
	youStyle lipgloss.Style,
	textWidth int,
	maxLines int,
) string {
	lines := m.renderLogLines(statusStyle, errorStyle, assistantStyle, youStyle, textWidth)
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	if maxLines > 0 && len(lines) < maxLines {
		pad := make([]string, maxLines-len(lines))
		lines = append(pad, lines...)
	}
	return strings.Join(lines, "\n")
}

func (m model) renderChatView(statusStyle, errorStyle, assistantStyle, youStyle lipgloss.Style) string {
	textWidth, _ := m.chatTranscriptLayout()
	composer := m.renderChatComposer(statusStyle, textWidth, assistantStyle)

	// In inline mode, we don't render the historical transcript in View().
	// It's already printed to the terminal above via tea.Printf.
	// Just return the composer (input box or spinner).
	return "\n" + composer
}

func (m model) renderChatComposer(statusStyle lipgloss.Style, textWidth int, highlightStyle lipgloss.Style) string {
	if textWidth <= 0 {
		textWidth = 80
	}

	if m.pending {
		// Like Claude Code spinner
		text := fmt.Sprintf("%s Bloviating... (thought for...)", m.spinner.View())
		styled := highlightStyle.Render(text)
		return styled
	}

	return m.chatInput.View()
}

func (m model) latestActivity() string {
	for i := len(m.lines) - 1; i >= 0; i-- {
		line := m.lines[i]
		if strings.TrimSpace(line.text) == "" {
			continue
		}
		if line.role == "system" && strings.Contains(strings.ToLower(line.text), "welcome to nekoclaw") {
			continue
		}
		return line.text
	}
	return "No recent activity"
}

func (m model) chatTranscriptLayout() (textWidth int, maxLines int) {
	totalWidth := m.width
	if totalWidth <= 0 {
		totalWidth = 100
	}
	// Keep one extra safety column so rounded borders and prompt markers do not
	// clip at the terminal edge after resize.
	textWidth = totalWidth - 2
	if textWidth < 1 {
		textWidth = 1
	}
	if m.height > 0 {
		maxLines = m.height - 2
		if maxLines < 1 {
			maxLines = 1
		}
	}
	return textWidth, maxLines
}

func (m model) wrapAndStyle(text string, style lipgloss.Style) string {
	textWidth := m.width - 2
	if textWidth < 10 {
		textWidth = 80 // fallback if width not known yet
	}
	wrapped := wrapToWidth(text, textWidth)
	var out []string
	for _, line := range wrapped {
		out = append(out, style.Render(line))
	}
	return strings.Join(out, "\n")
}

func (m model) renderLogLines(statusStyle, errorStyle, assistantStyle, youStyle lipgloss.Style, maxLineWidth int) []string {
	entries := m.visibleLines(200)
	out := make([]string, 0, len(entries))
	for _, line := range entries {
		prefix := "[system] "
		indent := strings.Repeat(" ", len(prefix))
		renderStyle := statusStyle
		switch line.role {
		case "assistant":
			prefix = "" // Claude code doesn't use bullet prefix for assistant response usually, or handles it as markdown
			indent = ""
			renderStyle = assistantStyle
		case "you":
			prefix = "› "
			indent = "  "
			renderStyle = youStyle
		case "error":
			prefix = "✕ "
			indent = "  "
			renderStyle = errorStyle
		}
		contentWidth := maxLineWidth - lipgloss.Width(prefix)
		if contentWidth < 1 {
			contentWidth = 1
		}
		wrapped := wrapToWidth(line.text, contentWidth)
		for idx, chunk := range wrapped {
			renderText := prefix + chunk
			if idx > 0 {
				renderText = indent + chunk
			}
			switch line.role {
			case "assistant":
				out = append(out, renderStyle.Render(renderText))
			case "you":
				out = append(out, renderStyle.Render(renderText))
			case "error":
				out = append(out, renderStyle.Render(renderText))
			default:
				out = append(out, renderStyle.Render(renderText))
			}
		}
	}
	return out
}

func (m model) visibleLines(max int) []chatLine {
	if max <= 0 || len(m.lines) <= max {
		return m.lines
	}
	return m.lines[len(m.lines)-max:]
}

func (m *model) renderProfileStatusLines(profiles []client.GeminiAuthProfile) {
	if len(profiles) == 0 {
		m.lines = append(m.lines, chatLine{role: "system", text: "No Gemini profiles found."})
		return
	}
	for _, profile := range profiles {
		prefix := ""
		if profile.Preferred {
			prefix = "* "
		}
		m.lines = append(m.lines, chatLine{role: "system", text: prefix + formatProfileLine(profile)})
	}
}

func (m *model) renderAIStudioProfileLines(profiles []client.AIStudioProfile) {
	if len(profiles) == 0 {
		m.lines = append(m.lines, chatLine{role: "system", text: "No AI Studio key profiles found."})
		return
	}
	for _, profile := range profiles {
		prefix := ""
		if profile.Preferred {
			prefix = "* "
		}
		state := "available"
		if !profile.Available {
			if !profile.DisabledUntil.IsZero() {
				state = "disabled_until=" + profile.DisabledUntil.Format(time.RFC3339)
			} else if !profile.CooldownUntil.IsZero() {
				state = "cooldown_until=" + profile.CooldownUntil.Format(time.RFC3339)
			} else {
				state = "unavailable"
			}
		}
		if profile.DisabledReason != "" {
			state += " reason=" + profile.DisabledReason
		}
		m.lines = append(m.lines, chatLine{
			role: "system",
			text: fmt.Sprintf(
				"%s%s · name=%s · key=%s · %s",
				prefix,
				profile.ProfileID,
				fallback(profile.DisplayName, "-"),
				fallback(profile.KeyHint, "-"),
				state,
			),
		})
	}
}

func (m *model) refreshActiveProfileFromKnown() {
	if len(m.knownProfiles) == 0 {
		m.activeProfile = ""
		return
	}
	active := strings.TrimSpace(m.activeProfile)
	if active != "" {
		for _, profile := range m.knownProfiles {
			if profile.ProfileID == active {
				return
			}
		}
	}
	for _, profile := range m.knownProfiles {
		if profile.Preferred {
			m.activeProfile = profile.ProfileID
			return
		}
	}
	for _, profile := range m.knownProfiles {
		if profile.Available {
			m.activeProfile = profile.ProfileID
			return
		}
	}
	m.activeProfile = m.knownProfiles[0].ProfileID
}

func (m model) profileDisplay() string {
	if m.provider == "google-ai-studio" {
		profile, ok := m.currentAIStudioProfile()
		if ok {
			return profile.ProfileID
		}
		if active := strings.TrimSpace(m.activeProfile); active != "" {
			return active
		}
		return "<auto by pool>"
	}
	profile, ok := m.currentProfile()
	if ok {
		return profile.ProfileID
	}
	if active := strings.TrimSpace(m.activeProfile); active != "" {
		return active
	}
	return "<auto by pool>"
}

func (m model) projectDisplay() string {
	if m.provider != "google-gemini-cli" {
		return "<n/a>"
	}
	profile, ok := m.currentProfile()
	if !ok {
		return "<auto discovery>"
	}
	if !profile.ProjectReady {
		return "<missing>"
	}
	return "<auto discovered>"
}

func (m model) defaultModelDisplay() string {
	if m.provider != "google-gemini-cli" && m.provider != "google-ai-studio" {
		return "<n/a>"
	}
	if !strings.EqualFold(m.modelID, "default") {
		return "<explicit>"
	}
	if resolved := strings.TrimSpace(m.defaultModel); resolved != "" {
		return resolved
	}
	return "<runtime auto>"
}

func (m model) currentProfile() (client.GeminiAuthProfile, bool) {
	if len(m.knownProfiles) == 0 {
		return client.GeminiAuthProfile{}, false
	}
	active := strings.TrimSpace(m.activeProfile)
	if active != "" {
		for _, profile := range m.knownProfiles {
			if profile.ProfileID == active {
				return profile, true
			}
		}
	}
	for _, profile := range m.knownProfiles {
		if profile.Preferred {
			return profile, true
		}
	}
	return m.knownProfiles[0], true
}

func (m model) currentAIStudioProfile() (client.AIStudioProfile, bool) {
	if len(m.knownAIKeys) == 0 {
		return client.AIStudioProfile{}, false
	}
	active := strings.TrimSpace(m.activeProfile)
	if active != "" {
		for _, profile := range m.knownAIKeys {
			if profile.ProfileID == active {
				return profile, true
			}
		}
	}
	for _, profile := range m.knownAIKeys {
		if profile.Preferred {
			return profile, true
		}
	}
	return m.knownAIKeys[0], true
}

func (m model) aiStudioKeyHintDisplay() string {
	profile, ok := m.currentAIStudioProfile()
	if !ok {
		return "<unknown>"
	}
	return fallback(strings.TrimSpace(profile.KeyHint), "<unknown>")
}

func (m *model) addKnownSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	m.knownSessions = appendUnique(m.knownSessions, sessionID)
}

func (m *model) addKnownState(state string) {
	state = strings.TrimSpace(state)
	if state == "" {
		return
	}
	m.knownStates = appendUnique(m.knownStates, state)
}

func (m *model) appendInputHistory(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != text {
		m.inputHistory = append(m.inputHistory, text)
	}
	m.inputHistoryIndex = -1
	m.inputDraft = ""
}

func (m *model) historyMoveUp() {
	if len(m.inputHistory) == 0 {
		return
	}
	if m.inputHistoryIndex == -1 {
		m.inputDraft = m.chatInput.Value()
		m.inputHistoryIndex = len(m.inputHistory) - 1
	} else if m.inputHistoryIndex > 0 {
		m.inputHistoryIndex--
	}
	if m.inputHistoryIndex >= 0 && m.inputHistoryIndex < len(m.inputHistory) {
		m.chatInput.SetValue(m.inputHistory[m.inputHistoryIndex])
	}
}

func (m *model) historyMoveDown() {
	if len(m.inputHistory) == 0 || m.inputHistoryIndex == -1 {
		return
	}
	if m.inputHistoryIndex < len(m.inputHistory)-1 {
		m.inputHistoryIndex++
		m.chatInput.SetValue(m.inputHistory[m.inputHistoryIndex])
		return
	}
	m.inputHistoryIndex = -1
	m.chatInput.SetValue(m.inputDraft)
	m.inputDraft = ""
}

func (m *model) handleLocalChatCommand(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "/menu":
		m.chatInput.SetValue("")
		m.chatInput.Blur()
		m.view = viewMenu
		m.status = "menu"
		m.lines = append(m.lines, chatLine{role: "system", text: "Opened menu."})
		return true
	case "/clear":
		m.chatInput.SetValue("")
		m.lines = []chatLine{{
			role: "system",
			text: "Chat cleared.",
		}}
		m.status = "chat"
		return true
	case "/help":
		m.chatInput.SetValue("")
		m.lines = append(m.lines, chatLine{
			role: "system",
			text: "Local commands: /help, /menu, /clear",
		})
		m.status = "chat"
		return true
	}
	return false
}

func sortedProfiles(in []client.GeminiAuthProfile) []client.GeminiAuthProfile {
	profiles := append([]client.GeminiAuthProfile(nil), in...)
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Preferred != profiles[j].Preferred {
			return profiles[i].Preferred
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles
}

func sortedAIStudioProfiles(in []client.AIStudioProfile) []client.AIStudioProfile {
	profiles := append([]client.AIStudioProfile(nil), in...)
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Preferred != profiles[j].Preferred {
			return profiles[i].Preferred
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles
}

func providerOptions(providers []string, current string) []pickerOption {
	set := map[string]struct{}{}
	for _, provider := range providers {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			continue
		}
		set[provider] = struct{}{}
	}
	if len(set) == 0 {
		for _, fallbackProvider := range []string{"mock", "google-gemini-cli", "google-ai-studio"} {
			set[fallbackProvider] = struct{}{}
		}
	}
	current = strings.TrimSpace(current)
	if current != "" {
		set[current] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]pickerOption, 0, len(keys))
	for _, key := range keys {
		out = append(out, pickerOption{Label: key, Value: key})
	}
	return out
}

func modelOptionsForProvider(providerID, current string) []pickerOption {
	providerID = strings.TrimSpace(providerID)
	models := []string{"default"}
	switch providerID {
	case "google-gemini-cli":
		models = []string{"default", "gemini-3-pro-preview", "gemini-2.5-pro", "gemini-3-flash-preview", "gemini-2.5-flash"}
	case "google-ai-studio":
		models = []string{"default", "gemini-2.5-pro", "gemini-2.5-flash"}
	case "mock":
		models = []string{"default"}
	}
	if current = strings.TrimSpace(current); current != "" {
		models = appendUnique(models, current)
	}
	out := make([]pickerOption, 0, len(models))
	for _, model := range models {
		out = append(out, pickerOption{Label: model, Value: model})
	}
	return out
}

func sessionOptions(known []string, current string) []pickerOption {
	ids := []string{"main"}
	for _, v := range known {
		ids = appendUnique(ids, strings.TrimSpace(v))
	}
	if current = strings.TrimSpace(current); current != "" {
		ids = appendUnique(ids, current)
	}
	sort.Strings(ids)
	out := make([]pickerOption, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		out = append(out, pickerOption{Label: id, Value: id})
	}
	return out
}

func sessionOptionsFromInfo(persisted []client.SessionInfo, known []string, currentID string) []pickerOption {
	infoByID := map[string]client.SessionInfo{}
	for _, s := range persisted {
		infoByID[s.SessionID] = s
	}

	ids := []string{"main"}
	for _, s := range persisted {
		ids = appendUnique(ids, strings.TrimSpace(s.SessionID))
	}
	for _, id := range known {
		ids = appendUnique(ids, strings.TrimSpace(id))
	}
	if currentID = strings.TrimSpace(currentID); currentID != "" {
		ids = appendUnique(ids, currentID)
	}
	sort.Strings(ids)

	out := []pickerOption{{Label: "+ New Session", Value: "__new__"}}
	for _, id := range ids {
		if id == "" {
			continue
		}
		label := id
		if info, ok := infoByID[id]; ok {
			age := formatTimeAgo(info.UpdatedAt)
			label = fmt.Sprintf("%-20s %3d msgs  %s", id, info.MessageCount, age)
		}
		if id == currentID {
			label += "  *"
		}
		out = append(out, pickerOption{Label: label, Value: id})
	}
	return out
}

func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	dur := time.Since(t)
	switch {
	case dur < time.Minute:
		return "just now"
	case dur < time.Hour:
		return fmt.Sprintf("%dm ago", int(dur.Minutes()))
	case dur < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(dur.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(dur.Hours()/24))
	}
}

func removeFromSlice(items []string, target string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != target {
			out = append(out, item)
		}
	}
	return out
}

func profilePickerOptions(profiles []client.GeminiAuthProfile, allowEmpty bool) []pickerOption {
	out := make([]pickerOption, 0, len(profiles)+1)
	if allowEmpty {
		out = append(out, pickerOption{Label: "<empty>", Value: ""})
	}
	for _, profile := range profiles {
		label := fmt.Sprintf("%s (%s)", profile.ProfileID, fallback(profile.Email, "no-email"))
		out = append(out, pickerOption{Label: label, Value: profile.ProfileID})
	}
	return out
}

func aiStudioProfilePickerOptions(profiles []client.AIStudioProfile) []pickerOption {
	out := make([]pickerOption, 0, len(profiles))
	for _, profile := range profiles {
		label := fmt.Sprintf("%s (%s)", profile.ProfileID, fallback(profile.KeyHint, "no-hint"))
		out = append(out, pickerOption{Label: label, Value: profile.ProfileID})
	}
	return out
}

func aiStudioModelOptions(models []string, current string) []pickerOption {
	ordered := []string{"default"}
	for _, model := range models {
		ordered = appendUnique(ordered, strings.TrimSpace(model))
	}
	if current = strings.TrimSpace(current); current != "" {
		ordered = appendUnique(ordered, current)
	}
	out := make([]pickerOption, 0, len(ordered))
	for _, model := range ordered {
		if model == "" {
			continue
		}
		out = append(out, pickerOption{Label: model, Value: model})
	}
	return out
}

func redirectOptions(current string) []pickerOption {
	opts := []string{"<default>", "http://localhost:8085/oauth2callback", "http://127.0.0.1:8085/oauth2callback"}
	if current = strings.TrimSpace(current); current != "" {
		opts = appendUnique(opts, current)
	}
	out := make([]pickerOption, 0, len(opts))
	for _, v := range opts {
		label := v
		if v == "<default>" {
			label = "<default localhost>"
		}
		out = append(out, pickerOption{Label: label, Value: v})
	}
	return out
}

func manualStateOptions(states []string, fallbackState string) []pickerOption {
	uniq := []string{}
	for _, v := range states {
		if t := strings.TrimSpace(v); t != "" {
			uniq = appendUnique(uniq, t)
		}
	}
	if t := strings.TrimSpace(fallbackState); t != "" {
		uniq = appendUnique(uniq, t)
	}
	if len(uniq) == 0 {
		return []pickerOption{{Label: "<custom state>", Value: "<custom>"}}
	}
	out := make([]pickerOption, 0, len(uniq)+1)
	for _, st := range uniq {
		out = append(out, pickerOption{Label: st, Value: st})
	}
	out = append(out, pickerOption{Label: "<custom state>", Value: "<custom>"})
	return out
}

func formatProfileLine(profile client.GeminiAuthProfile) string {
	state := "available"
	if !profile.Available {
		if !profile.DisabledUntil.IsZero() {
			state = "disabled_until=" + profile.DisabledUntil.Format(time.RFC3339)
		} else if !profile.CooldownUntil.IsZero() {
			state = "cooldown_until=" + profile.CooldownUntil.Format(time.RFC3339)
		} else {
			state = "unavailable"
		}
	}
	if profile.DisabledReason != "" {
		state += " reason=" + profile.DisabledReason
	}
	if !profile.ProjectReady {
		state += " project=missing"
	}
	if reason := strings.TrimSpace(profile.UnavailableReason); reason != "" {
		state += " unavailable_reason=" + reason
	}
	return fmt.Sprintf(
		"%s · email=%s · project=%s · endpoint=%s · %s",
		profile.ProfileID,
		fallback(profile.Email, "-"),
		fallback(profile.ProjectID, "-"),
		fallback(profile.Endpoint, "-"),
		state,
	)
}

func appendUnique(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if strings.TrimSpace(item) == value {
			return items
		}
	}
	return append(items, value)
}

func listSessionsCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sessions, err := apiClient.ListSessions(ctx)
		return sessionsListMsg{sessions: sessions, err: err}
	}
}

func deleteSessionCmd(apiClient *client.APIClient, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := apiClient.DeleteSession(ctx, sessionID)
		return sessionDeleteMsg{sessionID: sessionID, err: err}
	}
}

func sendChatCmd(apiClient *client.APIClient, req core.ChatRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		response, err := apiClient.Chat(ctx, req)
		return chatResultMsg{response: response, err: err}
	}
}

func loadProvidersCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		providers, err := apiClient.Providers(ctx)
		return providersMsg{providers: providers, err: err}
	}
}

func startGeminiOAuthCmd(apiClient *client.APIClient, req client.GeminiAuthStartRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		response, err := apiClient.StartGeminiOAuth(ctx, req)
		return authStartMsg{response: response, err: err}
	}
}

func completeGeminiOAuthManualCmd(apiClient *client.APIClient, state string, callbackURLOrCode string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		response, err := apiClient.CompleteGeminiOAuthManual(ctx, client.GeminiAuthManualCompleteRequest{
			State:             strings.TrimSpace(state),
			CallbackURLOrCode: strings.TrimSpace(callbackURLOrCode),
		})
		return authManualCompleteMsg{response: response, err: err}
	}
}

func listGeminiProfilesCmd(apiClient *client.APIClient, purpose pickerPurpose) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		profiles, err := apiClient.ListGeminiProfiles(ctx)
		return authProfilesMsg{profiles: profiles, purpose: purpose, err: err}
	}
}

func useGeminiProfileCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := apiClient.UseGeminiProfile(ctx, strings.TrimSpace(profileID))
		return authUseMsg{profileID: strings.TrimSpace(profileID), err: err}
	}
}

func addAIStudioKeyCmd(apiClient *client.APIClient, apiKey string, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		response, err := apiClient.AddAIStudioKey(ctx, client.AIStudioAddKeyRequest{
			APIKey:      strings.TrimSpace(apiKey),
			DisplayName: strings.TrimSpace(displayName),
		})
		return aiStudioAddKeyMsg{response: response, err: err}
	}
}

func listAIStudioProfilesCmd(apiClient *client.APIClient, purpose pickerPurpose) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		profiles, err := apiClient.ListAIStudioProfiles(ctx)
		return aiStudioProfilesMsg{profiles: profiles, purpose: purpose, err: err}
	}
}

func useAIStudioProfileCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := apiClient.UseAIStudioProfile(ctx, strings.TrimSpace(profileID))
		return aiStudioProfileActionMsg{profileID: strings.TrimSpace(profileID), err: err}
	}
}

func deleteAIStudioProfileCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := apiClient.DeleteAIStudioProfile(ctx, strings.TrimSpace(profileID))
		return aiStudioProfileActionMsg{profileID: strings.TrimSpace(profileID), deleted: true, err: err}
	}
}

func listAIStudioModelsCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		response, err := apiClient.ListAIStudioModels(ctx, strings.TrimSpace(profileID))
		return aiStudioModelsMsg{response: response, err: err}
	}
}

func fallback(value string, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return strings.TrimSpace(value)
}

func clampLine(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func wrapToWidth(text string, max int) []string {
	if max <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	out := make([]string, 0, (len(runes)/max)+1)
	for len(runes) > 0 {
		n := max
		if n > len(runes) {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func openExternalURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("empty url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func fitToTerminalWidth(rendered string, width int) string {
	if width <= 0 || strings.TrimSpace(rendered) == "" {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	maxWidthStyle := lipgloss.NewStyle().MaxWidth(width)
	for idx, line := range lines {
		if lipgloss.Width(line) <= width {
			continue
		}
		lines[idx] = maxWidthStyle.Render(line)
	}
	return strings.Join(lines, "\n")
}

// formatChatError translates API errors into user-friendly Traditional Chinese messages.
func formatChatError(err error) string {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline exceeded") {
			return "請求逾時，請稍後再試。"
		}
		if strings.Contains(err.Error(), "connection refused") {
			return "無法連線至 API 伺服器，請確認伺服器是否正在執行。"
		}
		return err.Error()
	}

	switch {
	case apiErr.StatusCode == http.StatusConflict: // 409
		msg := "所有帳號暫時不可用。"
		if reason := parseFieldFromMessage(apiErr.Message, "reason"); reason != "" {
			msg += "可能原因：" + translateFailureReason(reason) + "。"
		}
		if retryAt := parseFieldFromMessage(apiErr.Message, "retry_at"); retryAt != "" {
			if t, parseErr := time.Parse(time.RFC3339, retryAt); parseErr == nil {
				remaining := time.Until(t).Round(time.Second)
				if remaining > 0 {
					msg += fmt.Sprintf(" 預計 %s 後可恢復。", remaining)
				}
			}
		}
		msg += " 請稍後再試或新增帳號。"
		return msg
	case apiErr.Code == "missing_project":
		return "Gemini 缺少 Project ID，請重新 OAuth 或設定環境變數 GOOGLE_CLOUD_PROJECT。"
	case apiErr.StatusCode == http.StatusBadGateway: // 502
		return "API 連線失敗，請確認網路或 provider 狀態。"
	case apiErr.StatusCode == http.StatusServiceUnavailable: // 503
		return "服務暫時不可用，請稍後再試。"
	default:
		return apiErr.Error()
	}
}

// parseFieldFromMessage extracts "key=value" from a message like
// "no available account: provider=google-gemini-cli reason=unknown retry_at=2026-03-02T22:25:53+08:00"
func parseFieldFromMessage(message, key string) string {
	prefix := key + "="
	idx := strings.Index(message, prefix)
	if idx < 0 {
		return ""
	}
	rest := message[idx+len(prefix):]
	if spaceIdx := strings.IndexByte(rest, ' '); spaceIdx >= 0 {
		return rest[:spaceIdx]
	}
	return rest
}

// translateFailureReason maps failure reason codes to user-facing labels.
func translateFailureReason(reason string) string {
	switch reason {
	case "rate_limit":
		return "API 請求頻率限制"
	case "billing":
		return "帳單/配額問題"
	case "auth", "auth_permanent":
		return "驗證失敗"
	case "timeout":
		return "請求逾時"
	case "model_not_found":
		return "找不到指定模型"
	case "format":
		return "請求格式錯誤"
	default:
		return "未知原因"
	}
}
