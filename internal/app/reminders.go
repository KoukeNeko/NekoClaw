package app

import (
	"strings"
	"sync"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

type TurnSummary struct {
	ExecutedToolNames    []string
	HasMutatingExecution bool
	HadMutatingDenial    bool
	FailureSignature     string
	HadZeroToolExecution bool
	At                   time.Time
}

type ReminderState struct {
	LastTurn                  TurnSummary
	LastAppliedReminder       core.ReminderKind
	PendingDenialReminder     bool
	FailureSignature          string
	FailureStreak             int
	FailureReminderSuppressed bool
	CompletedTurns            []TurnSummary
	ReadOnlyLoopSuppressed    bool
}

type ReminderEvalOptions struct {
	ActivePlanInProgress bool
}

type ReminderEngine struct {
	mu       sync.Mutex
	sessions map[string]*ReminderState
}

func NewReminderEngine() *ReminderEngine {
	return &ReminderEngine{
		sessions: map[string]*ReminderState{},
	}
}

func (e *ReminderEngine) Evaluate(sessionID string, opts ReminderEvalOptions) []core.ReminderEvent {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.ensureState(sessionID)
	now := time.Now().UTC()
	event := core.ReminderEvent{}

	switch {
	case state.PendingDenialReminder:
		event = core.ReminderEvent{
			Kind:    core.ReminderKindRespectToolDenial,
			Message: "The user denied a mutating tool in the previous turn. Do not retry the same action unless the user explicitly changes direction. Choose a different strategy or explain the blocker.",
			Count:   1,
			At:      now,
		}
		state.PendingDenialReminder = false
	case opts.ActivePlanInProgress && state.LastTurn.HadZeroToolExecution:
		event = core.ReminderEvent{
			Kind:    core.ReminderKindActivePlanInProgress,
			Message: "An approved or executing plan is still in progress. Do not conclude the task yet. Continue executing the plan or clearly explain the blocker.",
			Count:   1,
			At:      now,
		}
	case state.FailureSignature != "" && state.FailureStreak >= 2 && !state.FailureReminderSuppressed:
		event = core.ReminderEvent{
			Kind:    core.ReminderKindRepeatFailureChangeStrategy,
			Message: "You hit the same failure repeatedly. Change strategy instead of retrying the same approach.",
			Count:   state.FailureStreak,
			At:      now,
		}
		state.FailureReminderSuppressed = true
	case hasReadOnlyLoop(state.CompletedTurns) && !state.ReadOnlyLoopSuppressed:
		event = core.ReminderEvent{
			Kind:    core.ReminderKindReadOnlyLoopBreakout,
			Message: "You have repeated the same read-only tool sequence without making progress. Stop looping; take the next concrete action or explain the blocker.",
			Count:   3,
			At:      now,
		}
		state.ReadOnlyLoopSuppressed = true
	}

	if event.Kind == "" {
		state.LastAppliedReminder = ""
		return nil
	}
	state.LastAppliedReminder = event.Kind
	return []core.ReminderEvent{event}
}

func (e *ReminderEngine) RecordCompleted(sessionID string, resp core.ChatResponse) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.ensureState(sessionID)
	summary := summarizeReminderTurn(resp.ToolEvents, time.Now().UTC())
	state.LastTurn = summary
	state.PendingDenialReminder = summary.HadMutatingDenial
	state.CompletedTurns = append(state.CompletedTurns, summary)
	if len(state.CompletedTurns) > 3 {
		state.CompletedTurns = append([]TurnSummary(nil), state.CompletedTurns[len(state.CompletedTurns)-3:]...)
	}
	if !hasReadOnlyLoop(state.CompletedTurns) {
		state.ReadOnlyLoopSuppressed = false
	}
	e.updateFailureTrackingLocked(state, summary.FailureSignature)
}

func (e *ReminderEngine) RecordFailure(sessionID string, failureSignature string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.ensureState(sessionID)
	summary := TurnSummary{
		FailureSignature:     strings.TrimSpace(failureSignature),
		HadZeroToolExecution: true,
		At:                   time.Now().UTC(),
	}
	state.LastTurn = summary
	state.PendingDenialReminder = false
	state.CompletedTurns = nil
	state.ReadOnlyLoopSuppressed = false
	e.updateFailureTrackingLocked(state, summary.FailureSignature)
}

func (e *ReminderEngine) ensureState(sessionID string) *ReminderState {
	state, ok := e.sessions[sessionID]
	if ok {
		return state
	}
	state = &ReminderState{}
	e.sessions[sessionID] = state
	return state
}

func (e *ReminderEngine) updateFailureTrackingLocked(state *ReminderState, signature string) {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		state.FailureSignature = ""
		state.FailureStreak = 0
		state.FailureReminderSuppressed = false
		return
	}
	if signature == state.FailureSignature {
		state.FailureStreak++
		return
	}
	state.FailureSignature = signature
	state.FailureStreak = 1
	state.FailureReminderSuppressed = false
}

func summarizeReminderTurn(events []core.ToolEvent, at time.Time) TurnSummary {
	summary := TurnSummary{At: at}
	for _, evt := range events {
		switch evt.Phase {
		case "executed":
			if strings.TrimSpace(evt.ToolName) == "" {
				continue
			}
			summary.ExecutedToolNames = append(summary.ExecutedToolNames, evt.ToolName)
			if evt.Mutating {
				summary.HasMutatingExecution = true
			}
		case "denied":
			if evt.Mutating {
				summary.HadMutatingDenial = true
			}
		case "failed":
			if summary.FailureSignature == "" && strings.TrimSpace(evt.ToolName) != "" {
				summary.FailureSignature = "tool:" + strings.TrimSpace(evt.ToolName) + ":failed"
			}
		}
	}
	summary.HadZeroToolExecution = len(summary.ExecutedToolNames) == 0
	return summary
}

func hasReadOnlyLoop(turns []TurnSummary) bool {
	if len(turns) < 3 {
		return false
	}
	window := turns[len(turns)-3:]
	var signature string
	for _, turn := range window {
		if len(turn.ExecutedToolNames) == 0 {
			return false
		}
		if turn.HasMutatingExecution || turn.HadMutatingDenial {
			return false
		}
		current := strings.Join(turn.ExecutedToolNames, "\x00")
		if signature == "" {
			signature = current
			continue
		}
		if current != signature {
			return false
		}
	}
	return signature != ""
}

func reminderSystemMessages(reminders []core.ReminderEvent) []core.Message {
	if len(reminders) == 0 {
		return nil
	}
	messages := make([]core.Message, 0, len(reminders))
	for _, reminder := range reminders {
		messages = append(messages, core.Message{
			Role:      core.RoleSystem,
			Content:   reminder.Message,
			CreatedAt: reminder.At,
		})
	}
	return messages
}
