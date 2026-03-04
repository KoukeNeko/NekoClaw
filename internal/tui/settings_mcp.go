package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

// MCPSection displays MCP server status and tool lists in the settings overlay.
type MCPSection struct {
	servers []client.MCPServerInfo
	tools   []client.MCPToolInfo
	cursor  int
	loading bool
	err     error
}

// NewMCPSection creates an empty MCP section.
func NewMCPSection() MCPSection {
	return MCPSection{}
}

// HasActiveInput returns false — this section has no text inputs.
func (ms *MCPSection) HasActiveInput() bool { return false }

// Update handles key events within the MCP section.
func (ms *MCPSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if ms.cursor > 0 {
			ms.cursor--
		}
	case "down", "j":
		if ms.cursor < len(ms.servers)-1 {
			ms.cursor++
		}
	case "r":
		// Reload server list
		ms.loading = true
		return listMCPServersCmd(apiClient)
	}
	return nil
}

// HandleServers processes the API response for MCP servers and tools.
func (ms *MCPSection) HandleServers(msg MCPServersMsg) tea.Cmd {
	ms.loading = false
	ms.err = msg.Err
	if msg.Err == nil {
		ms.servers = msg.Servers
		ms.tools = msg.Tools
	}
	// Clamp cursor
	if ms.cursor >= len(ms.servers) {
		ms.cursor = max(0, len(ms.servers)-1)
	}
	return nil
}

// View renders the MCP section content.
func (ms *MCPSection) View(width int) string {
	var lines []string

	lines = append(lines, theme.HeaderStyle.Render("MCP Servers"))
	lines = append(lines, "")

	if ms.loading {
		lines = append(lines, theme.HintStyle.Render("  載入中..."))
		return strings.Join(lines, "\n")
	}

	if ms.err != nil {
		lines = append(lines, theme.ErrorStyle.Render("  錯誤: "+ms.err.Error()))
		lines = append(lines, "")
	}

	if len(ms.servers) == 0 {
		lines = append(lines, theme.HintStyle.Render("  未設定任何 MCP 伺服器。"))
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render("  配置目錄：~/.nekoclaw/mcp/"))
		lines = append(lines, theme.HintStyle.Render("  每個伺服器一個 JSON 檔案。"))
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render("  範例 (stdio):"))
		lines = append(lines, theme.HintStyle.Render(`  {`))
		lines = append(lines, theme.HintStyle.Render(`    "name": "filesystem",`))
		lines = append(lines, theme.HintStyle.Render(`    "transport": "stdio",`))
		lines = append(lines, theme.HintStyle.Render(`    "command": "npx",`))
		lines = append(lines, theme.HintStyle.Render(`    "args": ["-y", "@anthropic/mcp-server-filesystem", "/path"],`))
		lines = append(lines, theme.HintStyle.Render(`    "trust": "trusted"`))
		lines = append(lines, theme.HintStyle.Render(`  }`))
		lines = append(lines, "")
		lines = append(lines, theme.HintStyle.Render("r 重新載入  ·  Esc 返回"))
		return strings.Join(lines, "\n")
	}

	// Render server list
	for i, srv := range ms.servers {
		prefix := "  "
		if i == ms.cursor {
			prefix = "▸ "
		}

		statusIcon := statusIndicator(srv.Status)
		trustBadge := ""
		if srv.Trust == "trusted" {
			trustBadge = " ✓"
		}

		line := fmt.Sprintf("%s%s %s [%s]%s (%d tools)",
			prefix, statusIcon, srv.Name, srv.Transport, trustBadge, srv.ToolCount)

		if i == ms.cursor {
			line = theme.SelectedStyle.Render(line)
		}
		lines = append(lines, clampLine(line, width))

		// Show error if server has one
		if srv.Error != "" && i == ms.cursor {
			errLine := fmt.Sprintf("    error: %s", srv.Error)
			lines = append(lines, theme.ErrorStyle.Render(clampLine(errLine, width)))
		}
	}
	lines = append(lines, "")

	// Show tools for the selected server
	if ms.cursor < len(ms.servers) {
		selected := ms.servers[ms.cursor]
		lines = append(lines, theme.SectionStyle.Render(fmt.Sprintf("Tools — %s", selected.Name)))
		lines = append(lines, "")

		serverTools := filterToolsByServer(ms.tools, selected.Name)
		if len(serverTools) == 0 {
			lines = append(lines, theme.HintStyle.Render("  無可用工具"))
		} else {
			for _, tool := range serverTools {
				toolLine := fmt.Sprintf("  • %s", tool.Name)
				if tool.Description != "" {
					// Truncate long descriptions
					desc := tool.Description
					maxDesc := width - len(toolLine) - 5
					if maxDesc > 10 && len(desc) > maxDesc {
						desc = desc[:maxDesc-3] + "..."
					}
					if maxDesc > 10 {
						toolLine += " — " + desc
					}
				}
				lines = append(lines, clampLine(toolLine, width))
			}
		}
		lines = append(lines, "")
	}

	lines = append(lines, theme.HintStyle.Render("↑↓ 選擇  ·  r 重新載入  ·  Esc 返回"))

	return strings.Join(lines, "\n")
}

// statusIndicator returns a colored status dot for an MCP server.
func statusIndicator(status string) string {
	switch status {
	case "ready":
		return "●"
	case "connecting":
		return "◌"
	case "error":
		return "✗"
	default:
		return "○"
	}
}

// filterToolsByServer returns tools belonging to the given server.
func filterToolsByServer(tools []client.MCPToolInfo, serverName string) []client.MCPToolInfo {
	var filtered []client.MCPToolInfo
	for _, t := range tools {
		if t.Server == serverName {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
