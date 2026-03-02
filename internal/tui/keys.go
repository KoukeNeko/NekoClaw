package tui

import "github.com/charmbracelet/bubbles/key"

// AppKeyMap defines global key bindings.
type AppKeyMap struct {
	Quit     key.Binding
	Settings key.Binding
}

// ChatKeyMap defines key bindings for the chat view.
type ChatKeyMap struct {
	Submit       key.Binding
	NewLine      key.Binding
	OpenSettings key.Binding
	ScrollUp     key.Binding
	ScrollDown   key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	GoToTop      key.Binding
	GoToBottom   key.Binding
	HistoryPrev  key.Binding
	HistoryNext  key.Binding
}

// SettingsKeyMap defines key bindings for the settings view.
type SettingsKeyMap struct {
	Back        key.Binding
	Up          key.Binding
	Down        key.Binding
	Left        key.Binding
	Right       key.Binding
	Select      key.Binding
	Delete      key.Binding
	Tab         key.Binding
	ShiftTab    key.Binding
}

var appKeys = AppKeyMap{
	Quit:     key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	Settings: key.NewBinding(key.WithKeys("ctrl+,"), key.WithHelp("ctrl+,", "settings")),
}

var chatKeys = ChatKeyMap{
	Submit:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
	NewLine:      key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "new line")),
	OpenSettings: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "settings")),
	ScrollUp:     key.NewBinding(key.WithKeys("ctrl+up"), key.WithHelp("ctrl+↑", "scroll up")),
	ScrollDown:   key.NewBinding(key.WithKeys("ctrl+down"), key.WithHelp("ctrl+↓", "scroll down")),
	PageUp:       key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	PageDown:     key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	GoToTop:      key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
	GoToBottom:   key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
	HistoryPrev:  key.NewBinding(key.WithKeys("alt+up"), key.WithHelp("alt+↑", "prev input")),
	HistoryNext:  key.NewBinding(key.WithKeys("alt+down"), key.WithHelp("alt+↓", "next input")),
}

var settingsKeys = SettingsKeyMap{
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
	Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
	Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Delete:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next section")),
	ShiftTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev section")),
}
