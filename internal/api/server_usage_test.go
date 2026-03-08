package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/usage"
)

func TestUsageSummaryEndpointReturnsAggregatedShape(t *testing.T) {
	store := core.NewSessionStore()
	now := time.Now()

	store.Append("one",
		core.NewMessageEntry(core.RoleUser, "hello"),
		core.SessionEntry{
			Type:        core.EntryMessage,
			ID:          core.NewEntryID(),
			Timestamp:   now,
			Role:        core.RoleAssistant,
			Content:     "reply",
			MsgProvider: "openai",
			MsgModel:    "gpt-5",
			MsgUsage: &core.UsageInfo{
				InputTokens:  100,
				OutputTokens: 20,
				TotalTokens:  120,
			},
		},
		core.NewCompactionEntry("summary", 1, 50, "next"),
	)
	store.Append("two",
		core.SessionEntry{
			Type:        core.EntryMessage,
			ID:          core.NewEntryID(),
			Timestamp:   now.Add(-time.Hour),
			Role:        core.RoleAssistant,
			Content:     "reply",
			MsgProvider: "anthropic",
			MsgModel:    "claude-sonnet-4",
			MsgUsage: &core.UsageInfo{
				InputTokens:  30,
				OutputTokens: 10,
				TotalTokens:  40,
			},
		},
	)

	svc := app.NewService(app.ServiceOptions{SessionStore: store})
	handler := NewServer(svc).Handler()

	resp := performJSONRequest(t, handler, http.MethodGet, "/v1/usage/summary", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var got usage.Summary
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode summary: %v", err)
	}

	if got.GeneratedAt.IsZero() {
		t.Fatalf("generated_at should be set")
	}
	if got.Totals.SessionCount != 2 || got.Totals.RequestCount != 2 || got.Totals.CompactionCount != 1 {
		t.Fatalf("unexpected totals: %+v", got.Totals)
	}
	if len(got.Providers) != 2 || got.Providers[0].Provider != "openai" || got.Providers[1].Provider != "anthropic" {
		t.Fatalf("unexpected providers: %+v", got.Providers)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("unexpected session summaries: %+v", got.Sessions)
	}
	if got.Sessions[0].UpdatedAt.Before(got.Sessions[1].UpdatedAt) {
		t.Fatalf("sessions should be sorted by updated_at desc: %+v", got.Sessions)
	}
}
