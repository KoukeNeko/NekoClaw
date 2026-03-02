package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestGoogleAIStudioGenerateParsesText(t *testing.T) {
	var gotPath string
	var gotKey string
	var gotRoles []string
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotKey = req.URL.Query().Get("key")

			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			contents, _ := payload["contents"].([]any)
			for _, item := range contents {
				obj, _ := item.(map[string]any)
				role, _ := obj["role"].(string)
				gotRoles = append(gotRoles, role)
			}
			return newHTTPResponse(http.StatusOK, `{
				"candidates": [{
					"content": {"parts":[{"text":"hello"},{"text":"world"}]}
				}]
			}`), nil
		}),
	}
	p := NewGoogleAIStudioProvider(GoogleAIStudioOptions{
		BaseURL:    "https://generativelanguage.googleapis.com/v1beta",
		HTTPClient: client,
	})

	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-2.5-pro",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
			{Role: core.RoleAssistant, Content: "previous"},
		},
		Account: core.Account{
			ID:       "k1",
			Provider: "google-ai-studio",
			Type:     core.AccountAPIKey,
			Token:    "key-1",
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if gotPath != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotKey != "key-1" {
		t.Fatalf("unexpected key query: %q", gotKey)
	}
	if len(gotRoles) != 2 || gotRoles[0] != "user" || gotRoles[1] != "model" {
		t.Fatalf("unexpected role mapping: %#v", gotRoles)
	}
	if resp.Text != "hello\nworld" {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
}

func TestGoogleAIStudioGenerateClassifiesInvalidAPIKey(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(http.StatusBadRequest, `{
				"error": {
					"code": 400,
					"message": "API key not valid. Please pass a valid API key."
				}
			}`), nil
		}),
	}
	p := NewGoogleAIStudioProvider(GoogleAIStudioOptions{
		HTTPClient: client,
	})

	_, err := p.Generate(context.Background(), GenerateRequest{
		Model: "gemini-2.5-pro",
		Messages: []core.Message{
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{
			ID:       "k1",
			Provider: "google-ai-studio",
			Type:     core.AccountAPIKey,
			Token:    "invalid",
		},
	})
	if err == nil {
		t.Fatalf("expected generate error")
	}
	failure, ok := err.(*FailureError)
	if !ok {
		t.Fatalf("expected FailureError, got %T", err)
	}
	if failure.Reason != core.FailureAuthPermanent {
		t.Fatalf("unexpected reason: %s", failure.Reason)
	}
	if failure.Status != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", failure.Status)
	}
}

func TestGoogleAIStudioListModelsUsesCacheAndDefaultSelection(t *testing.T) {
	modelCalls := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/v1beta/models" {
				modelCalls++
				return newHTTPResponse(http.StatusOK, `{
					"models":[
						{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"]},
						{"name":"models/gemini-2.5-pro","supportedGenerationMethods":["generateContent"]},
						{"name":"models/embedding-001","supportedGenerationMethods":["embedContent"]}
					]
				}`), nil
			}
			return newHTTPResponse(http.StatusNotFound, `{"error":"not found"}`), nil
		}),
	}
	p := NewGoogleAIStudioProvider(GoogleAIStudioOptions{
		BaseURL:    "https://generativelanguage.googleapis.com/v1beta",
		HTTPClient: client,
	})
	account := core.Account{
		ID:       "ai-studio-1",
		Provider: "google-ai-studio",
		Type:     core.AccountAPIKey,
		Token:    "key-1",
	}

	models, source, cachedUntil, err := p.ListModelsWithSource(context.Background(), account)
	if err != nil {
		t.Fatalf("list models first call: %v", err)
	}
	if source != "live" {
		t.Fatalf("unexpected source on first call: %q", source)
	}
	if cachedUntil.IsZero() {
		t.Fatalf("expected cached_until")
	}
	if len(models) != 2 || models[0] != "gemini-2.5-flash" || models[1] != "gemini-2.5-pro" {
		t.Fatalf("unexpected filtered models: %#v", models)
	}

	_, source, _, err = p.ListModelsWithSource(context.Background(), account)
	if err != nil {
		t.Fatalf("list models second call: %v", err)
	}
	if source != "cache" {
		t.Fatalf("unexpected source on second call: %q", source)
	}
	if modelCalls != 1 {
		t.Fatalf("expected one live call due to cache, got %d", modelCalls)
	}

	modelID, discoverSource, err := p.DiscoverPreferredModel(context.Background(), account)
	if err != nil {
		t.Fatalf("discover preferred model: %v", err)
	}
	if modelID != "gemini-2.5-pro" {
		t.Fatalf("unexpected discovered model: %q", modelID)
	}
	if discoverSource != "models.list" {
		t.Fatalf("unexpected discovered source: %q", discoverSource)
	}
}

func TestClassifyAIStudioStatus(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   core.FailureReason
	}{
		{status: http.StatusUnauthorized, body: "Unauthorized", want: core.FailureAuthPermanent},
		{status: http.StatusForbidden, body: "quota exceeded", want: core.FailureBilling},
		{status: http.StatusForbidden, body: "permission denied", want: core.FailureAuthPermanent},
		{status: http.StatusTooManyRequests, body: "RESOURCE_EXHAUSTED", want: core.FailureRateLimit},
		{status: http.StatusBadRequest, body: "API key not valid", want: core.FailureAuthPermanent},
		{status: http.StatusBadRequest, body: "INVALID_ARGUMENT", want: core.FailureFormat},
		{status: http.StatusInternalServerError, body: "internal", want: core.FailureUnknown},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(strings.ReplaceAll(string(tc.want), "_", "-"), func(t *testing.T) {
			got := classifyAIStudioStatus(tc.status, tc.body)
			if got != tc.want {
				t.Fatalf("classifyAIStudioStatus(%d, %q)=%s want=%s", tc.status, tc.body, got, tc.want)
			}
		})
	}
}
