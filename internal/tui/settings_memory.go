package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

// MemorySection handles memory search.
type MemorySection struct {
	input   textinput.Model
	results []client.MemorySearchResult
	loaded  bool
	query   string
	err     error
}

func NewMemorySection() MemorySection {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Prompt = "🔍 "
	ti.Placeholder = "輸入搜尋關鍵字..."
	return MemorySection{input: ti}
}

func (ms *MemorySection) Focus() tea.Cmd {
	return ms.input.Focus()
}

// HasActiveInput returns true when the search input has content,
// preventing ←→ from switching sections while editing a query.
func (ms *MemorySection) HasActiveInput() bool {
	return ms.input.Value() != ""
}

func (ms *MemorySection) HandleSearchResults(msg MemorySearchMsg) tea.Cmd {
	ms.loaded = true
	if msg.Err != nil {
		ms.err = msg.Err
		ms.results = nil
		return nil
	}
	ms.err = nil
	ms.results = msg.Results
	return nil
}

func (ms *MemorySection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	if key.Matches(msg, key.NewBinding(key.WithKeys("enter"))) {
		query := strings.TrimSpace(ms.input.Value())
		if query != "" {
			ms.query = query
			ms.loaded = false
			return searchMemoryCmd(apiClient, query, 10)
		}
		return nil
	}

	var cmd tea.Cmd
	ms.input, cmd = ms.input.Update(msg)
	return cmd
}

func (ms MemorySection) View(width, height int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

	var lines []string
	lines = append(lines, theme.HeaderStyle.Render("Memory Search"))
	lines = append(lines, "")
	lines = append(lines, ms.input.View())
	lines = append(lines, "")

	if ms.query != "" && !ms.loaded {
		lines = append(lines, theme.HintStyle.Render("搜尋中..."))
	} else if ms.err != nil {
		lines = append(lines, theme.ErrorStyle.Render(sanitizeDisplayText("搜尋失敗: "+ms.err.Error())))
	} else if ms.loaded && len(ms.results) == 0 {
		lines = append(lines, theme.HintStyle.Render("無結果。"))
	} else if ms.loaded {
		lines = append(lines, theme.SectionStyle.Render(fmt.Sprintf("結果 (%d)", len(ms.results))))
		lines = append(lines, "")
		for i, r := range ms.results {
			if i >= 10 {
				break
			}
			score := fmt.Sprintf("%.1f", r.Score)
			header := fmt.Sprintf("[%s] %s  score=%s", r.Role, r.SessionID, score)
			lines = append(lines, theme.SystemStyle.Render(clampLine(header, textW)))

			content := strings.TrimSpace(r.Content)
			if len([]rune(content)) > 120 {
				content = string([]rune(content)[:120]) + "…"
			}
			lines = append(lines, theme.NormalStyle.Render(clampLine("  "+content, textW)))
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}
