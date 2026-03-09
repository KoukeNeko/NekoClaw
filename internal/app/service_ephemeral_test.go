package app

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

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
		Text: "assistant:" + stripModelSentAtPrefix(req.Messages[len(req.Messages)-1].Content),
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

var minuteSentAtPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2})$`)

func stripModelSentAtPrefix(content string) string {
	_, body, ok := splitModelSentAtContent(content)
	if !ok {
		return content
	}
	return body
}

func splitModelSentAtContent(content string) (string, string, bool) {
	const prefix = "[sent_at: "
	if !strings.HasPrefix(content, prefix) {
		return "", "", false
	}
	end := strings.Index(content, "]")
	if end < 0 {
		return "", "", false
	}
	sentAt := content[len(prefix):end]
	body := strings.TrimPrefix(content[end+1:], "\n")
	return sentAt, body, true
}

func requireMinuteSentAtBody(t *testing.T, got string, wantBody string) {
	t.Helper()

	sentAt, body, ok := splitModelSentAtContent(got)
	if !ok {
		t.Fatalf("expected sent_at prefix, got %q", got)
	}
	if !minuteSentAtPattern.MatchString(sentAt) {
		t.Fatalf("expected minute-precision RFC3339 UTC timestamp, got %q", sentAt)
	}
	if body != wantBody {
		t.Fatalf("expected body %q, got %q", wantBody, body)
	}
}

func TestHandleChat_EphemeralMessages_AffectProviderInput_ButNotPersisted(t *testing.T) {
	svc, prov := newEphemeralTestService(t)
	sessionID := "ephemeral-1"
	ephemeralUserAt := time.Date(2026, 3, 8, 19, 13, 42, 0, time.FixedZone("UTC+8", 8*60*60))
	ephemeralAssistantAt := time.Date(2026, 3, 8, 19, 14, 9, 0, time.FixedZone("UTC+8", 8*60*60))

	_, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: sessionID,
		Surface:   core.SurfaceWeb,
		Provider:  "test-provider",
		Model:     "test-model",
		Message:   "current user message",
		EphemeralMessages: []core.Message{
			{Role: core.RoleUser, Content: "ephemeral-user", CreatedAt: ephemeralUserAt},
			{Role: core.RoleAssistant, Content: "ephemeral-assistant", CreatedAt: ephemeralAssistantAt},
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
	if got[0].Role != core.RoleUser || got[0].Content != "[sent_at: 2026-03-08T11:13Z]\nephemeral-user" {
		t.Fatalf("unexpected first provider message: role=%s content=%q", got[0].Role, got[0].Content)
	}
	if got[1].Role != core.RoleAssistant || got[1].Content != "[sent_at: 2026-03-08T11:14Z]\nephemeral-assistant" {
		t.Fatalf("unexpected second provider message: role=%s content=%q", got[1].Role, got[1].Content)
	}
	if got[2].Role != core.RoleUser {
		t.Fatalf("unexpected current provider role: %s", got[2].Role)
	}
	requireMinuteSentAtBody(t, got[2].Content, "current user message")

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
		if strings.Contains(msg.Content, "[sent_at: ") {
			t.Fatalf("timestamp prefix should not be persisted, got transcript: %+v", transcript)
		}
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
	ephemeralAt := time.Date(2026, 3, 8, 19, 18, 3, 0, time.FixedZone("UTC+8", 8*60*60))

	_, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: sessionID,
		Surface:   core.SurfaceWeb,
		Provider:  "test-provider",
		Model:     "test-model",
		Message:   "persisted turn",
	})
	if err != nil {
		t.Fatalf("first HandleChat failed: %v", err)
	}

	_, err = svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: sessionID,
		Surface:   core.SurfaceWeb,
		Provider:  "test-provider",
		Model:     "test-model",
		Message:   "current turn",
		EphemeralMessages: []core.Message{
			{Role: core.RoleUser, Content: "ephemeral turn", CreatedAt: ephemeralAt},
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
	if second[0].Role != core.RoleUser {
		t.Fatalf("unexpected first message role (persisted user): %s", second[0].Role)
	}
	requireMinuteSentAtBody(t, second[0].Content, "persisted turn")
	if second[1].Role != core.RoleAssistant {
		t.Fatalf("unexpected second message role (persisted assistant): %s", second[1].Role)
	}
	requireMinuteSentAtBody(t, second[1].Content, "assistant:persisted turn")
	if second[2].Role != core.RoleUser || second[2].Content != "[sent_at: 2026-03-08T11:18Z]\nephemeral turn" {
		t.Fatalf("unexpected third message (ephemeral): role=%s content=%q", second[2].Role, second[2].Content)
	}
	if second[3].Role != core.RoleUser {
		t.Fatalf("unexpected fourth message role (current user): %s", second[3].Role)
	}
	requireMinuteSentAtBody(t, second[3].Content, "current turn")

	transcript := svc.GetSessionTranscript(sessionID)
	if len(transcript) != 4 {
		t.Fatalf("expected 4 persisted transcript messages across two turns, got %d", len(transcript))
	}
	for _, msg := range transcript {
		if strings.Contains(msg.Content, "[sent_at: ") {
			t.Fatalf("timestamp prefix should not be persisted, got transcript: %+v", transcript)
		}
		if msg.Content == "ephemeral turn" {
			t.Fatalf("ephemeral message should not be persisted, got transcript: %+v", transcript)
		}
	}
}

func TestHandleChat_UsesClientTimezoneAndClientSentAt(t *testing.T) {
	svc, prov := newEphemeralTestService(t)
	sessionID := "client-timezone"

	_, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      sessionID,
		Surface:        core.SurfaceWeb,
		Provider:       "test-provider",
		Model:          "test-model",
		Message:        "timezone aware",
		ClientTimezone: "Asia/Taipei",
		ClientSentAt:   "2026-03-08T11:13:27Z",
	})
	if err != nil {
		t.Fatalf("HandleChat failed: %v", err)
	}

	reqs := prov.ChatRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 chat provider request, got %d", len(reqs))
	}
	if len(reqs[0].Messages) != 1 {
		t.Fatalf("expected 1 provider message, got %d", len(reqs[0].Messages))
	}
	if got := reqs[0].Messages[0].Content; got != "[sent_at: 2026-03-08T19:13+08:00]\ntimezone aware" {
		t.Fatalf("unexpected provider message content: %q", got)
	}

	transcript := svc.GetSessionTranscript(sessionID)
	if len(transcript) != 2 {
		t.Fatalf("expected 2 transcript messages, got %d", len(transcript))
	}
	if transcript[0].CreatedAt != "2026-03-08T11:13:27Z" {
		t.Fatalf("unexpected persisted created_at: %q", transcript[0].CreatedAt)
	}
	if strings.Contains(transcript[0].Content, "[sent_at: ") {
		t.Fatalf("timestamp prefix should not be persisted, got %q", transcript[0].Content)
	}
}
