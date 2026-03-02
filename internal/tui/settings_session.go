package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

// SessionSection handles session list, create, and delete.
type SessionSection struct {
	sessions       []client.SessionInfo
	selectedIdx    int
	currentSession string
	loaded         bool

	// New session input
	creatingNew bool
	input       textinput.Model
	statusMsg   string
}

func NewSessionSection(currentSession string) SessionSection {
	ti := textinput.New()
	ti.CharLimit = 100
	ti.Prompt = "› "
	ti.Placeholder = "Session 名稱"
	return SessionSection{
		currentSession: currentSession,
		input:          ti,
	}
}

func (ss *SessionSection) SetCurrentSession(s string) { ss.currentSession = s }

func (ss *SessionSection) HandleSessionsList(msg SessionsListMsg) tea.Cmd {
	if msg.Err != nil {
		ss.statusMsg = "載入 sessions 失敗: " + msg.Err.Error()
		return nil
	}
	ss.sessions = msg.Sessions
	ss.loaded = true
	// Find current session index
	for i, s := range ss.sessions {
		if s.SessionID == ss.currentSession {
			ss.selectedIdx = i
			break
		}
	}
	return nil
}

func (ss *SessionSection) HandleSessionDelete(msg SessionDeleteMsg, apiClient *client.APIClient) tea.Cmd {
	if msg.Err != nil {
		ss.statusMsg = "刪除失敗: " + msg.Err.Error()
		return nil
	}
	ss.statusMsg = "已刪除: " + msg.SessionID
	if ss.currentSession == msg.SessionID {
		ss.currentSession = "main"
	}
	// Reload sessions
	return listSessionsCmd(apiClient)
}

func (ss *SessionSection) HasActiveInput() bool {
	return ss.creatingNew
}

func (ss *SessionSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	// New session input mode
	if ss.creatingNew {
		return ss.handleNewSessionInput(msg)
	}

	switch {
	case key.Matches(msg, settingsKeys.Up):
		if ss.selectedIdx > 0 {
			ss.selectedIdx--
		}
	case key.Matches(msg, settingsKeys.Down):
		if ss.selectedIdx < len(ss.sessions)-1 {
			ss.selectedIdx++
		}
	case key.Matches(msg, settingsKeys.Select):
		if ss.selectedIdx < len(ss.sessions) {
			selected := ss.sessions[ss.selectedIdx].SessionID
			ss.currentSession = selected
			return func() tea.Msg { return SessionChangedMsg{SessionID: selected} }
		}
	case key.Matches(msg, settingsKeys.Delete):
		if ss.selectedIdx < len(ss.sessions) {
			sessionID := ss.sessions[ss.selectedIdx].SessionID
			return deleteSessionCmd(apiClient, sessionID)
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
		ss.creatingNew = true
		ss.input.SetValue("")
		ss.input.Focus()
		return nil
	}
	return nil
}

func (ss *SessionSection) handleNewSessionInput(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	if k == "esc" {
		ss.creatingNew = false
		ss.input.Blur()
		return nil
	}
	if k == "enter" {
		name := strings.TrimSpace(ss.input.Value())
		if name == "" {
			return nil
		}
		ss.creatingNew = false
		ss.input.Blur()
		ss.currentSession = name
		return func() tea.Msg { return SessionChangedMsg{SessionID: name} }
	}
	var cmd tea.Cmd
	ss.input, cmd = ss.input.Update(msg)
	return cmd
}

func (ss SessionSection) View(width int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Sessions"))
	lines = append(lines, "")

	// New session input
	if ss.creatingNew {
		lines = append(lines, theme.SectionStyle.Render("新建 Session"))
		lines = append(lines, ss.input.View())
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render("Enter 建立  ·  Esc 取消"))
		return strings.Join(lines, "\n")
	}

	// Session list
	if !ss.loaded {
		lines = append(lines, theme.HintStyle.Render("  載入中..."))
	} else if len(ss.sessions) == 0 {
		lines = append(lines, theme.HintStyle.Render("  尚無 sessions。"))
	} else {
		for i, s := range ss.sessions {
			prefix := "  "
			style := theme.NormalStyle
			if i == ss.selectedIdx {
				prefix = "› "
				style = theme.SelectedStyle
			}
			current := ""
			if s.SessionID == ss.currentSession {
				current = " ✓"
			}
			age := formatTimeAgo(s.UpdatedAt)
			label := fmt.Sprintf("%-18s %3d 訊息  %s%s", s.SessionID, s.MessageCount, age, current)
			lines = append(lines, style.Render(clampLine(prefix+label, textW)))
		}
	}

	lines = append(lines, "")

	// Status
	if ss.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render(ss.statusMsg))
		lines = append(lines, "")
	}

	lines = append(lines, theme.HintStyle.Render("Enter 選擇  ·  n 新建  ·  d 刪除  ·  Esc 返回"))

	return strings.Join(lines, "\n")
}
