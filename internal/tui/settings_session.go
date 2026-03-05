package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

// SessionSection handles session list, create, rename, and delete.
type SessionSection struct {
	sessions       []client.SessionInfo
	selectedIdx    int
	currentSession string
	loaded         bool

	// Shared text input for create/rename
	creatingNew     bool
	renamingSession bool
	input           textinput.Model
	statusMsg       string
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

func (ss *SessionSection) HandleSessionRename(msg SessionRenameMsg, apiClient *client.APIClient) tea.Cmd {
	if msg.Err != nil {
		ss.statusMsg = "重命名失敗: " + msg.Err.Error()
		return nil
	}
	ss.statusMsg = "已重命名: " + msg.Title
	return listSessionsCmd(apiClient)
}

func (ss *SessionSection) HasActiveInput() bool {
	return ss.creatingNew || ss.renamingSession
}

func (ss *SessionSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	// Input mode (create or rename)
	if ss.creatingNew || ss.renamingSession {
		return ss.handleInputMode(msg, apiClient)
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
		ss.renamingSession = false
		ss.input.Placeholder = "Session 名稱"
		ss.input.SetValue("")
		ss.input.Focus()
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
		if ss.selectedIdx < len(ss.sessions) {
			ss.renamingSession = true
			ss.creatingNew = false
			sess := ss.sessions[ss.selectedIdx]
			prefill := sess.Title
			if prefill == "" {
				prefill = sess.SessionID
			}
			ss.input.Placeholder = "新標題"
			ss.input.SetValue(prefill)
			ss.input.Focus()
			return nil
		}
	}
	return nil
}

func (ss *SessionSection) handleInputMode(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	k := msg.String()
	if k == "esc" {
		ss.creatingNew = false
		ss.renamingSession = false
		ss.input.Blur()
		return nil
	}
	if k == "enter" {
		name := strings.TrimSpace(ss.input.Value())
		if name == "" {
			return nil
		}
		ss.input.Blur()

		if ss.renamingSession {
			sessionID := ss.sessions[ss.selectedIdx].SessionID
			ss.renamingSession = false
			return renameSessionCmd(apiClient, sessionID, name)
		}

		// Creating new session
		ss.creatingNew = false
		ss.currentSession = name
		return func() tea.Msg { return SessionChangedMsg{SessionID: name} }
	}
	var cmd tea.Cmd
	ss.input, cmd = ss.input.Update(msg)
	return cmd
}

func (ss SessionSection) View(width, height int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Sessions"))
	lines = append(lines, "")

	// Input mode (create or rename)
	if ss.creatingNew {
		lines = append(lines, theme.SectionStyle.Render("新建 Session"))
		lines = append(lines, ss.input.View())
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render("Enter 建立  ·  Esc 取消"))
		return strings.Join(lines, "\n")
	}
	if ss.renamingSession {
		lines = append(lines, theme.SectionStyle.Render("重命名 Session"))
		lines = append(lines, ss.input.View())
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render("Enter 確認  ·  Esc 取消"))
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
			displayName := s.SessionID
			if s.Title != "" {
				displayName = s.Title
			}
			age := formatTimeAgo(s.UpdatedAt)
			label := fmt.Sprintf("%-18s %3d 訊息  %s%s", displayName, s.MessageCount, age, current)
			lines = append(lines, style.Render(clampLine(prefix+label, textW)))
		}
	}

	lines = append(lines, "")

	// Status
	if ss.statusMsg != "" {
		lines = append(lines, theme.SystemStyle.Render(sanitizeDisplayText(ss.statusMsg)))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}
