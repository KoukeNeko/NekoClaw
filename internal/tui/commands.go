package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/doeshing/nekoclaw/internal/client"
	"github.com/doeshing/nekoclaw/internal/core"
)

// ---------------------------------------------------------------------------
// Clipboard
// ---------------------------------------------------------------------------

// checkClipboardImageCmd asynchronously checks the system clipboard for an image.
func checkClipboardImageCmd() tea.Cmd {
	return func() tea.Msg {
		img, err := core.LoadImageFromClipboard()
		return ClipboardImageMsg{Image: img, Err: err}
	}
}

// ---------------------------------------------------------------------------
// Chat
// ---------------------------------------------------------------------------

func sendChatCmd(apiClient *client.APIClient, req core.ChatRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		resp, err := apiClient.Chat(ctx, req)
		if err != nil && resp.SessionID == "" {
			resp.SessionID = req.SessionID
		}
		return ChatResultMsg{Response: resp, Err: err}
	}
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

func loadProvidersCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		providers, err := apiClient.Providers(ctx)
		return ProvidersMsg{Providers: providers, Err: err}
	}
}

func listAIStudioModelsCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := apiClient.ListAIStudioModels(ctx, strings.TrimSpace(profileID))
		return AIStudioModelsMsg{Response: resp, Err: err}
	}
}

func listModelsCmd(apiClient *client.APIClient, providerID, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := apiClient.ListModels(ctx, strings.TrimSpace(providerID), strings.TrimSpace(profileID))
		return ModelsListMsg{Provider: providerID, Response: resp, Err: err}
	}
}

// ---------------------------------------------------------------------------
// Fallbacks
// ---------------------------------------------------------------------------

func loadFallbacksCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		entries, err := apiClient.GetFallbacks(ctx)
		return FallbacksMsg{Fallbacks: entries, Err: err}
	}
}

func saveFallbacksCmd(apiClient *client.APIClient, entries []core.FallbackEntry) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := apiClient.SetFallbacks(ctx, entries)
		return FallbacksSavedMsg{Err: err}
	}
}

func loadFallbackModelsCmd(apiClient *client.APIClient, slotIndex int, providerID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := apiClient.ListModels(ctx, strings.TrimSpace(providerID), "")
		return FallbackModelsMsg{SlotIndex: slotIndex, Provider: providerID, Response: resp, Err: err}
	}
}

// ---------------------------------------------------------------------------
// Auth — Gemini OAuth
// ---------------------------------------------------------------------------

func startGeminiOAuthCmd(apiClient *client.APIClient, req client.GeminiAuthStartRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.StartGeminiOAuth(ctx, req)
		return AuthStartMsg{Response: resp, Err: err}
	}
}

func completeGeminiOAuthManualCmd(apiClient *client.APIClient, state, callbackURLOrCode string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		resp, err := apiClient.CompleteGeminiOAuthManual(ctx, client.GeminiAuthManualCompleteRequest{
			State:             strings.TrimSpace(state),
			CallbackURLOrCode: strings.TrimSpace(callbackURLOrCode),
		})
		return AuthManualCompleteMsg{Response: resp, Err: err}
	}
}

func listGeminiProfilesCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		profiles, err := apiClient.ListGeminiProfiles(ctx)
		return AuthProfilesMsg{Profiles: profiles, Err: err}
	}
}

func useGeminiProfileCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := apiClient.UseGeminiProfile(ctx, strings.TrimSpace(profileID))
		return AuthUseMsg{ProfileID: strings.TrimSpace(profileID), Err: err}
	}
}

// ---------------------------------------------------------------------------
// Auth — AI Studio
// ---------------------------------------------------------------------------

func addAIStudioKeyCmd(apiClient *client.APIClient, apiKey, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.AddAIStudioKey(ctx, client.AIStudioAddKeyRequest{
			APIKey:      strings.TrimSpace(apiKey),
			DisplayName: strings.TrimSpace(displayName),
		})
		return AIStudioAddKeyMsg{Response: resp, Err: err}
	}
}

func listAIStudioProfilesCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		profiles, err := apiClient.ListAIStudioProfiles(ctx)
		return AIStudioProfilesMsg{Profiles: profiles, Err: err}
	}
}

func useAIStudioProfileCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := apiClient.UseAIStudioProfile(ctx, strings.TrimSpace(profileID))
		return AIStudioProfileActionMsg{ProfileID: strings.TrimSpace(profileID), Err: err}
	}
}

func deleteAIStudioProfileCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := apiClient.DeleteAIStudioProfile(ctx, strings.TrimSpace(profileID))
		return AIStudioProfileActionMsg{ProfileID: strings.TrimSpace(profileID), Deleted: true, Err: err}
	}
}

// ---------------------------------------------------------------------------
// Auth — Anthropic
// ---------------------------------------------------------------------------

func addAnthropicTokenCmd(apiClient *client.APIClient, setupToken, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.AddAnthropicToken(ctx, client.AnthropicAddTokenRequest{
			SetupToken:  strings.TrimSpace(setupToken),
			DisplayName: strings.TrimSpace(displayName),
		})
		return AnthropicAddMsg{Response: resp, Err: err}
	}
}

func addAnthropicAPIKeyCmd(apiClient *client.APIClient, apiKey, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.AddAnthropicAPIKey(ctx, client.AnthropicAddAPIKeyRequest{
			APIKey:      strings.TrimSpace(apiKey),
			DisplayName: strings.TrimSpace(displayName),
		})
		return AnthropicAddMsg{Response: resp, Err: err}
	}
}

func listAnthropicProfilesCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		profiles, err := apiClient.ListAnthropicProfiles(ctx)
		return AnthropicProfilesMsg{Profiles: profiles, Err: err}
	}
}

func useAnthropicProfileCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := apiClient.UseAnthropicProfile(ctx, strings.TrimSpace(profileID))
		return AnthropicProfileActionMsg{ProfileID: strings.TrimSpace(profileID), Err: err}
	}
}

func deleteAnthropicProfileCmd(apiClient *client.APIClient, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := apiClient.DeleteAnthropicProfile(ctx, strings.TrimSpace(profileID))
		return AnthropicProfileActionMsg{ProfileID: strings.TrimSpace(profileID), Deleted: true, Err: err}
	}
}

func startAnthropicBrowserLoginCmd(apiClient *client.APIClient, mode string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.StartAnthropicBrowserLogin(ctx, client.AnthropicBrowserStartRequest{
			Mode:         strings.TrimSpace(mode),
			SetPreferred: true,
		})
		return AnthropicBrowserStartMsg{Response: resp, Err: err}
	}
}

func pollAnthropicBrowserLoginJobCmd(apiClient *client.APIClient, jobID string, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := apiClient.GetAnthropicBrowserLoginJob(ctx, strings.TrimSpace(jobID))
		return AnthropicBrowserJobMsg{Response: resp, Err: err}
	})
}

func completeAnthropicBrowserManualCmd(apiClient *client.APIClient, jobID, setupToken string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.CompleteAnthropicBrowserManual(ctx, client.AnthropicBrowserManualCompleteRequest{
			JobID:        strings.TrimSpace(jobID),
			SetupToken:   strings.TrimSpace(setupToken),
			SetPreferred: true,
		})
		return AnthropicAddMsg{Response: resp, Err: err}
	}
}

func cancelAnthropicBrowserLoginCmd(apiClient *client.APIClient, jobID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := apiClient.CancelAnthropicBrowserLogin(ctx, strings.TrimSpace(jobID))
		return AnthropicBrowserCancelMsg{JobID: strings.TrimSpace(jobID), Err: err}
	}
}

func addOpenAIKeyCmd(apiClient *client.APIClient, apiKey, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.AddOpenAIKey(ctx, client.OpenAIAddKeyRequest{
			APIKey:      strings.TrimSpace(apiKey),
			DisplayName: strings.TrimSpace(displayName),
		})
		return OpenAIAddMsg{Response: resp, Err: err}
	}
}

func addOpenAICodexTokenCmd(apiClient *client.APIClient, token, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.AddOpenAICodexToken(ctx, client.OpenAICodexAddTokenRequest{
			Token:       strings.TrimSpace(token),
			DisplayName: strings.TrimSpace(displayName),
		})
		return OpenAIAddMsg{Response: resp, Err: err}
	}
}

func startOpenAICodexBrowserLoginCmd(apiClient *client.APIClient, mode string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.StartOpenAICodexBrowserLogin(ctx, client.OpenAICodexBrowserStartRequest{
			Mode: strings.TrimSpace(mode),
		})
		return OpenAICodexBrowserStartMsg{Response: resp, Err: err}
	}
}

func pollOpenAICodexBrowserLoginJobCmd(apiClient *client.APIClient, jobID string, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := apiClient.GetOpenAICodexBrowserLoginJob(ctx, strings.TrimSpace(jobID))
		return OpenAICodexBrowserJobMsg{Response: resp, Err: err}
	})
}

func completeOpenAICodexBrowserManualCmd(apiClient *client.APIClient, jobID, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := apiClient.CompleteOpenAICodexBrowserManual(ctx, client.OpenAICodexBrowserManualCompleteRequest{
			JobID: strings.TrimSpace(jobID),
			Token: strings.TrimSpace(token),
		})
		return OpenAIAddMsg{Response: resp, Err: err}
	}
}

func cancelOpenAICodexBrowserLoginCmd(apiClient *client.APIClient, jobID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := apiClient.CancelOpenAICodexBrowserLogin(ctx, strings.TrimSpace(jobID))
		return OpenAICodexBrowserCancelMsg{JobID: strings.TrimSpace(jobID), Err: err}
	}
}

func listOpenAIProfilesCmd(apiClient *client.APIClient, providerID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		providerID = strings.TrimSpace(providerID)
		profiles, err := apiClient.ListOpenAIProfiles(ctx, providerID)
		return OpenAIProfilesMsg{Provider: providerID, Profiles: profiles, Err: err}
	}
}

func useOpenAIProfileCmd(apiClient *client.APIClient, providerID, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		providerID = strings.TrimSpace(providerID)
		profileID = strings.TrimSpace(profileID)
		err := apiClient.UseOpenAIProfile(ctx, providerID, profileID)
		return OpenAIProfileActionMsg{Provider: providerID, ProfileID: profileID, Err: err}
	}
}

func deleteOpenAIProfileCmd(apiClient *client.APIClient, providerID, profileID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		providerID = strings.TrimSpace(providerID)
		profileID = strings.TrimSpace(profileID)
		err := apiClient.DeleteOpenAIProfile(ctx, providerID, profileID)
		return OpenAIProfileActionMsg{Provider: providerID, ProfileID: profileID, Deleted: true, Err: err}
	}
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

func listSessionsCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sessions, err := apiClient.ListSessions(ctx)
		return SessionsListMsg{Sessions: sessions, Err: err}
	}
}

func deleteSessionCmd(apiClient *client.APIClient, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := apiClient.DeleteSession(ctx, sessionID)
		return SessionDeleteMsg{SessionID: sessionID, Err: err}
	}
}

func renameSessionCmd(apiClient *client.APIClient, sessionID, title string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := apiClient.RenameSession(ctx, strings.TrimSpace(sessionID), strings.TrimSpace(title))
		return SessionRenameMsg{SessionID: sessionID, Title: title, Err: err}
	}
}

func loadSessionTranscriptCmd(apiClient *client.APIClient, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := apiClient.GetSessionTranscript(ctx, sessionID)
		return TranscriptLoadedMsg{SessionID: sessionID, Messages: msgs, Err: err}
	}
}

// ---------------------------------------------------------------------------
// Memory
// ---------------------------------------------------------------------------

func searchMemoryCmd(apiClient *client.APIClient, query string, limit int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		results, err := apiClient.SearchMemory(ctx, query, limit)
		return MemorySearchMsg{Results: results, Err: err}
	}
}

// ---------------------------------------------------------------------------
// MCP
// ---------------------------------------------------------------------------

func listMCPServersCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		servers, sErr := apiClient.ListMCPServers(ctx)
		tools, tErr := apiClient.ListMCPTools(ctx)
		// Prefer server error; if none, use tool error.
		err := sErr
		if err == nil {
			err = tErr
		}
		return MCPServersMsg{Servers: servers, Tools: tools, Err: err}
	}
}

func listMCPBuiltinCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		servers, err := apiClient.ListMCPBuiltinServers(ctx)
		return MCPBuiltinMsg{Servers: servers, Err: err}
	}
}

func toggleMCPBuiltinCmd(apiClient *client.APIClient, name string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := apiClient.ToggleMCPBuiltin(ctx, strings.TrimSpace(name), enabled)
		return MCPBuiltinToggleMsg{Name: strings.TrimSpace(name), Enabled: enabled, Err: err}
	}
}

// ---------------------------------------------------------------------------
// Persona commands
// ---------------------------------------------------------------------------

func listPersonasCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		personas, err := apiClient.ListPersonas(ctx)
		return PersonasListMsg{Personas: personas, Err: err}
	}
}

func activePersonaCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p, err := apiClient.ActivePersona(ctx)
		return PersonaActiveMsg{Persona: p, Err: err}
	}
}

func usePersonaCmd(apiClient *client.APIClient, dirName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := apiClient.UsePersona(ctx, dirName)
		return PersonaUseMsg{DirName: dirName, Err: err}
	}
}

func clearPersonaCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := apiClient.ClearPersona(ctx)
		return PersonaClearMsg{Err: err}
	}
}

func reloadPersonasCmd(apiClient *client.APIClient) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := apiClient.ReloadPersonas(ctx)
		if err != nil {
			return PersonasListMsg{Err: err}
		}
		// After reload, fetch the updated list.
		personas, listErr := apiClient.ListPersonas(ctx)
		return PersonasListMsg{Personas: personas, Err: listErr}
	}
}
