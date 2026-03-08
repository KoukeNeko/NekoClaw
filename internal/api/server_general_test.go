package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/app"
	"github.com/doeshing/nekoclaw/internal/core"
)

func TestGeneralConfigEndpoint_PersistsTimezone(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	configDir := t.TempDir()
	svc.SetConfigDir(configDir)

	handler := NewServer(svc).Handler()

	getResp := performJSONRequest(t, handler, http.MethodGet, "/v1/general/config", "")
	if getResp.Code != http.StatusOK {
		t.Fatalf("unexpected GET status: %d body=%s", getResp.Code, getResp.Body.String())
	}

	var initial core.GeneralConfig
	if err := json.Unmarshal(getResp.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial general config: %v", err)
	}
	if initial.Timezone != "" {
		t.Fatalf("expected empty initial timezone, got %q", initial.Timezone)
	}

	putResp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/general/config",
		`{"timezone":"Asia/Taipei"}`,
	)
	if putResp.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %d body=%s", putResp.Code, putResp.Body.String())
	}

	var saved core.GeneralConfig
	if err := json.Unmarshal(putResp.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved general config: %v", err)
	}
	if saved.Timezone != "Asia/Taipei" {
		t.Fatalf("saved timezone = %q, want %q", saved.Timezone, "Asia/Taipei")
	}

	config, err := core.LoadConfig(configDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if config.General.Timezone != "Asia/Taipei" {
		t.Fatalf("config general.timezone = %q, want %q", config.General.Timezone, "Asia/Taipei")
	}
}

func TestGeneralConfigEndpoint_RejectsInvalidTimezone(t *testing.T) {
	svc := app.NewService(app.ServiceOptions{})
	svc.SetConfigDir(t.TempDir())

	handler := NewServer(svc).Handler()
	resp := performJSONRequest(
		t,
		handler,
		http.MethodPut,
		"/v1/general/config",
		`{"timezone":"Invalid/Timezone"}`,
	)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid timezone") {
		t.Fatalf("expected invalid timezone error, got %s", resp.Body.String())
	}
}
