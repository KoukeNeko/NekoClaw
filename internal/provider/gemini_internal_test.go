package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverProjectEndpointFallback(t *testing.T) {
	prod := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:loadCodeAssist" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"prod down"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer prod.Close()

	daily := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:loadCodeAssist" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cloudaicompanionProject": "daily-project",
				"currentTier":             map[string]any{"id": "free-tier"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer daily.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{prod.URL, daily.URL},
	})
	result, err := p.DiscoverProject(context.Background(), DiscoverProjectRequest{
		Token: "test-token",
	})
	if err != nil {
		t.Fatalf("discover project failed: %v", err)
	}
	if result.ProjectID != "daily-project" {
		t.Fatalf("expected daily-project, got %q", result.ProjectID)
	}
	if result.ActiveEndpoint != daily.URL {
		t.Fatalf("expected daily endpoint, got %q", result.ActiveEndpoint)
	}
}

func TestDiscoverProjectSecurityPolicyUsesEnvProject(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "proj-lro")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"details": []any{
						map[string]any{"reason": "SECURITY_POLICY_VIOLATED"},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{srv.URL},
	})
	result, err := p.DiscoverProject(context.Background(), DiscoverProjectRequest{
		Token: "test-token",
	})
	if err != nil {
		t.Fatalf("discover project failed: %v", err)
	}
	if result.ProjectID != "proj-lro" {
		t.Fatalf("expected proj-lro, got %q", result.ProjectID)
	}
	if result.ActiveEndpoint != srv.URL {
		t.Fatalf("expected active endpoint %q, got %q", srv.URL, result.ActiveEndpoint)
	}
}

func TestDiscoverProjectRequiresProjectForNonFreeTier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:loadCodeAssist" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"currentTier": map[string]any{"id": "standard-tier"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{srv.URL},
	})
	_, err := p.DiscoverProject(context.Background(), DiscoverProjectRequest{
		Token: "test-token",
	})
	if err == nil {
		t.Fatalf("expected non-free tier project error")
	}
	if !strings.Contains(err.Error(), "GOOGLE_CLOUD_PROJECT") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverProjectUsesLegacyTierWhenAllowedTierHasNoDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:loadCodeAssist" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"allowedTiers": []map[string]any{
					{"id": "standard-tier"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{srv.URL},
	})
	_, err := p.DiscoverProject(context.Background(), DiscoverProjectRequest{
		Token: "test-token",
	})
	if err == nil {
		t.Fatalf("expected project env requirement error")
	}
	if !strings.Contains(err.Error(), "GOOGLE_CLOUD_PROJECT") {
		t.Fatalf("unexpected error: %v", err)
	}
}
