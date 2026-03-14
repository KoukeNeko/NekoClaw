package discord

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/binding"
	"github.com/doeshing/nekoclaw/internal/botcmd"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/logger"
	"github.com/doeshing/nekoclaw/internal/mcp"
	"github.com/doeshing/nekoclaw/internal/persona"
)

var logDiscord = logger.New("discord", logger.Magenta)

// discordLogSender adapts *discordgo.Session to logger.DiscordSender.
type discordLogSender struct{ session *discordgo.Session }

func (d *discordLogSender) SendMessage(channelID, content string) error {
	_, err := d.session.ChannelMessageSend(channelID, content)
	return err
}

// discordMessageLimit is the maximum length of a single Discord message.
const discordMessageLimit = 2000

// Channel history injection settings (KoukeBOT-aligned defaults).
const (
	discordHistoryFetchLimit        = 40
	discordHistoryTargetEntries     = 40
	discordHistoryCharBudget        = 4500
	discordHistoryEntryTextLimit    = 200
	discordHistoryImageScanLimit    = 10
	discordHistoryImageTotalLimit   = 5
	discordHistoryImageMaxAge       = 30 * time.Minute
	discordReplyReferenceImageLimit = 2
)

// Reaction emoji for message lifecycle.
const (
	emojiReceived   = "👀"
	emojiProcessing = "🔄"
	emojiDone       = "✅"
)

const (
	discordPlanApprovePrefix = "plan:approve:"
	discordPlanRejectPrefix  = "plan:reject:"
	discordToolAllowOncePrefix      = "tool:allow-once:"
	discordToolAllowSessionPrefix   = "tool:allow-session:"
	discordToolAllowWorkspacePrefix = "tool:allow-workspace:"
	discordToolDenyPrefix           = "tool:deny:"
)

// messageJob represents a queued message to be processed.
type messageJob struct {
	s *discordgo.Session
	m *discordgo.MessageCreate
}

type historyEntry struct {
	FormattedText string
	Images        []core.ImageData
	IsBot         bool
}

// historyImageExtractor can be overridden in tests to avoid network I/O.
var historyImageExtractor = extractDiscordImagesLimited

// Bot connects to Discord Gateway and forwards messages to the NekoClaw service.
type Bot struct {
	session *discordgo.Session
	svc     *app.Service

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Per-channel message queues for sequential processing.
	queues   map[string]chan messageJob
	queuesMu sync.Mutex

	// Per-channel active session tracking.
	activeSessions   map[string]string
	activeSessionsMu sync.RWMutex

	// Per-channel reset cutoff for Discord-native history injection.
	// Messages at or before this timestamp are excluded from ephemeral context.
	historyCutoffs   map[string]time.Time
	historyCutoffsMu sync.RWMutex

	// Persistent channel → session binding store (survives restarts).
	bindingStore *binding.Store
}

// Config holds the configuration needed to create a Discord bot.
type Config struct {
	Token    string // Discord bot token (required)
	StateDir string // Directory for persistent state (bindings, etc.)
}

// New creates a new Discord bot. Call Start() to connect.
func New(svc *app.Service, cfg Config) (*Bot, error) {
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("discord bot token is required")
	}
	// discordgo expects "Bot <token>" prefix.
	if !strings.HasPrefix(token, "Bot ") {
		token = "Bot " + token
	}

	dg, err := discordgo.New(token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	bot := &Bot{
		session:        dg,
		svc:            svc,
		queues:         make(map[string]chan messageJob),
		activeSessions: make(map[string]string),
		historyCutoffs: make(map[string]time.Time),
	}

	// Load persistent bindings from disk (if StateDir is configured).
	if dir := strings.TrimSpace(cfg.StateDir); dir != "" {
		storePath := filepath.Join(dir, "discord-bindings.json")
		bs, loadErr := binding.Load(storePath)
		if loadErr != nil {
			logDiscord.Errorf("load bindings: %v", loadErr)
		} else {
			bot.bindingStore = bs
			// Hydrate in-memory maps from persisted bindings.
			for channelID, entry := range bs.All() {
				bot.activeSessions[channelID] = entry.SessionID
				if entry.HistoryCutoff != nil {
					bot.historyCutoffs[channelID] = *entry.HistoryCutoff
				}
			}
			if n := len(bot.activeSessions); n > 0 {
				logDiscord.Logf("restored %d channel bindings from disk", n)
			}
		}
	}

	return bot, nil
}

// Start connects to Discord Gateway and begins handling messages.
// Blocks until the context is cancelled or an error occurs.
func (b *Bot) Start(ctx context.Context) error {
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.session.AddHandler(b.onMessageCreate)
	b.session.AddHandler(b.onInteractionCreate)

	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open discord gateway: %w", err)
	}
	if err := b.registerSlashCommands(); err != nil {
		_ = b.session.Close()
		return err
	}
	logDiscord.Logf("bot started: user=%s", b.session.State.User.Username)

	// Wire up Discord output for all module loggers.
	cfg := b.svc.GetDiscordConfig()
	if ch := strings.TrimSpace(cfg.ConsoleChannel); ch != "" {
		logger.SetDiscordOutput(&discordLogSender{session: b.session}, ch)
	}

	b.logToConsole(b.session, fmt.Sprintf("[啟動] Discord Bot 已上線 (%s)", b.session.State.User.Username))

	// Block until context is done.
	<-b.ctx.Done()

	// Close all queues so workers drain remaining jobs and exit.
	b.queuesMu.Lock()
	for _, ch := range b.queues {
		close(ch)
	}
	b.queues = nil
	b.queuesMu.Unlock()

	b.wg.Wait()
	return b.session.Close()
}

// Stop gracefully shuts down the bot.
func (b *Bot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
}

// ---------------------------------------------------------------------------
// Session management
// ---------------------------------------------------------------------------

// getSessionID returns the active session ID for the given channel.
// Falls back to the default per-channel format when no reset session exists.
func (b *Bot) getSessionID(channelID string) string {
	b.activeSessionsMu.RLock()
	sid, ok := b.activeSessions[channelID]
	b.activeSessionsMu.RUnlock()
	if ok {
		return sid
	}
	return fmt.Sprintf("discord:%s", channelID)
}

// resetSession creates a new active session ID and cutoff for the channel,
// and persists the binding to disk so it survives restarts.
func (b *Bot) resetSession(channelID string) string {
	newID := fmt.Sprintf("discord:%s-%s", channelID, time.Now().Format("0102-150405"))
	cutoff := time.Now().UTC()

	b.activeSessionsMu.Lock()
	b.activeSessions[channelID] = newID
	b.activeSessionsMu.Unlock()

	b.historyCutoffsMu.Lock()
	b.historyCutoffs[channelID] = cutoff
	b.historyCutoffsMu.Unlock()

	// Persist binding so the session survives bot restarts.
	if b.bindingStore != nil {
		b.bindingStore.Set(channelID, binding.Entry{
			SessionID:     newID,
			HistoryCutoff: &cutoff,
		})
	}

	return newID
}

func (b *Bot) historyCutoff(channelID string) *time.Time {
	b.historyCutoffsMu.RLock()
	cutoff, ok := b.historyCutoffs[channelID]
	b.historyCutoffsMu.RUnlock()
	if !ok {
		return nil
	}
	c := cutoff
	return &c
}

// ---------------------------------------------------------------------------
// Message handling
// ---------------------------------------------------------------------------

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from bots (including self).
	if m.Author == nil || m.Author.Bot {
		return
	}

	if !b.shouldRespond(s, m) {
		return
	}

	// React to acknowledge receipt.
	_ = s.MessageReactionAdd(m.ChannelID, m.ID, emojiReceived)

	// Enqueue for sequential per-channel processing.
	b.queuesMu.Lock()
	if b.queues == nil {
		b.queuesMu.Unlock()
		return
	}
	ch, ok := b.queues[m.ChannelID]
	if !ok {
		ch = make(chan messageJob, 64)
		b.queues[m.ChannelID] = ch
		b.wg.Add(1)
		go b.channelWorker(ch)
	}
	b.queuesMu.Unlock()

	select {
	case ch <- messageJob{s: s, m: m}:
	case <-b.ctx.Done():
	}
}

// channelWorker processes messages for a single channel sequentially.
func (b *Bot) channelWorker(ch <-chan messageJob) {
	defer b.wg.Done()
	for job := range ch {
		b.handleMessage(job.s, job.m)
	}
}

// shouldRespond checks whether the bot should respond to this message.
// Responds to: @mention, reply to bot, or DM.
func (b *Bot) shouldRespond(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	// Reply to bot's own message.
	if m.ReferencedMessage != nil && m.ReferencedMessage.Author != nil {
		if m.ReferencedMessage.Author.ID == s.State.User.ID {
			return true
		}
	}

	// @mention the bot.
	for _, mention := range m.Mentions {
		if mention.ID == s.State.User.ID {
			return true
		}
	}

	// DM channel.
	channel, err := s.State.Channel(m.ChannelID)
	if err != nil {
		channel, err = s.Channel(m.ChannelID)
	}
	if err == nil && channel.Type == discordgo.ChannelTypeDM {
		return true
	}

	return false
}

func (b *Bot) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	startTime := time.Now()

	// Strip bot mention from message content.
	text := stripMention(m.Content, s.State.User.ID)
	text = strings.TrimSpace(text)

	// Extract images from attachments.
	images := extractDiscordImages(m.Attachments)

	// Skip if no text and no images.
	if text == "" && len(images) == 0 {
		return
	}

	// Default text when only images are sent.
	if text == "" && len(images) > 0 {
		text = "請描述這張圖片"
	}

	// --- Context building ---
	var contextParts []string

	// Sender identity so the LLM knows who is speaking.
	//    Include Discord user ID so the LLM can mention them with <@ID>.
	senderName := m.Author.Username
	if m.Member != nil && m.Member.Nick != "" {
		senderName = m.Member.Nick
	}
	contextParts = append(contextParts, fmt.Sprintf("[發送者: %s (ID:<@%s>)]", senderName, m.Author.ID))

	// Prepend context to the user message.
	if len(contextParts) > 0 {
		text = strings.Join(contextParts, "\n") + "\n" + text
	}

	// Build ephemeral Discord-native channel history (not persisted).
	cutoff := b.historyCutoff(m.ChannelID)
	historyEntries := b.buildChannelHistoryEntries(s, m.ChannelID, m.ID, cutoff)
	if replyEntry := b.buildReplyReferenceEntry(s, m, cutoff); replyEntry != nil {
		historyEntries = append(historyEntries, *replyEntry)
	}
	ephemeralMessages := buildEphemeralMessages(historyEntries)

	botID := s.State.User.ID
	sessionID := b.getSessionID(m.ChannelID)

	// Transition: 👀 → 🔄
	_ = s.MessageReactionRemove(m.ChannelID, m.ID, emojiReceived, botID)
	_ = s.MessageReactionAdd(m.ChannelID, m.ID, emojiProcessing)

	// Show typing indicator while processing.
	stopTyping := b.startTyping(s, m.ChannelID)

	// Send placeholder message for streaming updates.
	placeholder := &discordgo.MessageSend{
		Content: "🔄 處理中...",
		Reference: &discordgo.MessageReference{
			MessageID: m.ID,
			ChannelID: m.ChannelID,
			GuildID:   m.GuildID,
		},
	}
	placeholderMsg, placeholderErr := s.ChannelMessageSendComplex(m.ChannelID, placeholder)

	// Stream response chunks, updating placeholder in real time.
	var (
		fullText     strings.Builder
		lastEditTime time.Time
		editInterval = 1500 * time.Millisecond
		textStarted  bool
		streamResp   *core.ChatResponse
		streamErr    string
	)

	streamCh := b.svc.HandleChatStream(b.ctx, core.ChatRequest{
		SessionID:         sessionID,
		DisableSession:    false,
		EphemeralMessages: ephemeralMessages,
		Surface:           core.SurfaceDiscord,
		Provider:          b.svc.GetDefaultProvider(),
		Model:             b.svc.GetDefaultModel(),
		Message:           text,
		Images:            images,
		EnableTools:       true,
	})

	for chunk := range streamCh {
		switch chunk.Type {
		case core.ChunkToolStatus:
			displayName := chunk.ToolName
			if serverName, toolName, isMCP := mcp.ParseNamespacedTool(chunk.ToolName); isMCP {
				displayName = serverName + "/" + toolName
			}
			content := "🔧 正在使用 " + displayName + "..."
			if placeholderErr == nil {
				_, _ = s.ChannelMessageEdit(m.ChannelID, placeholderMsg.ID, content)
			}

		case core.ChunkRetryStatus:
			content := "🔄 處理中...（" + chunk.RetryStatus + "）"
			if placeholderErr == nil {
				_, _ = s.ChannelMessageEdit(m.ChannelID, placeholderMsg.ID, content)
			}

		case core.ChunkText:
			fullText.WriteString(chunk.Content)
			textStarted = true
			if time.Since(lastEditTime) >= editInterval {
				preview := fullText.String()
				if len(preview) > 1900 {
					preview = preview[:1900] + "..."
				}
				if placeholderErr == nil {
					_, _ = s.ChannelMessageEdit(m.ChannelID, placeholderMsg.ID, preview)
				}
				lastEditTime = time.Now()
			}

		case core.ChunkError:
			streamErr = chunk.Error

		case core.ChunkDone:
			streamResp = chunk.Response
		}
	}

	stopTyping()

	// Transition: 🔄 → ✅
	_ = s.MessageReactionRemove(m.ChannelID, m.ID, emojiProcessing, botID)
	_ = s.MessageReactionAdd(m.ChannelID, m.ID, emojiDone)

	// Handle error cases.
	if streamErr != "" {
		errMsg := "⚠️ " + streamErr
		logDiscord.Errorf("chat error: channel=%s user=%s error=%s", m.ChannelID, m.Author.ID, streamErr)
		if placeholderErr == nil {
			_, _ = s.ChannelMessageEdit(m.ChannelID, placeholderMsg.ID, errMsg)
		} else {
			b.sendReply(s, m, errMsg)
		}
		b.logToConsole(s, fmt.Sprintf("[錯誤] <#%s> %s: %s", m.ChannelID, m.Author.Username, streamErr))
		return
	}
	if streamResp == nil {
		errMsg := "⚠️ 未收到回應"
		logDiscord.Errorf("chat error: channel=%s user=%s error=nil response", m.ChannelID, m.Author.ID)
		if placeholderErr == nil {
			_, _ = s.ChannelMessageEdit(m.ChannelID, placeholderMsg.ID, errMsg)
		} else {
			b.sendReply(s, m, errMsg)
		}
		return
	}

	elapsed := responseElapsed(*streamResp, time.Since(startTime))

	// Build final reply from accumulated text or response.
	reply := strings.TrimSpace(fullText.String())
	if !textStarted && streamResp != nil {
		reply = strings.TrimSpace(streamResp.Reply)
	}
	if reply == "" {
		reply = "（無回應）"
	}

	// Append usage stats and tool summary (Discord -# small text).
	var footer []string
	if stats := formatUsageStats(streamResp.Usage, elapsed, streamResp.Provider, streamResp.Model); stats != "" {
		footer = append(footer, "-# "+stats)
	}
	if summary := formatToolSummary(streamResp.ToolEvents); summary != "" {
		footer = append(footer, summary)
	}
	if len(footer) > 0 {
		reply += "\n\n" + strings.Join(footer, "\n")
	}

	// Always delete the placeholder and resend as fresh messages.
	// This prevents the "(edited)" tag and handles overflow / message ordering
	// correctly when someone else sends a message during streaming.
	if placeholderErr == nil {
		_ = s.ChannelMessageDelete(m.ChannelID, placeholderMsg.ID)
	}
	b.sendReply(s, m, reply)

	// Log detailed traffic to console channel.
	resp := *streamResp
	b.logTraffic(s, m, resp, len(images), elapsed)
}

// ---------------------------------------------------------------------------
// Command handling
// ---------------------------------------------------------------------------

type discordCommandRuntime struct {
	bot       *Bot
	channelID string
}

func (r *discordCommandRuntime) ResetSession() string {
	return r.bot.resetSession(r.channelID)
}

func (r *discordCommandRuntime) ListPersonas() []persona.PersonaInfo {
	return r.bot.svc.ListPersonas()
}

func (r *discordCommandRuntime) ActivePersona() *persona.PersonaInfo {
	return r.bot.svc.ActivePersona()
}

func (r *discordCommandRuntime) FindPersonaByName(name string) (string, bool) {
	return r.bot.svc.FindPersonaByName(name)
}

func (r *discordCommandRuntime) SetActivePersona(dirName string) error {
	return r.bot.svc.SetActivePersona(dirName)
}

func (r *discordCommandRuntime) ClearActivePersona() error {
	return r.bot.svc.ClearActivePersona()
}

func (b *Bot) registerSlashCommands() error {
	appID := strings.TrimSpace(discordApplicationID(b.session))
	if appID == "" {
		return fmt.Errorf("register discord slash commands: missing application id")
	}
	if _, err := b.session.ApplicationCommandBulkOverwrite(appID, "", buildDiscordApplicationCommands()); err != nil {
		return fmt.Errorf("register discord slash commands: %w", err)
	}
	return nil
}

func buildDiscordApplicationCommands() []*discordgo.ApplicationCommand {
	specs := botcmd.Specs()
	commands := make([]*discordgo.ApplicationCommand, 0, len(specs))
	for _, spec := range specs {
		command := &discordgo.ApplicationCommand{
			Name:        spec.Name,
			Description: spec.Description,
		}
		if len(spec.Options) > 0 {
			command.Options = make([]*discordgo.ApplicationCommandOption, 0, len(spec.Options))
			for _, option := range spec.Options {
				command.Options = append(command.Options, &discordgo.ApplicationCommandOption{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        option.Name,
					Description: option.Description,
					Required:    option.Required,
				})
			}
		}
		commands = append(commands, command)
	}
	return commands
}

func discordApplicationID(session *discordgo.Session) string {
	if session == nil || session.State == nil {
		return ""
	}
	if session.State.Application != nil && strings.TrimSpace(session.State.Application.ID) != "" {
		return session.State.Application.ID
	}
	if session.State.User != nil {
		return strings.TrimSpace(session.State.User.ID)
	}
	return ""
}

func (b *Bot) onInteractionCreate(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	switch ic.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleApplicationCommandInteraction(s, ic)
	case discordgo.InteractionMessageComponent:
		b.handleMessageComponentInteraction(s, ic)
	}
}

func (b *Bot) handleApplicationCommandInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	inv, ok := parseDiscordApplicationCommandInvocation(ic)
	if !ok {
		return
	}
	if inv.Name == botcmd.CommandPlan {
		b.handlePlanCommandInteraction(s, ic, inv)
		return
	}

	result, event, handled := b.dispatchCommand(ic.ChannelID, inv)
	if !handled {
		return
	}

	content, components := buildDiscordCommandResponse(result)
	if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: components,
		},
	}); err != nil {
		logDiscord.Errorf("interaction respond error: type=command name=%s error=%v", inv.Name, err)
		return
	}

	b.logDiscordCommandEvent(s, ic, event)
}

func (b *Bot) handleMessageComponentInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	if b.handlePlannerComponentInteraction(s, ic) {
		return
	}

	inv, ok := parseDiscordComponentInvocation(ic)
	if !ok {
		return
	}

	result, event, handled := b.dispatchCommand(ic.ChannelID, inv)
	if !handled {
		return
	}

	content, components := buildDiscordCommandResponse(result)
	if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: components,
		},
	}); err != nil {
		logDiscord.Errorf("interaction respond error: type=component action=%s error=%v", inv.ComponentAction, err)
		return
	}

	b.logDiscordCommandEvent(s, ic, event)
}

func parseDiscordApplicationCommandInvocation(ic *discordgo.InteractionCreate) (botcmd.Invocation, bool) {
	if ic == nil {
		return botcmd.Invocation{}, false
	}
	data := ic.ApplicationCommandData()
	if strings.TrimSpace(data.Name) == "" {
		return botcmd.Invocation{}, false
	}

	inv := botcmd.Invocation{Name: data.Name}
	if option := data.GetOption(botcmd.OptionPersonaName); option != nil {
		inv.StringOptions = map[string]string{
			botcmd.OptionPersonaName: option.StringValue(),
		}
	}
	if option := data.GetOption(botcmd.OptionPlanPrompt); option != nil {
		if inv.StringOptions == nil {
			inv.StringOptions = map[string]string{}
		}
		inv.StringOptions[botcmd.OptionPlanPrompt] = option.StringValue()
	}
	return inv, true
}

func parseDiscordComponentInvocation(ic *discordgo.InteractionCreate) (botcmd.Invocation, bool) {
	if ic == nil {
		return botcmd.Invocation{}, false
	}
	data := ic.MessageComponentData()
	if data.CustomID != botcmd.ActionPersonaSelect || len(data.Values) == 0 {
		return botcmd.Invocation{}, false
	}
	return botcmd.Invocation{
		Name:            botcmd.CommandPersona,
		ComponentAction: botcmd.ActionPersonaSelect,
		ComponentValue:  data.Values[0],
	}, true
}

func (b *Bot) handlePlanCommandInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate, inv botcmd.Invocation) {
	prompt := strings.TrimSpace(inv.StringOption(botcmd.OptionPlanPrompt))
	if prompt == "" {
		_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "⚠️ 缺少計畫內容。",
			},
		})
		return
	}

	plan, err := b.svc.CreatePlan(b.ctx, core.CreatePlanRequest{
		SessionID: b.getSessionID(ic.ChannelID),
		Surface:   core.SurfaceDiscord,
		Provider:  b.svc.GetDefaultProvider(),
		Model:     b.svc.GetDefaultModel(),
		Prompt:    prompt,
	})
	if err != nil {
		logDiscord.Errorf("create plan error: channel=%s error=%v", ic.ChannelID, err)
		_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "⚠️ " + err.Error(),
			},
		})
		return
	}

	_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    buildDiscordPlanContent(plan),
			Components: buildDiscordPlanComponents(plan),
		},
	})
}

func (b *Bot) handlePlannerComponentInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate) bool {
	if ic == nil {
		return false
	}
	data := ic.MessageComponentData()
	customID := strings.TrimSpace(data.CustomID)
	if customID == "" {
		return false
	}

	switch {
	case strings.HasPrefix(customID, discordPlanApprovePrefix):
		planID := strings.TrimPrefix(customID, discordPlanApprovePrefix)
		plan, err := b.svc.ApprovePlan(planID)
		if err != nil {
			_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "⚠️ " + err.Error(),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return true
		}
		if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    buildDiscordPlanContent(plan),
				Components: nil,
			},
		}); err != nil {
			logDiscord.Errorf("approve plan respond: %v", err)
			return true
		}
		go b.streamPlanExecution(s, ic.ChannelID, plan.ID)
		return true
	case strings.HasPrefix(customID, discordPlanRejectPrefix):
		planID := strings.TrimPrefix(customID, discordPlanRejectPrefix)
		plan, err := b.svc.RejectPlan(planID)
		if err != nil {
			_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "⚠️ " + err.Error(),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return true
		}
		_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    buildDiscordPlanContent(plan),
				Components: nil,
			},
		})
		return true
	case strings.HasPrefix(customID, discordToolAllowOncePrefix),
		strings.HasPrefix(customID, discordToolAllowSessionPrefix),
		strings.HasPrefix(customID, discordToolAllowWorkspacePrefix),
		strings.HasPrefix(customID, discordToolDenyPrefix):
		decision := "allow_once"
		prefix := discordToolAllowOncePrefix
		if strings.HasPrefix(customID, discordToolAllowSessionPrefix) {
			decision = "allow_for_session"
			prefix = discordToolAllowSessionPrefix
		} else if strings.HasPrefix(customID, discordToolAllowWorkspacePrefix) {
			decision = "allow_for_workspace"
			prefix = discordToolAllowWorkspacePrefix
		} else if strings.HasPrefix(customID, discordToolDenyPrefix) {
			decision = "deny"
			prefix = discordToolDenyPrefix
		}
		payload := strings.SplitN(strings.TrimPrefix(customID, prefix), ":", 2)
		if len(payload) != 2 {
			return false
		}
		runID := payload[0]
		approvalID := payload[1]
		if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("已%s工具請求 `%s`。", map[string]string{
					"allow_once":          "單次核准",
					"allow_for_session":   "核准本 session",
					"allow_for_workspace": "核准工作區",
					"deny":                "拒絕",
				}[decision], approvalID),
			},
		}); err != nil {
			logDiscord.Errorf("tool approval respond: %v", err)
			return true
		}
		go b.streamPendingToolRun(s, ic.ChannelID, runID, approvalID, decision)
		return true
	default:
		return false
	}
}

func buildDiscordPlanContent(plan core.PlanRecord) string {
	status := map[core.PlanStatus]string{
		core.PlanStatusPendingApproval: "待核准",
		core.PlanStatusApproved:        "已核准",
		core.PlanStatusRejected:        "已拒絕",
		core.PlanStatusExecuting:       "執行中",
		core.PlanStatusCompleted:       "已完成",
		core.PlanStatusFailed:          "失敗",
		core.PlanStatusSuperseded:      "已取代",
	}[plan.Status]
	content := fmt.Sprintf("## Plan\n狀態：%s\n\n%s", status, strings.TrimSpace(plan.PlanMarkdown))
	if strings.TrimSpace(plan.LastError) != "" {
		content += "\n\n錯誤：" + strings.TrimSpace(plan.LastError)
	}
	return limitDiscordContent(content)
}

func buildDiscordPlanComponents(plan core.PlanRecord) []discordgo.MessageComponent {
	if plan.Status != core.PlanStatusPendingApproval {
		return nil
	}
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					CustomID: discordPlanApprovePrefix + plan.ID,
					Label:    "Approve",
					Style:    discordgo.SuccessButton,
				},
				discordgo.Button{
					CustomID: discordPlanRejectPrefix + plan.ID,
					Label:    "Reject",
					Style:    discordgo.SecondaryButton,
				},
			},
		},
	}
}

func buildDiscordToolApprovalComponents(runID string, approvals []core.PendingToolApproval) []discordgo.MessageComponent {
	rows := make([]discordgo.MessageComponent, 0, len(approvals))
	for _, approval := range approvals {
		label := truncateRunesWithEllipsis(approval.ToolName, 40)
		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					CustomID: discordToolAllowOncePrefix + runID + ":" + approval.ApprovalID,
					Label:    "Once " + label,
					Style:    discordgo.SuccessButton,
				},
				discordgo.Button{
					CustomID: discordToolAllowSessionPrefix + runID + ":" + approval.ApprovalID,
					Label:    "Session",
					Style:    discordgo.PrimaryButton,
				},
				discordgo.Button{
					CustomID: discordToolAllowWorkspacePrefix + runID + ":" + approval.ApprovalID,
					Label:    "Workspace",
					Style:    discordgo.SecondaryButton,
				},
				discordgo.Button{
					CustomID: discordToolDenyPrefix + runID + ":" + approval.ApprovalID,
					Label:    "Deny",
					Style:    discordgo.DangerButton,
				},
			},
		})
	}
	return rows
}

func (b *Bot) streamPlanExecution(s *discordgo.Session, channelID, planID string) {
	ch, err := b.svc.RunApprovedPlan(b.ctx, planID)
	if err != nil {
		_, _ = s.ChannelMessageSend(channelID, "⚠️ "+err.Error())
		return
	}
	b.streamDiscordExecution(s, channelID, ch)
}

func (b *Bot) streamPendingToolRun(s *discordgo.Session, channelID, runID, approvalID, decision string) {
	ch, err := b.svc.ContinuePendingToolRunStream(b.ctx, runID, []core.ToolApprovalDecision{
		{ApprovalID: approvalID, Decision: decision},
	})
	if err != nil {
		_, _ = s.ChannelMessageSend(channelID, "⚠️ "+err.Error())
		return
	}
	b.streamDiscordExecution(s, channelID, ch)
}

func (b *Bot) streamDiscordExecution(s *discordgo.Session, channelID string, streamCh <-chan core.StreamChunk) {
	placeholderMsg, placeholderErr := s.ChannelMessageSend(channelID, "🔄 處理中...")

	var (
		fullText     strings.Builder
		lastEditTime time.Time
		editInterval = 1500 * time.Millisecond
		streamResp   *core.ChatResponse
		streamErr    string
	)

	for chunk := range streamCh {
		switch chunk.Type {
		case core.ChunkToolStatus:
			displayName := chunk.ToolName
			if serverName, toolName, isMCP := mcp.ParseNamespacedTool(chunk.ToolName); isMCP {
				displayName = serverName + "/" + toolName
			}
			if placeholderErr == nil {
				_, _ = s.ChannelMessageEdit(channelID, placeholderMsg.ID, "🔧 正在使用 "+displayName+"...")
			}
		case core.ChunkRetryStatus:
			if placeholderErr == nil {
				_, _ = s.ChannelMessageEdit(channelID, placeholderMsg.ID, "🔄 處理中...（"+chunk.RetryStatus+"）")
			}
		case core.ChunkText:
			fullText.WriteString(chunk.Content)
			if time.Since(lastEditTime) >= editInterval && placeholderErr == nil {
				preview := fullText.String()
				if len(preview) > 1900 {
					preview = preview[:1900] + "..."
				}
				_, _ = s.ChannelMessageEdit(channelID, placeholderMsg.ID, preview)
				lastEditTime = time.Now()
			}
		case core.ChunkError:
			streamErr = chunk.Error
		case core.ChunkDone:
			streamResp = chunk.Response
		}
	}

	if streamErr != "" {
		if placeholderErr == nil {
			_, _ = s.ChannelMessageEdit(channelID, placeholderMsg.ID, "⚠️ "+streamErr)
		} else {
			_, _ = s.ChannelMessageSend(channelID, "⚠️ "+streamErr)
		}
		return
	}
	if streamResp == nil {
		if placeholderErr == nil {
			_, _ = s.ChannelMessageEdit(channelID, placeholderMsg.ID, "⚠️ 未收到回應")
		}
		return
	}
	if streamResp.Status == core.ChatStatusApprovalRequired && len(streamResp.PendingApprovals) > 0 {
		if placeholderErr == nil {
			_, _ = s.ChannelMessageEdit(channelID, placeholderMsg.ID, "⏸️ 需要工具核准。")
		}
		_, _ = s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Content:    "請核准或拒絕以下工具操作：",
			Components: buildDiscordToolApprovalComponents(streamResp.RunID, streamResp.PendingApprovals),
		})
		return
	}

	reply := strings.TrimSpace(fullText.String())
	if reply == "" {
		reply = strings.TrimSpace(streamResp.Reply)
	}
	if reply == "" {
		reply = "（無回應）"
	}
	if placeholderErr == nil {
		_ = s.ChannelMessageDelete(channelID, placeholderMsg.ID)
	}
	chunks := splitMessage(reply, discordMessageLimit)
	for _, chunk := range chunks {
		_, _ = s.ChannelMessageSend(channelID, chunk)
	}
}

func (b *Bot) dispatchCommand(channelID string, inv botcmd.Invocation) (botcmd.Result, *botcmd.Event, bool) {
	runtime := &discordCommandRuntime{
		bot:       b,
		channelID: channelID,
	}
	result, handled := botcmd.Dispatch(runtime, inv)
	if !handled {
		return botcmd.Result{}, nil, false
	}
	return result, result.Event, true
}

func buildDiscordCommandResponse(result botcmd.Result) (string, []discordgo.MessageComponent) {
	if result.Selector == nil {
		return limitDiscordContent(result.Message), nil
	}

	content := botcmd.RenderPersonaSelectorText(result.Selector, result.SelectorNotice)
	if len(result.Selector.Options) > botcmd.MaxPersonaSelectValues {
		content += "\n\n⚠️ 角色數量超過 Discord 選單上限，請改用 `/persona name:<名稱>`。"
		return limitDiscordContent(content), nil
	}
	return limitDiscordContent(content), buildDiscordPersonaComponents(result.Selector)
}

func buildDiscordPersonaComponents(selector *botcmd.PersonaSelector) []discordgo.MessageComponent {
	if selector == nil || len(selector.Options) == 0 {
		return nil
	}

	hasActive := false
	options := make([]discordgo.SelectMenuOption, 0, len(selector.Options)+1)
	for _, option := range selector.Options {
		hasActive = hasActive || option.Active
		selectOption := discordgo.SelectMenuOption{
			Label:       truncateRunesWithEllipsis(option.Name, 100),
			Value:       option.DirName,
			Description: truncateRunesWithEllipsis(option.Description, 100),
			Default:     option.Active,
		}
		options = append(options, selectOption)
	}
	if selector.OffLabel != "" && len(selector.Options) < botcmd.MaxPersonaSelectValues {
		options = append(options, discordgo.SelectMenuOption{
			Label:   truncateRunesWithEllipsis(selector.OffLabel, 100),
			Value:   botcmd.PersonaOffValue,
			Default: !hasActive,
		})
	}

	totalMenus := (len(options) + botcmd.MaxPersonaSelectOptions - 1) / botcmd.MaxPersonaSelectOptions
	rows := make([]discordgo.MessageComponent, 0, totalMenus)
	for i := 0; i < len(options); i += botcmd.MaxPersonaSelectOptions {
		end := i + botcmd.MaxPersonaSelectOptions
		if end > len(options) {
			end = len(options)
		}
		chunk := options[i:end]
		menuIndex := (i / botcmd.MaxPersonaSelectOptions) + 1
		placeholder := "選擇角色"
		if totalMenus > 1 {
			placeholder = fmt.Sprintf("選擇角色 (%d/%d)", menuIndex, totalMenus)
		}
		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					MenuType:    discordgo.StringSelectMenu,
					CustomID:    botcmd.ActionPersonaSelect,
					Placeholder: placeholder,
					MaxValues:   1,
					Options:     chunk,
				},
			},
		})
	}
	return rows
}

func limitDiscordContent(content string) string {
	if runeLen(content) <= discordMessageLimit {
		return content
	}
	return truncateRunesWithEllipsis(content, discordMessageLimit)
}

func (b *Bot) logDiscordCommandEvent(s *discordgo.Session, ic *discordgo.InteractionCreate, event *botcmd.Event) {
	if event == nil {
		return
	}
	actor := discordInteractionActorName(ic)
	switch event.Type {
	case botcmd.EventReset:
		logDiscord.Logf("session reset: channel=%s new_session=%s", ic.ChannelID, event.SessionID)
		b.logToConsole(s, fmt.Sprintf("[重置] <#%s> 對話已重置 → %s (by %s)", ic.ChannelID, event.SessionID, actor))
	case botcmd.EventPersonaSet:
		b.logToConsole(s, fmt.Sprintf("[角色] 切換至「%s」(by %s)", event.PersonaName, actor))
	case botcmd.EventPersonaCleared:
		b.logToConsole(s, fmt.Sprintf("[角色] 已停用角色 (by %s)", actor))
	}
}

func discordInteractionActorName(ic *discordgo.InteractionCreate) string {
	if ic == nil {
		return "unknown"
	}
	if ic.Member != nil {
		if strings.TrimSpace(ic.Member.Nick) != "" {
			return ic.Member.Nick
		}
		if ic.Member.User != nil {
			if strings.TrimSpace(ic.Member.User.GlobalName) != "" {
				return ic.Member.User.GlobalName
			}
			if strings.TrimSpace(ic.Member.User.Username) != "" {
				return ic.Member.User.Username
			}
		}
	}
	if ic.User != nil {
		if strings.TrimSpace(ic.User.GlobalName) != "" {
			return ic.User.GlobalName
		}
		if strings.TrimSpace(ic.User.Username) != "" {
			return ic.User.Username
		}
	}
	return "unknown"
}

// ---------------------------------------------------------------------------
// Ephemeral Discord History
// ---------------------------------------------------------------------------

func (b *Bot) buildChannelHistoryEntries(
	s *discordgo.Session,
	channelID string,
	currentMessageID string,
	cutoff *time.Time,
) []historyEntry {
	messages, err := s.ChannelMessages(channelID, discordHistoryFetchLimit, currentMessageID, "", "")
	if err != nil {
		logDiscord.Warnf("channel history fetch failed: channel=%s error=%v", channelID, err)
		return nil
	}
	if len(messages) == 0 {
		return nil
	}
	return buildChannelHistoryEntriesFromMessages(messages, s.State.User.ID, cutoff, time.Now().UTC())
}

func buildChannelHistoryEntriesFromMessages(
	messages []*discordgo.Message,
	botID string,
	cutoff *time.Time,
	now time.Time,
) []historyEntry {
	// Discord returns ChannelMessages in newest->oldest order.
	// Select from newest first (better recency under budget), then reverse to
	// chronological order for model input.
	entries := make([]historyEntry, 0, discordHistoryTargetEntries)
	totalImages := 0
	usedChars := 0

	for i := 0; i < len(messages); i++ {
		if len(entries) >= discordHistoryTargetEntries || usedChars >= discordHistoryCharBudget {
			break
		}

		msg := messages[i]
		if msg == nil || msg.Author == nil {
			continue
		}
		if !shouldIncludeDiscordTimestamp(msg.Timestamp, cutoff) {
			continue
		}

		content := strings.TrimSpace(msg.Content)
		if content == "" && len(msg.Attachments) > 0 {
			content = buildAttachmentPlaceholder(msg.Attachments)
		}
		if content == "" {
			continue
		}
		if runeLen(content) > discordHistoryEntryTextLimit {
			content = truncateRunesWithEllipsis(content, discordHistoryEntryTextLimit)
		}

		recentOffset := i
		entryImages := []core.ImageData(nil)
		if recentOffset < discordHistoryImageScanLimit && totalImages < discordHistoryImageTotalLimit &&
			now.Sub(msg.Timestamp.UTC()) <= discordHistoryImageMaxAge {
			remaining := discordHistoryImageTotalLimit - totalImages
			entryImages = historyImageExtractor(msg.Attachments, remaining)
			if len(entryImages) > remaining {
				entryImages = entryImages[:remaining]
			}
			totalImages += len(entryImages)
		}

		isBot := strings.TrimSpace(msg.Author.ID) == strings.TrimSpace(botID)
		ts := formatHistoryTimestamp(msg.Timestamp)
		sender := resolveHistorySenderName(msg.Author)

		formatted := content
		if !isBot {
			formatted = fmt.Sprintf("[%s · %s] %s", sender, ts, content)
		}

		formattedLen := runeLen(formatted)
		if usedChars+formattedLen > discordHistoryCharBudget {
			remaining := discordHistoryCharBudget - usedChars
			if remaining <= 0 {
				break
			}
			formatted = truncateRunesWithEllipsis(formatted, remaining)
			formattedLen = runeLen(formatted)
		}

		entries = append(entries, historyEntry{
			FormattedText: formatted,
			Images:        entryImages,
			IsBot:         isBot,
		})
		usedChars += formattedLen

		if usedChars >= discordHistoryCharBudget {
			break
		}
	}

	// Reverse to chronological order (oldest -> newest).
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	return entries
}

func (b *Bot) buildReplyReferenceEntry(
	s *discordgo.Session,
	m *discordgo.MessageCreate,
	cutoff *time.Time,
) *historyEntry {
	if m == nil || m.MessageReference == nil || strings.TrimSpace(m.MessageReference.MessageID) == "" {
		return nil
	}

	refMsg := m.ReferencedMessage
	if refMsg == nil {
		refChannelID := m.ChannelID
		if strings.TrimSpace(m.MessageReference.ChannelID) != "" {
			refChannelID = m.MessageReference.ChannelID
		}
		fetched, err := s.ChannelMessage(refChannelID, m.MessageReference.MessageID)
		if err != nil {
			logDiscord.Warnf("reply target fetch failed: channel=%s message=%s error=%v",
				refChannelID, m.MessageReference.MessageID, err)
			return nil
		}
		refMsg = fetched
	}

	if refMsg == nil || refMsg.Author == nil || !shouldIncludeDiscordTimestamp(refMsg.Timestamp, cutoff) {
		return nil
	}

	content := strings.TrimSpace(refMsg.Content)
	if content == "" && len(refMsg.Attachments) > 0 {
		content = buildAttachmentPlaceholder(refMsg.Attachments)
	}
	if content == "" {
		return nil
	}
	if runeLen(content) > discordHistoryEntryTextLimit {
		content = truncateRunesWithEllipsis(content, discordHistoryEntryTextLimit)
	}

	isBot := strings.TrimSpace(refMsg.Author.ID) == strings.TrimSpace(s.State.User.ID)
	ts := formatHistoryTimestamp(refMsg.Timestamp)
	sender := resolveHistorySenderName(refMsg.Author)

	formatted := content
	if !isBot {
		formatted = fmt.Sprintf("[↩ %s · %s] %s", sender, ts, content)
	}

	images := historyImageExtractor(refMsg.Attachments, discordReplyReferenceImageLimit)
	if len(images) > discordReplyReferenceImageLimit {
		images = images[:discordReplyReferenceImageLimit]
	}

	return &historyEntry{
		FormattedText: formatted,
		Images:        images,
		IsBot:         isBot,
	}
}

func buildEphemeralMessages(entries []historyEntry) []core.Message {
	out := make([]core.Message, 0, len(entries))
	for _, entry := range entries {
		content := strings.TrimSpace(entry.FormattedText)
		if content == "" {
			continue
		}
		role := core.RoleUser
		if entry.IsBot {
			role = core.RoleAssistant
		}
		out = append(out, core.Message{
			Role:      role,
			Content:   content,
			Images:    entry.Images,
			CreatedAt: time.Now(),
		})
	}
	return out
}

func shouldIncludeDiscordTimestamp(ts time.Time, cutoff *time.Time) bool {
	if cutoff == nil {
		return true
	}
	return ts.After(*cutoff)
}

func formatHistoryTimestamp(ts time.Time) string {
	return ts.Local().Format("01/02 15:04")
}

func resolveHistorySenderName(user *discordgo.User) string {
	if user == nil {
		return "Unknown"
	}
	if strings.TrimSpace(user.GlobalName) != "" {
		return user.GlobalName
	}
	if strings.TrimSpace(user.Username) != "" {
		return user.Username
	}
	return "Unknown"
}

func buildAttachmentPlaceholder(attachments []*discordgo.MessageAttachment) string {
	if len(attachments) == 0 {
		return ""
	}
	if len(attachments) == 1 {
		return "[圖片]"
	}
	return fmt.Sprintf("[圖片 x%d]", len(attachments))
}

func runeLen(s string) int {
	return len([]rune(s))
}

func truncateRunesWithEllipsis(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

// ---------------------------------------------------------------------------
// Typing indicator
// ---------------------------------------------------------------------------

// startTyping sends typing indicators every 8 seconds (Discord typing lasts ~10s).
// Returns a stop function.
func (b *Bot) startTyping(s *discordgo.Session, channelID string) func() {
	_ = s.ChannelTyping(channelID)

	ctx, cancel := context.WithCancel(b.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.ChannelTyping(channelID)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// ---------------------------------------------------------------------------
// Tool status polling
// ---------------------------------------------------------------------------

// toolStatusInterval controls how often tool status is polled.
const toolStatusInterval = 800 * time.Millisecond

// startToolStatusPolling periodically checks active tool status and updates the placeholder message.
// Returns a stop function. Safe to call even if placeholderMsg is nil (no-op).
func (b *Bot) startToolStatusPolling(s *discordgo.Session, channelID string, placeholderMsg *discordgo.Message, placeholderErr error, sessionID string) func() {
	if placeholderErr != nil || placeholderMsg == nil {
		return func() {} // no placeholder to update
	}

	ctx, cancel := context.WithCancel(b.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		lastText := ""
		ticker := time.NewTicker(toolStatusInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var display string

				// Priority: retry/failback status > tool status > default.
				if retryStatus := b.svc.GetActiveRetryStatus(sessionID); retryStatus != "" {
					display = "🔄 處理中...（" + retryStatus + "）"
				} else if toolName := b.svc.GetActiveToolStatus(sessionID); toolName != "" {
					displayName := toolName
					if serverName, tn, isMCP := mcp.ParseNamespacedTool(toolName); isMCP {
						displayName = serverName + "/" + tn
					}
					display = "🔧 正在使用 " + displayName + "..."
				} else {
					display = "🔄 處理中..."
				}
				if display != lastText {
					b.editMessage(s, channelID, placeholderMsg.ID, display)
					lastText = display
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// editMessage edits an existing Discord message.
func (b *Bot) editMessage(s *discordgo.Session, channelID, messageID, content string) {
	if len(content) > discordMessageLimit {
		content = content[:discordMessageLimit]
	}
	if _, err := s.ChannelMessageEdit(channelID, messageID, content); err != nil {
		logDiscord.Errorf("edit error: channel=%s message=%s error=%v", channelID, messageID, err)
	}
}

// ---------------------------------------------------------------------------
// Reply formatting
// ---------------------------------------------------------------------------

// sendReply sends a reply to the original message, splitting if needed.
// Respects the ReplyToOriginal config setting.
func (b *Bot) sendReply(s *discordgo.Session, m *discordgo.MessageCreate, content string) {
	cfg := b.svc.GetDiscordConfig()
	shouldReply := cfg.ShouldReplyToOriginal()

	var ref *discordgo.MessageReference
	if shouldReply {
		ref = &discordgo.MessageReference{
			MessageID: m.ID,
			ChannelID: m.ChannelID,
			GuildID:   m.GuildID,
		}
	}

	chunks := splitMessage(content, discordMessageLimit)
	for i, chunk := range chunks {
		msg := &discordgo.MessageSend{
			Content: chunk,
		}
		// Only first chunk references the original message (if reply mode is on).
		if i == 0 && ref != nil {
			msg.Reference = ref
		}
		if _, err := s.ChannelMessageSendComplex(m.ChannelID, msg); err != nil {
			logDiscord.Errorf("send error: channel=%s error=%v", m.ChannelID, err)
			return
		}
	}
}

// logToConsole sends a log message to the configured console channel.
// Silently returns if no console channel is configured.
func (b *Bot) logToConsole(s *discordgo.Session, message string) {
	cfg := b.svc.GetDiscordConfig()
	channelID := strings.TrimSpace(cfg.ConsoleChannel)
	if channelID == "" {
		return
	}
	text := "```\n" + message + "\n```"
	if _, err := s.ChannelMessageSend(channelID, text); err != nil {
		logDiscord.Errorf("console send error: channel=%s error=%v", channelID, err)
	}
}

// logTraffic logs detailed request/response info to the console channel.
func (b *Bot) logTraffic(s *discordgo.Session, m *discordgo.MessageCreate, resp core.ChatResponse, imageCount int, elapsed time.Duration) {
	// Truncate message preview.
	preview := strings.TrimSpace(m.Content)
	if len(preview) > 60 {
		preview = preview[:60] + "…"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[對話] <#%s> %s\n", m.ChannelID, m.Author.Username))
	sb.WriteString(fmt.Sprintf("  訊息: %s\n", preview))
	if imageCount > 0 {
		sb.WriteString(fmt.Sprintf("  圖片: %d 張\n", imageCount))
	}
	sb.WriteString(fmt.Sprintf("  模型: %s/%s\n", resp.Provider, resp.Model))
	if resp.AccountID != "" {
		sb.WriteString(fmt.Sprintf("  帳號: %s\n", resp.AccountID))
	}
	sb.WriteString(fmt.Sprintf("  Token: %d in / %d out / %d total\n",
		resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens))
	if resp.Compressed {
		c := resp.Compression
		sb.WriteString(fmt.Sprintf("  壓縮: %d → %d tokens (丟棄 %d 則)\n",
			c.OriginalTokens, c.CompressedTokens, c.DroppedMessages))
	}
	if len(resp.ToolEvents) > 0 {
		executed := 0
		for _, evt := range resp.ToolEvents {
			if evt.Phase == "executed" || evt.Phase == "failed" {
				executed++
			}
		}
		if executed > 0 {
			sb.WriteString(fmt.Sprintf("  工具: %d 次呼叫\n", executed))
		}
	}
	sb.WriteString(fmt.Sprintf("  耗時: %s", elapsed.Round(time.Millisecond)))

	b.logToConsole(s, sb.String())
}

func responseElapsed(resp core.ChatResponse, fallback time.Duration) time.Duration {
	if resp.ElapsedMs > 0 {
		return time.Duration(resp.ElapsedMs) * time.Millisecond
	}
	return fallback
}

// formatUsageStats builds a compact usage summary:
// ⏱ 2.3s · ↑1.2K ↓567 · 245 tok/s · google-gemini-cli/gemini-2.0-flash
func formatUsageStats(usage core.UsageInfo, elapsed time.Duration, provider, model string) string {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && elapsed == 0 {
		return ""
	}

	var parts []string

	// Elapsed time.
	if elapsed > 0 {
		secs := elapsed.Seconds()
		switch {
		case secs >= 60:
			parts = append(parts, fmt.Sprintf("⏱ %s", elapsed.Truncate(time.Second)))
		case secs >= 10:
			parts = append(parts, fmt.Sprintf("⏱ %.1fs", secs))
		default:
			parts = append(parts, fmt.Sprintf("⏱ %.2fs", secs))
		}
	}

	// Token counts: ↑input ↓output (total)
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		total := usage.InputTokens + usage.OutputTokens
		parts = append(parts, fmt.Sprintf("↑%s ↓%s (%s)",
			formatTokenCount(usage.InputTokens), formatTokenCount(usage.OutputTokens),
			formatTokenCount(total)))
	}

	// Throughput.
	if usage.OutputTokens > 0 && elapsed > 0 {
		tokPerSec := float64(usage.OutputTokens) / elapsed.Seconds()
		parts = append(parts, fmt.Sprintf("%.0f tok/s", tokPerSec))
	}

	// Provider/model tag (useful when fallback occurs).
	if model != "" {
		tag := model
		if provider != "" {
			tag = provider + "/" + tag
		}
		parts = append(parts, tag)
	}

	return strings.Join(parts, " · ")
}

// formatTokenCount formats a token count with K/M suffixes.
func formatTokenCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// memoryToolNames lists tools whose output is shown in the summary.
var memoryToolNames = map[string]bool{
	"memory_save":   true,
	"memory_search": true,
	"memory_get":    true,
}

// formatToolSummary builds a short summary of executed tools.
// Memory tools include an output preview so the user can see what was
// saved or retrieved.
func formatToolSummary(events []core.ToolEvent) string {
	if len(events) == 0 {
		return ""
	}
	type entry struct {
		name    string
		count   int
		preview string // output preview for memory tools
	}
	seen := map[string]int{}
	var entries []entry
	for _, evt := range events {
		if evt.Phase != "executed" && evt.Phase != "failed" {
			continue
		}
		display := evt.ToolName
		if serverName, toolName, isMCP := mcp.ParseNamespacedTool(evt.ToolName); isMCP {
			display = serverName + "/" + toolName
		}
		if idx, ok := seen[evt.ToolName]; ok {
			entries[idx].count++
			continue
		}
		seen[evt.ToolName] = len(entries)
		preview := ""
		if memoryToolNames[evt.ToolName] && evt.OutputPreview != "" {
			preview = truncatePreview(evt.OutputPreview, 100)
		}
		entries = append(entries, entry{name: display, count: 1, preview: preview})
	}
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("-# 🔧 使用的工具：")
	for i, e := range entries {
		sb.WriteString(fmt.Sprintf("\n-# %d. %s", i+1, e.name))
		if e.count > 1 {
			sb.WriteString(fmt.Sprintf(" (×%d)", e.count))
		}
		if e.preview != "" {
			sb.WriteString(fmt.Sprintf("\n-#    → %s", e.preview))
		}
	}
	return sb.String()
}

// truncatePreview shortens s to max runes, replacing newlines with spaces.
func truncatePreview(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// ---------------------------------------------------------------------------
// Image extraction
// ---------------------------------------------------------------------------

// imageContentTypes maps Discord attachment content types to supported MIME types.
var imageContentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

const maxImageDownloadSize = 20 * 1024 * 1024 // 20 MB

// extractDiscordImages downloads image attachments and returns base64-encoded ImageData.
func extractDiscordImages(attachments []*discordgo.MessageAttachment) []core.ImageData {
	return extractDiscordImagesLimited(attachments, 0)
}

// extractDiscordImagesLimited behaves like extractDiscordImages, but optionally
// caps the number of images when limit > 0.
func extractDiscordImagesLimited(attachments []*discordgo.MessageAttachment, limit int) []core.ImageData {
	var images []core.ImageData
	for _, att := range attachments {
		if att == nil || att.URL == "" {
			continue
		}
		if limit > 0 && len(images) >= limit {
			break
		}

		// Check content type or infer from filename extension.
		mime := att.ContentType
		if !imageContentTypes[mime] {
			ext := strings.ToLower(path.Ext(att.Filename))
			switch ext {
			case ".png":
				mime = "image/png"
			case ".jpg", ".jpeg":
				mime = "image/jpeg"
			case ".gif":
				mime = "image/gif"
			case ".webp":
				mime = "image/webp"
			default:
				continue // Not an image attachment.
			}
		}

		// Skip oversized attachments.
		if att.Size > maxImageDownloadSize {
			logDiscord.Warnf("image skip: file=%s reason=too_large size=%d", att.Filename, att.Size)
			continue
		}

		data, err := downloadImage(att.URL)
		if err != nil {
			logDiscord.Errorf("image download error: file=%s error=%v", att.Filename, err)
			continue
		}

		// Detect MIME type from actual image bytes to avoid mismatches
		// (e.g. Discord reports image/jpeg but file is actually PNG).
		if detected := core.DetectImageMimeType(data); detected != "" {
			mime = detected
		}

		images = append(images, core.ImageData{
			MimeType: mime,
			Data:     base64.StdEncoding.EncodeToString(data),
			FileName: att.Filename,
		})
	}
	return images
}

// downloadImage fetches raw bytes from a URL with size limit.
func downloadImage(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image: status %d", resp.StatusCode)
	}

	// Limit read to prevent memory exhaustion.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if len(data) > maxImageDownloadSize {
		return nil, fmt.Errorf("image exceeds %d MB limit", maxImageDownloadSize/(1024*1024))
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stripMention removes the bot's <@ID> mention from the message content.
func stripMention(content, botID string) string {
	content = strings.ReplaceAll(content, "<@"+botID+">", "")
	content = strings.ReplaceAll(content, "<@!"+botID+">", "")
	return content
}

// splitMessage splits a long message into chunks that fit Discord's limit.
// Tries to split at paragraph boundaries (\n\n), then newlines, then hard-cut.
func splitMessage(content string, limit int) []string {
	if len(content) <= limit {
		return []string{content}
	}

	var chunks []string
	remaining := content

	for len(remaining) > 0 {
		if len(remaining) <= limit {
			chunks = append(chunks, remaining)
			break
		}

		// Find the best split point within limit.
		cutAt := findSplitPoint(remaining, limit)
		chunks = append(chunks, remaining[:cutAt])
		remaining = strings.TrimLeft(remaining[cutAt:], "\n")
	}
	return chunks
}

// findSplitPoint finds the best place to split within the limit.
func findSplitPoint(s string, limit int) int {
	segment := s[:limit]

	// Prefer splitting at paragraph boundary.
	if idx := strings.LastIndex(segment, "\n\n"); idx > 0 {
		return idx + 1
	}
	// Fall back to newline.
	if idx := strings.LastIndex(segment, "\n"); idx > 0 {
		return idx + 1
	}
	// Fall back to space.
	if idx := strings.LastIndex(segment, " "); idx > 0 {
		return idx + 1
	}
	// Hard cut.
	return limit
}
