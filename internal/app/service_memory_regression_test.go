package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/memory"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type authenticatedMemoryFlushProvider struct {
	id string

	mu       sync.Mutex
	requests []provider.GenerateRequest
}

func (p *authenticatedMemoryFlushProvider) ID() string { return p.id }

func (p *authenticatedMemoryFlushProvider) ContextWindow(string) int { return 20_000 }

func (p *authenticatedMemoryFlushProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	if isMemoryFlushRequest(req) {
		if strings.TrimSpace(req.Account.ID) == "" {
			return provider.GenerateResponse{}, errors.New("missing account for memory flush")
		}
		return provider.GenerateResponse{
			Text:     "- durable note",
			Endpoint: "https://example.invalid/" + p.id,
		}, nil
	}

	return provider.GenerateResponse{
		Text:     "ok",
		Endpoint: "https://example.invalid/" + p.id,
	}, nil
}

func (p *authenticatedMemoryFlushProvider) flushRequest() (provider.GenerateRequest, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.requests {
		if isMemoryFlushRequest(req) {
			return req, true
		}
	}
	return provider.GenerateRequest{}, false
}

func isMemoryFlushRequest(req provider.GenerateRequest) bool {
	if len(req.Messages) == 0 {
		return false
	}
	msg := req.Messages[0]
	return msg.Role == core.RoleSystem && strings.Contains(msg.Content, "在這段對話即將被壓縮之前")
}

func TestSaveMemoryReindexesDailyLogImmediately(t *testing.T) {
	root := t.TempDir()
	memoryDir := filepath.Join(root, "memory")
	idx, err := memory.NewSearchIndex(filepath.Join(memoryDir, "search.db"))
	if err != nil {
		t.Fatalf("create search index: %v", err)
	}
	defer idx.Close()

	svc := NewService(ServiceOptions{MemoryDir: memoryDir})
	svc.searchIndex = idx
	backend := serviceToolBackend{svc: svc}

	if err := backend.SaveMemory("remember zebra mango protocol"); err != nil {
		t.Fatalf("SaveMemory() error = %v", err)
	}

	results, err := svc.SearchMemory("zebra", 10)
	if err != nil {
		t.Fatalf("SearchMemory() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected saved memory to be searchable immediately")
	}
	if got := results[0].Path; got == "" {
		t.Fatal("expected indexed memory result to include a path")
	}
}

func TestHandleChatPreCompactionFlushUsesAcquiredAccount(t *testing.T) {
	memoryDir := t.TempDir()
	svc := NewService(ServiceOptions{MemoryDir: memoryDir})
	prov := &authenticatedMemoryFlushProvider{id: "google-ai-studio"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "acct-1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-1"},
	}, []string{"acct-1"}, core.DefaultCooldownConfig()))

	sessionID := "flush-session"
	svc.sessions.AppendMessage(sessionID, core.Message{
		Role:    core.RoleUser,
		Content: strings.Repeat("history ", 600),
	})

	resp, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID: sessionID,
		Surface:   core.SurfaceWeb,
		Provider:  "google-ai-studio",
		Model:     "default",
		Message:   "continue",
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}
	if resp.Reply != "ok" {
		t.Fatalf("reply = %q, want ok", resp.Reply)
	}

	flushReq, ok := prov.flushRequest()
	if !ok {
		t.Fatal("expected pre-compaction memory flush request")
	}
	if flushReq.Account.ID != "acct-1" {
		t.Fatalf("flush account = %q, want acct-1", flushReq.Account.ID)
	}

	content, err := os.ReadFile(memory.DailyLogPath(memoryDir, time.Now()))
	if err != nil {
		t.Fatalf("read daily log: %v", err)
	}
	if !strings.Contains(string(content), "durable note") {
		t.Fatalf("expected daily log to contain flushed note, got %q", string(content))
	}
}
