package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/doeshing/nekoclaw/internal/client"
)

// SettingsSection identifies a section within the settings overlay.
type SettingsSection int

const (
	SectionProvider SettingsSection = iota
	SectionPersona
	SectionAuth
	SectionSessions
	SectionMemory
	SectionUsage
	SectionMCP
	SectionDiscord
	SectionTelegram
	SectionTools
)

var sectionNames = []string{"Provider", "Persona", "Auth", "Sessions", "Memory", "Usage", "MCP", "Discord", "Telegram", "Tools"}

// SettingsView is a modal overlay with tabbed navigation.
type SettingsView struct {
	visible       bool
	activeSection SettingsSection
	initialized   bool // tracks whether enterSection has been called for current tab

	provider ProviderSection
	persona  PersonaSection
	auth     AuthSection
	session  SessionSection
	memory   MemorySection
	usage    UsageSection
	mcp      MCPSection
	discord  DiscordSection
	telegram TelegramSection
	tools    ToolsSection

	// Shared state from parent
	apiClient     *client.APIClient
	providerID    string
	modelID       string
	sessionID     string
	activeProfile string

	width, height int
}

func NewSettingsView(apiClient *client.APIClient, providerID, modelID, sessionID string, width, height int) SettingsView {
	return SettingsView{
		activeSection: SectionProvider,
		provider:      NewProviderSection(providerID, modelID),
		persona:       NewPersonaSection(),
		auth:          NewAuthSection(),
		session:       NewSessionSection(sessionID),
		memory:        NewMemorySection(),
		usage:         NewUsageSection(providerID, modelID),
		mcp:           NewMCPSection(),
		discord:       NewDiscordSection(),
		telegram:      NewTelegramSection(),
		tools:         NewToolsSection(),
		apiClient:     apiClient,
		providerID:    providerID,
		modelID:       modelID,
		sessionID:     sessionID,
		width:         width,
		height:        height,
	}
}

func (sv *SettingsView) SetSize(width, height int) {
	sv.width = width
	sv.height = height
}

func (sv *SettingsView) SetProvider(p string) {
	sv.providerID = p
	sv.provider.SetProvider(p)
	sv.usage.SetProvider(p)
}
func (sv *SettingsView) SetModel(m string) {
	sv.modelID = m
	sv.provider.SetModel(m)
	sv.usage.SetModel(m)
}
func (sv *SettingsView) SetSession(s string)       { sv.sessionID = s; sv.session.SetCurrentSession(s) }
func (sv *SettingsView) SetActiveProfile(p string) { sv.activeProfile = p }

// Usage returns a pointer to the usage section for recording token usage.
func (sv *SettingsView) Usage() *UsageSection { return &sv.usage }

// Show makes the overlay visible and loads data for the active tab.
func (sv *SettingsView) Show() tea.Cmd {
	sv.visible = true
	sv.initialized = false
	return sv.enterSection()
}

// ShowSection opens the overlay and navigates to a specific section.
func (sv *SettingsView) ShowSection(section SettingsSection) tea.Cmd {
	sv.visible = true
	sv.activeSection = section
	sv.initialized = false
	return sv.enterSection()
}

// Hide closes the overlay.
func (sv *SettingsView) Hide() {
	sv.visible = false
	sv.initialized = false
}

func (sv *SettingsView) Init() tea.Cmd {
	return nil
}

func (sv *SettingsView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		activeInput := sv.sectionHasActiveInput()

		// If current section is in an input flow, Esc should first cancel that flow.
		// This keeps users inside Settings instead of immediately closing the overlay.
		if activeInput && key.Matches(msg, settingsKeys.Back) {
			return sv.delegateToSection(msg)
		}

		// Close overlay (don't call Hide here — let app.go handle toggle)
		if key.Matches(msg, settingsKeys.Back) {
			return func() tea.Msg { return ToggleSettingsMsg{} }
		}

		// Section switching: ←→ (primary) and Tab/Shift+Tab (secondary)
		// Only intercept Left/Right when section has no active text input
		if !activeInput {
			if key.Matches(msg, settingsKeys.Right) || key.Matches(msg, settingsKeys.Tab) {
				sv.activeSection = (sv.activeSection + 1) % SettingsSection(len(sectionNames))
				sv.initialized = false
				return sv.enterSection()
			}
			if key.Matches(msg, settingsKeys.Left) || key.Matches(msg, settingsKeys.ShiftTab) {
				if sv.activeSection > 0 {
					sv.activeSection--
				} else {
					sv.activeSection = SettingsSection(len(sectionNames) - 1)
				}
				sv.initialized = false
				return sv.enterSection()
			}
		} else {
			// Still allow Tab/Shift+Tab for section switching even with active input
			if key.Matches(msg, settingsKeys.Tab) {
				sv.activeSection = (sv.activeSection + 1) % SettingsSection(len(sectionNames))
				sv.initialized = false
				return sv.enterSection()
			}
			if key.Matches(msg, settingsKeys.ShiftTab) {
				if sv.activeSection > 0 {
					sv.activeSection--
				} else {
					sv.activeSection = SettingsSection(len(sectionNames) - 1)
				}
				sv.initialized = false
				return sv.enterSection()
			}
		}

		// Delegate to active section
		return sv.delegateToSection(msg)

	// Forward API result messages to appropriate sections
	case ProvidersMsg:
		return sv.provider.HandleProviders(msg, sv.apiClient)
	case AIStudioModelsMsg:
		return sv.provider.HandleModels(msg)
	case ModelsListMsg:
		return sv.provider.HandleModelsList(msg)
	case FallbacksMsg:
		cmds := sv.provider.HandleFallbacks(msg, sv.apiClient)
		if len(cmds) > 0 {
			return tea.Batch(cmds...)
		}
		return nil
	case FallbacksSavedMsg:
		sv.provider.HandleFallbacksSaved(msg)
		return nil
	case FallbackModelsMsg:
		sv.provider.HandleFallbackModels(msg)
		return nil
	case AuthStartMsg:
		return sv.auth.HandleAuthStart(msg)
	case AuthManualCompleteMsg:
		return sv.auth.HandleAuthManualComplete(msg)
	case AuthProfilesMsg:
		return sv.auth.HandleProfiles(msg)
	case AuthUseMsg:
		return sv.auth.HandleUseProfile(msg)
	case AIStudioAddKeyMsg:
		return sv.auth.HandleAddKey(msg)
	case AIStudioProfilesMsg:
		return sv.auth.HandleAIStudioProfiles(msg)
	case AIStudioProfileActionMsg:
		return sv.auth.HandleAIStudioAction(msg)
	case AnthropicAddMsg:
		return sv.auth.HandleAnthropicAdd(msg)
	case AnthropicProfilesMsg:
		return sv.auth.HandleAnthropicProfiles(msg)
	case AnthropicProfileActionMsg:
		return sv.auth.HandleAnthropicAction(msg)
	case AnthropicBrowserStartMsg:
		return sv.auth.HandleAnthropicBrowserStart(msg, sv.apiClient)
	case AnthropicBrowserJobMsg:
		return sv.auth.HandleAnthropicBrowserJob(msg, sv.apiClient)
	case AnthropicBrowserCancelMsg:
		return sv.auth.HandleAnthropicBrowserCancel(msg)
	case OpenAIAddMsg:
		return sv.auth.HandleOpenAIAdd(msg)
	case OpenAIProfilesMsg:
		return sv.auth.HandleOpenAIProfiles(msg)
	case OpenAIProfileActionMsg:
		return sv.auth.HandleOpenAIAction(msg)
	case OpenAICodexBrowserStartMsg:
		return sv.auth.HandleOpenAICodexBrowserStart(msg, sv.apiClient)
	case OpenAICodexBrowserJobMsg:
		return sv.auth.HandleOpenAICodexBrowserJob(msg, sv.apiClient)
	case OpenAICodexBrowserCancelMsg:
		return sv.auth.HandleOpenAICodexBrowserCancel(msg)
	case SessionsListMsg:
		return sv.session.HandleSessionsList(msg)
	case SessionDeleteMsg:
		return sv.session.HandleSessionDelete(msg, sv.apiClient)
	case SessionRenameMsg:
		return sv.session.HandleSessionRename(msg, sv.apiClient)
	case MemorySearchMsg:
		return sv.memory.HandleSearchResults(msg)
	case MCPServersMsg:
		return sv.mcp.HandleServers(msg)
	case MCPBuiltinMsg:
		return sv.mcp.HandleBuiltins(msg)
	case MCPBuiltinToggleMsg:
		cmd := sv.mcp.HandleBuiltinToggle(msg)
		// Refresh full list after toggle to get accurate tool counts and statuses.
		return tea.Batch(cmd, listMCPServersCmd(sv.apiClient), listMCPBuiltinCmd(sv.apiClient))
	case PersonasListMsg:
		return sv.persona.HandlePersonasList(msg)
	case PersonaActiveMsg:
		return sv.persona.HandlePersonaActive(msg)
	case PersonaUseMsg:
		cmd := sv.persona.HandlePersonaUse(msg)
		if msg.Err == nil {
			// Notify the app about the persona change.
			name := msg.DirName
			for _, p := range sv.persona.personas {
				if p.DirName == msg.DirName {
					name = p.Name
					break
				}
			}
			return tea.Batch(cmd, func() tea.Msg {
				return PersonaChangedMsg{Name: name}
			})
		}
		return cmd
	case DiscordConfigMsg:
		return sv.discord.HandleConfig(msg)
	case DiscordSaveMsg:
		return sv.discord.HandleSave(msg)
	case TelegramConfigMsg:
		return sv.telegram.HandleConfig(msg)
	case TelegramSaveMsg:
		return sv.telegram.HandleSave(msg)
	case ToolsConfigMsg:
		return sv.tools.HandleConfig(msg)
	case ToolsSaveMsg:
		return sv.tools.HandleSave(msg)
	case PersonaClearMsg:
		cmd := sv.persona.HandlePersonaClear(msg)
		if msg.Err == nil {
			return tea.Batch(cmd, func() tea.Msg {
				return PersonaChangedMsg{Name: ""}
			})
		}
		return cmd
	}

	return nil
}

func (sv *SettingsView) enterSection() tea.Cmd {
	sv.initialized = true
	switch sv.activeSection {
	case SectionProvider:
		return tea.Batch(
			loadProvidersCmd(sv.apiClient),
			loadFallbacksCmd(sv.apiClient),
		)
	case SectionPersona:
		return tea.Batch(
			listPersonasCmd(sv.apiClient),
			activePersonaCmd(sv.apiClient),
		)
	case SectionAuth:
		return tea.Batch(
			listGeminiProfilesCmd(sv.apiClient),
			listAIStudioProfilesCmd(sv.apiClient),
			listAnthropicProfilesCmd(sv.apiClient),
			listOpenAIProfilesCmd(sv.apiClient, "openai"),
			listOpenAIProfilesCmd(sv.apiClient, "openai-codex"),
		)
	case SectionSessions:
		return listSessionsCmd(sv.apiClient)
	case SectionMemory:
		return sv.memory.Focus()
	case SectionUsage:
		return nil // Usage is local state, no API call needed
	case SectionMCP:
		return tea.Batch(
			listMCPServersCmd(sv.apiClient),
			listMCPBuiltinCmd(sv.apiClient),
		)
	case SectionDiscord:
		return loadDiscordConfigCmd(sv.apiClient)
	case SectionTelegram:
		return loadTelegramConfigCmd(sv.apiClient)
	case SectionTools:
		return loadToolsConfigCmd(sv.apiClient)
	}
	return nil
}

func (sv *SettingsView) sectionHasActiveInput() bool {
	switch sv.activeSection {
	case SectionProvider:
		return sv.provider.HasActiveInput()
	case SectionPersona:
		return sv.persona.HasActiveInput()
	case SectionAuth:
		return sv.auth.HasActiveInput()
	case SectionSessions:
		return sv.session.HasActiveInput()
	case SectionMemory:
		return sv.memory.HasActiveInput()
	case SectionUsage:
		return false
	case SectionMCP:
		return false
	case SectionDiscord:
		return sv.discord.HasActiveInput()
	case SectionTelegram:
		return sv.telegram.HasActiveInput()
	case SectionTools:
		return sv.tools.HasActiveInput()
	}
	return false
}

func (sv *SettingsView) delegateToSection(msg tea.KeyMsg) tea.Cmd {
	switch sv.activeSection {
	case SectionProvider:
		return sv.provider.Update(msg, sv.apiClient, sv.providerID)
	case SectionPersona:
		return sv.persona.Update(msg, sv.apiClient)
	case SectionAuth:
		return sv.auth.Update(msg, sv.apiClient)
	case SectionSessions:
		return sv.session.Update(msg, sv.apiClient)
	case SectionMemory:
		return sv.memory.Update(msg, sv.apiClient)
	case SectionUsage:
		return sv.usage.Update(msg)
	case SectionMCP:
		return sv.mcp.Update(msg, sv.apiClient)
	case SectionDiscord:
		return sv.discord.Update(msg, sv.apiClient)
	case SectionTelegram:
		return sv.telegram.Update(msg, sv.apiClient)
	case SectionTools:
		return sv.tools.Update(msg, sv.apiClient)
	}
	return nil
}

// RenderOverlay renders the settings overlay on top of dimmed chat content.
func (sv SettingsView) RenderOverlay(chatBg string, width, height int) string {
	dimmed := dimLines(chatBg)

	// Calculate overlay box dimensions proportionally to terminal size.
	// Use 92% of terminal width, minimum 20.
	boxW := width * 92 / 100
	if boxW < 20 {
		boxW = 20
	}

	boxH := height * 80 / 100
	if boxH < 10 {
		boxH = 10
	}

	// DialogBoxStyle uses Padding(1, 2) + RoundedBorder (1 each side).
	// lipgloss Width() sets inner width INCLUDING padding but EXCLUDING border.
	// So: renderedWidth = Width + 2 (border)
	//     textWidth     = Width - 2*horizontalPadding = Width - 4
	// We want the total rendered box to be ~boxW, so:
	innerW := boxW - 2 // subtract border
	if innerW < 20 {
		innerW = 20
	}
	textW := innerW - 4 // subtract horizontal padding (2 each side)
	if textW < 10 {
		textW = 10
	}

	tabBar := sv.renderTabBar(textW)

	// Available content height for sections to fill.
	maxContentLines := boxH - 6
	if maxContentLines < 3 {
		maxContentLines = 3
	}

	var sectionContent string
	switch sv.activeSection {
	case SectionProvider:
		sectionContent = sv.provider.View(textW, maxContentLines)
	case SectionPersona:
		sectionContent = sv.persona.View(textW, maxContentLines)
	case SectionAuth:
		sectionContent = sv.auth.View(textW, maxContentLines)
	case SectionSessions:
		sectionContent = sv.session.View(textW, maxContentLines)
	case SectionMemory:
		sectionContent = sv.memory.View(textW, maxContentLines)
	case SectionUsage:
		sectionContent = sv.usage.View(textW, maxContentLines)
	case SectionMCP:
		sectionContent = sv.mcp.View(textW, maxContentLines)
	case SectionDiscord:
		sectionContent = sv.discord.View(textW, maxContentLines)
	case SectionTelegram:
		sectionContent = sv.telegram.View(textW, maxContentLines)
	case SectionTools:
		sectionContent = sv.tools.View(textW, maxContentLines)
	}

	var lines []string
	lines = append(lines, tabBar)
	lines = append(lines, theme.SystemStyle.Render(strings.Repeat("─", textW)))
	lines = append(lines, "")

	// Section content (clamp to available height)
	contentLines := strings.Split(sectionContent, "\n")
	if len(contentLines) > maxContentLines {
		contentLines = contentLines[:maxContentLines]
	}
	lines = append(lines, contentLines...)

	// Pad to fill (box height stays fixed)
	for len(lines) < boxH-3 {
		lines = append(lines, "")
	}

	lines = append(lines, "")
	lines = append(lines, theme.HintStyle.Render(sv.sectionHintText()))

	box := theme.DialogBoxStyle.Copy().
		Width(innerW).
		Render(strings.Join(lines, "\n"))

	return centerOverlay(dimmed, box, width, height)
}

// renderTabBar renders the horizontal tab bar.
func (sv SettingsView) renderTabBar(maxW int) string {
	var parts []string
	for i, name := range sectionNames {
		style := theme.TabInactiveStyle
		if SettingsSection(i) == sv.activeSection {
			name = "[" + name + "]"
			style = theme.TabActiveStyle
		}
		parts = append(parts, style.Render(name))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(parts, "   "))
}

// sectionHintText returns context-sensitive help text for the active section.
func (sv SettingsView) sectionHintText() string {
	switch sv.activeSection {
	case SectionProvider:
		if sv.provider.focusField >= 2 {
			return "←→ 切換欄位  ·  ↑↓ 選擇  ·  Enter 切換選項  ·  d 清除  ·  Tab 分頁  ·  Esc 關閉"
		}
		return "←→ 切換欄位  ·  ↑↓ 選擇  ·  Enter 確認  ·  Tab 分頁  ·  Esc 關閉"
	case SectionPersona:
		return "↑↓ 選擇  ·  Enter 啟用/停用  ·  d 停用  ·  r 重新載入  ·  o 開啟資料夾  ·  Tab 分頁  ·  Esc 關閉"
	case SectionAuth:
		return "↑↓ 選擇  ·  Enter/a 新增  ·  d 刪除  ·  Tab 分頁  ·  Esc 關閉"
	case SectionSessions:
		return "Enter 選擇  ·  n 新建  ·  r 重命名  ·  d 刪除  ·  Tab 分頁  ·  Esc 關閉"
	case SectionMemory:
		return "Enter 搜尋  ·  Tab 分頁  ·  Esc 關閉"
	case SectionUsage:
		return "↑↓ 瀏覽  ·  Tab 分頁  ·  Esc 關閉"
	case SectionMCP:
		return "↑↓ 選擇  ·  Enter 切換啟用  ·  r 重新載入  ·  o 開啟資料夾  ·  Tab 分頁  ·  Esc 關閉"
	case SectionDiscord:
		return "↑↓ 切換欄位  ·  Enter 儲存  ·  Tab 分頁  ·  Esc 關閉"
	case SectionTelegram:
		return "Enter 儲存  ·  Tab 分頁  ·  Esc 關閉"
	case SectionTools:
		return "Enter 儲存  ·  Tab 分頁  ·  Esc 關閉"
	default:
		return "↑↓ 選擇  ·  Enter 確認  ·  Tab 分頁  ·  Esc 關閉"
	}
}

func (sv SettingsView) View() string {
	// Fallback: if called directly (not as overlay), render just the content
	return sv.provider.View(sv.width, sv.height)
}
