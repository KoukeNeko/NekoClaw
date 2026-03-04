package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/doeshing/nekoclaw/internal/client"
)

// ViewID identifies the active top-level view.
type ViewID int

const (
	ViewChat ViewID = iota
	ViewSettings
)

// appModel is the root bubbletea model.
type appModel struct {
	chatView ChatView
	settings SettingsView
	sidebar  Sidebar

	currentView    ViewID
	sidebarFocused bool

	client    *client.APIClient
	sessionID string
	provider  string
	modelID   string

	width, height int
}

// Run launches the TUI program.
func Run(apiBaseURL, providerID, modelID, sessionID string) error {
	apiClient := client.New(apiBaseURL)

	session := fallback(sessionID, "main")
	prov := fallback(providerID, "mock")
	model := fallback(modelID, "default")

	m := appModel{
		client:      apiClient,
		sessionID:   session,
		provider:    prov,
		modelID:     model,
		currentView: ViewChat,
		sidebar:     NewSidebar(prov, model, session),
		chatView:    NewChatView(apiClient, prov, model, session, 80, 24),
		settings:    NewSettingsView(apiClient, prov, model, session, 80, 24),
	}

	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}

func (m appModel) Init() tea.Cmd {
	return tea.Batch(
		m.chatView.Init(),
		m.settings.Show(),
		listSessionsCmd(m.client),
		loadSessionTranscriptCmd(m.client, m.sessionID),
	)
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Sidebar on left (25%), Chat on right (75%)
		leftW := (msg.Width * 25) / 100
		rightW := msg.Width - leftW

		m.sidebar.SetSize(leftW, msg.Height)

		contentH := msg.Height
		if contentH < 3 {
			contentH = 3
		}
		m.chatView.SetSize(rightW, contentH)
		m.settings.SetSize(rightW, contentH)
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	// Toggle settings overlay
	case ToggleSettingsMsg:
		if m.currentView == ViewSettings {
			m.currentView = ViewChat
			m.settings.Hide()
			m.sidebarFocused = false
			m.sidebar.SetFocus(false)
			return m, m.chatView.Focus()
		}
		m.currentView = ViewSettings
		m.sidebarFocused = false
		m.sidebar.SetFocus(false)
		return m, m.settings.Show()

	// Open settings overlay to a specific section tab
	case OpenSettingsSectionMsg:
		m.currentView = ViewSettings
		m.sidebarFocused = false
		m.sidebar.SetFocus(false)
		return m, m.settings.ShowSection(msg.Section)

	// Legacy: SwitchViewMsg still works for compatibility
	case SwitchViewMsg:
		if msg.View == ViewSettings {
			m.currentView = ViewSettings
			return m, m.settings.Show()
		}
		m.currentView = ViewChat
		m.settings.Hide()
		return m, m.chatView.Focus()

	// Toggle sidebar focus
	case SidebarToggleFocusMsg:
		m.sidebarFocused = !m.sidebarFocused
		m.sidebar.SetFocus(m.sidebarFocused)
		if m.sidebarFocused {
			m.chatView.Blur()
			return m, nil
		}
		return m, m.chatView.Focus()

	// Session list loaded — forward to both sidebar and settings
	case SessionsListMsg:
		m.sidebar.HandleSessionsList(msg)
		if m.currentView == ViewSettings {
			cmd := m.settings.Update(msg)
			return m, cmd
		}
		return m, nil

	// Record token usage from chat responses
	case ChatResultMsg:
		if msg.Err == nil {
			m.settings.Usage().RecordUsage(msg.Response.Usage, msg.Response.Model)
			m.sidebar.SetCost(m.settings.Usage().TotalCost())
		}
		// Delegate to chat view, then schedule delayed session refresh
		// to pick up auto-generated titles.
		cmd := m.chatView.Update(msg)
		if msg.Err == nil {
			return m, tea.Batch(cmd, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
				return refreshSessionsTickMsg{}
			}))
		}
		return m, cmd

	// Delayed session list reload (picks up auto-generated titles)
	case refreshSessionsTickMsg:
		return m, listSessionsCmd(m.client)

	// Status bar updates
	case StatusUpdateMsg:
		m.sidebar.SetContextPercent(msg.ContextPercent)
		m.sidebar.SetCost(msg.Cost)
		m.sidebar.SetMessageCount(msg.MessageCount)

	// Shared state changes
	case ProviderChangedMsg:
		m.provider = msg.Provider
		m.sidebar.SetProvider(msg.Provider)
		m.chatView.SetProvider(msg.Provider)
		m.settings.SetProvider(msg.Provider)

	case ModelChangedMsg:
		m.modelID = msg.ModelID
		m.sidebar.SetModel(msg.ModelID)
		m.chatView.SetModel(msg.ModelID)
		m.settings.SetModel(msg.ModelID)

	case SessionChangedMsg:
		m.sessionID = msg.SessionID
		m.sidebar.SetCurrentSession(msg.SessionID)
		m.settings.SetSession(msg.SessionID)
		m.settings.Usage().Reset()
		m.sidebar.SetCost(0)
		// Delegate to chatView so it triggers transcript loading
		chatCmd := m.chatView.Update(msg)
		return m, tea.Batch(chatCmd, listSessionsCmd(m.client))

	case TranscriptLoadedMsg:
		// Always route to chatView regardless of current view
		cmd := m.chatView.Update(msg)
		return m, cmd

	case ProfileChangedMsg:
		m.settings.SetActiveProfile(msg.ProfileID)
	}

	// When settings overlay is visible, delegate all events to settings first
	if m.currentView == ViewSettings {
		cmd := m.settings.Update(msg)
		return m, cmd
	}

	// When sidebar is focused, delegate key events to sidebar
	if m.sidebarFocused {
		cmd := m.sidebar.Update(msg)
		return m, cmd
	}

	// Otherwise delegate to chat view
	var cmd tea.Cmd
	cmd = m.chatView.Update(msg)
	return m, cmd
}

func (m appModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	sidebarContent := m.sidebar.View()
	chatContent := m.chatView.View()

	// Sidebar (Left) + Chat (Right)
	rendered := lipgloss.JoinHorizontal(lipgloss.Top, sidebarContent, chatContent)

	// If in Settings mode, overlay the settings modal on top of the dimmed background
	if m.currentView == ViewSettings {
		contentH := m.height
		if contentH < 3 {
			contentH = 3
		}
		rendered = m.settings.RenderOverlay(rendered, m.width, contentH)
	}

	return fitToTerminal(rendered, m.width, m.height)
}
