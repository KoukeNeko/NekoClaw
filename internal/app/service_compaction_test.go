package app

import (
	"context"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type compactionQueueTestProvider struct {
	contextWindow int
}

func (p *compactionQueueTestProvider) ID() string { return "test-provider" }

func (p *compactionQueueTestProvider) ContextWindow(_ string) int {
	return p.contextWindow
}

func (p *compactionQueueTestProvider) Generate(_ context.Context, _ provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{}, nil
}

func TestMaybeEnqueueSessionCompactionDeduplicates(t *testing.T) {
	store, err := core.NewPersistentSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	store.AppendMessage("oversized", core.Message{
		Role:    core.RoleUser,
		Content: strings.Repeat("x", 12000),
	})

	svc := NewService(ServiceOptions{SessionStore: store})
	svc.compactionActive = true

	svc.maybeEnqueueSessionCompaction("oversized", 1000)
	svc.maybeEnqueueSessionCompaction("oversized", 1000)

	if got := len(svc.compactionQueue); got != 1 {
		t.Fatalf("queue length = %d, want 1", got)
	}
	if got := len(svc.compactionQueued); got != 1 {
		t.Fatalf("queued set size = %d, want 1", got)
	}
}

func TestStartupCompactionCatchupQueuesOversizedSessions(t *testing.T) {
	store, err := core.NewPersistentSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	store.AppendMessage("oversized", core.Message{
		Role:    core.RoleUser,
		Content: strings.Repeat("x", 12000),
	})
	store.AppendMessage("small", core.Message{
		Role:    core.RoleUser,
		Content: "short",
	})

	svc := NewService(ServiceOptions{SessionStore: store})
	svc.providers["test-provider"] = &compactionQueueTestProvider{contextWindow: 1000}
	svc.defaultProvider = "test-provider"
	svc.defaultModel = "test-model"

	svc.enqueueStartupCompactionCandidates(context.Background())

	if got := len(svc.compactionQueue); got != 1 {
		t.Fatalf("queue length = %d, want 1", got)
	}
	select {
	case sessionID := <-svc.compactionQueue:
		if sessionID != "oversized" {
			t.Fatalf("queued session = %q, want oversized", sessionID)
		}
	default:
		t.Fatalf("expected queued session")
	}
}
