package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

func TestAppModelInit(t *testing.T) {
	m := newTestAppModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a command")
	}
}

func TestAppModelWindowResize(t *testing.T) {
	m := newTestAppModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app := updated.(appModel)
	if app.width != 120 || app.height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", app.width, app.height)
	}
}

func TestAppModelToggleSettings(t *testing.T) {
	m := newTestAppModel()

	// Settings should start hidden
	if m.settings.visible {
		t.Fatal("expected settings to be hidden initially")
	}

	// Toggle on
	updated, _ := m.Update(ToggleSettingsMsg{})
	app := updated.(appModel)
	if !app.settings.visible {
		t.Fatal("expected settings to be visible after toggle")
	}

	// Toggle off
	updated, _ = app.Update(ToggleSettingsMsg{})
	app = updated.(appModel)
	if app.settings.visible {
		t.Fatal("expected settings to be hidden after second toggle")
	}
}

func TestSettingsEscCloses(t *testing.T) {
	m := newTestAppModel()

	// Open settings
	updated, _ := m.Update(ToggleSettingsMsg{})
	app := updated.(appModel)
	if !app.settings.visible {
		t.Fatal("expected settings to be visible")
	}

	// Simulate Esc from within settings — settings_view sends ToggleSettingsMsg
	// (without calling Hide first, which was the bug fix)
	updated, _ = app.Update(ToggleSettingsMsg{})
	app = updated.(appModel)
	if app.settings.visible {
		t.Fatal("expected settings to close after Esc toggle")
	}
}

func TestAppModelProviderChanged(t *testing.T) {
	m := newTestAppModel()
	updated, _ := m.Update(ProviderChangedMsg{Provider: "google-ai-studio"})
	app := updated.(appModel)
	if app.provider != "google-ai-studio" {
		t.Fatalf("expected google-ai-studio, got %s", app.provider)
	}
}

func TestAppModelSessionChanged(t *testing.T) {
	m := newTestAppModel()
	updated, _ := m.Update(SessionChangedMsg{SessionID: "test-session"})
	app := updated.(appModel)
	if app.sessionID != "test-session" {
		t.Fatalf("expected test-session, got %s", app.sessionID)
	}
}

func TestAppModelModelChanged(t *testing.T) {
	m := newTestAppModel()
	updated, _ := m.Update(ModelChangedMsg{ModelID: "gemini-2.5-pro"})
	app := updated.(appModel)
	if app.modelID != "gemini-2.5-pro" {
		t.Fatalf("expected gemini-2.5-pro, got %s", app.modelID)
	}
}

func TestAppModelViewRender(t *testing.T) {
	m := newTestAppModel()
	m.width = 80
	m.height = 24
	m.statusBar.SetSize(80)
	m.chatView.SetSize(80, 22)
	m.settings.SetSize(80, 22)

	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestStatusBarComponent(t *testing.T) {
	sb := NewStatusBar("mock", "default", "main")
	sb.SetSize(80)
	sb.SetContextPercent(12)
	sb.SetCost(0.05)
	sb.SetMessageCount(3)

	view := sb.View()
	if !strings.Contains(view, "mock") {
		t.Fatal("expected provider in status bar")
	}
	if !strings.Contains(view, "default") {
		t.Fatal("expected model in status bar")
	}
	if !strings.Contains(view, "main") {
		t.Fatal("expected session in status bar")
	}
	if !strings.Contains(view, "12%") {
		t.Fatal("expected context percent in status bar")
	}
}

func TestChatViewportAppendAndRender(t *testing.T) {
	cv := NewChatViewport(80, 20)
	cv.AppendMessage(ChatMessage{Role: "user", Content: "Hello", Timestamp: time.Now()})
	cv.AppendMessage(ChatMessage{Role: "assistant", Content: "Hi there!", Timestamp: time.Now()})

	view := cv.View()
	if !strings.Contains(view, "Hello") {
		t.Fatal("expected user message in viewport")
	}
}

func TestChatViewportSetMessages(t *testing.T) {
	cv := NewChatViewport(80, 20)
	cv.SetMessages([]ChatMessage{
		{Role: "system", Content: "Welcome", Timestamp: time.Now()},
		{Role: "user", Content: "Test", Timestamp: time.Now()},
	})
	view := cv.View()
	if view == "" {
		t.Fatal("expected non-empty viewport")
	}
}

func TestChatViewportMessageCount(t *testing.T) {
	cv := NewChatViewport(80, 20)
	cv.AppendMessage(ChatMessage{Role: "user", Content: "one", Timestamp: time.Now()})
	cv.AppendMessage(ChatMessage{Role: "assistant", Content: "two", Timestamp: time.Now()})
	if cv.MessageCount() != 2 {
		t.Fatalf("expected 2, got %d", cv.MessageCount())
	}
}

func TestChatInputSlashSuggestions(t *testing.T) {
	ci := NewChatInput(80, 24)
	ci.textarea.SetValue("/he")
	ci.updateSuggestions()
	if !ci.showSuggestions {
		t.Fatal("expected suggestions to show for /he")
	}
	if len(ci.suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(ci.suggestions))
	}
	if ci.suggestions[0].Name != "/help" {
		t.Fatalf("expected /help suggestion, got %s", ci.suggestions[0].Name)
	}
}

func TestChatInputNoSuggestionsForNonSlash(t *testing.T) {
	ci := NewChatInput(80, 24)
	ci.textarea.SetValue("hello")
	ci.updateSuggestions()
	if ci.showSuggestions {
		t.Fatal("expected no suggestions for non-slash input")
	}
}

func TestChatInputNoSuggestionsAfterSpace(t *testing.T) {
	ci := NewChatInput(80, 24)
	ci.textarea.SetValue("/memory test")
	ci.updateSuggestions()
	if ci.showSuggestions {
		t.Fatal("expected no suggestions after space in command")
	}
}

func TestChatInputHistory(t *testing.T) {
	ci := NewChatInput(80, 24)
	ci.appendHistory("first")
	ci.appendHistory("second")
	ci.appendHistory("third")

	if len(ci.history) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(ci.history))
	}

	// Navigate up
	ci.historyMoveUp()
	if ci.textarea.Value() != "third" {
		t.Fatalf("expected 'third', got %q", ci.textarea.Value())
	}
	ci.historyMoveUp()
	if ci.textarea.Value() != "second" {
		t.Fatalf("expected 'second', got %q", ci.textarea.Value())
	}

	// Navigate down
	ci.historyMoveDown()
	if ci.textarea.Value() != "third" {
		t.Fatalf("expected 'third', got %q", ci.textarea.Value())
	}
}

func TestChatInputDuplicateHistory(t *testing.T) {
	ci := NewChatInput(80, 24)
	ci.appendHistory("same")
	ci.appendHistory("same")
	if len(ci.history) != 1 {
		t.Fatalf("expected 1 entry (no duplicates), got %d", len(ci.history))
	}
}

func TestChatInputNewCommand(t *testing.T) {
	ci := NewChatInput(80, 24)
	ci.textarea.SetValue("/ne")
	ci.updateSuggestions()
	if !ci.showSuggestions {
		t.Fatal("expected suggestions to show for /ne")
	}
	found := false
	for _, s := range ci.suggestions {
		if s.Name == "/new" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected /new in suggestions")
	}
}

func TestChatInputConfigCommand(t *testing.T) {
	ci := NewChatInput(80, 24)
	ci.textarea.SetValue("/con")
	ci.updateSuggestions()
	if !ci.showSuggestions {
		t.Fatal("expected suggestions to show for /con")
	}
	found := false
	for _, s := range ci.suggestions {
		if s.Name == "/config" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected /config in suggestions")
	}
}

func TestChatInputViewHasPrompt(t *testing.T) {
	ci := NewChatInput(80, 24)
	view := ci.View()
	if !strings.Contains(view, ">") {
		t.Fatal("expected > prompt in input view")
	}
}

func TestHelperFallback(t *testing.T) {
	if fallback("", "default") != "default" {
		t.Fatal("expected fallback for empty string")
	}
	if fallback("value", "default") != "value" {
		t.Fatal("expected value, not fallback")
	}
}

func TestHelperClampLine(t *testing.T) {
	result := clampLine("hello world", 5)
	if result != "hell…" {
		t.Fatalf("expected 'hell…', got %q", result)
	}
	result = clampLine("hi", 10)
	if result != "hi" {
		t.Fatalf("expected 'hi', got %q", result)
	}
}

func TestHelperWrapToWidth(t *testing.T) {
	lines := wrapToWidth("abcdefghij", 5)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "abcde" {
		t.Fatalf("expected 'abcde', got %q", lines[0])
	}
}

func TestHelperFormatTimeAgo(t *testing.T) {
	if formatTimeAgo(time.Now()) != "剛剛" {
		t.Fatal("expected '剛剛' for recent time")
	}
	if formatTimeAgo(time.Time{}) != "" {
		t.Fatal("expected empty for zero time")
	}
}

func TestHelperAppendUnique(t *testing.T) {
	items := []string{"a", "b"}
	items = appendUnique(items, "c")
	if len(items) != 3 {
		t.Fatalf("expected 3, got %d", len(items))
	}
	items = appendUnique(items, "b")
	if len(items) != 3 {
		t.Fatalf("expected 3 (no duplicate), got %d", len(items))
	}
}

func TestHelperRemoveFromSlice(t *testing.T) {
	items := removeFromSlice([]string{"a", "b", "c"}, "b")
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
}

func TestHelperStripAnsi(t *testing.T) {
	input := "\x1b[31mhello\x1b[0m world"
	result := stripAnsi(input)
	if result != "hello world" {
		t.Fatalf("expected 'hello world', got %q", result)
	}
}

func TestHelperStripAnsi_RemovesOSC(t *testing.T) {
	input := "x\x1b]11;rgb:11/22/33\x07y"
	result := stripAnsi(input)
	if result != "xy" {
		t.Fatalf("expected 'xy', got %q", result)
	}
}

func TestHelperDimLines(t *testing.T) {
	input := "line one\nline two"
	result := dimLines(input)
	if result == "" {
		t.Fatal("expected non-empty dimmed output")
	}
	if !strings.Contains(stripAnsi(result), "line one") {
		t.Fatal("expected content preserved in dimmed output")
	}
}

func TestFormatChatError(t *testing.T) {
	err := &client.APIError{StatusCode: 409, Message: "no available account: reason=rate_limit"}
	msg := formatChatError(err)
	if !strings.Contains(msg, "頻率限制") {
		t.Fatalf("expected rate limit message, got %q", msg)
	}
}

func TestModelOptionsForProvider(t *testing.T) {
	models := modelOptionsForProvider("google-gemini-cli", "custom")
	found := false
	for _, m := range models {
		if m == "custom" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected custom model in list")
	}
	if models[0] != "default" {
		t.Fatal("expected 'default' as first model")
	}
}

func TestSettingsViewSectionNav(t *testing.T) {
	sv := NewSettingsView(nil, "mock", "default", "main", 80, 24)
	if sv.activeSection != SectionProvider {
		t.Fatalf("expected SectionProvider, got %d", sv.activeSection)
	}
}

func TestSettingsOverlayRender(t *testing.T) {
	sv := NewSettingsView(nil, "mock", "default", "main", 80, 24)
	sv.visible = true
	chatBg := strings.Repeat("chat content line\n", 20)
	result := sv.RenderOverlay(chatBg, 80, 22)
	if result == "" {
		t.Fatal("expected non-empty overlay render")
	}
	if !strings.Contains(result, "Provider") {
		t.Fatal("expected Provider tab in overlay")
	}
}

func TestChatViewportRemoveLastMessage(t *testing.T) {
	cv := NewChatViewport(80, 20)
	cv.AppendMessage(ChatMessage{Role: "user", Content: "Hello", Timestamp: time.Now()})
	cv.AppendMessage(ChatMessage{Role: "thinking", Content: "thinking...", Timestamp: time.Now()})

	if cv.MessageCount() != 2 {
		t.Fatalf("expected 2 messages, got %d", cv.MessageCount())
	}

	cv.RemoveLastMessage()
	if cv.MessageCount() != 1 {
		t.Fatalf("expected 1 message after remove, got %d", cv.MessageCount())
	}

	view := cv.View()
	if !strings.Contains(view, "Hello") {
		t.Fatal("expected user message preserved after removing last")
	}
}

func TestChatInputMultiLineView(t *testing.T) {
	ci := NewChatInput(80, 24)
	view := ci.View()

	// Should contain separator line
	if !strings.Contains(view, "─") {
		t.Fatal("expected separator line in input view")
	}

	// Should contain hint line
	if !strings.Contains(view, "Shift+Enter") {
		t.Fatal("expected Shift+Enter hint in input view")
	}

	// Should contain prompt
	if !strings.Contains(view, ">") {
		t.Fatal("expected > prompt in input view")
	}
}

func TestChatViewWelcomeMessage(t *testing.T) {
	cv := NewChatView(nil, "mock", "default", "main", 80, 24)
	view := cv.View()
	if !strings.Contains(view, "NekoClaw") {
		t.Fatal("expected welcome message containing NekoClaw")
	}
	if !strings.Contains(view, "/help") {
		t.Fatal("expected /help hint in welcome message")
	}
}

func TestChatInputInputHeight(t *testing.T) {
	ci := NewChatInput(80, 24)
	h := ci.InputHeight()
	// Minimum: textarea(3) + separator(1) + hint(1) = 5
	if h < 5 {
		t.Fatalf("expected input height >= 5, got %d", h)
	}
}

// newTestAppModel creates an appModel for testing without a real API client.
func newTestAppModel() appModel {
	return appModel{
		sessionID: "main",
		provider:  "mock",
		modelID:   "default",
		statusBar: NewStatusBar("mock", "default", "main"),
		chatView:  NewChatView(nil, "mock", "default", "main", 80, 24),
		settings:  NewSettingsView(nil, "mock", "default", "main", 80, 24),
	}
}
