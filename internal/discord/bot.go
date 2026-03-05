package discord

import (
	"context"
	"fmt"
	"log"
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

// Bot connects to Discord Gateway and forwards messages to the NekoClaw service.
type Bot struct {
	session     *discordgo.Session
	svc         *app.Service
	provider    string
	model       string
	activeChans map[string]bool // channels where bot responds to all messages

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config holds the configuration needed to create a Discord bot.
type Config struct {
	Token          string   // Discord bot token (required)
	Provider       string   // default provider ID
	Model          string   // default model ID
	ActiveChannels []string // channel IDs that respond to all messages
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

	activeChans := make(map[string]bool, len(cfg.ActiveChannels))
	for _, ch := range cfg.ActiveChannels {
		if id := strings.TrimSpace(ch); id != "" {
			activeChans[id] = true
		}
	}

	return &Bot{
		session:     dg,
		svc:         svc,
		provider:    cfg.Provider,
		model:       cfg.Model,
		activeChans: activeChans,
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

	// Block until context is done.
	<-b.ctx.Done()
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

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.handleMessage(s, m)
	}()
}

// shouldRespond checks whether the bot should respond to this message.
func (b *Bot) shouldRespond(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	// Active channel: respond to everything.
	if b.activeChans[m.ChannelID] {
		return true
	}

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
	// Strip bot mention from message content.
	text := stripMention(m.Content, s.State.User.ID)
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	// Show typing indicator while processing.
	stopTyping := b.startTyping(s, m.ChannelID)
	defer stopTyping()

	sessionID := fmt.Sprintf("discord:%s:%s", m.ChannelID, m.Author.ID)

	resp, err := b.svc.HandleChat(b.ctx, core.ChatRequest{
		SessionID:   sessionID,
		Surface:     core.SurfaceDiscord,
		Provider:    b.provider,
		Model:       b.model,
		Message:     text,
		EnableTools: true,
	})
	if err != nil {
		log.Printf("event=discord_chat_error channel=%s user=%s error=%q", m.ChannelID, m.Author.ID, err)
		b.sendReply(s, m, "⚠️ "+err.Error())
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

	b.sendReply(s, m, reply)
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
// Reply formatting
// ---------------------------------------------------------------------------

// sendReply sends a reply to the original message, splitting if needed.
func (b *Bot) sendReply(s *discordgo.Session, m *discordgo.MessageCreate, content string) {
	ref := &discordgo.MessageReference{
		MessageID: m.ID,
		ChannelID: m.ChannelID,
		GuildID:   m.GuildID,
	}

	chunks := splitMessage(content, discordMessageLimit)
	for i, chunk := range chunks {
		msg := &discordgo.MessageSend{
			Content: chunk,
		}
		// Only first chunk references the original message.
		if i == 0 {
			msg.Reference = ref
		}
		if _, err := s.ChannelMessageSendComplex(m.ChannelID, msg); err != nil {
			log.Printf("event=discord_send_error channel=%s error=%q", m.ChannelID, err)
			return
		}
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
