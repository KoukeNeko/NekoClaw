package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// ChatView orchestrates the chat viewport, input, and spinner.
type ChatView struct {
	viewport ChatViewport
	input    ChatInput
	spinner  spinner.Model
	pending  bool

	// Thinking pseudo-message tracking
	thinkingActive bool

	// Shared state (set by parent)
	client    *client.APIClient
	sessionID string
	provider  string
	modelID   string

	// Profile tracking
	activeProfile string
	defaultModel  string

	// Tool-approval flow (blocking)
	approvalActive    bool
	approvalRunID     string
	approvalItems     []core.PendingToolApproval
	approvalCursor    int
	approvalDecisions []core.ToolApprovalDecision

	width, height int
}

func NewChatView(apiClient *client.APIClient, provider, modelID, sessionID string, width, height int) ChatView {
	input := NewChatInput(width, height)
	contentH := height - input.InputHeight()
	if contentH < 3 {
		contentH = 3
	}

	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(theme.Primary)

	cv := ChatView{
		viewport:  NewChatViewport(width, contentH),
		input:     input,
		spinner:   s,
		client:    apiClient,
		sessionID: sessionID,
		provider:  provider,
		modelID:   modelID,
		width:     width,
		height:    height,
	}

	// Welcome message with keybinding hints
	cv.viewport.SetMessages([]ChatMessage{{
		Role: "system",
		Content: "NekoClaw — AI Chat CLI\n\n" +
			"快捷鍵：\n" +
			"  Enter       送出訊息\n" +
			"  Shift+Enter 換行\n" +
			"  Esc         開啟設定\n" +
			"  /help       查看所有指令",
		Timestamp: time.Now(),
	}})

	return cv
}

func (cv *ChatView) SetSize(width, height int) {
	cv.width = width
	cv.height = height

	cv.input.SetSize(width, height)
	inputH := cv.input.InputHeight()
	viewportH := height - inputH
	if viewportH < 3 {
		viewportH = 3
	}
	cv.viewport.SetSize(width, viewportH)
}

func (cv *ChatView) SetSession(sessionID string) { cv.sessionID = sessionID }
func (cv *ChatView) SetProvider(provider string) { cv.provider = provider }
func (cv *ChatView) SetModel(modelID string)     { cv.modelID = modelID }

func (cv *ChatView) Focus() tea.Cmd {
	return cv.input.Focus()
}

func (cv *ChatView) Blur() {
	cv.input.Blur()
}

func (cv ChatView) Init() tea.Cmd {
	return tea.Batch(cv.input.Focus(), cv.spinner.Tick)
}

func (cv *ChatView) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if cv.approvalActive {
			if key.Matches(msg, chatKeys.Submit) {
				return cv.handleApprovalDecision("allow")
			}
			if key.Matches(msg, chatKeys.OpenSettings) {
				return cv.handleApprovalDecision("deny")
			}
			// Keep scroll behavior during approval mode.
			if key.Matches(msg, chatKeys.PageUp) || key.Matches(msg, chatKeys.PageDown) ||
				key.Matches(msg, chatKeys.GoToTop) || key.Matches(msg, chatKeys.GoToBottom) ||
				key.Matches(msg, chatKeys.ScrollUp) || key.Matches(msg, chatKeys.ScrollDown) {
				_, cmd := cv.viewport.Update(msg)
				return cmd
			}
			return nil
		}

		// Sidebar toggle (not during pending/approval flow)
		if key.Matches(msg, chatKeys.ToggleSidebar) && !cv.pending {
			return func() tea.Msg { return SidebarToggleFocusMsg{} }
		}

		// Settings shortcut (always available)
		if key.Matches(msg, chatKeys.OpenSettings) {
			cv.input.Blur()
			return func() tea.Msg { return ToggleSettingsMsg{} }
		}

		// Viewport scroll bindings (always work, even during pending)
		if key.Matches(msg, chatKeys.PageUp) || key.Matches(msg, chatKeys.PageDown) ||
			key.Matches(msg, chatKeys.GoToTop) || key.Matches(msg, chatKeys.GoToBottom) ||
			key.Matches(msg, chatKeys.ScrollUp) || key.Matches(msg, chatKeys.ScrollDown) {
			_, cmd := cv.viewport.Update(msg)
			return cmd
		}

		// Always pass key events to input (user can type ahead)
		cmd := cv.input.Update(msg)
		cmds = append(cmds, cmd)

	case SubmitMsg:
		if cv.pending || cv.approvalActive {
			return nil // block submit during pending
		}
		return cv.handleSubmit(msg.Text)

	case ChatResultMsg:
		return cv.handleChatResult(msg)

	case StreamTickMsg:
		// Legacy: ignored (streaming removed)
		return nil

	case ClearChatMsg:
		cv.viewport.SetMessages([]ChatMessage{{
			Role:      "system",
			Content:   "Chat cleared.",
			Timestamp: time.Now(),
		}})
		return nil

	case SessionChangedMsg:
		cv.sessionID = msg.SessionID
		cv.viewport.SetMessages([]ChatMessage{{
			Role:      "system",
			Content:   "Switched to session: " + msg.SessionID,
			Timestamp: time.Now(),
		}})
		return nil

	case ProviderChangedMsg:
		cv.provider = msg.Provider
		cv.activeProfile = ""
		cv.defaultModel = ""
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   "Provider changed to " + msg.Provider,
			Timestamp: time.Now(),
		})
		return nil

	case ModelChangedMsg:
		cv.modelID = msg.ModelID
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   "Model changed to " + msg.ModelID,
			Timestamp: time.Now(),
		})
		return nil

	case spinner.TickMsg:
		if cv.pending {
			var cmd tea.Cmd
			cv.spinner, cmd = cv.spinner.Update(msg)
			if cv.thinkingActive {
				cv.viewport.UpdateLastMessage(cv.spinner.View() + " thinking...")
			}
			cmds = append(cmds, cmd)
		}

	default:
		// Pass mouse events to viewport
		_, cmd := cv.viewport.Update(msg)
		cmds = append(cmds, cmd)

		// Pass remaining to input
		cmd = cv.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

func (cv ChatView) View() string {
	var sb strings.Builder

	// Viewport (includes thinking pseudo-message when pending)
	sb.WriteString(cv.viewport.View())
	sb.WriteString("\n")

	// Input (includes separator, textarea, and hints)
	sb.WriteString(cv.input.View())
	return sb.String()
}

func (cv *ChatView) handleSubmit(text string) tea.Cmd {
	// Check for local commands first
	if cmd := cv.handleSlashCommand(text); cmd != nil {
		return cmd
	}

	// Add user message to viewport
	cv.viewport.AppendMessage(ChatMessage{
		Role:      "user",
		Content:   text,
		Timestamp: time.Now(),
	})

	// Append thinking pseudo-message (animated by spinner ticks)
	cv.pending = true
	cv.thinkingActive = true
	cv.viewport.AppendMessage(ChatMessage{
		Role:      "thinking",
		Content:   cv.spinner.View() + " thinking...",
		Timestamp: time.Now(),
	})

	req := core.ChatRequest{
		SessionID:   cv.sessionID,
		Surface:     core.SurfaceTUI,
		Provider:    cv.provider,
		Model:       cv.modelID,
		Message:     text,
		EnableTools: providerToolEnabled(cv.provider),
	}
	return tea.Batch(
		sendChatCmd(cv.client, req),
		cv.spinner.Tick,
	)
}

func (cv *ChatView) handleChatResult(msg ChatResultMsg) tea.Cmd {
	cv.pending = false

	if msg.Response.SessionID != cv.sessionID {
		return nil
	}

	// Remove the thinking pseudo-message
	if cv.thinkingActive {
		cv.viewport.RemoveLastMessage()
		cv.thinkingActive = false
	}

	if msg.Err != nil {
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "error",
			Content:   formatChatError(msg.Err),
			Timestamp: time.Now(),
		})
		return cv.input.Focus()
	}

	if msg.Response.Status == core.ChatStatusApprovalRequired {
		cv.approvalActive = true
		cv.approvalRunID = strings.TrimSpace(msg.Response.RunID)
		cv.approvalItems = append([]core.PendingToolApproval(nil), msg.Response.PendingApprovals...)
		cv.approvalCursor = 0
		cv.approvalDecisions = nil
		if len(cv.approvalItems) == 0 {
			cv.approvalActive = false
			cv.viewport.AppendMessage(ChatMessage{
				Role:      "error",
				Content:   "Tool approval requested but no pending approvals were provided.",
				Timestamp: time.Now(),
			})
			return cv.input.Focus()
		}
		cv.input.Blur()
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   cv.renderApprovalPrompt(),
			Timestamp: time.Now(),
		})
		return nil
	}

	// Track profile and model info
	if cv.provider == "google-gemini-cli" || cv.provider == "google-ai-studio" || cv.provider == "anthropic" {
		if strings.EqualFold(cv.modelID, "default") {
			cv.defaultModel = strings.TrimSpace(msg.Response.Model)
		}
		cv.activeProfile = strings.TrimSpace(msg.Response.AccountID)
	}

	// Show full response immediately
	cv.viewport.AppendMessage(ChatMessage{
		Role:      "assistant",
		Content:   msg.Response.Reply,
		Timestamp: time.Now(),
	})

	statusCmd := func() tea.Msg {
		return StatusUpdateMsg{MessageCount: cv.viewport.MessageCount()}
	}
	return tea.Batch(cv.input.Focus(), statusCmd)
}

func (cv *ChatView) handleApprovalDecision(decision string) tea.Cmd {
	if !cv.approvalActive || cv.approvalCursor >= len(cv.approvalItems) {
		return nil
	}
	current := cv.approvalItems[cv.approvalCursor]
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "allow" && decision != "deny" {
		decision = "deny"
	}
	cv.approvalDecisions = append(cv.approvalDecisions, core.ToolApprovalDecision{
		ApprovalID: current.ApprovalID,
		Decision:   decision,
	})
	cv.approvalCursor++
	if cv.approvalCursor < len(cv.approvalItems) {
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   cv.renderApprovalPrompt(),
			Timestamp: time.Now(),
		})
		return nil
	}

	req := core.ChatRequest{
		SessionID:     cv.sessionID,
		Surface:       core.SurfaceTUI,
		Provider:      cv.provider,
		Model:         cv.modelID,
		EnableTools:   providerToolEnabled(cv.provider),
		RunID:         cv.approvalRunID,
		ToolApprovals: append([]core.ToolApprovalDecision(nil), cv.approvalDecisions...),
	}
	cv.approvalActive = false
	cv.approvalRunID = ""
	cv.approvalItems = nil
	cv.approvalCursor = 0
	cv.approvalDecisions = nil

	cv.pending = true
	cv.thinkingActive = true
	cv.viewport.AppendMessage(ChatMessage{
		Role:      "thinking",
		Content:   cv.spinner.View() + " applying approvals...",
		Timestamp: time.Now(),
	})
	return tea.Batch(sendChatCmd(cv.client, req), cv.spinner.Tick)
}

func (cv *ChatView) renderApprovalPrompt() string {
	if cv.approvalCursor >= len(cv.approvalItems) {
		return "No pending approval."
	}
	item := cv.approvalItems[cv.approvalCursor]
	total := len(cv.approvalItems)
	index := cv.approvalCursor + 1
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Tool approval [%d/%d]\n", index, total))
	b.WriteString(fmt.Sprintf("- Tool: %s\n", strings.TrimSpace(item.ToolName)))
	if preview := strings.TrimSpace(item.ArgumentsPreview); preview != "" {
		b.WriteString(fmt.Sprintf("- Args: %s\n", preview))
	}
	if risk := strings.TrimSpace(item.RiskLevel); risk != "" {
		b.WriteString(fmt.Sprintf("- Risk: %s\n", risk))
	}
	b.WriteString("Enter = allow · Esc = deny")
	return b.String()
}

func providerToolEnabled(providerID string) bool {
	switch strings.TrimSpace(providerID) {
	case "anthropic", "openai", "openai-codex":
		return true
	default:
		return false
	}
}

func (cv *ChatView) handleSlashCommand(text string) tea.Cmd {
	lower := strings.ToLower(strings.TrimSpace(text))
	parts := strings.SplitN(lower, " ", 2)
	cmd := parts[0]

	switch cmd {
	case "/help":
		cv.viewport.AppendMessage(ChatMessage{
			Role: "system",
			Content: "Available commands:\n" +
				"  /help      — Show this help\n" +
				"  /new       — Start new conversation\n" +
				"  /clear     — Clear chat history\n" +
				"  /config    — Open settings\n" +
				"  /model     — Switch model\n" +
				"  /session   — Switch session\n" +
				"  /memory    — Search memory\n" +
				"\n  Esc — Settings  ·  Shift+Enter — New line",
			Timestamp: time.Now(),
		})
		return nil

	case "/new":
		newSession := fmt.Sprintf("chat-%s", time.Now().Format("0102-150405"))
		return func() tea.Msg { return SessionChangedMsg{SessionID: newSession} }

	case "/clear":
		return func() tea.Msg { return ClearChatMsg{} }

	case "/config", "/settings":
		cv.input.Blur()
		return func() tea.Msg { return ToggleSettingsMsg{} }

	case "/model":
		cv.input.Blur()
		return func() tea.Msg { return ToggleSettingsMsg{} }

	case "/session":
		cv.input.Blur()
		return func() tea.Msg { return ToggleSettingsMsg{} }

	case "/memory":
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			query := strings.TrimSpace(text[len("/memory"):])
			cv.viewport.AppendMessage(ChatMessage{
				Role:      "system",
				Content:   "Searching memory: " + query,
				Timestamp: time.Now(),
			})
			return searchMemoryCmd(cv.client, query, 10)
		}
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   "Usage: /memory <search query>",
			Timestamp: time.Now(),
		})
		return nil
	}

	return nil // not a slash command, proceed with normal send
}
