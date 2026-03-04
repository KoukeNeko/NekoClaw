package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcptypes "github.com/mark3labs/mcp-go/mcp"
)

// ConnectionStatus tracks the state of an MCP server connection.
type ConnectionStatus string

const (
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusConnecting   ConnectionStatus = "connecting"
	StatusReady        ConnectionStatus = "ready"
	StatusError        ConnectionStatus = "error"
)

// Connection wraps a single MCP server client with lifecycle and tool cache.
type Connection struct {
	config    ServerConfig
	client    mcpclient.MCPClient
	status    ConnectionStatus
	lastError error
	tools     []mcptypes.Tool
	mu        sync.RWMutex
}

// NewConnection creates a Connection for the given config (not yet connected).
func NewConnection(cfg ServerConfig) *Connection {
	return &Connection{
		config: cfg,
		status: StatusDisconnected,
	}
}

// Connect initializes the MCP client based on transport config.
func (c *Connection) Connect(ctx context.Context) error {
	c.mu.Lock()
	c.status = StatusConnecting
	c.lastError = nil
	c.mu.Unlock()

	client, err := c.createClient(ctx)
	if err != nil {
		c.mu.Lock()
		c.status = StatusError
		c.lastError = err
		c.mu.Unlock()
		return fmt.Errorf("create mcp client %q: %w", c.config.Name, err)
	}

	// Initialize the MCP protocol handshake.
	initReq := mcptypes.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcptypes.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcptypes.Implementation{
		Name:    "nekoclaw",
		Version: "1.0.0",
	}
	initReq.Params.Capabilities = mcptypes.ClientCapabilities{}

	if _, err := client.Initialize(ctx, initReq); err != nil {
		_ = client.Close()
		c.mu.Lock()
		c.status = StatusError
		c.lastError = err
		c.mu.Unlock()
		return fmt.Errorf("initialize mcp server %q: %w", c.config.Name, err)
	}

	c.mu.Lock()
	c.client = client
	c.mu.Unlock()

	// Fetch the tool list after successful init.
	if err := c.RefreshTools(ctx); err != nil {
		_ = client.Close()
		c.mu.Lock()
		c.client = nil
		c.status = StatusError
		c.lastError = err
		c.mu.Unlock()
		return fmt.Errorf("refresh tools for %q: %w", c.config.Name, err)
	}

	c.mu.Lock()
	c.status = StatusReady
	c.mu.Unlock()
	return nil
}

// Disconnect gracefully shuts down the client.
func (c *Connection) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		c.status = StatusDisconnected
		c.tools = nil
		return err
	}
	c.status = StatusDisconnected
	return nil
}

// RefreshTools calls ListTools and caches the result.
func (c *Connection) RefreshTools(ctx context.Context) error {
	c.mu.RLock()
	cl := c.client
	c.mu.RUnlock()
	if cl == nil {
		return fmt.Errorf("not connected")
	}

	result, err := cl.ListTools(ctx, mcptypes.ListToolsRequest{})
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.tools = result.Tools
	c.mu.Unlock()
	return nil
}

// Tools returns the cached tool list.
func (c *Connection) Tools() []mcptypes.Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]mcptypes.Tool(nil), c.tools...)
}

// CallTool invokes a tool on this server.
func (c *Connection) CallTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
	c.mu.RLock()
	cl := c.client
	c.mu.RUnlock()
	if cl == nil {
		return "", fmt.Errorf("not connected")
	}

	req := mcptypes.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args

	result, err := cl.CallTool(ctx, req)
	if err != nil {
		return "", err
	}

	return extractTextFromResult(result), nil
}

// Status returns the current connection status.
func (c *Connection) Status() ConnectionStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// LastError returns the last connection error, if any.
func (c *Connection) LastError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastError
}

// Config returns the server configuration.
func (c *Connection) Config() ServerConfig {
	return c.config
}

func (c *Connection) createClient(ctx context.Context) (mcpclient.MCPClient, error) {
	switch c.config.Transport {
	case TransportStdio:
		env := mapToEnvSlice(c.config.Env)
		return mcpclient.NewStdioMCPClient(c.config.Command, env, c.config.Args...)
	case TransportSSE:
		var opts []mcptransport.ClientOption
		if len(c.config.Headers) > 0 {
			opts = append(opts, mcptransport.WithHeaders(c.config.Headers))
		}
		return mcpclient.NewSSEMCPClient(c.config.URL, opts...)
	case TransportStreamableHTTP:
		var opts []mcptransport.StreamableHTTPCOption
		if len(c.config.Headers) > 0 {
			opts = append(opts, mcptransport.WithHTTPHeaders(c.config.Headers))
		}
		return mcpclient.NewStreamableHttpClient(c.config.URL, opts...)
	default:
		return nil, fmt.Errorf("unsupported transport: %s", c.config.Transport)
	}
}

// mapToEnvSlice converts a map to "KEY=VALUE" string slice.
func mapToEnvSlice(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// extractTextFromResult concatenates all text content from a CallToolResult.
func extractTextFromResult(result *mcptypes.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcptypes.TextContent); ok {
			parts = append(parts, tc.Text)
		} else {
			// For non-text content, marshal to JSON as a fallback.
			data, err := json.Marshal(c)
			if err == nil {
				parts = append(parts, string(data))
			}
		}
	}
	text := strings.Join(parts, "\n")
	if result.IsError && text != "" {
		text = "tool_error: " + text
	}
	return text
}
