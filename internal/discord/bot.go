package discord

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/mcp"
)

// discordMessageLimit is the maximum length of a single Discord message.
const discordMessageLimit = 2000

// Reaction emoji for message lifecycle.
const (
	emojiReceived   = "👀"
	emojiProcessing = "🔄"
	emojiDone       = "✅"
)

// messageJob represents a queued message to be processed.
type messageJob struct {
	s *discordgo.Session
	m *discordgo.MessageCreate
}

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
	// Key: channelID, Value: current sessionID.
	activeSessions   map[string]string
	activeSessionsMu sync.RWMutex
}

// Config holds the configuration needed to create a Discord bot.
type Config struct {
	Token string // Discord bot token (required)
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

	return &Bot{
		session:        dg,
		svc:            svc,
		queues:         make(map[string]chan messageJob),
		activeSessions: make(map[string]string),
	}, nil
}

// Start connects to Discord Gateway and begins handling messages.
// Blocks until the context is cancelled or an error occurs.
func (b *Bot) Start(ctx context.Context) error {
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.session.AddHandler(b.onMessageCreate)

	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open discord gateway: %w", err)
	}
	log.Printf("event=discord_bot_started user=%s", b.session.State.User.Username)
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
// Falls back to the default format if no active session is set.
func (b *Bot) getSessionID(channelID string) string {
	b.activeSessionsMu.RLock()
	sid, ok := b.activeSessions[channelID]
	b.activeSessionsMu.RUnlock()
	if ok {
		return sid
	}
	return fmt.Sprintf("discord:%s", channelID)
}

// resetSession creates a new session for the channel and returns its ID.
func (b *Bot) resetSession(channelID string) string {
	newID := fmt.Sprintf("discord:%s-%s", channelID, time.Now().Format("0102-150405"))
	b.activeSessionsMu.Lock()
	b.activeSessions[channelID] = newID
	b.activeSessionsMu.Unlock()
	return newID
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

	// Handle slash commands before sending to AI.
	if text != "" && b.handleCommand(s, m, text) {
		return
	}

	// Default text when only images are sent.
	if text == "" && len(images) > 0 {
		text = "請描述這張圖片"
	}

	botID := s.State.User.ID

	// Transition: 👀 → 🔄
	_ = s.MessageReactionRemove(m.ChannelID, m.ID, emojiReceived, botID)
	_ = s.MessageReactionAdd(m.ChannelID, m.ID, emojiProcessing)

	// Show typing indicator while processing.
	stopTyping := b.startTyping(s, m.ChannelID)

	sessionID := b.getSessionID(m.ChannelID)

	// Send placeholder message and start tool status polling.
	placeholder := &discordgo.MessageSend{
		Content: "🔄 處理中...",
		Reference: &discordgo.MessageReference{
			MessageID: m.ID,
			ChannelID: m.ChannelID,
			GuildID:   m.GuildID,
		},
	}
	placeholderMsg, placeholderErr := s.ChannelMessageSendComplex(m.ChannelID, placeholder)

	// Poll tool status and update placeholder while waiting for response.
	stopToolStatus := b.startToolStatusPolling(s, m.ChannelID, placeholderMsg, placeholderErr, sessionID)

	resp, err := b.svc.HandleChat(b.ctx, core.ChatRequest{
		SessionID:   sessionID,
		Surface:     core.SurfaceDiscord,
		Provider:    b.svc.GetDefaultProvider(),
		Model:       b.svc.GetDefaultModel(),
		Message:     text,
		Images:      images,
		EnableTools: true,
	})

	stopToolStatus()
	stopTyping()

	// Transition: 🔄 → ✅
	_ = s.MessageReactionRemove(m.ChannelID, m.ID, emojiProcessing, botID)
	_ = s.MessageReactionAdd(m.ChannelID, m.ID, emojiDone)

	// Delete the placeholder so it won't conflict with new messages in the channel.
	// The final reply is always sent as fresh messages to avoid edit-interruption issues.
	if placeholderErr == nil {
		_ = s.ChannelMessageDelete(m.ChannelID, placeholderMsg.ID)
	}

	if err != nil {
		log.Printf("event=discord_chat_error channel=%s user=%s error=%q", m.ChannelID, m.Author.ID, err)
		b.sendReply(s, m, "⚠️ "+err.Error())
		b.logToConsole(s, fmt.Sprintf("[錯誤] <#%s> %s: %s", m.ChannelID, m.Author.Username, err.Error()))
		return
	}

	elapsed := time.Since(startTime)

	reply := strings.TrimSpace(resp.Reply)
	if reply == "" {
		reply = "（無回應）"
	}

	// Append usage stats and tool summary (Discord -# small text).
	var footer []string
	if stats := formatUsageStats(resp.Usage, elapsed, resp.Provider, resp.Model); stats != "" {
		footer = append(footer, "-# "+stats)
	}
	if summary := formatToolSummary(resp.ToolEvents); summary != "" {
		footer = append(footer, summary)
	}
	if len(footer) > 0 {
		reply += "\n\n" + strings.Join(footer, "\n")
	}

	b.sendReply(s, m, reply)

	// Log detailed traffic to console channel.
	b.logTraffic(s, m, resp, len(images), elapsed)
}

// ---------------------------------------------------------------------------
// Command handling
// ---------------------------------------------------------------------------

// handleCommand checks if the message is a bot command and processes it.
// Returns true if the message was handled as a command.
func (b *Bot) handleCommand(s *discordgo.Session, m *discordgo.MessageCreate, text string) bool {
	// Remove the 👀 reaction for commands (no AI processing needed).
	removeReceived := func() {
		_ = s.MessageReactionRemove(m.ChannelID, m.ID, emojiReceived, s.State.User.ID)
	}

	lower := strings.ToLower(text)

	switch {
	case lower == "/reset":
		removeReceived()
		newID := b.resetSession(m.ChannelID)
		log.Printf("event=discord_session_reset channel=%s new_session=%s", m.ChannelID, newID)
		b.sendReply(s, m, "✅ 對話已重置，開始新的對話。")
		b.logToConsole(s, fmt.Sprintf("[重置] <#%s> 對話已重置 → %s (by %s)", m.ChannelID, newID, m.Author.Username))
		return true

	case lower == "/persona":
		removeReceived()
		b.handlePersonaList(s, m)
		return true

	case strings.HasPrefix(lower, "/persona "):
		removeReceived()
		arg := strings.TrimSpace(text[len("/persona "):])
		b.handlePersonaSwitch(s, m, arg)
		return true
	}

	return false
}

// handlePersonaList sends a list of available personas.
func (b *Bot) handlePersonaList(s *discordgo.Session, m *discordgo.MessageCreate) {
	personas := b.svc.ListPersonas()
	if len(personas) == 0 {
		b.sendReply(s, m, "📋 目前沒有可用的角色。")
		return
	}

	active := b.svc.ActivePersona()

	var sb strings.Builder
	sb.WriteString("📋 **可用角色：**\n")
	for _, p := range personas {
		marker := "　"
		if active != nil && active.DirName == p.DirName {
			marker = "▶ "
		}
		sb.WriteString(fmt.Sprintf("%s**%s**", marker, p.Name))
		if p.Description != "" {
			sb.WriteString(fmt.Sprintf(" — %s", p.Description))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n使用 `/persona <名稱>` 切換，`/persona off` 停用。")

	b.sendReply(s, m, sb.String())
}

// handlePersonaSwitch switches or clears the active persona.
func (b *Bot) handlePersonaSwitch(s *discordgo.Session, m *discordgo.MessageCreate, arg string) {
	lower := strings.ToLower(arg)

	// Clear persona.
	if lower == "off" || lower == "clear" || lower == "none" {
		if err := b.svc.ClearActivePersona(); err != nil {
			b.sendReply(s, m, "⚠️ "+err.Error())
			return
		}
		b.sendReply(s, m, "✅ 已停用角色。")
		b.logToConsole(s, fmt.Sprintf("[角色] 已停用角色 (by %s)", m.Author.Username))
		return
	}

	// Find persona by name.
	dirName, found := b.svc.FindPersonaByName(arg)
	if !found {
		b.sendReply(s, m, fmt.Sprintf("⚠️ 找不到名為「%s」的角色。使用 `/persona` 查看可用清單。", arg))
		return
	}

	if err := b.svc.SetActivePersona(dirName); err != nil {
		b.sendReply(s, m, "⚠️ "+err.Error())
		return
	}

	// Get the display name for confirmation.
	active := b.svc.ActivePersona()
	name := dirName
	if active != nil {
		name = active.Name
	}
	b.sendReply(s, m, fmt.Sprintf("✅ 已切換為角色「%s」。", name))
	b.logToConsole(s, fmt.Sprintf("[角色] 切換至「%s」(by %s)", name, m.Author.Username))
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
		log.Printf("event=discord_edit_error channel=%s message=%s error=%q", channelID, messageID, err)
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
			log.Printf("event=discord_send_error channel=%s error=%q", m.ChannelID, err)
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
		log.Printf("event=discord_console_send_error channel=%s error=%q", channelID, err)
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

// formatUsageStats builds a TUI-style usage summary:
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
	sb.WriteString("-# 🔧 使用的工具：")
	for i, e := range entries {
		sb.WriteString(fmt.Sprintf("\n-# %d. %s", i+1, e.name))
		if e.count > 1 {
			sb.WriteString(fmt.Sprintf(" (×%d)", e.count))
		}
	}
	return sb.String()
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
	var images []core.ImageData
	for _, att := range attachments {
		if att == nil || att.URL == "" {
			continue
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
			log.Printf("event=discord_image_skip file=%s reason=too_large size=%d", att.Filename, att.Size)
			continue
		}

		data, err := downloadImage(att.URL)
		if err != nil {
			log.Printf("event=discord_image_download_error file=%s error=%q", att.Filename, err)
			continue
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
