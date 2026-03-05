package telegram

import (
	"context"
	"fmt"
	"log"
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
		api:    api,
		svc:    svc,
		queues: make(map[int64]chan messageJob),
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
// Message handling
// ---------------------------------------------------------------------------

// shouldRespond checks whether the bot should respond to this message.
// Responds to: DM, @mention, or reply to bot.
func (b *Bot) shouldRespond(update tgbotapi.Update) bool {
	msg := update.Message
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return false
	}

	// Always respond in private chats (DM).
	if msg.Chat.Type == "private" {
		return true
	}

	// Respond if bot is @mentioned in group text.
	botMention := "@" + b.api.Self.UserName
	if strings.Contains(msg.Text, botMention) {
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
	msg := update.Message
	text := b.extractText(msg)
	if text == "" {
		return
	}

	chatID := msg.Chat.ID

	// Send a placeholder message that we will edit with the final reply.
	placeholder := tgbotapi.NewMessage(chatID, "🔄 處理中...")
	placeholder.ReplyToMessageID = msg.MessageID
	placeholderMsg, placeholderErr := b.api.Send(placeholder)

	// Show typing indicator while processing.
	stopTyping := b.startTyping(chatID)

	sessionID := fmt.Sprintf("telegram:%d:%d", chatID, msg.From.ID)

	resp, err := b.svc.HandleChat(b.ctx, core.ChatRequest{
		SessionID:   sessionID,
		Surface:     core.SurfaceTelegram,
		Provider:    b.svc.GetDefaultProvider(),
		Model:       b.svc.GetDefaultModel(),
		Message:     text,
		EnableTools: true,
	})

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

	reply := strings.TrimSpace(resp.Reply)
	if reply == "" {
		reply = "（無回應）"
	}

	// Append tool summary if tools were used.
	if summary := formatToolSummary(resp.ToolEvents); summary != "" {
		reply += "\n\n" + summary
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
