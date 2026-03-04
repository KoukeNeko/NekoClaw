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

// ---------------------------------------------------------------------------
// Memory messages
// ---------------------------------------------------------------------------

// MemorySearchMsg carries memory search results.
type MemorySearchMsg struct {
	Results []client.MemorySearchResult
	Err     error
}
