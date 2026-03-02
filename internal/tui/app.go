package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
	statusBar StatusBarComponent

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
		client:    apiClient,
		sessionID: session,
		provider:  prov,
		modelID:   model,
		statusBar: NewStatusBar(prov, model, session),
		chatView:  NewChatView(apiClient, prov, model, session, 80, 24),
		settings:  NewSettingsView(apiClient, prov, model, session, 80, 24),
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
		m.statusBar.SetSize(msg.Width)
		contentH := msg.Height - StatusBarHeight()
		if contentH < 3 {
			contentH = 3
		}
		m.chatView.SetSize(msg.Width, contentH)
		m.settings.SetSize(msg.Width, contentH)
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	// Toggle settings overlay
	case ToggleSettingsMsg:
		if m.settings.visible {
			m.settings.Hide()
			return m, m.chatView.Focus()
		}
		return m, m.settings.Show()

	// Legacy: SwitchViewMsg still works for compatibility
	case SwitchViewMsg:
		if msg.View == ViewSettings {
			return m, m.settings.Show()
		}
		m.settings.Hide()
		return m, m.chatView.Focus()

	// Record token usage from chat responses
	case ChatResultMsg:
		if msg.Err == nil {
			m.settings.Usage().RecordUsage(msg.Response.Usage, msg.Response.Model)
			m.statusBar.SetCost(m.settings.Usage().TotalCost())
		}

	// Status bar updates
	case StatusUpdateMsg:
		m.statusBar.SetContextPercent(msg.ContextPercent)
		m.statusBar.SetCost(msg.Cost)
		m.statusBar.SetMessageCount(msg.MessageCount)

	// Shared state changes
	case ProviderChangedMsg:
		m.provider = msg.Provider
		m.statusBar.SetProvider(msg.Provider)
		m.chatView.SetProvider(msg.Provider)
		m.settings.SetProvider(msg.Provider)

	case ModelChangedMsg:
		m.modelID = msg.ModelID
		m.statusBar.SetModel(msg.ModelID)
		m.chatView.SetModel(msg.ModelID)
		m.settings.SetModel(msg.ModelID)

	case SessionChangedMsg:
		m.sessionID = msg.SessionID
		m.statusBar.SetSession(msg.SessionID)
		m.chatView.SetSession(msg.SessionID)
		m.settings.SetSession(msg.SessionID)
		m.settings.Usage().Reset()
		m.statusBar.SetCost(0)

	case ProfileChangedMsg:
		m.settings.SetActiveProfile(msg.ProfileID)
	}

	// When settings overlay is visible, delegate all events to settings first
	if m.settings.visible {
		cmd := m.settings.Update(msg)
		return m, cmd
	}

	// Otherwise delegate to chat view
	var cmd tea.Cmd
	cmd = m.chatView.Update(msg)
	return m, cmd
}

func (m appModel) View() string {
	var sb strings.Builder

	// Chat content (always rendered)
	chatContent := m.chatView.View()

	if m.settings.visible {
		// Render settings overlay on top of dimmed chat
		contentH := m.height - StatusBarHeight()
		if contentH < 3 {
			contentH = 3
		}
		chatContent = m.settings.RenderOverlay(chatContent, m.width, contentH)
	}

	sb.WriteString(chatContent)

	// Pad remaining height
	rendered := sb.String()
	renderedLines := strings.Count(rendered, "\n") + 1
	remaining := m.height - renderedLines - StatusBarHeight()
	if remaining > 0 {
		rendered += strings.Repeat("\n", remaining)
	}

	// Status bar
	rendered += "\n" + m.statusBar.View()

	if m.width > 0 {
		return fitToTerminalWidth(rendered, m.width)
	}
	return rendered
}
