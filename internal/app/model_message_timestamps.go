package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/contextwindow"
	"github.com/doeshing/nekoclaw/internal/core"
)

const modelMessageSentAtLayout = "2006-01-02T15:04Z07:00"

func decorateModelMessagesWithSentAt(
	messages []core.Message,
	fallbackNow time.Time,
	location *time.Location,
) []core.Message {
	if len(messages) == 0 {
		return nil
	}
	if location == nil {
		location = time.UTC
	}

	decorated := cloneMessages(messages)
	fallbackNow = fallbackNow.In(location)
	for i := range decorated {
		if !shouldDecorateModelMessageWithSentAt(decorated[i]) {
			continue
		}
		sentAt := decorated[i].CreatedAt
		if sentAt.IsZero() {
			sentAt = fallbackNow
		}
		prefix := fmt.Sprintf("[sent_at: %s]", sentAt.In(location).Format(modelMessageSentAtLayout))
		if decorated[i].Content == "" {
			decorated[i].Content = prefix
			continue
		}
		decorated[i].Content = prefix + "\n" + decorated[i].Content
	}
	return decorated
}

func shouldDecorateModelMessageWithSentAt(msg core.Message) bool {
	switch msg.Role {
	case core.RoleUser:
		return true
	case core.RoleAssistant:
		return msg.ToolCallID == "" && msg.ToolName == ""
	default:
		return false
	}
}

func resolveMessageLocation(requestTimezone string, configuredTimezone string) *time.Location {
	for _, candidate := range []string{
		strings.TrimSpace(requestTimezone),
		strings.TrimSpace(configuredTimezone),
		"UTC",
	} {
		if candidate == "" {
			continue
		}
		location, err := time.LoadLocation(candidate)
		if err == nil {
			return location
		}
	}
	return time.UTC
}

func resolveCurrentUserMessageTime(clientSentAt string, fallback time.Time) time.Time {
	clientSentAt = strings.TrimSpace(clientSentAt)
	if clientSentAt == "" {
		return fallback
	}
	parsed, err := time.Parse(time.RFC3339Nano, clientSentAt)
	if err == nil {
		return parsed
	}
	parsed, err = time.Parse(time.RFC3339, clientSentAt)
	if err == nil {
		return parsed
	}
	return fallback
}

func sanitizeGeneralConfig(cfg core.GeneralConfig) core.GeneralConfig {
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	return cfg
}

func validateTimezoneName(timezone string) error {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return nil
	}
	_, err := time.LoadLocation(timezone)
	return err
}

func trimMessagesPreservingSystem(messages []core.Message, budgetTokens int) ([]core.Message, []core.Message) {
	if len(messages) == 0 {
		return nil, nil
	}

	systemTokens := 0
	nonSystem := make([]core.Message, 0, len(messages))
	nonSystemIndexes := make([]int, 0, len(messages))
	for i, msg := range messages {
		if msg.Role == core.RoleSystem {
			systemTokens += contextwindow.EstimateMessageTokens(msg)
			continue
		}
		nonSystem = append(nonSystem, msg)
		nonSystemIndexes = append(nonSystemIndexes, i)
	}

	remainingBudget := budgetTokens - systemTokens
	_, droppedNonSystem := contextwindow.KeepNewest(nonSystem, remainingBudget)
	keptIndexStart := len(droppedNonSystem)
	keptNonSystemIndexes := make(map[int]struct{}, len(nonSystemIndexes)-keptIndexStart)
	for _, idx := range nonSystemIndexes[keptIndexStart:] {
		keptNonSystemIndexes[idx] = struct{}{}
	}

	trimmed := make([]core.Message, 0, len(messages)-len(droppedNonSystem))
	dropped := make([]core.Message, 0, len(droppedNonSystem))
	for i, msg := range messages {
		if msg.Role == core.RoleSystem {
			trimmed = append(trimmed, msg)
			continue
		}
		if _, ok := keptNonSystemIndexes[i]; ok {
			trimmed = append(trimmed, msg)
			continue
		}
		dropped = append(dropped, msg)
	}

	return trimmed, dropped
}
