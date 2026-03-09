package telegram

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/binding"
	"github.com/doeshing/nekoclaw/internal/botcmd"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/logger"
	"github.com/doeshing/nekoclaw/internal/mcp"
	"github.com/doeshing/nekoclaw/internal/persona"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var logTelegram = logger.New("telegram", logger.Blue)

// telegramMessageLimit is the maximum length of a single Telegram message.
const telegramMessageLimit = 4096

const telegramCallbackPrefix = "cmd:"

// Group context buffer settings.
const (
	maxGroupHistoryMessages = 50
	groupHistoryMaxAge      = 30 * time.Minute
)

// groupMessage stores a single ambient group message for context injection.
type groupMessage struct {
	SenderName string
	Text       string
	HasImage   bool
	Timestamp  time.Time
}

// messageJob represents a queued message to be processed.
type messageJob struct {
	update tgbotapi.Update
}

// Bot connects to the Telegram Bot API and forwards messages to the NekoClaw service.
type Bot struct {
	api *tgbotapi.BotAPI
	svc *app.Service

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Per-chat message queues for sequential processing.
	queues   map[int64]chan messageJob
	queuesMu sync.Mutex

	// Per-chat active session tracking.
	// Key: chatID, Value: current sessionID.
	activeSessions   map[int64]string
	activeSessionsMu sync.RWMutex

	// Per-chat recent message buffer for group context injection.
	groupHistory   map[int64][]groupMessage
	groupHistoryMu sync.RWMutex

	// Persistent chat → session binding store (survives restarts).
	bindingStore *binding.Store
}

// Config holds the configuration needed to create a Telegram bot.
type Config struct {
	Token    string // Telegram bot token (required)
	StateDir string // Directory for persistent state (bindings, etc.)
}

// New creates a new Telegram bot. Call Start() to connect.
func New(svc *app.Service, cfg Config) (*Bot, error) {
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("telegram bot token is required")
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot api: %w", err)
	}

	bot := &Bot{
		api:            api,
		svc:            svc,
		queues:         make(map[int64]chan messageJob),
		activeSessions: make(map[int64]string),
		groupHistory:   make(map[int64][]groupMessage),
	}

	// Load persistent bindings from disk (if StateDir is configured).
	if dir := strings.TrimSpace(cfg.StateDir); dir != "" {
		storePath := filepath.Join(dir, "telegram-bindings.json")
		bs, loadErr := binding.Load(storePath)
		if loadErr != nil {
			logTelegram.Errorf("load bindings: %v", loadErr)
		} else {
			bot.bindingStore = bs
			// Hydrate in-memory map from persisted bindings.
			for key, entry := range bs.All() {
				chatID, parseErr := parseChatID(key)
				if parseErr != nil {
					logTelegram.Warnf("skip invalid binding key %q: %v", key, parseErr)
					continue
				}
				bot.activeSessions[chatID] = entry.SessionID
			}
			if n := len(bot.activeSessions); n > 0 {
				logTelegram.Logf("restored %d chat bindings from disk", n)
			}
		}
	}

	return bot, nil
}

// Start connects via long polling and begins handling messages.
// Blocks until the context is cancelled.
func (b *Bot) Start(ctx context.Context) error {
	b.ctx, b.cancel = context.WithCancel(ctx)

	// Register slash commands so Telegram shows the command menu.
	commands := tgbotapi.NewSetMyCommands(telegramBotCommands()...)
	if _, err := b.api.Request(commands); err != nil {
		logTelegram.Warnf("set commands failed: %v", err)
	}

	logTelegram.Logf("bot started: user=%s", b.api.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-b.ctx.Done():
			b.api.StopReceivingUpdates()

			// Close all queues so workers drain remaining jobs and exit.
			b.queuesMu.Lock()
			for _, ch := range b.queues {
				close(ch)
			}
			b.queues = nil
			b.queuesMu.Unlock()

			b.wg.Wait()
			return nil

		case update, ok := <-updates:
			if !ok {
				return nil
			}

			// Handle inline keyboard button presses.
			if update.CallbackQuery != nil {
				b.handleCallbackQuery(update.CallbackQuery)
				continue
			}

			if update.Message == nil {
				continue
			}

			respond := b.shouldRespond(update)

			// Record ambient group messages for context injection.
			// Only non-trigger messages are stored; triggered messages
			// are the user's actual input and already go to HandleChat.
			if !respond && update.Message.Chat.Type != "private" {
				b.recordGroupMessage(update.Message)
			}

			if !respond {
				continue
			}
			b.enqueue(update)
		}
	}
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

// getSessionID returns the active session ID for the given chat.
// Falls back to the default format if no active session is set.
func (b *Bot) getSessionID(chatID int64) string {
	b.activeSessionsMu.RLock()
	sid, ok := b.activeSessions[chatID]
	b.activeSessionsMu.RUnlock()
	if ok {
		return sid
	}
	return fmt.Sprintf("telegram:%d", chatID)
}

// resetSession creates a new session for the chat, persists the binding,
// and returns the new session ID.
func (b *Bot) resetSession(chatID int64) string {
	newID := fmt.Sprintf("telegram:%d-%s", chatID, time.Now().Format("0102-150405"))
	b.activeSessionsMu.Lock()
	b.activeSessions[chatID] = newID
	b.activeSessionsMu.Unlock()

	// Persist binding so the session survives bot restarts.
	if b.bindingStore != nil {
		b.bindingStore.Set(formatChatKey(chatID), binding.Entry{
			SessionID: newID,
		})
	}

	return newID
}

// formatChatKey converts a chatID to the string key used in the binding store.
func formatChatKey(chatID int64) string {
	return fmt.Sprintf("%d", chatID)
}

// parseChatID converts a binding store key back to a chatID.
func parseChatID(key string) (int64, error) {
	return strconv.ParseInt(key, 10, 64)
}

// ---------------------------------------------------------------------------
// Message handling
// ---------------------------------------------------------------------------

// shouldRespond checks whether the bot should respond to this message.
// Responds to: DM, @mention, reply to bot, or command.
func (b *Bot) shouldRespond(update tgbotapi.Update) bool {
	msg := update.Message
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return false
	}

	// Always respond to commands (even in groups without mention).
	if msg.IsCommand() {
		return true
	}

	// Always respond in private chats (DM).
	if msg.Chat.Type == "private" {
		return true
	}

	// Respond if bot is @mentioned in group text or caption.
	botMention := "@" + b.api.Self.UserName
	if strings.Contains(msg.Text, botMention) || strings.Contains(msg.Caption, botMention) {
		return true
	}

	// Respond if message is a reply to the bot's own message.
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil {
		if msg.ReplyToMessage.From.ID == b.api.Self.ID {
			return true
		}
	}

	return false
}

func (b *Bot) enqueue(update tgbotapi.Update) {
	chatID := update.Message.Chat.ID

	b.queuesMu.Lock()
	if b.queues == nil {
		b.queuesMu.Unlock()
		return
	}
	ch, ok := b.queues[chatID]
	if !ok {
		ch = make(chan messageJob, 64)
		b.queues[chatID] = ch
		b.wg.Add(1)
		go b.chatWorker(ch)
	}
	b.queuesMu.Unlock()

	select {
	case ch <- messageJob{update: update}:
	case <-b.ctx.Done():
	}
}

// chatWorker processes messages for a single chat sequentially.
func (b *Bot) chatWorker(ch <-chan messageJob) {
	defer b.wg.Done()
	for job := range ch {
		b.safeHandleMessage(job.update)
	}
}

// safeHandleMessage wraps handleMessage with panic recovery so a single
// bad message cannot kill the per-chat worker goroutine.
func (b *Bot) safeHandleMessage(update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			chatID := int64(0)
			if update.Message != nil {
				chatID = update.Message.Chat.ID
			}
			logTelegram.Errorf("panic recovered: chat=%d error=%v", chatID, r)
		}
	}()
	b.handleMessage(update)
}

func (b *Bot) handleMessage(update tgbotapi.Update) {
	startTime := time.Now()
	msg := update.Message

	// Handle commands before sending to AI.
	if msg.IsCommand() {
		b.handleCommand(msg)
		return
	}

	text := b.extractText(msg)
	images := b.extractImages(msg)

	// --- Context building ---
	var contextParts []string

	// 1. Recent group conversation (ambient messages not directed at bot).
	if msg.Chat.Type != "private" {
		if groupCtx := b.buildGroupContext(msg.Chat.ID); groupCtx != "" {
			contextParts = append(contextParts, groupCtx)
		}
	}

	// 2. Reply-to message context (when replying to another user, not bot).
	//    This is critical for cases like replying to a photo with "@bot 這張照片是什麼？".
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil &&
		msg.ReplyToMessage.From.ID != b.api.Self.ID {
		replyText := msg.ReplyToMessage.Text
		if replyText == "" {
			replyText = msg.ReplyToMessage.Caption
		}
		// Include images from the replied-to message (e.g. user replies to a photo).
		replyImages := b.extractImages(msg.ReplyToMessage)
		images = append(replyImages, images...) // replied images first, then user's own

		replyFrom := msg.ReplyToMessage.From
		senderName := formatSenderName(replyFrom)
		if replyFrom.UserName != "" {
			senderName += " (@" + replyFrom.UserName + ")"
		}
		if replyText != "" {
			contextParts = append(contextParts, fmt.Sprintf("[回覆 %s 的訊息]\n%s", senderName, replyText))
		} else if len(replyImages) > 0 {
			contextParts = append(contextParts, fmt.Sprintf("[回覆 %s 的圖片]", senderName))
		}
	}

	// 3. Sender identity so the LLM knows who is speaking.
	// Include @username when available so the LLM can tag people in replies.
	senderTag := formatSenderTag(msg.From)
	contextParts = append(contextParts, senderTag)

	// Prepend context to the user message.
	if text != "" {
		text = strings.Join(contextParts, "\n") + "\n" + text
	} else {
		text = strings.Join(contextParts, "\n")
	}

	// Skip if no text and no images.
	if text == "" && len(images) == 0 {
		return
	}

	// Default text when only images are sent.
	if text == "" && len(images) > 0 {
		text = "請描述這張圖片"
	}

	chatID := msg.Chat.ID

	// Send a placeholder message that we will edit with streaming updates.
	placeholder := tgbotapi.NewMessage(chatID, "🔄 處理中...")
	placeholder.ReplyToMessageID = msg.MessageID
	placeholderMsg, placeholderErr := b.api.Send(placeholder)

	// Show typing indicator while processing.
	stopTyping := b.startTyping(chatID)

	sessionID := b.getSessionID(chatID)

	// Stream response chunks instead of blocking for the full reply.
	var (
		fullText     strings.Builder
		lastEditTime time.Time
		editInterval = 2 * time.Second // Telegram rate limits are strict
		textStarted  bool
		streamResp   *core.ChatResponse
		streamErr    string
	)

	streamCh := b.svc.HandleChatStream(b.ctx, core.ChatRequest{
		SessionID:   sessionID,
		Surface:     core.SurfaceTelegram,
		Provider:    b.svc.GetDefaultProvider(),
		Model:       b.svc.GetDefaultModel(),
		Message:     text,
		Images:      images,
		EnableTools: true,
	})

	for chunk := range streamCh {
		switch chunk.Type {
		case core.ChunkToolStatus:
			if placeholderErr == nil {
				displayName := chunk.ToolName
				if serverName, tn, isMCP := mcp.ParseNamespacedTool(chunk.ToolName); isMCP {
					displayName = serverName + "/" + tn
				}
				edit := tgbotapi.NewEditMessageText(chatID, placeholderMsg.MessageID, "🔧 正在使用 "+displayName+"...")
				_, _ = b.api.Send(edit)
			}
		case core.ChunkRetryStatus:
			if placeholderErr == nil {
				edit := tgbotapi.NewEditMessageText(chatID, placeholderMsg.MessageID, "🔄 處理中...（"+chunk.RetryStatus+"）")
				_, _ = b.api.Send(edit)
			}
		case core.ChunkText:
			fullText.WriteString(chunk.Content)
			textStarted = true
			if time.Since(lastEditTime) >= editInterval {
				preview := fullText.String()
				if len(preview) > 4000 {
					preview = preview[:4000] + "..."
				}
				if placeholderErr == nil {
					edit := tgbotapi.NewEditMessageText(chatID, placeholderMsg.MessageID, preview)
					_, _ = b.api.Send(edit)
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

	// Handle error from stream.
	if streamErr != "" {
		logTelegram.Errorf("chat error: chat=%d user=%d error=%s", chatID, msg.From.ID, streamErr)
		if placeholderErr == nil {
			b.editMessage(chatID, placeholderMsg.MessageID, "⚠️ "+streamErr)
		} else {
			b.sendReply(chatID, msg.MessageID, "⚠️ "+streamErr)
		}
		return
	}

	// Handle missing response (stream ended without done chunk).
	if streamResp == nil {
		errMsg := "⚠️ 未收到回應"
		if placeholderErr == nil {
			b.editMessage(chatID, placeholderMsg.MessageID, errMsg)
		} else {
			b.sendReply(chatID, msg.MessageID, errMsg)
		}
		return
	}

	elapsed := responseElapsed(*streamResp, time.Since(startTime))

	reply := strings.TrimSpace(streamResp.Reply)
	// If streaming accumulated text but the response reply is empty, use accumulated text.
	if reply == "" && textStarted {
		reply = strings.TrimSpace(fullText.String())
	}
	if reply == "" {
		reply = "（無回應）"
	}

	// Append usage stats and tool summary as a quoted footer so metadata
	// stays readable without competing with the main reply.
	var footer []string
	if stats := formatUsageStats(streamResp.Usage, elapsed, streamResp.Provider, streamResp.Model); stats != "" {
		footer = append(footer, stats)
	}
	if summary := formatToolSummary(streamResp.ToolEvents); summary != "" {
		footer = append(footer, summary)
	}
	formattedReply := formatReplyWithFooter(reply, footer)

	// Always delete the placeholder and resend as fresh messages.
	// This prevents the "(edited)" label and handles overflow / message ordering
	// correctly when someone else sends a message during streaming.
	if placeholderErr == nil {
		del := tgbotapi.NewDeleteMessage(chatID, placeholderMsg.MessageID)
		_, _ = b.api.Send(del)
	}
	b.sendMarkdownV2Reply(chatID, msg.MessageID, formattedReply)
}

// extractText returns the user's text from a message, stripping the bot mention.
func (b *Bot) extractText(msg *tgbotapi.Message) string {
	text := msg.Text
	if text == "" {
		text = msg.Caption // Photos/documents with captions.
	}
	botMention := "@" + b.api.Self.UserName
	text = strings.ReplaceAll(text, botMention, "")
	return strings.TrimSpace(text)
}

// extractImages downloads photos or image documents from a Telegram message.
func (b *Bot) extractImages(msg *tgbotapi.Message) []core.ImageData {
	var images []core.ImageData

	// Handle photo messages (Telegram sends multiple sizes; pick the largest).
	if len(msg.Photo) > 0 {
		largest := msg.Photo[len(msg.Photo)-1]
		img, err := b.downloadTelegramFile(largest.FileID, "photo.jpg", "image/jpeg")
		if err != nil {
			logTelegram.Errorf("image download error: file_id=%s error=%v", largest.FileID, err)
		} else {
			images = append(images, img)
		}
	}

	// Handle image documents (files sent as attachments, not compressed).
	if msg.Document != nil && isImageMIME(msg.Document.MimeType) {
		img, err := b.downloadTelegramFile(msg.Document.FileID, msg.Document.FileName, msg.Document.MimeType)
		if err != nil {
			logTelegram.Errorf("image download error: file_id=%s error=%v", msg.Document.FileID, err)
		} else {
			images = append(images, img)
		}
	}

	return images
}

// downloadTelegramFile fetches a file from Telegram servers and returns base64-encoded ImageData.
func (b *Bot) downloadTelegramFile(fileID, fileName, mimeType string) (core.ImageData, error) {
	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return core.ImageData{}, fmt.Errorf("get file info: %w", err)
	}

	fileURL := file.Link(b.api.Token)

	resp, err := http.Get(fileURL)
	if err != nil {
		return core.ImageData{}, fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return core.ImageData{}, fmt.Errorf("download file: status %d", resp.StatusCode)
	}

	const maxSize = 20 * 1024 * 1024 // 20 MB
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return core.ImageData{}, fmt.Errorf("read file: %w", err)
	}
	if len(data) > maxSize {
		return core.ImageData{}, fmt.Errorf("file exceeds 20 MB limit")
	}

	return core.ImageData{
		MimeType: mimeType,
		Data:     base64.StdEncoding.EncodeToString(data),
		FileName: fileName,
	}, nil
}

// isImageMIME checks whether a MIME type is a supported image format.
func isImageMIME(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Group context
// ---------------------------------------------------------------------------

// recordGroupMessage stores an ambient group message for later context injection.
func (b *Bot) recordGroupMessage(msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return
	}

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}
	hasImage := len(msg.Photo) > 0 || (msg.Document != nil && isImageMIME(msg.Document.MimeType))

	// Skip empty messages (stickers, voice notes, etc.).
	if text == "" && !hasImage {
		return
	}

	gm := groupMessage{
		SenderName: formatSenderName(msg.From),
		Text:       text,
		HasImage:   hasImage,
		Timestamp:  time.Unix(int64(msg.Date), 0),
	}

	chatID := msg.Chat.ID
	b.groupHistoryMu.Lock()
	defer b.groupHistoryMu.Unlock()

	history := b.groupHistory[chatID]
	history = append(history, gm)
	if len(history) > maxGroupHistoryMessages {
		history = history[len(history)-maxGroupHistoryMessages:]
	}
	b.groupHistory[chatID] = history
}

// buildGroupContext formats recent group messages as a context block.
func (b *Bot) buildGroupContext(chatID int64) string {
	b.groupHistoryMu.RLock()
	messages, ok := b.groupHistory[chatID]
	b.groupHistoryMu.RUnlock()
	if !ok || len(messages) == 0 {
		return ""
	}

	cutoff := time.Now().Add(-groupHistoryMaxAge)
	var lines []string
	for _, gm := range messages {
		if gm.Timestamp.Before(cutoff) {
			continue
		}
		ts := gm.Timestamp.Format("15:04")
		switch {
		case gm.Text != "" && gm.HasImage:
			lines = append(lines, fmt.Sprintf("%s (%s): %s [附圖]", gm.SenderName, ts, gm.Text))
		case gm.HasImage:
			lines = append(lines, fmt.Sprintf("%s (%s): [圖片]", gm.SenderName, ts))
		default:
			lines = append(lines, fmt.Sprintf("%s (%s): %s", gm.SenderName, ts, gm.Text))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "[近期群組對話]\n" + strings.Join(lines, "\n")
}

// formatSenderName builds a display name from a Telegram user.
func formatSenderName(user *tgbotapi.User) string {
	if user == nil {
		return "Unknown"
	}
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	return name
}

// formatSenderTag builds a sender identity tag including @username when available,
// so the LLM can use @username to mention people in Telegram replies.
func formatSenderTag(user *tgbotapi.User) string {
	name := formatSenderName(user)
	if user != nil && user.UserName != "" {
		return fmt.Sprintf("[發送者: %s (@%s)]", name, user.UserName)
	}
	return fmt.Sprintf("[發送者: %s]", name)
}

// ---------------------------------------------------------------------------
// Command handling
// ---------------------------------------------------------------------------

type telegramCommandRuntime struct {
	bot    *Bot
	chatID int64
}

func (r *telegramCommandRuntime) ResetSession() string {
	return r.bot.resetSession(r.chatID)
}

func (r *telegramCommandRuntime) ListPersonas() []persona.PersonaInfo {
	return r.bot.svc.ListPersonas()
}

func (r *telegramCommandRuntime) ActivePersona() *persona.PersonaInfo {
	return r.bot.svc.ActivePersona()
}

func (r *telegramCommandRuntime) FindPersonaByName(name string) (string, bool) {
	return r.bot.svc.FindPersonaByName(name)
}

func (r *telegramCommandRuntime) SetActivePersona(dirName string) error {
	return r.bot.svc.SetActivePersona(dirName)
}

func (r *telegramCommandRuntime) ClearActivePersona() error {
	return r.bot.svc.ClearActivePersona()
}

func telegramBotCommands() []tgbotapi.BotCommand {
	specs := botcmd.Specs()
	commands := make([]tgbotapi.BotCommand, 0, len(specs))
	for _, spec := range specs {
		commands = append(commands, tgbotapi.BotCommand{
			Command:     spec.Name,
			Description: spec.Description,
		})
	}
	return commands
}

func buildTelegramPersonaKeyboard(selector *botcmd.PersonaSelector) [][]tgbotapi.InlineKeyboardButton {
	if selector == nil {
		return nil
	}

	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(selector.Options)+1)
	for _, option := range selector.Options {
		label := option.Name
		if option.Active {
			label = "▶ " + label
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, encodeTelegramCallbackData(botcmd.ActionPersonaSelect, option.DirName)),
		))
	}
	if selector.OffLabel != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(selector.OffLabel, encodeTelegramCallbackData(botcmd.ActionPersonaSelect, botcmd.PersonaOffValue)),
		))
	}
	return rows
}

func encodeTelegramCallbackData(action, value string) string {
	return telegramCallbackPrefix + action + ":" + value
}

func parseTelegramCallbackInvocation(data string) (botcmd.Invocation, bool) {
	if !strings.HasPrefix(data, telegramCallbackPrefix) {
		return botcmd.Invocation{}, false
	}

	payload := strings.TrimPrefix(data, telegramCallbackPrefix)
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return botcmd.Invocation{}, false
	}

	switch parts[0] {
	case botcmd.ActionPersonaSelect:
		return botcmd.Invocation{
			Name:            botcmd.CommandPersona,
			ComponentAction: botcmd.ActionPersonaSelect,
			ComponentValue:  parts[1],
		}, true
	default:
		return botcmd.Invocation{}, false
	}
}

// handleCommand processes bot commands (/reset, /persona, etc.).
func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	inv := botcmd.Invocation{
		Name: msg.Command(),
	}
	if arg := strings.TrimSpace(msg.CommandArguments()); arg != "" {
		inv.StringOptions = map[string]string{
			botcmd.OptionPersonaName: arg,
		}
	}
	result, event, handled := b.dispatchCommand(msg.Chat.ID, inv)
	if !handled {
		return
	}
	if event != nil && event.Type == botcmd.EventReset {
		logTelegram.Logf("session reset: chat=%d new_session=%s", msg.Chat.ID, event.SessionID)
	}
	if result.Selector != nil {
		b.sendTelegramSelector(msg.Chat.ID, msg.MessageID, result)
		return
	}
	b.sendReply(msg.Chat.ID, msg.MessageID, result.Message)
}

// handleCallbackQuery processes inline keyboard button presses.
func (b *Bot) handleCallbackQuery(cq *tgbotapi.CallbackQuery) {
	callback := tgbotapi.NewCallback(cq.ID, "")
	inv, ok := parseTelegramCallbackInvocation(cq.Data)
	if !ok {
		callback.Text = "⚠️ 未知操作"
		_, _ = b.api.Request(callback)
		return
	}

	chatID := int64(0)
	if cq.Message != nil {
		chatID = cq.Message.Chat.ID
	}
	result, _, handled := b.dispatchCommand(chatID, inv)
	if !handled {
		callback.Text = "⚠️ 未知操作"
		_, _ = b.api.Request(callback)
		return
	}

	callback.Text = result.Message
	_, _ = b.api.Request(callback)

	if result.UpdateExisting && result.Selector != nil {
		b.editTelegramSelector(cq, result)
	}
}

func (b *Bot) dispatchCommand(chatID int64, inv botcmd.Invocation) (botcmd.Result, *botcmd.Event, bool) {
	runtime := &telegramCommandRuntime{
		bot:    b,
		chatID: chatID,
	}
	result, handled := botcmd.Dispatch(runtime, inv)
	if !handled {
		return botcmd.Result{}, nil, false
	}
	return result, result.Event, true
}

func (b *Bot) sendTelegramSelector(chatID int64, replyToID int, result botcmd.Result) {
	msg := tgbotapi.NewMessage(chatID, botcmd.RenderPersonaSelectorText(result.Selector, result.SelectorNotice))
	msg.ReplyToMessageID = replyToID
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buildTelegramPersonaKeyboard(result.Selector)...)
	if _, err := b.api.Send(msg); err != nil {
		logTelegram.Errorf("send persona selector: chat=%d error=%v", chatID, err)
	}
}

func (b *Bot) editTelegramSelector(cq *tgbotapi.CallbackQuery, result botcmd.Result) {
	if cq.Message == nil || result.Selector == nil {
		return
	}

	text := botcmd.RenderPersonaSelectorText(result.Selector, result.SelectorNotice)
	markup := tgbotapi.NewInlineKeyboardMarkup(buildTelegramPersonaKeyboard(result.Selector)...)
	edit := tgbotapi.NewEditMessageTextAndMarkup(
		cq.Message.Chat.ID,
		cq.Message.MessageID,
		text,
		markup,
	)
	if _, err := b.api.Request(edit); err != nil {
		logTelegram.Warnf("edit persona selector: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Typing indicator
// ---------------------------------------------------------------------------

// startTyping sends "typing" chat action every 4 seconds (Telegram typing expires ~5s).
// Returns a stop function.
func (b *Bot) startTyping(chatID int64) func() {
	action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	_, _ = b.api.Send(action)

	ctx, cancel := context.WithCancel(b.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
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
// Returns a stop function. Safe to call even if placeholder send failed.
func (b *Bot) startToolStatusPolling(chatID int64, placeholderMsgID int, placeholderErr error, sessionID string) func() {
	if placeholderErr != nil || placeholderMsgID == 0 {
		return func() {}
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
					b.editMessage(chatID, placeholderMsgID, display)
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

// ---------------------------------------------------------------------------
// Reply helpers
// ---------------------------------------------------------------------------

func (b *Bot) editMessage(chatID int64, messageID int, text string) {
	formatted := RenderMarkdownV2(text)
	// Telegram edit limit is the same as send limit.
	if len(formatted) > telegramMessageLimit {
		formatted = formatted[:telegramMessageLimit]
	}
	edit := tgbotapi.NewEditMessageText(chatID, messageID, formatted)
	edit.ParseMode = tgbotapi.ModeMarkdownV2
	if _, err := b.api.Send(edit); err != nil {
		logTelegram.Errorf("edit error: chat=%d message=%d error=%v", chatID, messageID, err)
	}
}

func (b *Bot) sendReply(chatID int64, replyToID int, content string) {
	b.sendMarkdownV2Reply(chatID, replyToID, RenderMarkdownV2(content))
}

func (b *Bot) sendMarkdownV2Reply(chatID int64, replyToID int, formatted string) {
	chunks := splitMessage(formatted, telegramMessageLimit)
	for i, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = tgbotapi.ModeMarkdownV2
		if i == 0 {
			msg.ReplyToMessageID = replyToID
		}
		if _, err := b.api.Send(msg); err != nil {
			logTelegram.Errorf("send error: chat=%d error=%v", chatID, err)
			return
		}
	}
}

func formatReplyWithFooter(body string, footer []string) string {
	renderedBody := RenderMarkdownV2(body)
	quotedFooter := formatFooterBlockquote(footer)
	switch {
	case renderedBody == "":
		return quotedFooter
	case quotedFooter == "":
		return renderedBody
	default:
		return renderedBody + "\n\n" + quotedFooter
	}
}

func formatFooterBlockquote(parts []string) string {
	if len(parts) == 0 {
		return ""
	}

	var quoted []string
	for _, part := range parts {
		for _, line := range strings.Split(strings.TrimSpace(part), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			quoted = append(quoted, "> "+escapeText(line))
		}
	}
	return strings.Join(quoted, "\n")
}

// ---------------------------------------------------------------------------
// Usage stats
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Tool summary
// ---------------------------------------------------------------------------

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
		preview string
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
	sb.WriteString("🔧 使用的工具：")
	for i, e := range entries {
		sb.WriteString(fmt.Sprintf("\n%d. %s", i+1, e.name))
		if e.count > 1 {
			sb.WriteString(fmt.Sprintf(" (×%d)", e.count))
		}
		if e.preview != "" {
			sb.WriteString(fmt.Sprintf("\n   → %s", e.preview))
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
// Message splitting
// ---------------------------------------------------------------------------

// splitMessage splits a long message into chunks that fit Telegram's limit.
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
		cutAt := findSplitPoint(remaining, limit)
		chunks = append(chunks, remaining[:cutAt])
		remaining = strings.TrimLeft(remaining[cutAt:], "\n")
	}
	return chunks
}

func findSplitPoint(s string, limit int) int {
	segment := s[:limit]
	if idx := strings.LastIndex(segment, "\n\n"); idx > 0 {
		return idx + 1
	}
	if idx := strings.LastIndex(segment, "\n"); idx > 0 {
		return idx + 1
	}
	if idx := strings.LastIndex(segment, " "); idx > 0 {
		return idx + 1
	}
	return limit
}
