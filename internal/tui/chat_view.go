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
	"github.com/doeshing/nekoclaw/internal/mcp"
)

// ChatView orchestrates the chat viewport, input, and spinner.
type ChatView struct {
	viewport ChatViewport
	input    ChatInput
	spinner  spinner.Model
	pending  bool

	// Thinking pseudo-message tracking
	thinkingActive  bool
	thinkingStart   time.Time
	currentToolName    string // active tool name shown in spinner (polled from server)
	currentRetryStatus string // failback retry status shown in spinner (polled from server)

	// Shared state (set by parent)
	client    *client.APIClient
	sessionID string
	provider  string
	modelID   string

	// Profile tracking
	activeProfile string
	defaultModel  string

	// Active stream channel for SSE streaming
	streamCh <-chan core.StreamChunk

	// Pending image attachments for next submit
	pendingImages []core.ImageData

	// Tool-approval flow (blocking)
	approvalActive    bool
	approvalRunID     string
	approvalItems     []core.PendingToolApproval
	approvalCursor    int
	approvalDecisions []core.ToolApprovalDecision

	width, height int
}

// headerHeight is the number of lines the chat header occupies (label + separator).
const headerHeight = 2

func NewChatView(apiClient *client.APIClient, provider, modelID, sessionID string, width, height int) ChatView {
	input := NewChatInput(width, height)
	contentH := height - input.InputHeight() - headerHeight
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
			"  Ctrl+N      新對話\n" +
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
	viewportH := height - inputH - headerHeight
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

		// New chat (Ctrl+N)
		if key.Matches(msg, chatKeys.NewChat) && !cv.pending {
			newSession := fmt.Sprintf("chat-%s", time.Now().Format("0102-150405"))
			return func() tea.Msg { return SessionChangedMsg{SessionID: newSession} }
		}

		// Paste image from clipboard (Ctrl+V)
		if key.Matches(msg, chatKeys.PasteImage) && !cv.pending {
			return checkClipboardImageCmd()
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

	case streamStartMsg:
		cv.streamCh = msg.ch
		if msg.sessionID != cv.sessionID {
			return nil
		}
		return streamNextCmd(cv.streamCh)

	case StreamChunkMsg:
		return cv.handleStreamChunk(msg)

	case streamDoneMsg:
		cv.streamCh = nil
		return nil

	case ToolStatusMsg:
		if cv.pending {
			cv.currentToolName = msg.ToolName
			cv.currentRetryStatus = msg.RetryStatus
			if cv.thinkingActive {
				cv.viewport.UpdateLastMessage(cv.thinkingStatus())
			}
			// Schedule next poll while still pending.
			cmds = append(cmds, scheduleToolStatusTick())
		}

	case ToolStatusTickMsg:
		if cv.pending {
			return pollToolStatusCmd(cv.client, cv.sessionID)
		}

	case StreamTickMsg:
		// Legacy: ignored (streaming removed)
		return nil

	case ClipboardImageMsg:
		if msg.Err != nil {
			// No image in clipboard — silently ignore (text paste uses bracketed paste)
			return nil
		}
		cv.pendingImages = append(cv.pendingImages, msg.Image)
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   "📎 Pasted: " + msg.Image.FileName + " — type your message and press Enter to send",
			Timestamp: time.Now(),
		})
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
			Content:   "Loading session…",
			Timestamp: time.Now(),
		}})
		return loadSessionTranscriptCmd(cv.client, msg.SessionID)

	case TranscriptLoadedMsg:
		if msg.SessionID != cv.sessionID {
			return nil // stale response from a previous session switch
		}
		if msg.Err != nil {
			cv.viewport.SetMessages([]ChatMessage{{
				Role:      "error",
				Content:   "Failed to load history: " + msg.Err.Error(),
				Timestamp: time.Now(),
			}})
			return nil
		}
		chatMsgs := transcriptToChatMessages(msg.Messages)
		if len(chatMsgs) == 0 {
			chatMsgs = []ChatMessage{{
				Role:    "system",
				Content: "No messages yet. Start chatting!",
				Timestamp: time.Now(),
			}}
		}
		cv.viewport.SetMessages(chatMsgs)
		cv.viewport.viewport.GotoBottom()
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
				cv.viewport.UpdateLastMessage(cv.thinkingStatus())
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

	// Header: provider · model ▾
	sb.WriteString(cv.renderHeader())
	sb.WriteString("\n")

	// Viewport (includes thinking pseudo-message when pending)
	sb.WriteString(cv.viewport.View())
	sb.WriteString("\n")

	// Input (includes separator, textarea, and hints)
	sb.WriteString(cv.input.View())
	return sb.String()
}

// displayModel returns the model label, appending the resolved name when using "default".
func (cv ChatView) displayModel() string {
	if strings.EqualFold(cv.modelID, "default") && cv.defaultModel != "" {
		return cv.modelID + " (" + cv.defaultModel + ")"
	}
	return cv.modelID
}

// renderHeader renders the centered provider/model header bar.
func (cv ChatView) renderHeader() string {
	providerLabel := theme.SubtleStyle.Render(cv.provider)
	dot := theme.SubtleStyle.Render(" · ")
	modelLabel := theme.HighlightStyle.Render(cv.displayModel())
	chevron := theme.SubtleStyle.Render(" ▾")

	label := providerLabel + dot + modelLabel + chevron
	labelWidth := lipgloss.Width(label)

	// Center within the chat pane
	padding := (cv.width - labelWidth) / 2
	if padding < 0 {
		padding = 0
	}

	header := strings.Repeat(" ", padding) + label
	separator := theme.SystemStyle.Render(strings.Repeat("─", cv.width))

	return header + "\n" + separator
}

// thinkingStatus builds a single-line status: ⠋ provider · model · 3.2s
// Priority: retry status (failback) > tool status > default spinner.
func (cv ChatView) thinkingStatus() string {
	elapsed := time.Since(cv.thinkingStart).Truncate(100 * time.Millisecond)
	status := fmt.Sprintf("%s %s · %s · %s", cv.spinner.View(), cv.provider, cv.displayModel(), elapsed)
	if cv.currentRetryStatus != "" {
		status += " · " + cv.currentRetryStatus
	} else if cv.currentToolName != "" {
		// Show friendly name for MCP tools: "server/tool" instead of "mcp__server__tool"
		displayName := cv.currentToolName
		if serverName, toolName, isMCP := mcp.ParseNamespacedTool(cv.currentToolName); isMCP {
			displayName = serverName + "/" + toolName
		}
		status += " · 🔧 正在使用 " + displayName + "…"
	}
	return status
}

func (cv *ChatView) handleSubmit(text string) tea.Cmd {
	// Check for local commands first
	if cmd, handled := cv.handleSlashCommand(text); handled {
		return cmd
	}

	// Auto-detect: if user typed a single image path, attach it automatically
	trimmed := strings.TrimSpace(text)
	if core.IsImagePath(trimmed) && len(cv.pendingImages) == 0 {
		img, err := core.LoadImageFromPath(trimmed)
		if err == nil {
			cv.pendingImages = append(cv.pendingImages, img)
			text = "" // image-only message
		}
	}

	// Collect image file names for display
	var imageNames []string
	for _, img := range cv.pendingImages {
		imageNames = append(imageNames, img.FileName)
	}

	// Build display content
	displayText := text
	if displayText == "" && len(imageNames) > 0 {
		displayText = "(image)"
	}

	// Add user message to viewport
	cv.viewport.AppendMessage(ChatMessage{
		Role:      "user",
		Content:   displayText,
		Images:    imageNames,
		Timestamp: time.Now(),
	})

	// Append status pseudo-message (animated by spinner ticks)
	cv.pending = true
	cv.thinkingActive = true
	cv.thinkingStart = time.Now()
	cv.currentToolName = ""    // reset tool status
	cv.currentRetryStatus = "" // reset retry status
	cv.viewport.AppendMessage(ChatMessage{
		Role:      "thinking",
		Content:   cv.thinkingStatus(),
		Timestamp: time.Now(),
	})

	req := core.ChatRequest{
		SessionID:   cv.sessionID,
		Surface:     core.SurfaceTUI,
		Provider:    cv.provider,
		Model:       cv.modelID,
		Message:     text,
		Images:      cv.pendingImages,
		EnableTools: providerToolEnabled(cv.provider),
	}
	cv.pendingImages = nil // clear after submit
	cmds := []tea.Cmd{
		sendChatCmd(cv.client, req),
		cv.spinner.Tick,
	}
	// Start tool status polling when tools are enabled.
	if req.EnableTools {
		cmds = append(cmds, scheduleToolStatusTick())
	}
	return tea.Batch(cmds...)
}

func (cv *ChatView) handleChatResult(msg ChatResultMsg) tea.Cmd {
	cv.pending = false
	cv.currentToolName = ""    // clear tool status on completion
	cv.currentRetryStatus = "" // clear retry status on completion

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

	// Calculate response elapsed time
	var elapsedMs int64
	if !cv.thinkingStart.IsZero() {
		elapsedMs = time.Since(cv.thinkingStart).Milliseconds()
	}

	// Show full response with token usage stats and tool summary
	cv.viewport.AppendMessage(ChatMessage{
		Role:         "assistant",
		Content:      msg.Response.Reply,
		Provider:     msg.Response.Provider,
		Model:        msg.Response.Model,
		InputTokens:  msg.Response.Usage.InputTokens,
		OutputTokens: msg.Response.Usage.OutputTokens,
		ElapsedMs:    elapsedMs,
		ToolEvents:   msg.Response.ToolEvents,
		Timestamp:    time.Now(),
	})

	statusCmd := func() tea.Msg {
		return StatusUpdateMsg{MessageCount: cv.viewport.MessageCount()}
	}
	return tea.Batch(cv.input.Focus(), statusCmd)
}

func (cv *ChatView) handleStreamChunk(msg StreamChunkMsg) tea.Cmd {
	chunk := msg.Chunk

	switch chunk.Type {
	case core.ChunkToolStatus:
		cv.currentToolName = chunk.ToolName
		if cv.thinkingActive {
			cv.viewport.UpdateLastMessage(cv.thinkingStatus())
		}
		return streamNextCmd(cv.streamCh)

	case core.ChunkRetryStatus:
		cv.currentRetryStatus = chunk.RetryStatus
		if cv.thinkingActive {
			cv.viewport.UpdateLastMessage(cv.thinkingStatus())
		}
		return streamNextCmd(cv.streamCh)

	case core.ChunkText:
		// First text chunk: replace thinking with assistant message
		if cv.thinkingActive {
			cv.viewport.RemoveLastMessage()
			cv.thinkingActive = false
			cv.viewport.AppendMessage(ChatMessage{
				Role:      "assistant",
				Content:   chunk.Content,
				Timestamp: time.Now(),
			})
		} else {
			// Subsequent chunks: append to last message
			cv.viewport.AppendToLastMessage(chunk.Content)
		}
		return streamNextCmd(cv.streamCh)

	case core.ChunkError:
		if cv.thinkingActive {
			cv.viewport.RemoveLastMessage()
			cv.thinkingActive = false
		}
		cv.pending = false
		cv.currentToolName = ""
		cv.currentRetryStatus = ""
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "error",
			Content:   chunk.Error,
			Timestamp: time.Now(),
		})
		return cv.input.Focus()

	case core.ChunkDone:
		cv.pending = false
		cv.currentToolName = ""
		cv.currentRetryStatus = ""
		cv.streamCh = nil

		if chunk.Response == nil {
			return cv.input.Focus()
		}

		resp := chunk.Response

		// Track profile and model
		if cv.provider == "google-gemini-cli" || cv.provider == "google-ai-studio" || cv.provider == "anthropic" {
			if strings.EqualFold(cv.modelID, "default") {
				cv.defaultModel = strings.TrimSpace(resp.Model)
			}
			cv.activeProfile = strings.TrimSpace(resp.AccountID)
		}

		var elapsedMs int64
		if !cv.thinkingStart.IsZero() {
			elapsedMs = time.Since(cv.thinkingStart).Milliseconds()
		}

		// If thinking is still active (no text was streamed), show the full reply
		if cv.thinkingActive {
			cv.viewport.RemoveLastMessage()
			cv.thinkingActive = false
			cv.viewport.AppendMessage(ChatMessage{
				Role:         "assistant",
				Content:      resp.Reply,
				Provider:     resp.Provider,
				Model:        resp.Model,
				InputTokens:  resp.Usage.InputTokens,
				OutputTokens: resp.Usage.OutputTokens,
				ElapsedMs:    elapsedMs,
				ToolEvents:   resp.ToolEvents,
				Timestamp:    time.Now(),
			})
		} else {
			// Update the last assistant message with metadata
			cv.viewport.UpdateLastMessageMeta(ChatMessage{
				Provider:     resp.Provider,
				Model:        resp.Model,
				InputTokens:  resp.Usage.InputTokens,
				OutputTokens: resp.Usage.OutputTokens,
				ElapsedMs:    elapsedMs,
				ToolEvents:   resp.ToolEvents,
			})
		}

		statusCmd := func() tea.Msg {
			return StatusUpdateMsg{MessageCount: cv.viewport.MessageCount()}
		}
		return tea.Batch(cv.input.Focus(), statusCmd)
	}

	return streamNextCmd(cv.streamCh)
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
	cv.thinkingStart = time.Now()
	cv.currentToolName = ""    // reset tool status
	cv.currentRetryStatus = "" // reset retry status
	cv.viewport.AppendMessage(ChatMessage{
		Role:      "thinking",
		Content:   cv.thinkingStatus(),
		Timestamp: time.Now(),
	})
	cmds := []tea.Cmd{
		sendChatCmd(cv.client, req),
		cv.spinner.Tick,
	}
	// Resume tool status polling when tools are enabled.
	if req.EnableTools {
		cmds = append(cmds, scheduleToolStatusTick())
	}
	return tea.Batch(cmds...)
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
	// All providers support tool calling except mock.
	// Service layer validates ToolCallingProvider interface as a safety net.
	return strings.TrimSpace(providerID) != "mock"
}

func (cv *ChatView) handleSlashCommand(text string) (tea.Cmd, bool) {
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
				"  /image     — Attach image file\n" +
				"  /paste     — Paste image from clipboard\n" +
				"  /config    — Open settings\n" +
				"  /model     — Switch model\n" +
				"  /session   — Switch session\n" +
				"  /memory    — Search memory\n" +
				"  /mcp       — Show MCP servers\n" +
				"\n  Esc — Settings  ·  Shift+Enter — New line",
			Timestamp: time.Now(),
		})
		return nil, true

	case "/new":
		newSession := fmt.Sprintf("chat-%s", time.Now().Format("0102-150405"))
		return func() tea.Msg { return SessionChangedMsg{SessionID: newSession} }, true

	case "/clear":
		return func() tea.Msg { return ClearChatMsg{} }, true

	case "/config", "/settings":
		cv.input.Blur()
		return func() tea.Msg { return ToggleSettingsMsg{} }, true

	case "/model":
		cv.input.Blur()
		return func() tea.Msg { return ToggleSettingsMsg{} }, true

	case "/session":
		cv.input.Blur()
		return func() tea.Msg { return ToggleSettingsMsg{} }, true

	case "/image":
		// Use the original text (not lowered) to preserve file path case
		rawArgs := strings.TrimSpace(text[len("/image"):])
		if rawArgs == "" {
			cv.viewport.AppendMessage(ChatMessage{
				Role:      "system",
				Content:   "Usage: /image <file path>",
				Timestamp: time.Now(),
			})
			return nil, true
		}
		img, err := core.LoadImageFromPath(rawArgs)
		if err != nil {
			cv.viewport.AppendMessage(ChatMessage{
				Role:      "error",
				Content:   "Failed to load image: " + err.Error(),
				Timestamp: time.Now(),
			})
			return nil, true
		}
		cv.pendingImages = append(cv.pendingImages, img)
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   "📎 Attached: " + img.FileName + " — type your message and press Enter to send",
			Timestamp: time.Now(),
		})
		return nil, true

	case "/paste":
		img, err := core.LoadImageFromClipboard()
		if err != nil {
			cv.viewport.AppendMessage(ChatMessage{
				Role:      "error",
				Content:   "Clipboard: " + err.Error(),
				Timestamp: time.Now(),
			})
			return nil, true
		}
		cv.pendingImages = append(cv.pendingImages, img)
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   "📎 Pasted: " + img.FileName + " — type your message and press Enter to send",
			Timestamp: time.Now(),
		})
		return nil, true

	case "/memory":
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			query := strings.TrimSpace(text[len("/memory"):])
			cv.viewport.AppendMessage(ChatMessage{
				Role:      "system",
				Content:   "Searching memory: " + query,
				Timestamp: time.Now(),
			})
			return searchMemoryCmd(cv.client, query, 10), true
		}
		cv.viewport.AppendMessage(ChatMessage{
			Role:      "system",
			Content:   "Usage: /memory <search query>",
			Timestamp: time.Now(),
		})
		return nil, true

	case "/mcp":
		cv.input.Blur()
		return func() tea.Msg { return OpenSettingsSectionMsg{Section: SectionMCP} }, true
	}

	return nil, false // not a slash command, proceed with normal send
}

// transcriptToChatMessages converts API transcript messages to TUI ChatMessages.
func transcriptToChatMessages(msgs []client.TranscriptMessage) []ChatMessage {
	out := make([]ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		ts, _ := time.Parse(time.RFC3339Nano, m.CreatedAt)
		out = append(out, ChatMessage{
			Role:      m.Role,
			Content:   m.Content,
			Images:    m.ImageNames,
			Timestamp: ts,
		})
	}
	return out
}
