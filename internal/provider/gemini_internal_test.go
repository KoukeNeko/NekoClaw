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

func TestDiscoverProjectUsesAcceptedPlatformEnum(t *testing.T) {
	var capturedPlatform string
	var capturedClientMetadata map[string]any
	var capturedAPIClient string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:loadCodeAssist" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		metadata, _ := payload["metadata"].(map[string]any)
		capturedPlatform, _ = metadata["platform"].(string)
		capturedAPIClient = strings.TrimSpace(r.Header.Get("X-Goog-Api-Client"))
		if metaHeader := strings.TrimSpace(r.Header.Get("Client-Metadata")); metaHeader != "" {
			_ = json.Unmarshal([]byte(metaHeader), &capturedClientMetadata)
		}
		switch capturedPlatform {
		case "MACOS", "LINUX", "WINDOWS":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cloudaicompanionProject": "proj-accepted-platform",
				"currentTier":             map[string]any{"id": "free-tier"},
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    400,
				"message": "Invalid value at 'metadata.platform'",
			},
		})
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
	if result.ProjectID != "proj-accepted-platform" {
		t.Fatalf("expected proj-accepted-platform, got %q", result.ProjectID)
	}
	if strings.TrimSpace(capturedPlatform) == "" {
		t.Fatalf("expected platform metadata to be set")
	}
	if capturedPlatform != "MACOS" && capturedPlatform != "LINUX" && capturedPlatform != "WINDOWS" {
		t.Fatalf("unexpected platform enum %q", capturedPlatform)
	}
	if !strings.HasPrefix(capturedAPIClient, "gl-node/") {
		t.Fatalf("expected X-Goog-Api-Client to start with gl-node/, got %q", capturedAPIClient)
	}
	if capturedClientMetadata == nil {
		t.Fatalf("expected Client-Metadata header to be present")
	}
	if got, _ := capturedClientMetadata["platform"].(string); got != capturedPlatform {
		t.Fatalf("expected Client-Metadata platform %q, got %q", capturedPlatform, got)
	}
}

func TestDiscoverProjectRetriesWithFallbackPlatformWhenPrimaryRejected(t *testing.T) {
	var firstPlatform string
	var acceptedPlatform string
	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:loadCodeAssist" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		metadata, _ := payload["metadata"].(map[string]any)
		platform, _ := metadata["platform"].(string)
		platform = strings.TrimSpace(platform)

		if firstPlatform == "" {
			firstPlatform = platform
		}
		if platform == firstPlatform {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":    400,
					"message": "Invalid value at 'metadata.platform'",
				},
			})
			return
		}
		acceptedPlatform = platform
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cloudaicompanionProject": "proj-fallback-platform",
			"currentTier":             map[string]any{"id": "free-tier"},
		})
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
	if result.ProjectID != "proj-fallback-platform" {
		t.Fatalf("expected fallback project, got %q", result.ProjectID)
	}
	if requests < 2 {
		t.Fatalf("expected at least two loadCodeAssist attempts for platform fallback, got %d", requests)
	}
	if strings.TrimSpace(firstPlatform) == "" {
		t.Fatalf("expected first platform to be captured")
	}
	if strings.TrimSpace(acceptedPlatform) == "" || acceptedPlatform == firstPlatform {
		t.Fatalf("expected accepted fallback platform different from first; first=%q accepted=%q", firstPlatform, acceptedPlatform)
	}
}

func TestDiscoverProjectFallsBackToEnvProjectWhenAllLoadEndpointsFail(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project-fallback")

	failA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:loadCodeAssist" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"endpoint down"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer failA.Close()

	failB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:loadCodeAssist" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"endpoint down"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer failB.Close()

	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints: []string{failA.URL, failB.URL},
	})
	result, err := p.DiscoverProject(context.Background(), DiscoverProjectRequest{
		Token: "test-token",
	})
	if err != nil {
		t.Fatalf("discover project failed: %v", err)
	}
	if result.ProjectID != "env-project-fallback" {
		t.Fatalf("expected env fallback project, got %q", result.ProjectID)
	}
	if result.ActiveEndpoint != "" {
		t.Fatalf("expected empty active endpoint for env fallback, got %q", result.ActiveEndpoint)
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
