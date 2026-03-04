package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
)

// MCPSection displays MCP server status and tool lists in the settings overlay.
// It separates builtin servers (with enable/disable toggle) from user-defined ones.
type MCPSection struct {
	builtins []client.MCPBuiltinInfo
	servers  []client.MCPServerInfo
	tools    []client.MCPToolInfo
	cursor   int
	loading  bool
	err      error
}

// NewMCPSection creates an empty MCP section.
func NewMCPSection() MCPSection {
	return MCPSection{}
}

// HasActiveInput returns false — this section has no text inputs.
func (ms *MCPSection) HasActiveInput() bool { return false }

// customServers returns only user-defined (non-builtin) servers.
func (ms *MCPSection) customServers() []client.MCPServerInfo {
	var custom []client.MCPServerInfo
	for _, srv := range ms.servers {
		if !srv.Builtin {
			custom = append(custom, srv)
		}
	}
	return custom
}

// totalItems returns the total number of selectable items across both categories.
func (ms *MCPSection) totalItems() int {
	return len(ms.builtins) + len(ms.customServers())
}

// clampCursor ensures cursor is within bounds.
func (ms *MCPSection) clampCursor() {
	total := ms.totalItems()
	if ms.cursor >= total {
		ms.cursor = max(0, total-1)
	}
}

// Update handles key events within the MCP section.
func (ms *MCPSection) Update(msg tea.KeyMsg, apiClient *client.APIClient) tea.Cmd {
	total := ms.totalItems()

	switch msg.String() {
	case "up", "k":
		if ms.cursor > 0 {
			ms.cursor--
		}
	case "down", "j":
		if ms.cursor < total-1 {
			ms.cursor++
		}
	case "enter":
		// Toggle is only available for builtin servers.
		if ms.cursor < len(ms.builtins) {
			b := ms.builtins[ms.cursor]
			return toggleMCPBuiltinCmd(apiClient, b.Name, !b.Enabled)
		}
	case "r":
		ms.loading = true
		return tea.Batch(
			listMCPServersCmd(apiClient),
			listMCPBuiltinCmd(apiClient),
		)
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
	ms.clampCursor()
	return nil
}

// HandleBuiltins processes the API response for builtin MCP servers.
func (ms *MCPSection) HandleBuiltins(msg MCPBuiltinMsg) tea.Cmd {
	ms.loading = false
	if msg.Err != nil {
		ms.err = msg.Err
		return nil
	}
	ms.builtins = msg.Servers
	ms.clampCursor()
	return nil
}

// HandleBuiltinToggle processes the result of toggling a builtin server.
func (ms *MCPSection) HandleBuiltinToggle(msg MCPBuiltinToggleMsg) tea.Cmd {
	if msg.Err != nil {
		ms.err = msg.Err
		return nil
	}
	// Update local state immediately for responsive UI.
	for i, b := range ms.builtins {
		if b.Name == msg.Name {
			ms.builtins[i].Enabled = msg.Enabled
			if !msg.Enabled {
				ms.builtins[i].Status = "disconnected"
				ms.builtins[i].ToolCount = 0
			}
			break
		}
	}
	return nil
}

// View renders the MCP section content.
func (ms *MCPSection) View(width int) string {
	textW := width - 4
	if textW < 10 {
		textW = 10
	}

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

	custom := ms.customServers()
	total := len(ms.builtins) + len(custom)

	if total == 0 {
		ms.renderEmptyState(&lines)
		return strings.Join(lines, "\n")
	}

	// ── 內建 (Builtin) ──
	if len(ms.builtins) > 0 {
		lines = append(lines, theme.SectionStyle.Render("── 內建 ──"))
		lines = append(lines, "")
		for i, b := range ms.builtins {
			prefix := "  "
			if i == ms.cursor {
				prefix = "▸ "
			}

			statusIcon := "○"
			enableBadge := "✗ 已停用"
			if b.Enabled {
				statusIcon = statusIndicator(b.Status)
				enableBadge = "✓ 已啟用"
			}

			line := fmt.Sprintf("%s%s %s  %s (%d tools)",
				prefix, statusIcon, b.Name, enableBadge, b.ToolCount)

			if i == ms.cursor {
				line = theme.SelectedStyle.Render(line)
			}
			lines = append(lines, clampLine(line, textW))

			// Show description for selected builtin.
			if i == ms.cursor && b.Description != "" {
				descLine := fmt.Sprintf("    %s", b.Description)
				lines = append(lines, theme.HintStyle.Render(clampLine(descLine, textW)))
			}

			// Show error for selected builtin.
			if b.Error != "" && i == ms.cursor {
				errLine := fmt.Sprintf("    error: %s", b.Error)
				lines = append(lines, theme.ErrorStyle.Render(clampLine(errLine, textW)))
			}
		}
		lines = append(lines, "")
	}

	// ── 自訂 (Custom) ──
	if len(custom) > 0 {
		lines = append(lines, theme.SectionStyle.Render("── 自訂 ──"))
		lines = append(lines, "")
		for i, srv := range custom {
			globalIdx := len(ms.builtins) + i
			prefix := "  "
			if globalIdx == ms.cursor {
				prefix = "▸ "
			}

			statusIcon := statusIndicator(srv.Status)
			trustBadge := ""
			if srv.Trust == "trusted" {
				trustBadge = " ✓"
			}

			line := fmt.Sprintf("%s%s %s [%s]%s (%d tools)",
				prefix, statusIcon, srv.Name, srv.Transport, trustBadge, srv.ToolCount)

			if globalIdx == ms.cursor {
				line = theme.SelectedStyle.Render(line)
			}
			lines = append(lines, clampLine(line, textW))

			// Show error for selected server.
			if srv.Error != "" && globalIdx == ms.cursor {
				errLine := fmt.Sprintf("    error: %s", srv.Error)
				lines = append(lines, theme.ErrorStyle.Render(clampLine(errLine, textW)))
			}
		}
		lines = append(lines, "")
	}

	// Show tools for the selected server.
	ms.renderSelectedTools(&lines, custom, textW)

	lines = append(lines, theme.HintStyle.Render("↑↓ 選擇  ·  Enter 切換啟用  ·  r 重新載入  ·  Esc 返回"))

	return strings.Join(lines, "\n")
}

// renderEmptyState renders help text when no servers are configured.
func (ms *MCPSection) renderEmptyState(lines *[]string) {
	*lines = append(*lines, theme.HintStyle.Render("  未設定任何 MCP 伺服器。"))
	*lines = append(*lines, "")
	*lines = append(*lines, theme.HintStyle.Render("  配置目錄：~/.nekoclaw/mcp/"))
	*lines = append(*lines, theme.HintStyle.Render("  每個伺服器一個 JSON 檔案。"))
	*lines = append(*lines, "")
	*lines = append(*lines, theme.HintStyle.Render("  範例 (stdio):"))
	*lines = append(*lines, theme.HintStyle.Render(`  {`))
	*lines = append(*lines, theme.HintStyle.Render(`    "name": "filesystem",`))
	*lines = append(*lines, theme.HintStyle.Render(`    "transport": "stdio",`))
	*lines = append(*lines, theme.HintStyle.Render(`    "command": "npx",`))
	*lines = append(*lines, theme.HintStyle.Render(`    "args": ["-y", "@anthropic/mcp-server-filesystem", "/path"],`))
	*lines = append(*lines, theme.HintStyle.Render(`    "trust": "trusted"`))
	*lines = append(*lines, theme.HintStyle.Render(`  }`))
	*lines = append(*lines, "")
	*lines = append(*lines, theme.HintStyle.Render("r 重新載入  ·  Esc 返回"))
}

// renderSelectedTools shows tools for the currently selected server.
func (ms *MCPSection) renderSelectedTools(lines *[]string, custom []client.MCPServerInfo, width int) {
	if ms.cursor < len(ms.builtins) {
		selected := ms.builtins[ms.cursor]
		if !selected.Enabled {
			return
		}
		*lines = append(*lines, theme.SectionStyle.Render(fmt.Sprintf("Tools — %s", selected.Name)))
		*lines = append(*lines, "")
		serverTools := filterToolsByServer(ms.tools, selected.Name)
		renderToolItems(lines, serverTools, width)
		return
	}

	idx := ms.cursor - len(ms.builtins)
	if idx < len(custom) {
		selected := custom[idx]
		*lines = append(*lines, theme.SectionStyle.Render(fmt.Sprintf("Tools — %s", selected.Name)))
		*lines = append(*lines, "")
		serverTools := filterToolsByServer(ms.tools, selected.Name)
		renderToolItems(lines, serverTools, width)
	}
}

// renderToolItems renders a list of MCP tools.
func renderToolItems(lines *[]string, tools []client.MCPToolInfo, width int) {
	if len(tools) == 0 {
		*lines = append(*lines, theme.HintStyle.Render("  無可用工具"))
		*lines = append(*lines, "")
		return
	}
	for _, tool := range tools {
		toolLine := fmt.Sprintf("  • %s", tool.Name)
		if tool.Description != "" {
			desc := tool.Description
			maxDesc := width - len(toolLine) - 5
			if maxDesc > 10 && len(desc) > maxDesc {
				desc = desc[:maxDesc-3] + "..."
			}
			if maxDesc > 10 {
				toolLine += " — " + desc
			}
		}
		*lines = append(*lines, clampLine(toolLine, width))
	}
	*lines = append(*lines, "")
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
