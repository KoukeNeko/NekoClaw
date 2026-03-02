package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

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
	pickProvider     pickerPurpose = "provider"
	pickModel        pickerPurpose = "model"
	pickSession      pickerPurpose = "session"
	pickUseProfile   pickerPurpose = "use_profile"
	pickAuthProfile  pickerPurpose = "auth_profile"
	pickAuthRedirect pickerPurpose = "auth_redirect"
	pickManualState  pickerPurpose = "manual_state"
	pickAuthStatus   pickerPurpose = "auth_status"
)

type promptPurpose string

const (
	promptManualStateCustom promptPurpose = "manual_state_custom"
	promptManualCallbackURL promptPurpose = "manual_callback_url"
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
	actionSetAuthProfile
	actionSetAuthRedirect
	actionQuit
)

type menuItem struct {
	Title  string
	Detail string
	Action menuAction
}

var menuItems = []menuItem{
	{Title: "Chat", Detail: "Open conversation panel", Action: actionOpenChat},
	{Title: "Provider", Detail: "Select provider from list", Action: actionSetProvider},
	{Title: "Model", Detail: "Select model from list", Action: actionSetModel},
	{Title: "Session", Detail: "Select session from list", Action: actionSetSession},
	{Title: "OAuth Auto", Detail: "Start OAuth with auto mode", Action: actionAuthLoginAuto},
	{Title: "OAuth Local", Detail: "Force localhost callback mode", Action: actionAuthLoginLocal},
	{Title: "OAuth Remote", Detail: "Force remote/manual mode", Action: actionAuthLoginRemote},
	{Title: "Profiles", Detail: "Show Gemini profile status", Action: actionAuthStatus},
	{Title: "Use Profile", Detail: "Switch runtime profile from list", Action: actionAuthUseProfile},
	{Title: "Manual Complete", Detail: "Complete OAuth with pasted callback/code", Action: actionAuthManualComplete},
	{Title: "Auth Profile", Detail: "Select default profile_id for OAuth start", Action: actionSetAuthProfile},
	{Title: "Auth Redirect", Detail: "Select redirect_uri override", Action: actionSetAuthRedirect},
	{Title: "Quit", Detail: "Exit TUI", Action: actionQuit},
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

type model struct {
	client *client.APIClient

	chatInput   textinput.Model
	promptInput textinput.Model

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

	authProfileID string
	authRedirect  string

	manualState   string
	lastAuthState string

	knownProfiles []client.GeminiAuthProfile
	knownSessions []string
	knownStates   []string
	lines         []chatLine
}

func Run(apiBaseURL string, providerID string, modelID string, sessionID string) error {
	chatInput := textinput.New()
	chatInput.Placeholder = "Type message and press Enter"
	chatInput.CharLimit = 0
	chatInput.Prompt = "▸ "

	promptInput := textinput.New()
	promptInput.CharLimit = 0
	promptInput.Prompt = "▸ "

	initSession := fallback(sessionID, "main")
	m := model{
		client:        client.New(apiBaseURL),
		chatInput:     chatInput,
		promptInput:   promptInput,
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
			text: "Arrow keys navigate lists. Enter confirms. Esc returns.",
		}},
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case chatResultMsg:
		m.pending = false
		if msg.err != nil {
			m.status = "error"
			m.lines = append(m.lines, chatLine{role: "error", text: msg.err.Error()})
			return m, nil
		}
		m.status = fmt.Sprintf("ok · account=%s", msg.response.AccountID)
		m.lines = append(m.lines, chatLine{role: "assistant", text: msg.response.Reply})
		m.addKnownSession(m.sessionID)
		m.view = viewMenu
		return m, nil

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
		switch msg.purpose {
		case pickAuthStatus:
			m.renderProfileStatusLines(m.knownProfiles)
			m.status = fmt.Sprintf("profiles=%d", len(m.knownProfiles))
			m.view = viewMenu
			return m, nil
		case pickUseProfile:
			opts := profilePickerOptions(m.knownProfiles, false)
			if len(opts) == 0 {
				m.lines = append(m.lines, chatLine{role: "system", text: "No Gemini profiles available."})
				m.view = viewMenu
				return m, nil
			}
			m.openPicker(pickUseProfile, "Select Runtime Profile", "Choose profile to use now", opts)
			return m, nil
		case pickAuthProfile:
			opts := profilePickerOptions(m.knownProfiles, true)
			m.openPicker(pickAuthProfile, "Select OAuth profile_id", "Choose default profile_id for login", opts)
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
		case "esc":
			m.chatInput.Blur()
			m.view = viewMenu
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.chatInput.Value())
			if text == "" {
				return m, nil
			}
			m.chatInput.SetValue("")
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
			return m, sendChatCmd(m.client, req)
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
		}
		return m, nil

	case viewPrompt:
		switch key {
		case "esc":
			m.promptInput.Blur()
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
	const columns = 2
	next := m.menuIndex - columns
	if next >= 0 {
		m.menuIndex = next
	}
}

func (m *model) menuMoveDown() {
	const columns = 2
	next := m.menuIndex + columns
	if next < len(menuItems) {
		m.menuIndex = next
	}
}

func (m *model) menuMoveLeft() {
	const columns = 2
	if m.menuIndex%columns == 0 {
		return
	}
	m.menuIndex--
}

func (m *model) menuMoveRight() {
	const columns = 2
	if m.menuIndex%columns == columns-1 {
		return
	}
	next := m.menuIndex + 1
	if next < len(menuItems) {
		m.menuIndex = next
	}
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
		opts := modelOptionsForProvider(m.provider, m.modelID)
		m.openPicker(pickModel, "Select Model", "Choose model", opts)
		return m, nil

	case actionSetSession:
		opts := sessionOptions(m.knownSessions, m.sessionID)
		m.openPicker(pickSession, "Select Session", "Choose active session", opts)
		return m, nil

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

	case actionSetAuthProfile:
		m.pending = true
		m.status = "loading_profiles"
		return m, listGeminiProfilesCmd(m.client, pickAuthProfile)

	case actionSetAuthRedirect:
		opts := redirectOptions(m.authRedirect)
		m.openPicker(pickAuthRedirect, "Select redirect_uri", "Choose redirect URL", opts)
		return m, nil

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
	m.promptInput.SetValue(strings.TrimSpace(initial))
	m.promptInput.Placeholder = hint
	m.promptInput.Focus()
	m.view = viewPrompt
	m.status = "input"
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

	case pickSession:
		m.sessionID = picked.Value
		m.addKnownSession(m.sessionID)
		m.lines = append(m.lines, chatLine{role: "system", text: "session set to " + m.sessionID})
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

	case pickAuthProfile:
		m.authProfileID = strings.TrimSpace(picked.Value)
		m.lines = append(m.lines, chatLine{role: "system", text: "oauth profile_id set to " + fallback(m.authProfileID, "<empty>")})
		m.status = "updated"
		m.view = viewMenu
		return m, nil

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
	}

	m.view = viewMenu
	return m, nil
}

func (m model) submitPrompt() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.promptInput.Value())
	switch m.promptPurpose {
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
		m.view = viewMenu
		return m, completeGeminiOAuthManualCmd(m.client, m.manualState, value)
	}

	m.view = viewMenu
	return m, nil
}

func (m model) startOAuthWithMode(mode string) (tea.Model, tea.Cmd) {
	m.pending = true
	m.status = "oauth_starting"
	m.view = viewMenu
	return m, startGeminiOAuthCmd(m.client, client.GeminiAuthStartRequest{
		ProfileID:   strings.TrimSpace(m.authProfileID),
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
		parts = append(parts, panelStyle.Render(strings.Join(m.renderLogLines(statusStyle, errorStyle, assistantStyle, youStyle), "\n")))
		parts = append(parts, statusStyle.Render("Enter send · Esc back"))
		parts = append(parts, m.chatInput.View())

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

	parts = append(parts, statusStyle.Render("status: "+m.status))
	return strings.Join(parts, "\n")
}

func (m model) menuLayoutWidths() (menuWidth int, summaryWidth int, stacked bool) {
	// Use a safe fallback for environments where terminal size isn't reported yet.
	total := m.width
	if total <= 0 {
		total = 100
	}
	// Very narrow screens: stack panels vertically so nothing overflows.
	if total < 90 {
		w := total - 2
		if w < 34 {
			w = 34
		}
		return w, w, true
	}

	gap := 1
	menuWidth = total * 45 / 100
	if menuWidth < 34 {
		menuWidth = 34
	}
	if menuWidth > 60 {
		menuWidth = 60
	}
	summaryWidth = total - gap - menuWidth
	if summaryWidth < 34 {
		summaryWidth = 34
		menuWidth = total - gap - summaryWidth
		if menuWidth < 34 {
			// Cannot maintain side-by-side safely; fall back to stacked mode.
			w := total - 2
			if w < 34 {
				w = 34
			}
			return w, w, true
		}
	}
	return menuWidth, summaryWidth, false
}

func (m model) renderMenuPanel(selectedStyle, normalStyle lipgloss.Style, width int) string {
	const columns = 2
	outerWidth := width
	if outerWidth <= 0 {
		outerWidth = 36
	}
	textWidth := outerWidth - 4 // border(2) + horizontal padding(2)
	if textWidth < 24 {
		textWidth = 24
	}
	blockWidth := textWidth + 2 // include horizontal padding, exclude border
	if blockWidth > outerWidth-2 {
		blockWidth = outerWidth - 2
		if blockWidth < 24 {
			blockWidth = 24
		}
		textWidth = blockWidth - 2
	}
	colWidth := (textWidth - 2) / columns
	if colWidth < 14 {
		colWidth = 14
	}

	rows := make([]string, 0, (len(menuItems)+columns-1)/columns)
	for row := 0; row*columns < len(menuItems); row++ {
		cells := make([]string, 0, columns)
		for col := 0; col < columns; col++ {
			idx := row*columns + col
			if idx >= len(menuItems) {
				break
			}
			item := menuItems[idx]
			label := item.Title
			if idx == m.menuIndex {
				cells = append(cells, selectedStyle.Width(colWidth).Render(label))
			} else {
				cells = append(cells, normalStyle.Width(colWidth).Render(label))
			}
		}
		rows = append(rows, strings.Join(cells, "  "))
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
	textWidth := outerWidth - 4 // border(2) + horizontal padding(2)
	if textWidth < 20 {
		textWidth = 20
	}
	blockWidth := textWidth + 2 // include horizontal padding, exclude border
	if blockWidth > outerWidth-2 {
		blockWidth = outerWidth - 2
		if blockWidth < 20 {
			blockWidth = 20
		}
		textWidth = blockWidth - 2
	}
	lines := []string{
		"Context",
		clampLine(fmt.Sprintf("provider: %s", m.provider), textWidth),
		clampLine(fmt.Sprintf("model: %s", m.modelID), textWidth),
		clampLine(fmt.Sprintf("session: %s", m.sessionID), textWidth),
		"",
		"OAuth Draft",
		clampLine(fmt.Sprintf("profile_id: %s", fallback(m.authProfileID, "<empty>")), textWidth),
		clampLine("project: <auto discovery>", textWidth),
		clampLine("endpoint: <auto selection>", textWidth),
		clampLine(fmt.Sprintf("redirect: %s", fallback(m.authRedirect, "<default localhost>")), textWidth),
		clampLine(fmt.Sprintf("last_state: %s", fallback(m.lastAuthState, "<none>")), textWidth),
		"",
		"Recent Events",
	}
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

func (m model) renderLogLines(statusStyle, errorStyle, assistantStyle, youStyle lipgloss.Style) []string {
	entries := m.visibleLines(100)
	if m.height > 0 {
		max := m.height - 8
		if max < 5 {
			max = 5
		}
		if len(entries) > max {
			entries = entries[len(entries)-max:]
		}
	}
	out := make([]string, 0, len(entries))
	for _, line := range entries {
		text := fmt.Sprintf("[%s] %s", line.role, line.text)
		switch line.role {
		case "assistant":
			out = append(out, assistantStyle.Render(text))
		case "you":
			out = append(out, youStyle.Render(text))
		case "error":
			out = append(out, errorStyle.Render(text))
		default:
			out = append(out, statusStyle.Render(text))
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
		for _, fallbackProvider := range []string{"mock", "google-gemini-cli"} {
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
		models = []string{"gemini-2.5-pro", "gemini-3-pro-preview", "default"}
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
