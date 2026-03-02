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
