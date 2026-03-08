package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type fakeElapsedProvider struct {
	id    string
	delay time.Duration
}

func (p *fakeElapsedProvider) ID() string {
	return p.id
}

func (p *fakeElapsedProvider) ContextWindow(string) int {
	return 200_000
}

func (p *fakeElapsedProvider) Generate(ctx context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	select {
	case <-ctx.Done():
		return provider.GenerateResponse{}, ctx.Err()
	case <-time.After(p.delay):
	}

	return provider.GenerateResponse{
		Text: "echo:" + req.Messages[len(req.Messages)-1].Content,
		Usage: core.UsageInfo{
			InputTokens:  12,
			OutputTokens: 6,
			TotalTokens:  18,
		},
	}, nil
}

func newElapsedTestServer(t *testing.T) (*app.Service, http.Handler) {
	t.Helper()

	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakeElapsedProvider{id: "elapsed-provider", delay: 5 * time.Millisecond})
	svc.RegisterPool(core.NewAccountPool("elapsed-provider", []core.Account{
		{
			ID:       "elapsed-account",
			Provider: "elapsed-provider",
			Type:     core.AccountToken,
			Token:    "token-1",
		},
	}, nil, core.DefaultCooldownConfig()))

	server := NewServer(svc)
	return svc, server.Handler()
}

func TestChatResponseIncludesElapsedMsAndMatchesTranscript(t *testing.T) {
	svc, handler := newElapsedTestServer(t)

	chatReq := `{"session_id":"elapsed-1","surface":"web","provider":"elapsed-provider","model":"test-model","message":"hello"}`
	chatResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat", chatReq)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("unexpected chat status: %d body=%s", chatResp.Code, chatResp.Body.String())
	}

	var payload core.ChatResponse
	if err := json.Unmarshal(chatResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if payload.ElapsedMs <= 0 {
		t.Fatalf("expected elapsed_ms > 0, got %d", payload.ElapsedMs)
	}

	transcript := svc.GetSessionTranscript("elapsed-1")
	if len(transcript) != 2 {
		t.Fatalf("expected 2 transcript entries, got %d", len(transcript))
	}
	if transcript[1].ElapsedMs != payload.ElapsedMs {
		t.Fatalf("assistant transcript elapsed_ms = %d, want %d", transcript[1].ElapsedMs, payload.ElapsedMs)
	}
}

func TestChatStreamDoneIncludesElapsedMs(t *testing.T) {
	_, handler := newElapsedTestServer(t)

	streamReq := `{"session_id":"elapsed-stream","surface":"web","provider":"elapsed-provider","model":"test-model","message":"hello"}`
	streamResp := performJSONRequest(t, handler, http.MethodPost, "/v1/chat/stream", streamReq)
	if streamResp.Code != http.StatusOK {
		t.Fatalf("unexpected stream status: %d body=%s", streamResp.Code, streamResp.Body.String())
	}

	var doneChunk core.StreamChunk
	for _, line := range strings.Split(streamResp.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var chunk core.StreamChunk
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
			t.Fatalf("decode stream chunk: %v", err)
		}
		if chunk.Type == core.ChunkDone {
			doneChunk = chunk
			break
		}
	}

	if doneChunk.Type != core.ChunkDone || doneChunk.Response == nil {
		t.Fatalf("expected done chunk with response, got %+v", doneChunk)
	}
	if doneChunk.Response.ElapsedMs <= 0 {
		t.Fatalf("expected done.response.elapsed_ms > 0, got %d", doneChunk.Response.ElapsedMs)
	}
}
