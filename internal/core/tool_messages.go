package core

import "strings"

// SanitizeToolMessagePairs keeps only well-formed adjacent assistant-tool
// pairs. Orphaned tool calls/results are dropped to avoid sending invalid tool
// history to providers that require strict pairing semantics.
func SanitizeToolMessagePairs(messages []Message) ([]Message, []Message) {
	if len(messages) == 0 {
		return nil, nil
	}

	kept := make([]Message, 0, len(messages))
	dropped := make([]Message, 0)
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if isAssistantToolMessage(msg) {
			if i+1 < len(messages) && isMatchingToolResult(messages[i+1], msg.ToolCallID) {
				kept = append(kept, msg, messages[i+1])
				i++
				continue
			}
			dropped = append(dropped, msg)
			continue
		}
		if isToolResultMessage(msg) {
			dropped = append(dropped, msg)
			continue
		}
		kept = append(kept, msg)
	}
	return kept, dropped
}

func isAssistantToolMessage(msg Message) bool {
	return msg.Role == RoleAssistant &&
		strings.TrimSpace(msg.ToolCallID) != "" &&
		strings.TrimSpace(msg.ToolName) != ""
}

func isToolResultMessage(msg Message) bool {
	return msg.Role == RoleTool && strings.TrimSpace(msg.ToolCallID) != ""
}

func isMatchingToolResult(msg Message, toolCallID string) bool {
	return isToolResultMessage(msg) && strings.TrimSpace(msg.ToolCallID) == strings.TrimSpace(toolCallID)
}
