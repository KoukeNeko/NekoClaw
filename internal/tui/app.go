package tui

import (
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
	chatView  ChatView
	settings  SettingsView
	inspector Inspector

	currentView ViewID

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
		inspector:   NewInspector(prov, model, session),
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
	return tea.Batch(m.chatView.Init(), m.settings.Show())
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// macOS Settings Style: left is ~25-30% for Sidebar (Inspector), right is 70-75% for Content
		// Chat Style: left is 75% for Chat, right is 25% for Inspector
		// To simplify state machine resizing, we assign fixed ratios: Inspector always 25%, Content/Chat 75%
		leftW := (msg.Width * 75) / 100
		rightW := msg.Width - leftW

		m.inspector.SetSize(rightW, msg.Height)

		contentH := msg.Height
		if contentH < 3 {
			contentH = 3
		}
		m.chatView.SetSize(leftW, contentH)
		m.settings.SetSize(leftW, contentH)
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
			return m, m.chatView.Focus()
		}
		m.currentView = ViewSettings
		return m, m.settings.Show()

	// Legacy: SwitchViewMsg still works for compatibility
	case SwitchViewMsg:
		if msg.View == ViewSettings {
			m.currentView = ViewSettings
			return m, m.settings.Show()
		}
		m.currentView = ViewChat
		m.settings.Hide()
		return m, m.chatView.Focus()

	// Record token usage from chat responses
	case ChatResultMsg:
		if msg.Err == nil {
			m.settings.Usage().RecordUsage(msg.Response.Usage, msg.Response.Model)
			m.inspector.SetCost(m.settings.Usage().TotalCost())
		}

	// Status bar updates
	case StatusUpdateMsg:
		m.inspector.SetContextPercent(msg.ContextPercent)
		m.inspector.SetCost(msg.Cost)
		m.inspector.SetMessageCount(msg.MessageCount)

	// Shared state changes
	case ProviderChangedMsg:
		m.provider = msg.Provider
		m.inspector.SetProvider(msg.Provider)
		m.chatView.SetProvider(msg.Provider)
		m.settings.SetProvider(msg.Provider)

	case ModelChangedMsg:
		m.modelID = msg.ModelID
		m.inspector.SetModel(msg.ModelID)
		m.chatView.SetModel(msg.ModelID)
		m.settings.SetModel(msg.ModelID)

	case SessionChangedMsg:
		m.sessionID = msg.SessionID
		m.inspector.SetSession(msg.SessionID)
		m.chatView.SetSession(msg.SessionID)
		m.settings.SetSession(msg.SessionID)
		m.settings.Usage().Reset()
		m.inspector.SetCost(0)

	case ProfileChangedMsg:
		m.settings.SetActiveProfile(msg.ProfileID)
	}

	// When settings overlay is visible, delegate all events to settings first
	if m.currentView == ViewSettings {
		cmd := m.settings.Update(msg)
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

	chatContent := m.chatView.View()
	inspectorContent := m.inspector.View()

	// The background is always Chat (Left) + Inspector (Right)
	rendered := lipgloss.JoinHorizontal(lipgloss.Top, chatContent, inspectorContent)

	// If in Settings mode, overlay the settings modal on top of the dimmed background
	if m.currentView == ViewSettings {
		contentH := m.height
		if contentH < 3 {
			contentH = 3
		}
		rendered = m.settings.RenderOverlay(rendered, m.width, contentH)
	}

	if m.width > 0 {
		return fitToTerminalWidth(rendered, m.width)
	}
	return rendered
}
