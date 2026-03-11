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

type fakeReminderToolProvider struct {
	turn int
}

func (p *fakeReminderToolProvider) ID() string { return "anthropic" }

func (p *fakeReminderToolProvider) ContextWindow(string) int { return 200_000 }

func (p *fakeReminderToolProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{
		Text: "echo:" + req.Messages[len(req.Messages)-1].Content,
	}, nil
}

func (p *fakeReminderToolProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{SupportsTools: true, SupportsParallelCalls: true, MaxToolCalls: 8}
}

func (p *fakeReminderToolProvider) GenerateToolTurn(_ context.Context, _ provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	switch p.turn {
	case 0:
		p.turn++
		return provider.ToolTurnResponse{
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "file_write", Arguments: json.RawMessage(`{"path":"tmp.txt","content":"hello"}`)},
			},
		}, nil
	case 1:
		p.turn++
		return provider.ToolTurnResponse{Text: "denial handled"}, nil
	default:
		p.turn++
		return provider.ToolTurnResponse{Text: "next action"}, nil
	}
}

func TestChatReminderAfterToolDenial(t *testing.T) {
	workspace := t.TempDir()
	svc := app.NewService(app.ServiceOptions{WorkspaceRoot: workspace})
	svc.RegisterProvider(&fakeReminderToolProvider{})
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

	firstReq := `{"session_id":"tool-1","surface":"web","provider":"anthropic","model":"default","message":"write file","enable_tools":true}`
	firstResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("unexpected first status: %d body=%s", firstResp.Code, firstResp.Body.String())
	}

	var firstPayload core.ChatResponse
	if err := json.Unmarshal(firstResp.Body.Bytes(), &firstPayload); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstPayload.Status != core.ChatStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %+v", firstPayload)
	}

	secondReq := `{"session_id":"tool-1","surface":"web","provider":"anthropic","model":"default","enable_tools":true,"run_id":"` + firstPayload.RunID + `","tool_approvals":[{"approval_id":"call-1","decision":"deny"}]}`
	secondResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", secondReq)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("unexpected second status: %d body=%s", secondResp.Code, secondResp.Body.String())
	}

	var secondPayload core.ChatResponse
	if err := json.Unmarshal(secondResp.Body.Bytes(), &secondPayload); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if secondPayload.Status != core.ChatStatusCompleted {
		t.Fatalf("expected completed status after denial, got %+v", secondPayload)
	}

	thirdReq := `{"session_id":"tool-1","surface":"web","provider":"anthropic","model":"default","message":"what next","enable_tools":true}`
	thirdResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", thirdReq)
	if thirdResp.Code != http.StatusOK {
		t.Fatalf("unexpected third status: %d body=%s", thirdResp.Code, thirdResp.Body.String())
	}

	var thirdPayload core.ChatResponse
	if err := json.Unmarshal(thirdResp.Body.Bytes(), &thirdPayload); err != nil {
		t.Fatalf("decode third response: %v", err)
	}
	if len(thirdPayload.Reminders) != 1 || thirdPayload.Reminders[0].Kind != core.ReminderKindRespectToolDenial {
		t.Fatalf("expected respect_tool_denial reminder, got %+v", thirdPayload.Reminders)
	}

	transcriptResp := performJSONRequest(t, handler, http.MethodGet, "/v1/sessions/transcript?session_id=tool-1", "")
	if transcriptResp.Code != http.StatusOK {
		t.Fatalf("unexpected transcript status: %d body=%s", transcriptResp.Code, transcriptResp.Body.String())
	}

	var transcriptPayload struct {
		Messages []app.TranscriptMessage `json:"messages"`
	}
	if err := json.Unmarshal(transcriptResp.Body.Bytes(), &transcriptPayload); err != nil {
		t.Fatalf("decode transcript response: %v", err)
	}
	if len(transcriptPayload.Messages) == 0 {
		t.Fatalf("expected transcript messages")
	}
	last := transcriptPayload.Messages[len(transcriptPayload.Messages)-1]
	if len(last.Reminders) != 1 || last.Reminders[0].Kind != core.ReminderKindRespectToolDenial {
		t.Fatalf("expected transcript reminder metadata, got %+v", last.Reminders)
	}
}
