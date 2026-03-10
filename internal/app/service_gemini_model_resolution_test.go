package app

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type geminiResolutionProvider struct {
	mu             sync.Mutex
	models         []string
	listErr        error
	generateReqs   []provider.GenerateRequest
	streamReqs     []provider.GenerateRequest
	toolTurnReqs   []provider.ToolTurnRequest
	streamEndpoint string
}

func (p *geminiResolutionProvider) ID() string {
	return "google-gemini-cli"
}

func (p *geminiResolutionProvider) ContextWindow(string) int {
	return 1_000_000
}

func (p *geminiResolutionProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	p.mu.Lock()
	p.generateReqs = append(p.generateReqs, req)
	p.mu.Unlock()
	return provider.GenerateResponse{
		Text:     "ok",
		Endpoint: "https://example.invalid/generate",
	}, nil
}

func (p *geminiResolutionProvider) GenerateStream(_ context.Context, req provider.GenerateRequest) (<-chan provider.GenerateStreamChunk, error) {
	p.mu.Lock()
	p.streamReqs = append(p.streamReqs, req)
	endpoint := p.streamEndpoint
	p.mu.Unlock()

	if endpoint == "" {
		endpoint = "https://example.invalid/stream"
	}
	ch := make(chan provider.GenerateStreamChunk, 2)
	ch <- provider.GenerateStreamChunk{Text: "stream"}
	ch <- provider.GenerateStreamChunk{
		Done:     true,
		Endpoint: endpoint,
	}
	close(ch)
	return ch, nil
}

func (p *geminiResolutionProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{
		SupportsTools:         true,
		SupportsParallelCalls: true,
		MaxToolCalls:          8,
	}
}

func (p *geminiResolutionProvider) GenerateToolTurn(_ context.Context, req provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	p.mu.Lock()
	p.toolTurnReqs = append(p.toolTurnReqs, req)
	p.mu.Unlock()
	return provider.ToolTurnResponse{
		Text:     "tool",
		Endpoint: "https://example.invalid/tool",
	}, nil
}

func (p *geminiResolutionProvider) ListModels(_ context.Context, _ core.Account) ([]string, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return append([]string(nil), p.models...), nil
}

func (p *geminiResolutionProvider) lastGenerateRequest() provider.GenerateRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.generateReqs) == 0 {
		return provider.GenerateRequest{}
	}
	return p.generateReqs[len(p.generateReqs)-1]
}

func (p *geminiResolutionProvider) lastStreamRequest() provider.GenerateRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.streamReqs) == 0 {
		return provider.GenerateRequest{}
	}
	return p.streamReqs[len(p.streamReqs)-1]
}

func (p *geminiResolutionProvider) lastToolTurnRequest() provider.ToolTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.toolTurnReqs) == 0 {
		return provider.ToolTurnRequest{}
	}
	return p.toolTurnReqs[len(p.toolTurnReqs)-1]
}

func newGeminiResolutionService(t *testing.T, prov *geminiResolutionProvider) *Service {
	t.Helper()
	svc := NewService(ServiceOptions{})
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("google-gemini-cli", []core.Account{
		{
			ID:       "g1",
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    "token-g1",
			Metadata: core.Metadata{
				"project_id": "proj-1",
			},
		},
	}, []string{"g1"}, core.DefaultCooldownConfig()))
	return svc
}

func TestHandleChatDowngradesExplicitGemini31ToLegacy3ProWhenUnavailable(t *testing.T) {
	svc := newGeminiResolutionService(t, &geminiResolutionProvider{
		models: []string{"gemini-3-pro-preview", "gemini-2.5-pro"},
	})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-runtime-downgrade",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-gemini-cli",
		Model:          "gemini-3.1-pro-preview",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Model != "gemini-3-pro-preview" {
		t.Fatalf("ChatResponse.Model = %q, want %q", resp.Model, "gemini-3-pro-preview")
	}

	prov := svc.providers["google-gemini-cli"].(*geminiResolutionProvider)
	if got := prov.lastGenerateRequest().Model; got != "gemini-3-pro-preview" {
		t.Fatalf("GenerateRequest.Model = %q, want %q", got, "gemini-3-pro-preview")
	}
}

func TestHandleChatKeepsExplicitGemini31WhenAvailable(t *testing.T) {
	svc := newGeminiResolutionService(t, &geminiResolutionProvider{
		models: []string{"gemini-3.1-pro-preview", "gemini-3-pro-preview"},
	})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-runtime-keep-31",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-gemini-cli",
		Model:          "gemini-3.1-pro-preview",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Model != "gemini-3.1-pro-preview" {
		t.Fatalf("ChatResponse.Model = %q, want %q", resp.Model, "gemini-3.1-pro-preview")
	}

	prov := svc.providers["google-gemini-cli"].(*geminiResolutionProvider)
	if got := prov.lastGenerateRequest().Model; got != "gemini-3.1-pro-preview" {
		t.Fatalf("GenerateRequest.Model = %q, want %q", got, "gemini-3.1-pro-preview")
	}
}

func TestHandleChatKeepsExplicitGemini31WhenModelListingFails(t *testing.T) {
	svc := newGeminiResolutionService(t, &geminiResolutionProvider{
		listErr: fmt.Errorf("list models failed"),
	})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-runtime-list-error",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-gemini-cli",
		Model:          "gemini-3.1-pro-preview",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Model != "gemini-3.1-pro-preview" {
		t.Fatalf("ChatResponse.Model = %q, want %q", resp.Model, "gemini-3.1-pro-preview")
	}

	prov := svc.providers["google-gemini-cli"].(*geminiResolutionProvider)
	if got := prov.lastGenerateRequest().Model; got != "gemini-3.1-pro-preview" {
		t.Fatalf("GenerateRequest.Model = %q, want %q", got, "gemini-3.1-pro-preview")
	}
}

func TestHandleChatKeepsExplicitGemini31WhenLegacy3ProUnavailable(t *testing.T) {
	svc := newGeminiResolutionService(t, &geminiResolutionProvider{
		models: []string{"gemini-2.5-pro"},
	})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-runtime-no-legacy",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-gemini-cli",
		Model:          "gemini-3.1-pro-preview",
		Message:        "hello",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Model != "gemini-3.1-pro-preview" {
		t.Fatalf("ChatResponse.Model = %q, want %q", resp.Model, "gemini-3.1-pro-preview")
	}

	prov := svc.providers["google-gemini-cli"].(*geminiResolutionProvider)
	if got := prov.lastGenerateRequest().Model; got != "gemini-3.1-pro-preview" {
		t.Fatalf("GenerateRequest.Model = %q, want %q", got, "gemini-3.1-pro-preview")
	}
}

func TestHandleChatWithToolsUsesResolvedGeminiModel(t *testing.T) {
	svc := newGeminiResolutionService(t, &geminiResolutionProvider{
		models: []string{"gemini-3-pro-preview"},
	})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "gemini-runtime-tools",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-gemini-cli",
		Model:          "gemini-3.1-pro-preview",
		Message:        "hello",
		EnableTools:    true,
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Model != "gemini-3-pro-preview" {
		t.Fatalf("ChatResponse.Model = %q, want %q", resp.Model, "gemini-3-pro-preview")
	}

	prov := svc.providers["google-gemini-cli"].(*geminiResolutionProvider)
	if got := prov.lastToolTurnRequest().Model; got != "gemini-3-pro-preview" {
		t.Fatalf("ToolTurnRequest.Model = %q, want %q", got, "gemini-3-pro-preview")
	}
}

func TestHandleChatStreamUsesResolvedGeminiModel(t *testing.T) {
	svc := newGeminiResolutionService(t, &geminiResolutionProvider{
		models: []string{"gemini-3-pro-preview"},
	})

	var done *core.ChatResponse
	for chunk := range svc.HandleChatStream(context.Background(), core.ChatRequest{
		SessionID:      "gemini-runtime-stream",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-gemini-cli",
		Model:          "gemini-3.1-pro-preview",
		Message:        "hello",
	}) {
		if chunk.Type == core.ChunkError {
			t.Fatalf("unexpected stream error: %s", chunk.Error)
		}
		if chunk.Type == core.ChunkDone {
			done = chunk.Response
		}
	}
	if done == nil {
		t.Fatal("expected done response")
	}
	if done.Model != "gemini-3-pro-preview" {
		t.Fatalf("ChatResponse.Model = %q, want %q", done.Model, "gemini-3-pro-preview")
	}

	prov := svc.providers["google-gemini-cli"].(*geminiResolutionProvider)
	if got := prov.lastStreamRequest().Model; got != "gemini-3-pro-preview" {
		t.Fatalf("GenerateStream request model = %q, want %q", got, "gemini-3-pro-preview")
	}
}

func TestListModelsGeminiFallbackIncludesLegacy3Pro(t *testing.T) {
	svc := NewService(ServiceOptions{})

	result, err := svc.ListModels(context.Background(), "google-gemini-cli", "")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if result.Source != "fallback" {
		t.Fatalf("ModelsResult.Source = %q, want %q", result.Source, "fallback")
	}

	want := []string{
		"gemini-3.1-pro-preview",
		"gemini-3-pro-preview",
		"gemini-3-flash-preview",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
	}
	if len(result.Models) != len(want) {
		t.Fatalf("models len = %d, want %d (%#v)", len(result.Models), len(want), result.Models)
	}
	for i, modelID := range want {
		if result.Models[i] != modelID {
			t.Fatalf("models[%d] = %q, want %q", i, result.Models[i], modelID)
		}
	}
}
