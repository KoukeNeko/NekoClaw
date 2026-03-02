package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/core"
)

func newChatModelForTest() model {
	chatInput := textinput.New()
	chatInput.Prompt = "▸ "
	chatInput.Focus()
	return model{
		view:              viewChat,
		status:            "chat",
		sessionID:         "main",
		provider:          "mock",
		modelID:           "default",
		chatInput:         chatInput,
		promptInput:       textinput.New(),
		lines:             []chatLine{{role: "system", text: "ready"}},
		inputHistoryIndex: -1,
	}
}

func TestChatResultKeepsChatView(t *testing.T) {
	m := newChatModelForTest()
	out, _ := m.Update(chatResultMsg{
		response: core.ChatResponse{
			Reply:     "hello",
			AccountID: "acc-1",
		},
	})
	got, ok := out.(model)
	if !ok {
		t.Fatalf("unexpected model type %T", out)
	}
	if got.view != viewChat {
		t.Fatalf("expected view %q, got %q", viewChat, got.view)
	}
	if len(got.lines) == 0 || got.lines[len(got.lines)-1].role != "assistant" {
		t.Fatalf("expected assistant line appended")
	}
}

func TestChatHistoryNavigation(t *testing.T) {
	m := newChatModelForTest()
	m.appendInputHistory("first")
	m.appendInputHistory("second")
	m.chatInput.SetValue("draft")

	m.historyMoveUp()
	if got := m.chatInput.Value(); got != "second" {
		t.Fatalf("expected second history item, got %q", got)
	}
	m.historyMoveUp()
	if got := m.chatInput.Value(); got != "first" {
		t.Fatalf("expected first history item, got %q", got)
	}
	m.historyMoveDown()
	if got := m.chatInput.Value(); got != "second" {
		t.Fatalf("expected second history item after down, got %q", got)
	}
	m.historyMoveDown()
	if got := m.chatInput.Value(); got != "draft" {
		t.Fatalf("expected draft restored after history exit, got %q", got)
	}
}

func TestChatLocalCommands(t *testing.T) {
	m := newChatModelForTest()

	m.chatInput.SetValue("/help")
	out, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := out.(model)
	if !ok {
		t.Fatalf("unexpected model type %T", out)
	}
	if got.pending {
		t.Fatalf("expected local command not to trigger API request")
	}
	if got.lines[len(got.lines)-1].text != "Local commands: /help, /menu, /clear" {
		t.Fatalf("unexpected help output: %q", got.lines[len(got.lines)-1].text)
	}

	got.chatInput.SetValue("/menu")
	out, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok = out.(model)
	if !ok {
		t.Fatalf("unexpected model type %T", out)
	}
	if got.view != viewMenu {
		t.Fatalf("expected /menu to switch to menu view, got %q", got.view)
	}
}

func TestChatViewUsesClaudeLikeLayout(t *testing.T) {
	m := newChatModelForTest()
	m.applyWindowSize(120, 36)

	// We no longer expect history to be inside m.View() in chat mode,
	// because history is now printed inline using tea.Printf rather than kept in the persistent frame.
	m.chatInput.SetValue("my test input")

	rendered := m.View()
	if strings.Contains(rendered, "NekoClaw Chat") && strings.Contains(rendered, "Recent activity") {
		t.Fatalf("expected old welcome card to be removed in the new minimalistic chat view")
	}
	if !strings.Contains(rendered, "my test input") {
		t.Fatalf("expected composer to contain standard input text, got: \n%s", rendered)
	}
	if strings.Contains(rendered, "status: ") {
		t.Fatalf("chat view should not render generic status footer")
	}
	if width := maxRenderedLineWidth(rendered); width > 120 {
		t.Fatalf("chat view overflow: got %d want <= 120", width)
	}

	// Test pending state shows a spinner/pending indicator
	m.pending = true
	renderedPending := m.View()
	if !strings.Contains(renderedPending, "Thinking") && !strings.Contains(renderedPending, "generating") && !strings.Contains(renderedPending, "⠋") && strings.Contains(renderedPending, "Bloviating") == false && !strings.Contains(renderedPending, "...") {
		t.Fatalf("expected spinner or pending indicator in composer when pending")
	}
}

func TestChatViewStaysWithinNarrowTerminalWidth(t *testing.T) {
	m := newChatModelForTest()
	m.applyWindowSize(32, 18)
	m.lines = append(m.lines, chatLine{role: "assistant", text: strings.Repeat("x", 120)})

	rendered := m.View()
	if width := maxRenderedLineWidth(rendered); width > 32 {
		t.Fatalf("chat view overflow on narrow terminal: got %d want <= 32", width)
	}
}
