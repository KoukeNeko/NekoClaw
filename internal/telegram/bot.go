package telegram

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/mcp"
)

// telegramMessageLimit is the maximum length of a single Telegram message.
const telegramMessageLimit = 4096

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
}

// Config holds the configuration needed to create a Telegram bot.
type Config struct {
	Token string // Telegram bot token (required)
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

	return &Bot{
		api:            api,
		svc:            svc,
		queues:         make(map[int64]chan messageJob),
		activeSessions: make(map[int64]string),
	}, nil
}

// Start connects via long polling and begins handling messages.
// Blocks until the context is cancelled.
func (b *Bot) Start(ctx context.Context) error {
	b.ctx, b.cancel = context.WithCancel(ctx)

	log.Printf("event=telegram_bot_started user=%s", b.api.Self.UserName)

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
			if update.Message == nil {
				continue
			}
			if !b.shouldRespond(update) {
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

// resetSession creates a new session for the chat and returns its ID.
func (b *Bot) resetSession(chatID int64) string {
	newID := fmt.Sprintf("telegram:%d-%s", chatID, time.Now().Format("0102-150405"))
	b.activeSessionsMu.Lock()
	b.activeSessions[chatID] = newID
	b.activeSessionsMu.Unlock()
	return newID
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
		b.handleMessage(job.update)
	}
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

	// Extract images from photos or document attachments.
	images := b.extractImages(msg)

	// Skip if no text and no images.
	if text == "" && len(images) == 0 {
		return
	}

	// Default text when only images are sent.
	if text == "" && len(images) > 0 {
		text = "請描述這張圖片"
	}

	chatID := msg.Chat.ID

	// Send a placeholder message that we will edit with the final reply.
	placeholder := tgbotapi.NewMessage(chatID, "🔄 處理中...")
	placeholder.ReplyToMessageID = msg.MessageID
	placeholderMsg, placeholderErr := b.api.Send(placeholder)

	// Show typing indicator while processing.
	stopTyping := b.startTyping(chatID)

	sessionID := b.getSessionID(chatID)

	// Poll tool status and update placeholder while waiting for response.
	stopToolStatus := b.startToolStatusPolling(chatID, placeholderMsg.MessageID, placeholderErr, sessionID)

	resp, err := b.svc.HandleChat(b.ctx, core.ChatRequest{
		SessionID:   sessionID,
		Surface:     core.SurfaceTelegram,
		Provider:    b.svc.GetDefaultProvider(),
		Model:       b.svc.GetDefaultModel(),
		Message:     text,
		Images:      images,
		EnableTools: true,
	})

	stopToolStatus()
	stopTyping()

	if err != nil {
		log.Printf("event=telegram_chat_error chat=%d user=%d error=%q", chatID, msg.From.ID, err)
		errReply := "⚠️ " + err.Error()
		if placeholderErr == nil {
			b.editMessage(chatID, placeholderMsg.MessageID, errReply)
		} else {
			b.sendReply(chatID, msg.MessageID, errReply)
		}
		return
	}

	elapsed := time.Since(startTime)

	reply := strings.TrimSpace(resp.Reply)
	if reply == "" {
		reply = "（無回應）"
	}

	// Append usage stats and tool summary.
	var footer []string
	if stats := formatUsageStats(resp.Usage, elapsed); stats != "" {
		footer = append(footer, stats)
	}
	if summary := formatToolSummary(resp.ToolEvents); summary != "" {
		footer = append(footer, summary)
	}
	if len(footer) > 0 {
		reply += "\n\n" + strings.Join(footer, "\n")
	}

	// Edit the placeholder with the first chunk; send remaining chunks as new messages.
	if placeholderErr == nil {
		chunks := splitMessage(reply, telegramMessageLimit)
		b.editMessage(chatID, placeholderMsg.MessageID, chunks[0])
		for i := 1; i < len(chunks); i++ {
			b.sendReply(chatID, msg.MessageID, chunks[i])
		}
	} else {
		b.sendReply(chatID, msg.MessageID, reply)
	}
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
			log.Printf("event=telegram_image_download_error file_id=%s error=%q", largest.FileID, err)
		} else {
			images = append(images, img)
		}
	}

	// Handle image documents (files sent as attachments, not compressed).
	if msg.Document != nil && isImageMIME(msg.Document.MimeType) {
		img, err := b.downloadTelegramFile(msg.Document.FileID, msg.Document.FileName, msg.Document.MimeType)
		if err != nil {
			log.Printf("event=telegram_image_download_error file_id=%s error=%q", msg.Document.FileID, err)
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
// Command handling
// ---------------------------------------------------------------------------

// handleCommand processes bot commands (/reset, /persona, etc.).
func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	switch msg.Command() {
	case "reset":
		newID := b.resetSession(chatID)
		log.Printf("event=telegram_session_reset chat=%d new_session=%s", chatID, newID)
		b.sendReply(chatID, msg.MessageID, "✅ 對話已重置，開始新的對話。")

	case "persona":
		arg := strings.TrimSpace(msg.CommandArguments())
		if arg == "" {
			b.handlePersonaList(chatID, msg.MessageID)
		} else {
			b.handlePersonaSwitch(chatID, msg.MessageID, arg)
		}

	default:
		// Unknown command — ignore silently.
	}
}

// handlePersonaList sends a list of available personas.
func (b *Bot) handlePersonaList(chatID int64, replyToID int) {
	personas := b.svc.ListPersonas()
	if len(personas) == 0 {
		b.sendReply(chatID, replyToID, "📋 目前沒有可用的角色。")
		return
	}

	active := b.svc.ActivePersona()

	var sb strings.Builder
	sb.WriteString("📋 可用角色：\n")
	for _, p := range personas {
		marker := "　"
		if active != nil && active.DirName == p.DirName {
			marker = "▶ "
		}
		sb.WriteString(fmt.Sprintf("%s%s", marker, p.Name))
		if p.Description != "" {
			sb.WriteString(fmt.Sprintf(" — %s", p.Description))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n使用 /persona <名稱> 切換，/persona off 停用。")

	b.sendReply(chatID, replyToID, sb.String())
}

// handlePersonaSwitch switches or clears the active persona.
func (b *Bot) handlePersonaSwitch(chatID int64, replyToID int, arg string) {
	lower := strings.ToLower(arg)

	// Clear persona.
	if lower == "off" || lower == "clear" || lower == "none" {
		if err := b.svc.ClearActivePersona(); err != nil {
			b.sendReply(chatID, replyToID, "⚠️ "+err.Error())
			return
		}
		b.sendReply(chatID, replyToID, "✅ 已停用角色。")
		return
	}

	// Find persona by name.
	dirName, found := b.svc.FindPersonaByName(arg)
	if !found {
		b.sendReply(chatID, replyToID, fmt.Sprintf("⚠️ 找不到名為「%s」的角色。使用 /persona 查看可用清單。", arg))
		return
	}

	if err := b.svc.SetActivePersona(dirName); err != nil {
		b.sendReply(chatID, replyToID, "⚠️ "+err.Error())
		return
	}

	// Get the display name for confirmation.
	active := b.svc.ActivePersona()
	name := dirName
	if active != nil {
		name = active.Name
	}
	b.sendReply(chatID, replyToID, fmt.Sprintf("✅ 已切換為角色「%s」。", name))
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
				toolName := b.svc.GetActiveToolStatus(sessionID)
				var display string
				if toolName != "" {
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
	// Telegram edit limit is the same as send limit.
	if len(text) > telegramMessageLimit {
		text = text[:telegramMessageLimit]
	}
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	if _, err := b.api.Send(edit); err != nil {
		log.Printf("event=telegram_edit_error chat=%d message=%d error=%q", chatID, messageID, err)
	}
}

func (b *Bot) sendReply(chatID int64, replyToID int, content string) {
	chunks := splitMessage(content, telegramMessageLimit)
	for i, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		if i == 0 {
			msg.ReplyToMessageID = replyToID
		}
		if _, err := b.api.Send(msg); err != nil {
			log.Printf("event=telegram_send_error chat=%d error=%q", chatID, err)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Usage stats
// ---------------------------------------------------------------------------

// formatUsageStats builds a TUI-style usage summary: ⏱ 2.3s · ↑1.2K ↓567 · 245 tok/s
func formatUsageStats(usage core.UsageInfo, elapsed time.Duration) string {
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

	// Token counts.
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("↑%s ↓%s",
			formatTokenCount(usage.InputTokens), formatTokenCount(usage.OutputTokens)))
	}

	// Throughput.
	if usage.OutputTokens > 0 && elapsed > 0 {
		tokPerSec := float64(usage.OutputTokens) / elapsed.Seconds()
		parts = append(parts, fmt.Sprintf("%.0f tok/s", tokPerSec))
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

// formatToolSummary builds a short summary of executed tools.
func formatToolSummary(events []core.ToolEvent) string {
	if len(events) == 0 {
		return ""
	}
	type entry struct {
		name  string
		count int
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
		entries = append(entries, entry{name: display, count: 1})
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
	}
	return sb.String()
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
