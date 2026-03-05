package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// ANSI color codes for terminal output.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
)

// defaultWidth is the fixed padding width for module names (Docker Compose style).
const defaultWidth = 10

const (
	discordMaxContentLength = 2000
	discordTruncateSuffix   = "... [truncated]"
)

// DiscordSender is an interface for sending messages to Discord.
type DiscordSender interface {
	SendMessage(channelID, content string) error
}

var (
	discordSender DiscordSender
	logChannelID  string
	senderMu      sync.RWMutex

	// output is the destination for log messages (defaults to os.Stderr).
	// In TUI mode this is redirected to a file to avoid corrupting the screen.
	output   io.Writer = os.Stderr
	outputMu sync.RWMutex

	// useColors controls whether ANSI color codes are emitted.
	// Disabled automatically when output is redirected to a file.
	useColors = true
)

// SetOutput redirects all logger output to w.
// When w is not os.Stderr, ANSI colors are automatically disabled.
func SetOutput(w io.Writer) {
	outputMu.Lock()
	defer outputMu.Unlock()
	output = w
	useColors = (w == os.Stderr)
}

// SetDiscordOutput configures Discord channel output for all loggers.
func SetDiscordOutput(sender DiscordSender, channelID string) {
	senderMu.Lock()
	defer senderMu.Unlock()
	discordSender = sender
	logChannelID = channelID
}

// Module is a lightweight logger that prints messages with a colored module prefix.
// Styled after Docker Compose's per-container output format.
//
//	discord    | connected as Bot#1234
//	http       | listening on :8080
type Module struct {
	name  string
	color string
}

// New creates a module logger with the given name and ANSI color.
func New(name, color string) *Module {
	return &Module{name: name, color: color}
}

// Logf prints a formatted info message with the colored module prefix.
func (m *Module) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	m.writeLine(msg, "")
	m.sendToDiscord("info", msg)
}

// Warnf prints a warning (yellow) message.
func (m *Module) Warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	m.writeLine(msg, Yellow)
	m.sendToDiscord("warn", msg)
}

// Errorf prints an error (red) message.
func (m *Module) Errorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	m.writeLine(msg, Red)
	m.sendToDiscord("error", msg)
}

// writeLine formats and writes a single log line.
// msgColor is the ANSI color for the message body (empty for default).
func (m *Module) writeLine(msg, msgColor string) {
	outputMu.RLock()
	w := output
	colors := useColors
	outputMu.RUnlock()

	padded := m.name + strings.Repeat(" ", max(1, defaultWidth-len(m.name)))
	if colors {
		if msgColor != "" {
			fmt.Fprintf(w, "%s%s%s%s| %s%s%s\n", m.color, Bold, padded, Reset+Dim, Reset+msgColor, msg, Reset)
		} else {
			fmt.Fprintf(w, "%s%s%s%s| %s%s\n", m.color, Bold, padded, Reset+Dim, Reset, msg)
		}
	} else {
		// Plain text for file output — include timestamp for grep-ability.
		ts := time.Now().Format("15:04:05")
		fmt.Fprintf(w, "%s %s| %s\n", ts, padded, msg)
	}
}

// sendToDiscord forwards log messages to Discord channel if configured.
func (m *Module) sendToDiscord(level, msg string) {
	senderMu.RLock()
	sender := discordSender
	channelID := logChannelID
	senderMu.RUnlock()

	if sender == nil || channelID == "" {
		return
	}

	timestamp := time.Now().Format("15:04:05")
	var emoji string
	switch level {
	case "error":
		emoji = "🔴"
	case "warn":
		emoji = "⚠️"
	default:
		emoji = "ℹ️"
	}

	discordMsg := fmt.Sprintf("`%s` %s **%s** | %s", timestamp, emoji, m.name, msg)
	discordMsg = truncateDiscordContent(discordMsg, discordMaxContentLength)

	go func() {
		if err := sender.SendMessage(channelID, discordMsg); err != nil {
			padded := "logger" + strings.Repeat(" ", max(1, defaultWidth-len("logger")))
			fmt.Fprintf(os.Stderr, "%s%s%s%s| %s%s%s\n", Yellow, Bold, padded, Reset+Dim, Reset+Red, "discord send failed: "+err.Error(), Reset)
		}
	}()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncateDiscordContent(msg string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(msg)
	if len(runes) <= maxChars {
		return msg
	}
	suffix := []rune(discordTruncateSuffix)
	if len(suffix) >= maxChars {
		return string(runes[:maxChars])
	}
	keep := maxChars - len(suffix)
	return string(runes[:keep]) + discordTruncateSuffix
}
