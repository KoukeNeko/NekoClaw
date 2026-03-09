package core

import "testing"

func TestSanitizeToolMessagePairs_DropsOrphanedToolMessages(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, ToolName: "search", ToolCallID: "tc-1", Content: `{"q":"a"}`},
		{Role: RoleAssistant, Content: "plain reply"},
		{Role: RoleTool, ToolName: "search", ToolCallID: "tc-1", Content: `{"result":"late"}`},
		{Role: RoleAssistant, ToolName: "fetch", ToolCallID: "tc-2", Content: `{"url":"https://example.com"}`},
		{Role: RoleTool, ToolName: "fetch", ToolCallID: "tc-2", Content: `{"ok":true}`},
		{Role: RoleTool, ToolName: "search", ToolCallID: "tc-3", Content: `{"result":"orphan"}`},
	}

	kept, dropped := SanitizeToolMessagePairs(messages)

	if len(kept) != 4 {
		t.Fatalf("expected 4 kept messages, got %d", len(kept))
	}
	if len(dropped) != 3 {
		t.Fatalf("expected 3 dropped messages, got %d", len(dropped))
	}
	if kept[0].Role != RoleUser || kept[0].Content != "hello" {
		t.Fatalf("unexpected first kept message: %+v", kept[0])
	}
	if kept[1].Role != RoleAssistant || kept[1].Content != "plain reply" {
		t.Fatalf("unexpected second kept message: %+v", kept[1])
	}
	if kept[2].ToolCallID != "tc-2" || kept[2].Role != RoleAssistant {
		t.Fatalf("expected paired assistant tool call to be kept, got %+v", kept[2])
	}
	if kept[3].ToolCallID != "tc-2" || kept[3].Role != RoleTool {
		t.Fatalf("expected paired tool result to be kept, got %+v", kept[3])
	}
	if dropped[0].ToolCallID != "tc-1" || dropped[0].Role != RoleAssistant {
		t.Fatalf("expected orphaned tool call to drop first, got %+v", dropped[0])
	}
	if dropped[1].ToolCallID != "tc-1" || dropped[1].Role != RoleTool {
		t.Fatalf("expected orphaned delayed tool result to drop, got %+v", dropped[1])
	}
	if dropped[2].ToolCallID != "tc-3" || dropped[2].Role != RoleTool {
		t.Fatalf("expected trailing orphaned tool result to drop, got %+v", dropped[2])
	}
}

func TestSanitizeToolMessagePairs_KeepsToolMessagesWithoutCallID(t *testing.T) {
	messages := []Message{
		{Role: RoleTool, Content: "legacy tool output"},
	}

	kept, dropped := SanitizeToolMessagePairs(messages)

	if len(kept) != 1 || kept[0].Content != "legacy tool output" {
		t.Fatalf("expected legacy tool message to be preserved, got kept=%+v", kept)
	}
	if len(dropped) != 0 {
		t.Fatalf("expected no dropped messages, got %+v", dropped)
	}
}
