package contextwindow

import (
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestCompressAppliesTrimAndSlidingWindow(t *testing.T) {
	toolBlob := strings.Repeat("x", 12000)
	messages := []core.Message{
		{Role: core.RoleUser, Content: "hello"},
		{Role: core.RoleTool, Content: toolBlob},
		{Role: core.RoleAssistant, Content: "ok"},
		{Role: core.RoleUser, Content: strings.Repeat("message ", 200)},
		{Role: core.RoleAssistant, Content: "final"},
	}

	policy := DefaultPolicy(512)
	policy.ReserveTokens = 64
	compressed, meta, changed := Compress(messages, policy)

	if !changed {
		t.Fatalf("expected compression changes")
	}
	if meta.OriginalTokens <= meta.CompressedTokens {
		t.Fatalf("expected token reduction: before=%d after=%d", meta.OriginalTokens, meta.CompressedTokens)
	}
	if len(compressed) == 0 {
		t.Fatalf("expected non-empty output")
	}
}
