package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	mcptypes "github.com/mark3labs/mcp-go/mcp"

	"github.com/doeshing/nekoclaw/internal/logger"
	"github.com/doeshing/nekoclaw/internal/provider"
)

var logMCP = logger.New("mcp", logger.Green)

// ServerInfo exposes read-only status about an MCP server for display.
type ServerInfo struct {
	Name      string           `json:"name"`
	Transport TransportType    `json:"transport"`
	Trust     TrustLevel       `json:"trust"`
	Status    ConnectionStatus `json:"status"`
	Error     string           `json:"error,omitempty"`
	ToolCount int              `json:"tool_count"`
	Builtin   bool             `json:"builtin"`
}

// ToolInfo exposes MCP tool metadata for API/TUI display.
type ToolInfo struct {
	Server      string `json:"server"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Manager coordinates all MCP server connections.
type Manager struct {
	configDir    string
	connections  map[string]*Connection
	builtinState map[string]bool
	mu           sync.RWMutex
}

// NewManager creates a Manager that loads config from configDir.
func NewManager(configDir string) *Manager {
	return &Manager{
		configDir:    configDir,
		connections:  make(map[string]*Connection),
		builtinState: make(map[string]bool),
	}
}

// Start loads all configs and connects to all servers.
// Individual server failures are logged but do not prevent others from starting.
func (m *Manager) Start(ctx context.Context) error {
	// Load builtin state from disk.
	state, err := loadBuiltinState(m.configDir)
	if err != nil {
		logMCP.Errorf("builtin state error: %v", err)
		state = map[string]bool{}
	}
	m.mu.Lock()
	m.builtinState = state
	m.mu.Unlock()

	// Build combined config list: enabled builtins + user configs.
	var allConfigs []ServerConfig
	for _, def := range BuiltinDefs() {
		if isBuiltinEnabled(state, def.Name) {
			allConfigs = append(allConfigs, def.Config)
		}
	}

	userConfigs, errs := LoadConfigs(m.configDir)
	for _, err := range errs {
		logMCP.Errorf("config error: %v", err)
	}
	allConfigs = append(allConfigs, userConfigs...)

	if len(allConfigs) == 0 {
		return nil
	}

	// Detect duplicate server names.
	seen := map[string]bool{}
	for _, cfg := range allConfigs {
		name := strings.TrimSpace(cfg.Name)
		if seen[name] {
			logMCP.Warnf("duplicate name: %s", name)
			continue
		}
		seen[name] = true

		conn := NewConnection(cfg)
		m.mu.Lock()
		m.connections[name] = conn
		m.mu.Unlock()

		if err := conn.Connect(ctx); err != nil {
			logMCP.Errorf("connect error: server=%s error=%v", name, err)
		} else {
			logMCP.Logf("connected: server=%s transport=%s trust=%s tools=%d",
				name, cfg.Transport, cfg.Trust, len(conn.Tools()))
		}
	}
	return nil
}

// Stop disconnects all servers gracefully.
func (m *Manager) Stop() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var firstErr error
	for name, conn := range m.connections {
		if err := conn.Disconnect(); err != nil {
			logMCP.Errorf("disconnect error: server=%s error=%v", name, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Servers returns status info for all configured servers.
func (m *Manager) Servers() []ServerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]ServerInfo, 0, len(m.connections))
	for _, conn := range m.connections {
		info := ServerInfo{
			Name:      conn.Config().Name,
			Transport: conn.Config().Transport,
			Trust:     conn.Config().Trust,
			Status:    conn.Status(),
			ToolCount: len(conn.Tools()),
			Builtin:   conn.Config().Builtin,
		}
		if err := conn.LastError(); err != nil {
			info.Error = err.Error()
		}
		infos = append(infos, info)
	}
	return infos
}

// ToolDefinitions returns provider.ToolDefinition for all tools from all connected servers.
// Tool names are prefixed with "mcp__<serverName>__".
func (m *Manager) ToolDefinitions() []provider.ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var defs []provider.ToolDefinition
	for _, conn := range m.connections {
		if conn.Status() != StatusReady {
			continue
		}
		serverName := conn.Config().Name
		for _, tool := range conn.Tools() {
			schema := marshalInputSchema(tool)
			defs = append(defs, provider.ToolDefinition{
				Name:        NamespacedToolName(serverName, tool.Name),
				Description: fmt.Sprintf("[MCP: %s] %s", serverName, tool.Description),
				InputSchema: schema,
			})
		}
	}
	return defs
}

// ToolInfos returns metadata for all tools across all connected servers.
func (m *Manager) ToolInfos() []ToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var infos []ToolInfo
	for _, conn := range m.connections {
		if conn.Status() != StatusReady {
			continue
		}
		serverName := conn.Config().Name
		for _, tool := range conn.Tools() {
			infos = append(infos, ToolInfo{
				Server:      serverName,
				Name:        tool.Name,
				Description: tool.Description,
			})
		}
	}
	return infos
}

// HasTool returns true if the namespaced tool name belongs to any connected server.
func (m *Manager) HasTool(namespacedName string) bool {
	serverName, _, isMCP := ParseNamespacedTool(namespacedName)
	if !isMCP {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.connections[serverName]
	return ok && conn.Status() == StatusReady
}

// IsTrusted returns true if the server owning this tool is trusted.
func (m *Manager) IsTrusted(namespacedName string) bool {
	serverName, _, isMCP := ParseNamespacedTool(namespacedName)
	if !isMCP {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.connections[serverName]
	if !ok {
		return false
	}
	return conn.Config().Trust == TrustTrusted
}

// CallTool routes a namespaced tool call to the correct server.
func (m *Manager) CallTool(ctx context.Context, namespacedName string, rawArgs json.RawMessage) (string, error) {
	serverName, toolName, isMCP := ParseNamespacedTool(namespacedName)
	if !isMCP {
		return "", fmt.Errorf("not an MCP tool: %s", namespacedName)
	}

	m.mu.RLock()
	conn, ok := m.connections[serverName]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("mcp server %q not found", serverName)
	}
	if conn.Status() != StatusReady {
		return "", fmt.Errorf("mcp server %q not ready (status: %s)", serverName, conn.Status())
	}

	// Unmarshal raw JSON arguments to map[string]any.
	var args map[string]any
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("unmarshal arguments: %w", err)
		}
	}

	return conn.CallTool(ctx, toolName, args)
}

// Reconnect attempts to reconnect a failed server.
func (m *Manager) Reconnect(ctx context.Context, serverName string) error {
	m.mu.RLock()
	conn, ok := m.connections[serverName]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("mcp server %q not found", serverName)
	}
	_ = conn.Disconnect()
	return conn.Connect(ctx)
}

// BuiltinServers returns status info for all builtin servers (enabled and disabled).
func (m *Manager) BuiltinServers() []BuiltinServerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	defs := BuiltinDefs()
	infos := make([]BuiltinServerInfo, 0, len(defs))
	for _, def := range defs {
		enabled := isBuiltinEnabled(m.builtinState, def.Name)
		info := BuiltinServerInfo{
			Name:        def.Name,
			Description: def.Description,
			Enabled:     enabled,
			Status:      StatusDisconnected,
		}
		if conn, ok := m.connections[def.Name]; ok {
			info.Status = conn.Status()
			info.ToolCount = len(conn.Tools())
			if lastErr := conn.LastError(); lastErr != nil {
				info.Error = lastErr.Error()
			}
		}
		infos = append(infos, info)
	}
	return infos
}

// SetBuiltinEnabled toggles a builtin server on or off, persists the state,
// and connects or disconnects accordingly.
func (m *Manager) SetBuiltinEnabled(ctx context.Context, name string, enabled bool) error {
	// Verify the name belongs to a registered builtin.
	var def *BuiltinServerDef
	for _, d := range BuiltinDefs() {
		if d.Name == name {
			def = &d
			break
		}
	}
	if def == nil {
		return fmt.Errorf("unknown builtin server: %s", name)
	}

	m.mu.Lock()
	m.builtinState[name] = enabled
	stateCopy := make(map[string]bool, len(m.builtinState))
	for k, v := range m.builtinState {
		stateCopy[k] = v
	}
	m.mu.Unlock()

	// Persist state to disk.
	if err := saveBuiltinState(m.configDir, stateCopy); err != nil {
		logMCP.Errorf("builtin save error: %v", err)
		return fmt.Errorf("save builtin state: %w", err)
	}

	if enabled {
		conn := NewConnection(def.Config)
		m.mu.Lock()
		m.connections[name] = conn
		m.mu.Unlock()
		if err := conn.Connect(ctx); err != nil {
			logMCP.Errorf("connect error: server=%s error=%v", name, err)
			return err
		}
		logMCP.Logf("builtin enabled: server=%s", name)
	} else {
		m.mu.Lock()
		conn, ok := m.connections[name]
		if ok {
			delete(m.connections, name)
		}
		m.mu.Unlock()
		if ok {
			_ = conn.Disconnect()
		}
		logMCP.Logf("builtin disabled: server=%s", name)
	}
	return nil
}

// marshalInputSchema converts a Tool's InputSchema to json.RawMessage.
func marshalInputSchema(tool mcptypes.Tool) json.RawMessage {
	// Prefer RawInputSchema if available (arbitrary JSON).
	if len(tool.RawInputSchema) > 0 {
		return tool.RawInputSchema
	}
	data, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return data
}
