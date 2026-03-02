package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestDiscoverPreferredModelFromFetchAvailable(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/v1internal:fetchAvailableModels" {
				return newHTTPResponse(http.StatusOK, `{"models":{"gemini-2.5-pro":{},"gemini-3-pro-preview":{},"gemini-2.5-flash":{}}}`), nil
			}
			return newHTTPResponse(http.StatusNotFound, `{"error":"not found"}`), nil
		}),
	}
	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints:  []string{"https://endpoint-a.test"},
		HTTPClient: client,
	})

	modelID, source, err := p.DiscoverPreferredModel(context.Background(), core.Account{
		ID:       "p1",
		Provider: "google-gemini-cli",
		Token:    "token-1",
	})
	if err != nil {
		t.Fatalf("discover preferred model: %v", err)
	}
	if modelID != "gemini-3-pro-preview" {
		t.Fatalf("unexpected model: %q", modelID)
	}
	if source != "fetchAvailableModels" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestDiscoverPreferredModelFallsBackToQuota(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/v1internal:fetchAvailableModels":
				return newHTTPResponse(http.StatusInternalServerError, `{"error":"fail"}`), nil
			case "/v1internal:retrieveUserQuota":
				return newHTTPResponse(http.StatusOK, `{"buckets":[{"modelId":"gemini-2.5-flash"},{"modelId":"gemini-2.5-pro"}]}`), nil
			default:
				return newHTTPResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
		}),
	}
	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints:  []string{"https://endpoint-a.test"},
		HTTPClient: client,
	})

	modelID, source, err := p.DiscoverPreferredModel(context.Background(), core.Account{
		ID:       "p2",
		Provider: "google-gemini-cli",
		Token:    "token-2",
	})
	if err != nil {
		t.Fatalf("discover preferred model: %v", err)
	}
	if modelID != "gemini-2.5-pro" {
		t.Fatalf("unexpected model: %q", modelID)
	}
	if source != "quota" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestDiscoverPreferredModelFallsBackToDefault(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/v1internal:fetchAvailableModels":
				return newHTTPResponse(http.StatusInternalServerError, `{"error":"fail"}`), nil
			case "/v1internal:retrieveUserQuota":
				return newHTTPResponse(http.StatusInternalServerError, `{"error":"fail"}`), nil
			default:
				return newHTTPResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
		}),
	}
	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints:  []string{"https://endpoint-a.test"},
		HTTPClient: client,
	})

	modelID, source, err := p.DiscoverPreferredModel(context.Background(), core.Account{
		ID:       "p3",
		Provider: "google-gemini-cli",
		Token:    "token-3",
	})
	if err != nil {
		t.Fatalf("discover preferred model: %v", err)
	}
	if modelID != "gemini-3-pro-preview" {
		t.Fatalf("unexpected model: %q", modelID)
	}
	if source != "fallback" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestDiscoverPreferredModelUsesCache(t *testing.T) {
	fetchCalls := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/v1internal:fetchAvailableModels" {
				fetchCalls++
				return newHTTPResponse(http.StatusOK, `{"models":{"gemini-3-pro-preview":{}}}`), nil
			}
			return newHTTPResponse(http.StatusNotFound, `{"error":"not found"}`), nil
		}),
	}
	p := NewGeminiInternalProvider(GeminiInternalOptions{
		Endpoints:  []string{"https://endpoint-a.test"},
		HTTPClient: client,
	})

	account := core.Account{ID: "cache-1", Provider: "google-gemini-cli", Token: "token"}
	_, _, err := p.DiscoverPreferredModel(context.Background(), account)
	if err != nil {
		t.Fatalf("first discover preferred model: %v", err)
	}
	_, _, err = p.DiscoverPreferredModel(context.Background(), account)
	if err != nil {
		t.Fatalf("second discover preferred model: %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("expected cached model lookup, fetch calls=%d", fetchCalls)
	}
}
