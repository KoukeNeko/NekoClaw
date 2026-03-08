package usage

import (
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestSummarizeSessionStoreAggregatesUsage(t *testing.T) {
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	store := core.NewSessionStore()

	store.Append("alpha",
		userEntry(now.Add(-5*time.Minute), "hello"),
		assistantEntry(now, "openai", "gpt-5", &core.UsageInfo{InputTokens: 120, OutputTokens: 30, TotalTokens: 150}),
		assistantEntry(now.AddDate(0, 0, -3), "openai", "gpt-4o", &core.UsageInfo{InputTokens: 50, OutputTokens: 25, TotalTokens: 75}),
		compactionEntry(now.Add(-time.Minute)),
	)
	store.Append("beta",
		userEntry(now.AddDate(0, 0, -1), "question"),
		assistantEntry(now.AddDate(0, 0, -1), "anthropic", "claude-sonnet-4", &core.UsageInfo{InputTokens: 80, OutputTokens: 20, TotalTokens: 100}),
		assistantEntry(now.Add(10*time.Minute), "anthropic", "claude-haiku-3", nil),
		compactionEntry(now.Add(11*time.Minute)),
	)
	store.SetTitle("alpha", "Alpha Session")
	store.SetTitle("beta", "Beta Session")

	summary := SummarizeSessionStore(store, now)

	if summary.Totals.SessionCount != 2 {
		t.Fatalf("session_count=%d want 2", summary.Totals.SessionCount)
	}
	if summary.Totals.MessageCount != 6 {
		t.Fatalf("message_count=%d want 6", summary.Totals.MessageCount)
	}
	if summary.Totals.RequestCount != 4 {
		t.Fatalf("request_count=%d want 4", summary.Totals.RequestCount)
	}
	if summary.Totals.InputTokens != 250 || summary.Totals.OutputTokens != 75 || summary.Totals.TotalTokens != 325 {
		t.Fatalf("unexpected token totals: %+v", summary.Totals)
	}
	if summary.Totals.CompactionCount != 2 {
		t.Fatalf("compaction_count=%d want 2", summary.Totals.CompactionCount)
	}

	wantCost := CalculateCost(120, 30, "gpt-5") +
		CalculateCost(50, 25, "gpt-4o") +
		CalculateCost(80, 20, "claude-sonnet-4")
	if summary.Totals.EstimatedCostUSD != wantCost {
		t.Fatalf("estimated_cost_usd=%f want %f", summary.Totals.EstimatedCostUSD, wantCost)
	}

	if len(summary.Providers) != 2 {
		t.Fatalf("providers len=%d want 2", len(summary.Providers))
	}
	if summary.Providers[0].Provider != "openai" || summary.Providers[0].TotalTokens != 225 || summary.Providers[0].RequestCount != 2 {
		t.Fatalf("unexpected openai provider aggregate: %+v", summary.Providers[0])
	}
	if summary.Providers[1].Provider != "anthropic" || summary.Providers[1].TotalTokens != 100 || summary.Providers[1].RequestCount != 2 {
		t.Fatalf("unexpected anthropic provider aggregate: %+v", summary.Providers[1])
	}

	if len(summary.Models) != 4 {
		t.Fatalf("models len=%d want 4", len(summary.Models))
	}
	if summary.Models[0].Model != "gpt-5" || summary.Models[0].TotalTokens != 150 {
		t.Fatalf("unexpected top model: %+v", summary.Models[0])
	}
	if summary.Models[3].Model != "claude-haiku-3" || summary.Models[3].RequestCount != 1 || summary.Models[3].TotalTokens != 0 {
		t.Fatalf("missing-usage model aggregate incorrect: %+v", summary.Models[3])
	}

	if len(summary.Sessions) != 2 {
		t.Fatalf("sessions len=%d want 2", len(summary.Sessions))
	}
	var alpha, beta SessionSummary
	for _, session := range summary.Sessions {
		switch session.SessionID {
		case "alpha":
			alpha = session
		case "beta":
			beta = session
		}
	}
	if alpha.Title != "Alpha Session" || alpha.RequestCount != 2 || alpha.TotalTokens != 225 || alpha.CompactionCount != 1 || alpha.TopProvider != "openai" || alpha.TopModel != "gpt-5" {
		t.Fatalf("unexpected alpha session summary: %+v", alpha)
	}
	if beta.Title != "Beta Session" || beta.RequestCount != 2 || beta.TotalTokens != 100 || beta.CompactionCount != 1 || beta.TopProvider != "anthropic" || beta.TopModel != "claude-sonnet-4" {
		t.Fatalf("unexpected beta session summary: %+v", beta)
	}

	trendMap := map[string]TrendPoint{}
	for _, point := range summary.Trend {
		trendMap[point.Date] = point
	}
	if point := trendMap["2026-03-08"]; point.TotalTokens != 150 || point.RequestCount != 2 {
		t.Fatalf("today trend incorrect: %+v", point)
	}
	if point := trendMap["2026-03-07"]; point.TotalTokens != 100 || point.RequestCount != 1 {
		t.Fatalf("yesterday trend incorrect: %+v", point)
	}
	if point := trendMap["2026-03-05"]; point.TotalTokens != 75 || point.RequestCount != 1 {
		t.Fatalf("older trend incorrect: %+v", point)
	}
}

func TestSummarizeSessionStoreMergesTailModelsIntoOther(t *testing.T) {
	now := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	store := core.NewSessionStore()

	models := []struct {
		name   string
		tokens int
	}{
		{name: "model-a", tokens: 70},
		{name: "model-b", tokens: 60},
		{name: "model-c", tokens: 50},
		{name: "model-d", tokens: 40},
		{name: "model-e", tokens: 30},
		{name: "model-f", tokens: 20},
		{name: "model-g", tokens: 10},
	}

	entries := make([]core.SessionEntry, 0, len(models))
	for idx, model := range models {
		ts := now.Add(time.Duration(idx) * time.Minute)
		entries = append(entries, assistantEntry(ts, "mock", model.name, &core.UsageInfo{
			InputTokens:  model.tokens,
			OutputTokens: 0,
			TotalTokens:  model.tokens,
		}))
	}
	store.Append("gamma", entries...)

	summary := SummarizeSessionStore(store, now)
	if len(summary.Models) != 7 {
		t.Fatalf("models len=%d want 7", len(summary.Models))
	}
	if summary.Models[0].Model != "model-a" || summary.Models[5].Model != "model-f" {
		t.Fatalf("unexpected named model ordering: %+v", summary.Models)
	}
	last := summary.Models[len(summary.Models)-1]
	if last.Model != "other" || last.TotalTokens != 10 || last.RequestCount != 1 {
		t.Fatalf("unexpected other aggregate: %+v", last)
	}
}

func userEntry(ts time.Time, content string) core.SessionEntry {
	entry := core.NewMessageEntry(core.RoleUser, content)
	entry.Timestamp = ts
	return entry
}

func assistantEntry(ts time.Time, providerID, modelID string, info *core.UsageInfo) core.SessionEntry {
	return core.SessionEntry{
		Type:        core.EntryMessage,
		ID:          core.NewEntryID(),
		Timestamp:   ts,
		Role:        core.RoleAssistant,
		Content:     "reply",
		MsgProvider: providerID,
		MsgModel:    modelID,
		MsgUsage:    info,
	}
}

func compactionEntry(ts time.Time) core.SessionEntry {
	entry := core.NewCompactionEntry("compressed", 2, 150, "next")
	entry.Timestamp = ts
	return entry
}
