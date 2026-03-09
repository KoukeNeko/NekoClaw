package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestGenerateUsesStreamGenerateContentAndParsesSSE(t *testing.T) {
	var gotPath string
	var gotAccept string
	var gotProject string
	var gotModel string
	var gotRole string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAccept = r.Header.Get("Accept")

		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		gotProject, _ = payload["project"].(string)
		gotModel, _ = payload["model"].(string)

		requestRoot, _ := payload["request"].(map[string]any)
		contents, _ := requestRoot["contents"].([]any)
		if len(contents) > 1 {
			content, _ := contents[1].(map[string]any)
			gotRole, _ = content["role"].(string)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello \"}]}}]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"World\"}]}}],\"usageMetadata\":{\"totalTokenCount\":12}}}\n\n"))
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{srv.URL},
	})
	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-2.5-pro",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
			{Role: core.RoleAssistant, Content: "prev"},
		},
		Account: core.Account{
			ID:       "a1",
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    "token-1",
			Metadata: core.Metadata{
				"project_id": "project-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Text != "Hello World" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
	if gotPath != "/v1internal:streamGenerateContent?alt=sse" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if !strings.Contains(gotAccept, "text/event-stream") {
		t.Fatalf("unexpected accept header: %q", gotAccept)
	}
	if gotProject != "project-1" {
		t.Fatalf("unexpected project: %q", gotProject)
	}
	if gotModel != "gemini-2.5-pro" {
		t.Fatalf("unexpected model: %q", gotModel)
	}
	if gotRole != "model" {
		t.Fatalf("unexpected assistant role mapping: %q", gotRole)
	}
}

func TestGenerateOmitsPenaltyForFlashLiteModels(t *testing.T) {
	temp := 0.7
	topP := 0.9
	freq := 0.3
	pres := 0.2

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		requestRoot, _ := payload["request"].(map[string]any)
		genConfig, _ := requestRoot["generationConfig"].(map[string]any)
		if genConfig == nil {
			t.Fatalf("expected generationConfig in request payload")
		}
		if genConfig["temperature"] != 0.7 {
			t.Fatalf("expected temperature to be preserved, got %v", genConfig["temperature"])
		}
		if genConfig["topP"] != 0.9 {
			t.Fatalf("expected topP to be preserved, got %v", genConfig["topP"])
		}
		if _, ok := genConfig["frequencyPenalty"]; ok {
			t.Fatalf("expected frequencyPenalty to be omitted, got %v", genConfig["frequencyPenalty"])
		}
		if _, ok := genConfig["presencePenalty"]; ok {
			t.Fatalf("expected presencePenalty to be omitted, got %v", genConfig["presencePenalty"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}}\n\n"))
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{srv.URL},
	})
	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-3.1-flash-lite-preview",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Generation: &GenerationParams{
			Temperature:      &temp,
			TopP:             &topP,
			FrequencyPenalty: &freq,
			PresencePenalty:  &pres,
		},
		Account: core.Account{
			ID:       "a1",
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    "token-1",
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Text != "ok" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
}

func TestGenerateMapsThinkingBudgetForGemini25(t *testing.T) {
	mode := core.ThinkingModeHigh

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		requestRoot, _ := payload["request"].(map[string]any)
		genConfig, _ := requestRoot["generationConfig"].(map[string]any)
		if genConfig == nil {
			t.Fatalf("expected generationConfig in request payload")
		}
		thinkingConfig, _ := genConfig["thinkingConfig"].(map[string]any)
		if thinkingConfig == nil {
			t.Fatalf("expected thinkingConfig in request payload")
		}
		if thinkingConfig["thinkingBudget"] != float64(24576) {
			t.Fatalf("expected thinkingBudget 24576, got %v", thinkingConfig["thinkingBudget"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"budget\"}]}}]}}\n\n"))
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{srv.URL},
	})
	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-2.5-pro",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Generation: &GenerationParams{
			ThinkingMode: &mode,
		},
		Account: core.Account{
			ID:       "a1",
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    "token-1",
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Text != "budget" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
}

func TestGenerateToolTurnMapsThinkingLevelForGemini3Pro(t *testing.T) {
	mode := core.ThinkingModeMinimal

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		requestRoot, _ := payload["request"].(map[string]any)
		genConfig, _ := requestRoot["generationConfig"].(map[string]any)
		if genConfig == nil {
			t.Fatalf("expected generationConfig in request payload")
		}
		thinkingConfig, _ := genConfig["thinkingConfig"].(map[string]any)
		if thinkingConfig == nil {
			t.Fatalf("expected thinkingConfig in request payload")
		}
		if thinkingConfig["thinkingLevel"] != "low" {
			t.Fatalf("expected thinkingLevel low, got %v", thinkingConfig["thinkingLevel"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"tool\"}]}}]}}\n\n"))
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{srv.URL},
	})
	resp, err := p.GenerateToolTurn(context.Background(), ToolTurnRequest{
		Model: "gemini-3.1-pro-preview",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Generation: &GenerationParams{
			ThinkingMode: &mode,
		},
		Account: core.Account{
			ID:       "a1",
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    "token-1",
		},
	})
	if err != nil {
		t.Fatalf("generate tool turn failed: %v", err)
	}
	if resp.Text != "tool" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
}

func TestGenerateReturnsErrorWhenConfiguredPathReturns404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:generateMessage" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"reply":"ok"}`))
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints:    []string{srv.URL},
		GeneratePath: "/v1internal:generateMessage",
	})
	_, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-2.5-pro",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{ID: "a1", Provider: "google-gemini-cli", Type: core.AccountOAuth, Token: "token-1"},
	})
	if err == nil {
		t.Fatalf("expected error for invalid endpoint path")
	}
}

func TestGenerateParsesSSEWhenSingleEventUsesMultipleDataLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello\"}]}}]}\n"))
		_, _ = w.Write([]byte("data: }\n\n"))
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{srv.URL},
	})
	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-2.5-pro",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{
			ID:       "a1",
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    "token-1",
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Text != "Hello" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
}

func TestGenerateFallsBackWhenEndpointReturnsNoTextPayload(t *testing.T) {
	noText := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[]}}\n\n"))
	}))
	defer noText.Close()

	okText := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Recovered\"}]}}]}}\n\n"))
	}))
	defer okText.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{noText.URL, okText.URL},
	})
	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-2.5-pro",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{
			ID:       "a1",
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    "token-1",
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Text != "Recovered" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
	if resp.Endpoint != okText.URL {
		t.Fatalf("expected second endpoint, got %q", resp.Endpoint)
	}
}

func TestGenerateFallsBackFromServiceDisabledSandboxEndpoint(t *testing.T) {
	autopushCalls := 0
	autopush := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		autopushCalls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{
			"error": {
				"code": 403,
				"status": "PERMISSION_DENIED",
				"details": [{"reason":"SERVICE_DISABLED"}]
			}
		}`))
	}))
	defer autopush.Close()

	prodCalls := 0
	prod := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prodCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Recovered on prod\"}]}}]}}\n\n"))
	}))
	defer prod.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{prod.URL, autopush.URL},
	})
	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-3.1-pro-preview",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{
			ID:       "a1",
			Provider: "google-gemini-cli",
			Type:     core.AccountOAuth,
			Token:    "token-1",
			Metadata: core.Metadata{
				"endpoint": autopush.URL,
			},
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Text != "Recovered on prod" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
	if resp.Endpoint != prod.URL {
		t.Fatalf("expected prod endpoint, got %q", resp.Endpoint)
	}
	if autopushCalls == 0 {
		t.Fatalf("expected preferred autopush endpoint to be attempted first")
	}
	if prodCalls == 0 {
		t.Fatalf("expected fallback to prod endpoint")
	}
}
