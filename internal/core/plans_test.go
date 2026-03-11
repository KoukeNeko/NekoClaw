package core

import "testing"

func TestPlanStoreCreateReloadAndSupersede(t *testing.T) {
	store, err := NewPlanStore(t.TempDir())
	if err != nil {
		t.Fatalf("new plan store: %v", err)
	}

	first, err := store.Create(PlanRecord{
		SessionID:     "s1",
		Provider:      "anthropic",
		Model:         "claude",
		Status:        PlanStatusPendingApproval,
		RequestPrompt: "ship it",
		PlanMarkdown:  "## Goal\nship it",
		PlanSummary:   "ship it",
	})
	if err != nil {
		t.Fatalf("create first plan: %v", err)
	}
	second, err := store.Create(PlanRecord{
		SessionID:     "s1",
		Provider:      "anthropic",
		Model:         "claude",
		Status:        PlanStatusPendingApproval,
		RequestPrompt: "ship it again",
		PlanMarkdown:  "## Goal\nship it again",
		PlanSummary:   "ship it again",
	})
	if err != nil {
		t.Fatalf("create second plan: %v", err)
	}

	gotFirst, ok := store.Get(first.ID)
	if !ok {
		t.Fatalf("expected first plan to exist")
	}
	if gotFirst.Status != PlanStatusSuperseded {
		t.Fatalf("expected first plan to be superseded, got %s", gotFirst.Status)
	}

	reloaded, err := NewPlanStore(store.dataDir)
	if err != nil {
		t.Fatalf("reload plan store: %v", err)
	}
	gotSecond, ok := reloaded.Get(second.ID)
	if !ok {
		t.Fatalf("expected second plan to exist after reload")
	}
	if gotSecond.PlanMarkdown != second.PlanMarkdown {
		t.Fatalf("expected markdown to survive reload, got %q", gotSecond.PlanMarkdown)
	}

	list := reloaded.List("s1")
	if len(list) != 2 {
		t.Fatalf("expected 2 plans in session, got %d", len(list))
	}
	if list[0].ID != second.ID {
		t.Fatalf("expected newest plan first, got %s", list[0].ID)
	}
}
