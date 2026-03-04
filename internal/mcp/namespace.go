package mcp

import "strings"

const (
	namespacePrefix    = "mcp__"
	namespaceSeparator = "__"
)

// NamespacedToolName creates a namespaced tool name: mcp__<server>__<tool>.
func NamespacedToolName(serverName, toolName string) string {
	return namespacePrefix + serverName + namespaceSeparator + toolName
}

// ParseNamespacedTool splits a namespaced name into server and tool components.
// Returns isMCP=false if the name does not have the MCP namespace prefix.
func ParseNamespacedTool(namespacedName string) (serverName, toolName string, isMCP bool) {
	if !strings.HasPrefix(namespacedName, namespacePrefix) {
		return "", namespacedName, false
	}
	rest := namespacedName[len(namespacePrefix):]
	idx := strings.Index(rest, namespaceSeparator)
	if idx < 0 {
		return "", namespacedName, false
	}
	return rest[:idx], rest[idx+len(namespaceSeparator):], true
}

// IsMCPTool returns true if the tool name has the MCP namespace prefix.
func IsMCPTool(name string) bool {
	return strings.HasPrefix(name, namespacePrefix)
}
