package tooling

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/doeshing/nekoclaw/internal/mcp"
	"github.com/doeshing/nekoclaw/internal/provider"
)

// MCPToolSource abstracts the MCP manager for the tooling package.
// This avoids a direct dependency on the mcp.Manager concrete type.
type MCPToolSource interface {
	ToolDefinitions() []provider.ToolDefinition
	HasTool(name string) bool
	IsTrusted(name string) bool
	CallTool(ctx context.Context, name string, args json.RawMessage) (string, error)
}

// CompositeExecutor routes tool calls to either the built-in executor
// or the MCP manager based on the tool name prefix.
type CompositeExecutor struct {
	builtin    *RuntimeExecutor
	mcpSource  MCPToolSource
}

// NewCompositeExecutor wraps a built-in executor and an MCP source.
func NewCompositeExecutor(builtin *RuntimeExecutor, mcpSource MCPToolSource) *CompositeExecutor {
	return &CompositeExecutor{
		builtin:   builtin,
		mcpSource: mcpSource,
	}
}

// Definitions merges built-in and MCP tool definitions.
func (c *CompositeExecutor) Definitions() []provider.ToolDefinition {
	defs := c.builtin.Definitions()
	if c.mcpSource != nil {
		defs = append(defs, c.mcpSource.ToolDefinitions()...)
	}
	return defs
}

// HasTool checks both built-in and MCP.
func (c *CompositeExecutor) HasTool(toolName string) bool {
	if mcp.IsMCPTool(toolName) {
		return c.mcpSource != nil && c.mcpSource.HasTool(toolName)
	}
	return c.builtin.HasTool(toolName)
}

// IsMutating returns true for built-in mutating tools, or for untrusted MCP tools.
// Trusted MCP servers' tools are treated as non-mutating (auto-execute).
func (c *CompositeExecutor) IsMutating(toolName string) bool {
	if mcp.IsMCPTool(toolName) {
		if c.mcpSource == nil {
			return true
		}
		return !c.mcpSource.IsTrusted(toolName)
	}
	return c.builtin.IsMutating(toolName)
}

// IsCallMutating delegates to built-in for built-in tools.
// For MCP tools, returns true for untrusted servers.
func (c *CompositeExecutor) IsCallMutating(call provider.ToolCall) bool {
	name := strings.TrimSpace(call.Name)
	if mcp.IsMCPTool(name) {
		if c.mcpSource == nil {
			return true
		}
		return !c.mcpSource.IsTrusted(name)
	}
	return c.builtin.IsCallMutating(call)
}

// Run routes to built-in executor or MCP manager.
func (c *CompositeExecutor) Run(ctx context.Context, call provider.ToolCall) (string, error) {
	if mcp.IsMCPTool(call.Name) {
		if c.mcpSource == nil {
			return "", errUnknownTool(call.Name)
		}
		return c.mcpSource.CallTool(ctx, call.Name, call.Arguments)
	}
	return c.builtin.Run(ctx, call)
}

// ArgumentPreview returns a truncated preview of the arguments for display.
func (c *CompositeExecutor) ArgumentPreview(call provider.ToolCall) string {
	return trimPreview(string(call.Arguments), 220)
}

func errUnknownTool(name string) error {
	return &provider.FailureError{
		Message: "unknown tool: " + name,
	}
}
