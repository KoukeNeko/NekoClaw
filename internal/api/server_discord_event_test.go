package api

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type fakeDiscordEventProvider struct {
	mu           sync.Mutex
	chatRequests []provider.GenerateRequest
}

func (p *fakeDiscordEventProvider) ID() string {
	return "discord-test"
}

func (p *fakeDiscordEventProvider) ContextWindow(string) int {
	return 200_000
}

func (p *fakeDiscordEventProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	if isDiscordTitleGeneration(req) {
		return provider.GenerateResponse{Text: "title"}, nil
	}
	p.mu.Lock()
	p.chatRequests = append(p.chatRequests, copyGenerateRequest(req))
	p.mu.Unlock()
	return provider.GenerateResponse{
		Text: "ok:" + req.Messages[len(req.Messages)-1].Content,
	}, nil
}

func (p *fakeDiscordEventProvider) ChatRequests() []provider.GenerateRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]provider.GenerateRequest, 0, len(p.chatRequests))
	for _, req := range p.chatRequests {
		out = append(out, copyGenerateRequest(req))
	}
	return out
}

func isDiscordTitleGeneration(req provider.GenerateRequest) bool {
	if len(req.Messages) == 0 {
		return false
	}
	return req.Messages[0].Role == core.RoleSystem &&
		strings.HasPrefix(req.Messages[0].Content, "Generate a short title")
}

func copyGenerateRequest(req provider.GenerateRequest) provider.GenerateRequest {
	dup := provider.GenerateRequest{
		Model:   req.Model,
		Account: req.Account,
	}
	if len(req.Messages) > 0 {
		dup.Messages = make([]core.Message, 0, len(req.Messages))
		for _, msg := range req.Messages {
			msgCopy := msg
			if len(msg.Images) > 0 {
				msgCopy.Images = append([]core.ImageData(nil), msg.Images...)
			}
			dup.Messages = append(dup.Messages, msgCopy)
		}
	}
	return dup
}

func newDiscordEventTestServer(t *testing.T) (*app.Service, *fakeDiscordEventProvider, *Server) {
	t.Helper()

	svc := app.NewService(app.ServiceOptions{})
	prov := &fakeDiscordEventProvider{}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("discord-test", []core.Account{
		{
			ID:       "discord-acct-1",
			Provider: "discord-test",
			Type:     core.AccountToken,
			Token:    "token-1",
		},
	}, nil, core.DefaultCooldownConfig()))
	return svc, prov, NewServer(svc)
}

func TestDiscordEvent_DefaultSessionID_IsPerChannel(t *testing.T) {
	_, _, server := newDiscordEventTestServer(t)
	handler := server.Handler()

	resp := performJSONRequest(t, handler, "POST", "/v1/integrations/discord/events", `{
		"channel_id":"chan-1",
		"user_id":"user-1",
		"provider":"discord-test",
		"model":"test-model",
		"message":"hello"
	}`)
	if resp.Code != 200 {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}

	var payload core.ChatResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.SessionID != "discord:chan-1" {
		t.Fatalf("expected per-channel default session id, got %q", payload.SessionID)
	}
}

func TestDiscordEvent_UsesSessionHistoryWhenSameChannel(t *testing.T) {
	svc, prov, server := newDiscordEventTestServer(t)
	handler := server.Handler()

	first := performJSONRequest(t, handler, "POST", "/v1/integrations/discord/events", `{
		"channel_id":"chan-2",
		"user_id":"user-A",
		"provider":"discord-test",
		"model":"test-model",
		"message":"first message"
	}`)
	if first.Code != 200 {
		t.Fatalf("unexpected first status: %d body=%s", first.Code, first.Body.String())
	}

	second := performJSONRequest(t, handler, "POST", "/v1/integrations/discord/events", `{
		"channel_id":"chan-2",
		"user_id":"user-B",
		"provider":"discord-test",
		"model":"test-model",
		"message":"second message"
	}`)
	if second.Code != 200 {
		t.Fatalf("unexpected second status: %d body=%s", second.Code, second.Body.String())
	}

	sessions := svc.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected exactly 1 session for same channel, got %d", len(sessions))
	}
	if sessions[0].SessionID != "discord:chan-2" {
		t.Fatalf("unexpected session id: %q", sessions[0].SessionID)
	}
	if sessions[0].MessageCount != 4 {
		t.Fatalf("expected 4 persisted messages (2 turns), got %d", sessions[0].MessageCount)
	}

	reqs := prov.ChatRequests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 chat provider requests, got %d", len(reqs))
	}
	if len(reqs[1].Messages) < 3 {
		t.Fatalf("expected history to be present in second request, got %d messages", len(reqs[1].Messages))
	}
	if reqs[1].Messages[0].Content != "first message" {
		t.Fatalf("expected first user message in history, got %q", reqs[1].Messages[0].Content)
	}
	if reqs[1].Messages[len(reqs[1].Messages)-1].Content != "second message" {
		t.Fatalf("expected current message at end, got %q", reqs[1].Messages[len(reqs[1].Messages)-1].Content)
	}
}
