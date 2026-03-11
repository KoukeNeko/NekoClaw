package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

type fakePlanProvider struct{}

func (p *fakePlanProvider) ID() string { return "planner" }

func (p *fakePlanProvider) ContextWindow(string) int { return 200_000 }

func (p *fakePlanProvider) Generate(_ context.Context, req provider.GenerateRequest) (provider.GenerateResponse, error) {
	return provider.GenerateResponse{Text: req.Messages[len(req.Messages)-1].Content}, nil
}

func (p *fakePlanProvider) ToolCapabilities() provider.ToolCapabilities {
	return provider.ToolCapabilities{SupportsTools: true}
}

func (p *fakePlanProvider) GenerateToolTurn(_ context.Context, _ provider.ToolTurnRequest) (provider.ToolTurnResponse, error) {
	return provider.ToolTurnResponse{
		Text: strings.TrimSpace(`
## Goal
Add a planner

## Context
- Existing runtime

## Files to Modify
- internal/app/service.go

## New Files
- internal/app/service_plans.go

## Implementation Steps
- add plan store

## Verification
- go test ./...

## Risks
- surface wiring
`),
	}, nil
}

func TestPlansAPIFlow(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.RegisterProvider(&fakePlanProvider{})
	svc.RegisterPool(core.NewAccountPool("planner", []core.Account{
		{ID: "planner-1", Provider: "planner", Type: core.AccountToken, Token: "x"},
	}, nil, core.DefaultCooldownConfig()))

	server := NewServer(svc)
	handler := server.Handler()

	createResp := performJSONRequest(t, handler, http.MethodPost, "/v1/plans", `{"session_id":"s1","surface":"web","provider":"planner","model":"default","prompt":"plan this task"}`)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create plan status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var created core.PlanRecord
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created plan: %v", err)
	}
	if created.Status != core.PlanStatusPendingApproval {
		t.Fatalf("unexpected created status: %s", created.Status)
	}

	listResp := performJSONRequest(t, handler, http.MethodGet, "/v1/plans?session_id=s1", "")
	if listResp.Code != http.StatusOK || !strings.Contains(listResp.Body.String(), created.ID) {
		t.Fatalf("list plans failed: code=%d body=%s", listResp.Code, listResp.Body.String())
	}

	getResp := performJSONRequest(t, handler, http.MethodGet, "/v1/plans/"+created.ID, "")
	if getResp.Code != http.StatusOK || !strings.Contains(getResp.Body.String(), created.ID) {
		t.Fatalf("get plan failed: code=%d body=%s", getResp.Code, getResp.Body.String())
	}

	approveResp := performJSONRequest(t, handler, http.MethodPost, "/v1/plans/"+created.ID+"/approve", "")
	if approveResp.Code != http.StatusOK || !strings.Contains(approveResp.Body.String(), `"status":"approved"`) {
		t.Fatalf("approve plan failed: code=%d body=%s", approveResp.Code, approveResp.Body.String())
	}

	rejectCreate := performJSONRequest(t, handler, http.MethodPost, "/v1/plans", `{"session_id":"s1","surface":"web","provider":"planner","model":"default","prompt":"plan another task"}`)
	if rejectCreate.Code != http.StatusOK {
		t.Fatalf("create rejectable plan status=%d body=%s", rejectCreate.Code, rejectCreate.Body.String())
	}
	var rejectable core.PlanRecord
	if err := json.Unmarshal(rejectCreate.Body.Bytes(), &rejectable); err != nil {
		t.Fatalf("decode rejectable plan: %v", err)
	}
	rejectResp := performJSONRequest(t, handler, http.MethodPost, "/v1/plans/"+rejectable.ID+"/reject", "")
	if rejectResp.Code != http.StatusOK || !strings.Contains(rejectResp.Body.String(), `"status":"rejected"`) {
		t.Fatalf("reject plan failed: code=%d body=%s", rejectResp.Code, rejectResp.Body.String())
	}
}
