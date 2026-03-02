package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

// testProvider is a mock provider for compaction tests.
type testProvider struct {
	contextWindow int
	generateFn    func(ctx context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error)
}

func (p *testProvider) ID() string                  { return "test" }
func (p *testProvider) ContextWindow(_ string) int   { return p.contextWindow }
func (p *testProvider) Generate(ctx context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	if p.generateFn != nil {
		return p.generateFn(ctx, req)
	}
	return provider.GenerateResponse{Text: "Mock compaction summary."}, nil
}

func makeEntries(count int) []core.SessionEntry {
	entries := make([]core.SessionEntry, 0, count+1)
	entries = append(entries, core.NewSessionHeader("test-model", "test"))
	for i := 0; i < count; i++ {
		role := core.RoleUser
		if i%2 == 1 {
			role = core.RoleAssistant
		}
		entries = append(entries, core.NewMessageEntry(role, fmt.Sprintf("Message %d with some content to take up space in the context window.", i)))
	}
	return entries
}

func TestShouldCompact(t *testing.T) {
	prov := &testProvider{contextWindow: 1000}
	compactor := NewCompactor(prov, "test-model", core.Account{ID: "test"})

	// Small history should not trigger compaction.
	small := makeEntries(2)
	if compactor.ShouldCompact(small, 1000, 100) {
		t.Fatalf("small history should not trigger compaction")
	}

	// Large history should trigger compaction.
	large := makeEntries(100)
	if !compactor.ShouldCompact(large, 1000, 100) {
		t.Fatalf("large history should trigger compaction")
	}
}

func TestShouldCompact_ZeroBudget(t *testing.T) {
	prov := &testProvider{contextWindow: 100}
	compactor := NewCompactor(prov, "test-model", core.Account{ID: "test"})

	entries := makeEntries(5)
	// Reserve more than context window → budget <= 0 → should not compact.
	if compactor.ShouldCompact(entries, 100, 200) {
		t.Fatalf("should not compact when budget is zero or negative")
	}
}

func TestCompactWithMockProvider(t *testing.T) {
	summaryText := "This is the LLM-generated summary of dropped messages."
	prov := &testProvider{
		contextWindow: 1000,
		generateFn: func(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
			// Verify the summarization prompt includes dropped messages.
			lastMsg := req.Messages[len(req.Messages)-1]
			if !strings.Contains(lastMsg.Content, "即將被移除") {
				t.Errorf("expected summarization prompt, got: %s", lastMsg.Content[:100])
			}
			return provider.GenerateResponse{Text: summaryText}, nil
		},
	}
	compactor := NewCompactor(prov, "test-model", core.Account{ID: "test"})

	entries := makeEntries(50)
	result, err := compactor.Compact(context.Background(), CompactionRequest{
		Entries:          entries,
		ContextWindow:    1000,
		ReserveTokens:    100,
		KeepRecentTokens: 200,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DroppedCount == 0 {
		t.Fatalf("expected some messages to be dropped")
	}
	if result.CompactionEntry.Type != core.EntryCompaction {
		t.Fatalf("expected compaction entry, got %q", result.CompactionEntry.Type)
	}
	if result.CompactionEntry.Summary != summaryText {
		t.Fatalf("expected summary %q, got %q", summaryText, result.CompactionEntry.Summary)
	}
	if result.CompactionEntry.DroppedCount != result.DroppedCount {
		t.Fatalf("entry dropped_count=%d != result dropped_count=%d",
			result.CompactionEntry.DroppedCount, result.DroppedCount)
	}
}

func TestCompactFallback_NothingToDropp(t *testing.T) {
	prov := &testProvider{contextWindow: 100000}
	compactor := NewCompactor(prov, "test-model", core.Account{ID: "test"})

	entries := makeEntries(3)
	result, err := compactor.Compact(context.Background(), CompactionRequest{
		Entries:          entries,
		ContextWindow:    100000,
		ReserveTokens:    100,
		KeepRecentTokens: 100000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DroppedCount != 0 {
		t.Fatalf("expected 0 dropped, got %d", result.DroppedCount)
	}
	if len(result.KeptEntries) != len(entries) {
		t.Fatalf("expected all entries kept, got %d/%d", len(result.KeptEntries), len(entries))
	}
}

func TestCompactProviderError(t *testing.T) {
	prov := &testProvider{
		contextWindow: 1000,
		generateFn: func(_ context.Context, _ provider.GenerateRequest) (provider.GenerateResponse, error) {
			return provider.GenerateResponse{}, fmt.Errorf("LLM unavailable")
		},
	}
	compactor := NewCompactor(prov, "test-model", core.Account{ID: "test"})

	entries := makeEntries(50)
	_, err := compactor.Compact(context.Background(), CompactionRequest{
		Entries:          entries,
		ContextWindow:    1000,
		ReserveTokens:    100,
		KeepRecentTokens: 200,
	})
	if err == nil {
		t.Fatalf("expected error from provider failure")
	}
	if !strings.Contains(err.Error(), "LLM unavailable") {
		t.Fatalf("expected LLM error, got: %v", err)
	}
}

func TestCompactionEntryJSON(t *testing.T) {
	entry := core.NewCompactionEntry("summary text", 10, 5000, "abc12345")

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal compaction entry: %v", err)
	}

	var parsed core.SessionEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed.Type != core.EntryCompaction {
		t.Fatalf("expected compaction type, got %q", parsed.Type)
	}
	if parsed.Summary != "summary text" {
		t.Fatalf("expected summary 'summary text', got %q", parsed.Summary)
	}
	if parsed.DroppedCount != 10 {
		t.Fatalf("expected dropped_count=10, got %d", parsed.DroppedCount)
	}
}

func TestEstimateEntryTokens(t *testing.T) {
	entry := core.NewMessageEntry(core.RoleUser, "Hello world")
	tokens := EstimateEntryTokens(entry)
	if tokens <= 0 {
		t.Fatalf("expected positive token count, got %d", tokens)
	}

	// Longer content should yield more tokens.
	long := core.NewMessageEntry(core.RoleUser, strings.Repeat("a", 1000))
	longTokens := EstimateEntryTokens(long)
	if longTokens <= tokens {
		t.Fatalf("longer content should have more tokens: %d <= %d", longTokens, tokens)
	}
}

func TestSplitByTokenBudget(t *testing.T) {
	entries := makeEntries(20)
	kept, dropped := splitByTokenBudget(entries, 100)

	if len(kept) == 0 {
		t.Fatalf("expected some entries to be kept")
	}
	if len(dropped) == 0 {
		t.Fatalf("expected some entries to be dropped")
	}
	if len(kept)+len(dropped) != len(entries) {
		t.Fatalf("kept(%d) + dropped(%d) != total(%d)", len(kept), len(dropped), len(entries))
	}

	// Kept entries should be the tail of the original slice.
	lastKept := kept[len(kept)-1]
	lastOriginal := entries[len(entries)-1]
	if lastKept.ID != lastOriginal.ID {
		t.Fatalf("last kept entry should match last original entry")
	}
}

func TestSplitByTokenBudget_AllFit(t *testing.T) {
	entries := makeEntries(3)
	kept, dropped := splitByTokenBudget(entries, 100000)

	if len(dropped) != 0 {
		t.Fatalf("expected nothing dropped when budget is huge, got %d", len(dropped))
	}
	if len(kept) != len(entries) {
		t.Fatalf("expected all kept, got %d/%d", len(kept), len(entries))
	}
}
