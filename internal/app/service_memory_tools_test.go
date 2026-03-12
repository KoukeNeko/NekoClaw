package app

import (
	"context"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/provider"
)

func toolNames(defs []provider.ToolDefinition) map[string]struct{} {
	out := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		out[def.Name] = struct{}{}
	}
	return out
}

func TestHandleChatExposesMemoryToolsInActionMode(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &captureToolTurnProvider{id: "google-ai-studio"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "g1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-g1"},
	}, []string{"g1"}, core.DefaultCooldownConfig()))

	_, err := svc.HandleChat(context.Background(), core.ChatRequest{
		SessionID:      "memory-action",
		DisableSession: true,
		Surface:        core.SurfaceWeb,
		Provider:       "google-ai-studio",
		Model:          "default",
		Message:        "remember this",
		EnableTools:    true,
	})
	if err != nil {
		t.Fatalf("HandleChat() error = %v", err)
	}

	names := toolNames(prov.lastRequest().Tools)
	for _, name := range []string{"memory_search", "memory_get", "memory_save"} {
		if _, ok := names[name]; !ok {
			t.Fatalf("action tool schema missing %q: %#v", name, names)
		}
	}
}

func TestCreatePlanKeepsMemorySaveOutOfPlannerSchemas(t *testing.T) {
	svc := NewService(ServiceOptions{})
	prov := &captureToolTurnProvider{id: "google-ai-studio"}
	svc.RegisterProvider(prov)
	svc.RegisterPool(core.NewAccountPool("google-ai-studio", []core.Account{
		{ID: "g1", Provider: "google-ai-studio", Type: core.AccountAPIKey, Token: "token-g1"},
	}, []string{"g1"}, core.DefaultCooldownConfig()))
	svc.SetModelRolesConfig(core.ModelRolesConfig{
		Planner: core.ModelRoleConfig{
			Provider: "google-ai-studio",
			Model:    "default",
		},
	})

	_, err := svc.CreatePlan(context.Background(), core.CreatePlanRequest{
		SessionID: "memory-plan",
		Surface:   core.SurfaceWeb,
		Prompt:    "inspect the memory tools",
	})
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	names := toolNames(prov.lastRequest().Tools)
	for _, name := range []string{"memory_search", "memory_get"} {
		if _, ok := names[name]; !ok {
			t.Fatalf("planner tool schema missing %q: %#v", name, names)
		}
	}
	if _, ok := names["memory_save"]; ok {
		t.Fatalf("planner tool schema should not include memory_save: %#v", names)
	}
}
