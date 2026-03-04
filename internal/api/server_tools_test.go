package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type fakeToolChatProvider struct {
	turn int
}

func (p *fakeToolChatProvider) ID() string { return "anthropic" }

func (p *fakeToolChatProvider) ContextWindow(string) int { return 200_000 }

func (p *fakeToolChatProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{
		Text: "echo:" + req.Messages[len(req.Messages)-1].Content,
	}, nil
}

func (p *fakeToolChatProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{SupportsTools: true, SupportsParallelCalls: true, MaxToolCalls: 8}
}

func (p *fakeToolChatProvider) GenerateToolTurn(_ context.Context, _ provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	if p.turn == 0 {
		p.turn++
		return provider.ToolTurnResponse{
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "file_write", Arguments: json.RawMessage(`{"path":"tmp.txt","content":"hello"}`)},
			},
		}, nil
	}
	return provider.ToolTurnResponse{
		Text: "tool done",
	}, nil
}

func TestChatToolApprovalFlow(t *testing.T) {
	workspace := t.TempDir()
	svc := app.NewService(app.ServiceOptions{WorkspaceRoot: workspace})
	svc.RegisterProvider(&fakeToolChatProvider{})
	svc.RegisterPool(core.NewAccountPool("anthropic", []core.Account{
		{
			ID:       "anthropic-main",
			Provider: "anthropic",
			Type:     core.AccountToken,
			Token:    provider.AnthropicSetupTokenPrefix + strings.Repeat("a", provider.AnthropicSetupTokenMinLength),
		},
	}, nil, core.DefaultCooldownConfig()))

	server := NewServer(svc)
	handler := server.Handler()

	firstReq := `{"session_id":"tool-1","surface":"tui","provider":"anthropic","model":"default","message":"write file","enable_tools":true}`
	firstResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("unexpected first status: %d body=%s", firstResp.Code, firstResp.Body.String())
	}
	if !strings.Contains(firstResp.Body.String(), `"status":"approval_required"`) {
		t.Fatalf("expected approval_required response: %s", firstResp.Body.String())
	}

	var firstPayload core.ChatResponse
	if err := json.Unmarshal(firstResp.Body.Bytes(), &firstPayload); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if strings.TrimSpace(firstPayload.RunID) == "" {
		t.Fatalf("missing run_id in first response")
	}

	secondReq := `{"session_id":"tool-1","surface":"tui","provider":"anthropic","model":"default","enable_tools":true,"run_id":"` + firstPayload.RunID + `","tool_approvals":[{"approval_id":"call-1","decision":"allow"}]}`
	secondResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", secondReq)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("unexpected second status: %d body=%s", secondResp.Code, secondResp.Body.String())
	}
	if !strings.Contains(secondResp.Body.String(), `"status":"completed"`) {
		t.Fatalf("expected completed status: %s", secondResp.Body.String())
	}
	if !strings.Contains(secondResp.Body.String(), `"reply":"tool done"`) {
		t.Fatalf("expected final reply: %s", secondResp.Body.String())
	}
}
