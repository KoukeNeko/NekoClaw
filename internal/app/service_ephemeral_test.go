package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type captureEphemeralProvider struct {
	id string

	mu           sync.Mutex
	chatRequests []provider.GenerateRequest
}

func (p *captureEphemeralProvider) ID() string {
	return p.id
}

func (p *captureEphemeralProvider) ContextWindow(string) int {
	return 200_000
}

func (p *captureEphemeralProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	// Title generation is async and should not affect assertions here.
	if isTitleGenerationRequest(req) {
		return provider.GenerateResponse{Text: "title"}, nil
	}
	p.mu.Lock()
	p.chatRequests = append(p.chatRequests, cloneGenerateRequest(req))
	p.mu.Unlock()
	return provider.GenerateResponse{
		Text: "assistant:" + req.Messages[len(req.Messages)-1].Content,
	}, nil
}

func (p *captureEphemeralProvider) ChatRequests() []provider.GenerateRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]provider.GenerateRequest, 0, len(p.chatRequests))
	for _, req := range p.chatRequests {
		out = append(out, cloneGenerateRequest(req))
	}
	return out
}

func isTitleGenerationRequest(req provider.GenerateRequest) bool {
	if len(req.Messages) == 0 {
		return false
	}
	return req.Messages[0].Role == core.RoleSystem &&
		strings.HasPrefix(req.Messages[0].Content, "Generate a short title")
}

func cloneGenerateRequest(req provider.GenerateRequest) provider.GenerateRequest {
	dup := provider.GenerateRequest{
		Model:   req.Model,
		Account: req.Account,
	}
	if len(req.Messages) > 0 {
		dup.Messages = make([]core.Message, 0, len(req.Messages))
		for _, msg := range req.Messages {
			msgDup := msg
			if len(msg.Images) > 0 {
				msgDup.Images = append([]core.ImageData(nil), msg.Images...)
			}
			dup.Messages = append(dup.Messages, msgDup)
		}
	}
	return dup
}

func newEphemeralTestService(t *testing.T) (*Service, *captureEphemeralProvider) {
	t.Helper()

	svc := NewService(ServiceOptions{})
	prov := &captureEphemeralProvider{id: "test-provider"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("test-provider", []core.Account{
		{
			ID:       "acct-1",
			Provider: "test-provider",
			Type:     core.AccountToken,
			Token:    "token-1",
		},
	}, nil, core.DefaultCooldownConfig()))
	return svc, prov
}

func TestHandleChat_EphemeralMessages_AffectProviderInput_ButNotPersisted(t *testing.T) {
	svc, prov := newEphemeralTestService(t)
	sessionID := "ephemeral-1"

	_, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: sessionID,
		Surface:   core.SurfaceTUI,
		Provider:  "test-provider",
		Model:     "test-model",
		Message:   "current user message",
		EphemeralMessages: []core.Message{
			{Role: core.RoleUser, Content: "ephemeral-user"},
			{Role: core.RoleAssistant, Content: "ephemeral-assistant"},
		},
	})
	if err != nil {
		t.Fatalf("HandleChat failed: %v", err)
	}

	reqs := prov.ChatRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 chat provider request, got %d", len(reqs))
	}

	got := reqs[0].Messages
	if len(got) != 3 {
		t.Fatalf("expected 3 provider messages (2 ephemeral + current), got %d", len(got))
	}
	if got[0].Role != core.RoleUser || got[0].Content != "ephemeral-user" {
		t.Fatalf("unexpected first provider message: role=%s content=%q", got[0].Role, got[0].Content)
	}
	if got[1].Role != core.RoleAssistant || got[1].Content != "ephemeral-assistant" {
		t.Fatalf("unexpected second provider message: role=%s content=%q", got[1].Role, got[1].Content)
	}
	if got[2].Role != core.RoleUser || got[2].Content != "current user message" {
		t.Fatalf("unexpected current provider message: role=%s content=%q", got[2].Role, got[2].Content)
	}

	transcript := svc.GetSessionTranscript(sessionID)
	if len(transcript) != 2 {
		t.Fatalf("expected persisted transcript length 2 (user+assistant), got %d", len(transcript))
	}
	if transcript[0].Content != "current user message" {
		t.Fatalf("unexpected persisted user message: %q", transcript[0].Content)
	}
	if strings.Contains(transcript[1].Content, "ephemeral") {
		t.Fatalf("ephemeral content leaked into persisted assistant message: %q", transcript[1].Content)
	}
	for _, msg := range transcript {
		if strings.Contains(msg.Content, "ephemeral-user") || strings.Contains(msg.Content, "ephemeral-assistant") {
			t.Fatalf("ephemeral content should not be persisted, got: %+v", transcript)
		}
	}

	sessions := svc.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session metadata, got %d", len(sessions))
	}
	if sessions[0].SessionID != sessionID {
		t.Fatalf("unexpected session id: %q", sessions[0].SessionID)
	}
	if sessions[0].MessageCount != 2 {
		t.Fatalf("ephemeral messages should not count as persisted messages, got count=%d", sessions[0].MessageCount)
	}
}

func TestHandleChat_EphemeralMessages_Order(t *testing.T) {
	svc, prov := newEphemeralTestService(t)
	sessionID := "ephemeral-order"

	_, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: sessionID,
		Surface:   core.SurfaceTUI,
		Provider:  "test-provider",
		Model:     "test-model",
		Message:   "persisted turn",
	})
	if err != nil {
		t.Fatalf("first HandleChat failed: %v", err)
	}

	_, err = svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: sessionID,
		Surface:   core.SurfaceTUI,
		Provider:  "test-provider",
		Model:     "test-model",
		Message:   "current turn",
		EphemeralMessages: []core.Message{
			{Role: core.RoleUser, Content: "ephemeral turn"},
		},
	})
	if err != nil {
		t.Fatalf("second HandleChat failed: %v", err)
	}

	reqs := prov.ChatRequests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 chat provider requests, got %d", len(reqs))
	}

	second := reqs[1].Messages
	if len(second) != 4 {
		t.Fatalf("expected order slice length 4 (persisted user/assistant + ephemeral + current), got %d", len(second))
	}
	if second[0].Role != core.RoleUser || second[0].Content != "persisted turn" {
		t.Fatalf("unexpected first message (persisted user): role=%s content=%q", second[0].Role, second[0].Content)
	}
	if second[1].Role != core.RoleAssistant || second[1].Content != "assistant:persisted turn" {
		t.Fatalf("unexpected second message (persisted assistant): role=%s content=%q", second[1].Role, second[1].Content)
	}
	if second[2].Role != core.RoleUser || second[2].Content != "ephemeral turn" {
		t.Fatalf("unexpected third message (ephemeral): role=%s content=%q", second[2].Role, second[2].Content)
	}
	if second[3].Role != core.RoleUser || second[3].Content != "current turn" {
		t.Fatalf("unexpected fourth message (current user): role=%s content=%q", second[3].Role, second[3].Content)
	}

	transcript := svc.GetSessionTranscript(sessionID)
	if len(transcript) != 4 {
		t.Fatalf("expected 4 persisted transcript messages across two turns, got %d", len(transcript))
	}
	for _, msg := range transcript {
		if msg.Content == "ephemeral turn" {
			t.Fatalf("ephemeral message should not be persisted, got transcript: %+v", transcript)
		}
	}
}
