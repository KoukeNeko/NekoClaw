package app

import (
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestReminderEngineRespectToolDenialTriggersOnce(t *testing.T) {
	engine := NewReminderEngine()
	engine.RecordCompleted("session-1", core.ChatResponse{
		ToolEvents: []core.ToolEvent{{
			ToolName: "file_write",
			Phase:    "denied",
			Mutating: true,
		}},
	})

	got := engine.Evaluate("session-1", ReminderEvalOptions{})
	if len(got) != 1 || got[0].Kind != core.ReminderKindRespectToolDenial {
		t.Fatalf("expected respect_tool_denial reminder, got %#v", got)
	}

	if again := engine.Evaluate("session-1", ReminderEvalOptions{}); len(again) != 0 {
		t.Fatalf("expected denial reminder to trigger once, got %#v", again)
	}
}

func TestReminderEngineRepeatFailureTriggersAfterTwoMatchingFailures(t *testing.T) {
	engine := NewReminderEngine()

	engine.RecordFailure("session-1", "rate_limit")
	if got := engine.Evaluate("session-1", ReminderEvalOptions{}); len(got) != 0 {
		t.Fatalf("expected no reminder after first failure, got %#v", got)
	}

	engine.RecordFailure("session-1", "rate_limit")
	got := engine.Evaluate("session-1", ReminderEvalOptions{})
	if len(got) != 1 || got[0].Kind != core.ReminderKindRepeatFailureChangeStrategy {
		t.Fatalf("expected repeat_failure_change_strategy reminder, got %#v", got)
	}

	if again := engine.Evaluate("session-1", ReminderEvalOptions{}); len(again) != 0 {
		t.Fatalf("expected repeat failure reminder to be suppressed, got %#v", again)
	}

	engine.RecordFailure("session-1", "timeout")
	if got := engine.Evaluate("session-1", ReminderEvalOptions{}); len(got) != 0 {
		t.Fatalf("expected streak reset for different failure, got %#v", got)
	}
}

func TestReminderEngineReadOnlyLoopTriggersAfterThreeRepeatedTurns(t *testing.T) {
	engine := NewReminderEngine()
	resp := core.ChatResponse{
		ToolEvents: []core.ToolEvent{
			{ToolName: "rg", Phase: "executed"},
			{ToolName: "read_file", Phase: "executed"},
		},
	}

	engine.RecordCompleted("session-1", resp)
	engine.RecordCompleted("session-1", resp)
	if got := engine.Evaluate("session-1", ReminderEvalOptions{}); len(got) != 0 {
		t.Fatalf("expected no reminder before third repeated turn, got %#v", got)
	}

	engine.RecordCompleted("session-1", resp)
	got := engine.Evaluate("session-1", ReminderEvalOptions{})
	if len(got) != 1 || got[0].Kind != core.ReminderKindReadOnlyLoopBreakout {
		t.Fatalf("expected read_only_loop_breakout reminder, got %#v", got)
	}
}

func TestReminderEngineActivePlanInProgressRequiresZeroToolLastTurn(t *testing.T) {
	engine := NewReminderEngine()
	engine.RecordCompleted("session-1", core.ChatResponse{})

	if got := engine.Evaluate("session-1", ReminderEvalOptions{}); len(got) != 0 {
		t.Fatalf("expected no reminder without active plan, got %#v", got)
	}

	got := engine.Evaluate("session-1", ReminderEvalOptions{ActivePlanInProgress: true})
	if len(got) != 1 || got[0].Kind != core.ReminderKindActivePlanInProgress {
		t.Fatalf("expected active_plan_in_progress reminder, got %#v", got)
	}
}

func TestReminderEnginePriorityPrefersToolDenial(t *testing.T) {
	engine := NewReminderEngine()
	engine.RecordCompleted("session-1", core.ChatResponse{
		ToolEvents: []core.ToolEvent{{
			ToolName: "file_write",
			Phase:    "denied",
			Mutating: true,
		}},
	})

	got := engine.Evaluate("session-1", ReminderEvalOptions{ActivePlanInProgress: true})
	if len(got) != 1 || got[0].Kind != core.ReminderKindRespectToolDenial {
		t.Fatalf("expected denial reminder to win priority, got %#v", got)
	}
}

func TestPlannerModeDoesNotConsumeExecutionReminder(t *testing.T) {
	svc := NewService(ServiceOptions{})
	svc.reminders.RecordCompleted("session-1", core.ChatResponse{
		ToolEvents: []core.ToolEvent{{
			ToolName: "file_write",
			Phase:    "denied",
			Mutating: true,
		}},
	})

	if got := svc.requestReminders("session-1", core.ToolModePlanner); len(got) != 0 {
		t.Fatalf("expected planner mode to skip reminders, got %#v", got)
	}

	got := svc.requestReminders("session-1", core.ToolModeDefault)
	if len(got) != 1 || got[0].Kind != core.ReminderKindRespectToolDenial {
		t.Fatalf("expected reminder to remain available after planner run, got %#v", got)
	}
}
