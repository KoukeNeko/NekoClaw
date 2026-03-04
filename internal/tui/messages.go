package tui

import (
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// ---------------------------------------------------------------------------
// Navigation messages
// ---------------------------------------------------------------------------

// SwitchViewMsg requests the root model to switch to a different view.
type SwitchViewMsg struct{ View ViewID }

// ToggleSettingsMsg requests the settings overlay to toggle visibility.
type ToggleSettingsMsg struct{}

// OpenSettingsSectionMsg opens the settings overlay to a specific section.
type OpenSettingsSectionMsg struct{ Section SettingsSection }

// StatusUpdateMsg carries updated status bar information from chat view.
type StatusUpdateMsg struct {
	ContextPercent int
	Cost           float64
	MessageCount   int
}

// ProviderChangedMsg indicates the active provider was changed.
type ProviderChangedMsg struct{ Provider string }

// ModelChangedMsg indicates the active model was changed.
type ModelChangedMsg struct{ ModelID string }

// SessionChangedMsg indicates the active session was changed.
type SessionChangedMsg struct{ SessionID string }

// ProfileChangedMsg indicates the active profile was changed.
type ProfileChangedMsg struct{ ProfileID string }

// SidebarToggleFocusMsg toggles keyboard focus between sidebar and chat.
type SidebarToggleFocusMsg struct{}

// ---------------------------------------------------------------------------
// Chat messages
// ---------------------------------------------------------------------------

// ChatResultMsg carries the result of a chat API call.
type ChatResultMsg struct {
	Response core.ChatResponse
	Err      error
}

// StreamTickMsg drives simulated streaming display.
type StreamTickMsg struct{}

// ClearChatMsg requests the chat view to clear its history.
type ClearChatMsg struct{}

// ClipboardImageMsg carries the result of an async clipboard image check.
type ClipboardImageMsg struct {
	Image core.ImageData
	Err   error
}

// ---------------------------------------------------------------------------
// Provider messages
// ---------------------------------------------------------------------------

// ProvidersMsg carries the list of available providers.
type ProvidersMsg struct {
	Providers []string
	Err       error
}

// AIStudioModelsMsg carries the list of AI Studio models.
type AIStudioModelsMsg struct {
	Response client.AIStudioModelsResponse
	Err      error
}

// ModelsListMsg carries the list of available models for any provider.
type ModelsListMsg struct {
	Provider string
	Response client.ModelsResponse
	Err      error
}

// ---------------------------------------------------------------------------
// Auth messages
// ---------------------------------------------------------------------------

// AuthStartMsg carries the result of starting a Gemini OAuth flow.
type AuthStartMsg struct {
	Response client.GeminiAuthStartResponse
	Err      error
}

// AuthManualCompleteMsg carries the result of completing OAuth manually.
type AuthManualCompleteMsg struct {
	Response client.GeminiAuthCompleteResponse
	Err      error
}

// AuthProfilesMsg carries the list of Gemini OAuth profiles.
type AuthProfilesMsg struct {
	Profiles []client.GeminiAuthProfile
	Err      error
}

// AuthUseMsg carries the result of selecting a Gemini profile.
type AuthUseMsg struct {
	ProfileID string
	Err       error
}

// ---------------------------------------------------------------------------
// AI Studio messages
// ---------------------------------------------------------------------------

// AIStudioAddKeyMsg carries the result of adding an AI Studio key.
type AIStudioAddKeyMsg struct {
	Response client.AIStudioAddKeyResponse
	Err      error
}

// AIStudioProfilesMsg carries the list of AI Studio profiles.
type AIStudioProfilesMsg struct {
	Profiles []client.AIStudioProfile
	Err      error
}

// AIStudioProfileActionMsg carries the result of a profile action (use/delete).
type AIStudioProfileActionMsg struct {
	ProfileID string
	Deleted   bool
	Err       error
}

// ---------------------------------------------------------------------------
// Anthropic messages
// ---------------------------------------------------------------------------

// AnthropicAddMsg carries the result of adding an Anthropic credential.
type AnthropicAddMsg struct {
	Response client.AnthropicAddCredentialResponse
	Err      error
}

// AnthropicProfilesMsg carries the list of Anthropic profiles.
type AnthropicProfilesMsg struct {
	Profiles []client.AnthropicProfile
	Err      error
}

// AnthropicProfileActionMsg carries the result of Anthropic profile actions.
type AnthropicProfileActionMsg struct {
	ProfileID string
	Deleted   bool
	Err       error
}

// AnthropicBrowserStartMsg carries the result of starting browser login bridge.
type AnthropicBrowserStartMsg struct {
	Response client.AnthropicBrowserStartResponse
	Err      error
}

// AnthropicBrowserJobMsg carries the polled state for a browser login job.
type AnthropicBrowserJobMsg struct {
	Response client.AnthropicBrowserJobResponse
	Err      error
}

// AnthropicBrowserCancelMsg carries the result of cancelling a browser login job.
type AnthropicBrowserCancelMsg struct {
	JobID string
	Err   error
}

// ---------------------------------------------------------------------------
// OpenAI messages
// ---------------------------------------------------------------------------

// OpenAIAddMsg carries the result of adding an OpenAI credential.
type OpenAIAddMsg struct {
	Response client.OpenAIAddCredentialResponse
	Err      error
}

// OpenAIProfilesMsg carries the list of OpenAI/OpenAI-Codex profiles.
type OpenAIProfilesMsg struct {
	Provider string
	Profiles []client.OpenAIProfile
	Err      error
}

// OpenAIProfileActionMsg carries the result of OpenAI profile actions.
type OpenAIProfileActionMsg struct {
	Provider  string
	ProfileID string
	Deleted   bool
	Err       error
}

// OpenAICodexBrowserStartMsg carries the result of starting OpenAI Codex browser login.
type OpenAICodexBrowserStartMsg struct {
	Response client.OpenAICodexBrowserStartResponse
	Err      error
}

// OpenAICodexBrowserJobMsg carries the polled state for OpenAI Codex browser login.
type OpenAICodexBrowserJobMsg struct {
	Response client.OpenAICodexBrowserJobResponse
	Err      error
}

// OpenAICodexBrowserCancelMsg carries the result of cancelling OpenAI Codex browser login.
type OpenAICodexBrowserCancelMsg struct {
	JobID string
	Err   error
}

// ---------------------------------------------------------------------------
// Session messages
// ---------------------------------------------------------------------------

// SessionsListMsg carries the list of sessions.
type SessionsListMsg struct {
	Sessions []client.SessionInfo
	Err      error
}

// SessionDeleteMsg carries the result of deleting a session.
type SessionDeleteMsg struct {
	SessionID string
	Err       error
}

// SessionRenameMsg carries the result of renaming a session.
type SessionRenameMsg struct {
	SessionID string
	Title     string
	Err       error
}

// TranscriptLoadedMsg carries the loaded transcript for a session.
type TranscriptLoadedMsg struct {
	SessionID string
	Messages  []client.TranscriptMessage
	Err       error
}

// refreshSessionsTickMsg triggers a delayed session list reload.
type refreshSessionsTickMsg struct{}

// ---------------------------------------------------------------------------
// Memory messages
// ---------------------------------------------------------------------------

// MemorySearchMsg carries memory search results.
type MemorySearchMsg struct {
	Results []client.MemorySearchResult
	Err     error
}

// ---------------------------------------------------------------------------
// MCP messages
// ---------------------------------------------------------------------------

// MCPServersMsg carries MCP server and tool info for the settings tab.
type MCPServersMsg struct {
	Servers []client.MCPServerInfo
	Tools   []client.MCPToolInfo
	Err     error
}

// MCPBuiltinMsg carries builtin MCP server info for the settings tab.
type MCPBuiltinMsg struct {
	Servers []client.MCPBuiltinInfo
	Err     error
}

// MCPBuiltinToggleMsg carries the result of toggling a builtin MCP server.
type MCPBuiltinToggleMsg struct {
	Name    string
	Enabled bool
	Err     error
}

// ---------------------------------------------------------------------------
// Persona messages
// ---------------------------------------------------------------------------

// PersonasListMsg carries the list of available personas.
type PersonasListMsg struct {
	Personas []client.PersonaInfo
	Err      error
}

// PersonaActiveMsg carries the currently active persona (may be nil).
type PersonaActiveMsg struct {
	Persona *client.PersonaInfo
	Err     error
}

// PersonaUseMsg carries the result of activating a persona.
type PersonaUseMsg struct {
	DirName string
	Err     error
}

// PersonaClearMsg carries the result of deactivating a persona.
type PersonaClearMsg struct {
	Err error
}

// PersonaChangedMsg notifies the TUI that the active persona changed.
type PersonaChangedMsg struct {
	Name string
}
