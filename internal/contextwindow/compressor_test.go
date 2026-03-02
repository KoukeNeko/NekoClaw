package contextwindow

import (
	"strings"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

func testMessage(role core.MessageRole, text string, ts int) core.Message {
	return core.Message{
		Role:      role,
		Content:   text,
		CreatedAt: time.Unix(int64(ts), 0),
	}
}

func TestSplitMessagesByTokenSharePreservesOrder(t *testing.T) {
	messages := []core.Message{
		testMessage(core.RoleUser, "m1 "+strings.Repeat("a", 1200), 1),
		testMessage(core.RoleAssistant, "m2 "+strings.Repeat("b", 3200), 2),
		testMessage(core.RoleUser, "m3 "+strings.Repeat("c", 2200), 3),
		testMessage(core.RoleAssistant, "m4 "+strings.Repeat("d", 2800), 4),
	}

	chunks := splitMessagesByTokenShare(messages, 2)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	var flattened []string
	for _, chunk := range chunks {
		for _, msg := range chunk {
			flattened = append(flattened, msg.Content)
		}
	}
	for idx, msg := range messages {
		if flattened[idx] != msg.Content {
			t.Fatalf("message order changed at index %d", idx)
		}
	}
}

func TestPruneHistoryForContextShareDropsOldestChunks(t *testing.T) {
	messages := []core.Message{
		testMessage(core.RoleUser, "u1 "+strings.Repeat("x", 3000), 1),
		testMessage(core.RoleAssistant, "a1 "+strings.Repeat("x", 3000), 2),
		testMessage(core.RoleUser, "u2 "+strings.Repeat("x", 3000), 3),
		testMessage(core.RoleAssistant, "a2 "+strings.Repeat("x", 3000), 4),
		testMessage(core.RoleUser, "u3 "+strings.Repeat("x", 3000), 5),
		testMessage(core.RoleAssistant, "a3 "+strings.Repeat("x", 3000), 6),
	}

	pruned := pruneHistoryForContextShare(messages, 2000, 0.5, 2)

	if pruned.DroppedChunks == 0 {
		t.Fatalf("expected dropped chunks > 0")
	}
	if pruned.DroppedMessages == 0 {
		t.Fatalf("expected dropped messages > 0")
	}
	if pruned.KeptTokens > pruned.BudgetTokens {
		t.Fatalf("kept tokens exceed budget: kept=%d budget=%d", pruned.KeptTokens, pruned.BudgetTokens)
	}
	if len(pruned.Messages) == 0 {
		t.Fatalf("expected non-empty kept messages")
	}

	kept := pruned.Messages
	wantSuffix := messages[len(messages)-len(kept):]
	for i := range kept {
		if kept[i].Content != wantSuffix[i].Content {
			t.Fatalf("expected kept messages to be newest suffix")
		}
	}
}

func TestCompressAddsCompactionSummaryAndKeepsNewest(t *testing.T) {
	messages := []core.Message{
		testMessage(core.RoleUser, "u1 "+strings.Repeat("x", 5000), 1),
		testMessage(core.RoleAssistant, "a1 "+strings.Repeat("x", 5000), 2),
		testMessage(core.RoleUser, "u2 "+strings.Repeat("x", 5000), 3),
		testMessage(core.RoleAssistant, "a2 "+strings.Repeat("x", 5000), 4),
		testMessage(core.RoleUser, "u3 "+strings.Repeat("x", 5000), 5),
		testMessage(core.RoleAssistant, "a3 "+strings.Repeat("x", 5000), 6),
	}

	policy := DefaultPolicy(1024)
	policy.ReserveTokens = 128
	policy.MaxHistoryShare = 0.5
	policy.PruneParts = 2
	compressed, meta, changed := Compress(messages, policy)

	if !changed {
		t.Fatalf("expected compression changes")
	}
	if len(compressed) == 0 {
		t.Fatalf("expected non-empty output")
	}
	if compressed[0].Role != core.RoleSystem {
		t.Fatalf("expected summary prefix message, got role=%q", compressed[0].Role)
	}
	if !strings.Contains(compressed[0].Content, "History compacted: dropped") {
		t.Fatalf("expected compaction summary prefix")
	}
	if meta.DroppedMessages == 0 {
		t.Fatalf("expected dropped messages > 0")
	}
	if meta.OriginalTokens <= meta.CompressedTokens {
		t.Fatalf("expected token reduction: before=%d after=%d", meta.OriginalTokens, meta.CompressedTokens)
	}
	last := compressed[len(compressed)-1]
	if last.Content != messages[len(messages)-1].Content {
		t.Fatalf("expected newest message to be preserved")
	}
}

func TestCompressNoopWhenWithinBudget(t *testing.T) {
	messages := []core.Message{
		testMessage(core.RoleUser, "hello", 1),
		testMessage(core.RoleAssistant, "world", 2),
	}
	policy := DefaultPolicy(128000)
	compressed, meta, changed := Compress(messages, policy)

	if changed {
		t.Fatalf("expected unchanged compression result")
	}
	if meta.DroppedMessages != 0 {
		t.Fatalf("expected no dropped messages, got %d", meta.DroppedMessages)
	}
	if len(compressed) != len(messages) {
		t.Fatalf("message count changed unexpectedly")
	}
	for i := range messages {
		if compressed[i].Content != messages[i].Content || compressed[i].Role != messages[i].Role {
			t.Fatalf("message changed at index %d", i)
		}
	}
}
