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
// Chat
// ---------------------------------------------------------------------------

func sendChatCmd(apiClient *client.APIClient, req core.ChatRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		resp, err := apiClient.Chat(ctx, req)
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
			Mode: strings.TrimSpace(mode),
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
			JobID:      strings.TrimSpace(jobID),
			SetupToken: strings.TrimSpace(setupToken),
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
