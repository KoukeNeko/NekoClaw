package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

func TestGitHubModelsProviderGenerateParsesChatCompletions(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotModel string

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotAuth = req.Header.Get("Authorization")

			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			gotModel, _ = payload["model"].(string)

			return newHTTPResponse(http.StatusOK, `{
				"choices":[
					{"message":{"content":"hello from github models"}}
				],
				"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
			}`), nil
		}),
	}
	p := NewGitHubModelsProvider(GitHubModelsOptions{
		BaseURL:      "https://models.github.ai",
		DefaultModel: defaultGitHubModelsModel,
		HTTPClient:   client,
	})

	resp, err := p.Generate(context.Background(), GenerateRequest{
		Model: "default",
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: "system"},
			{Role: core.RoleUser, Content: "hi"},
		},
		Account: core.Account{
			Provider: "github-models",
			Type:     core.AccountToken,
			Token:    "ghp_test_pat",
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if gotPath != "/inference/chat/completions" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotAuth != "Bearer ghp_test_pat" {
		t.Fatalf("unexpected authorization header: %q", gotAuth)
	}
	if gotModel != defaultGitHubModelsModel {
		t.Fatalf("unexpected model: %q", gotModel)
	}
	if resp.Text != "hello from github models" {
		t.Fatalf("unexpected text: %q", resp.Text)
	}
	if resp.Usage.TotalTokens != 18 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestGitHubModelsProviderListModelsFiltersAndSorts(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/catalog/models" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			return newHTTPResponse(http.StatusOK, `[
				{"id":"openai/gpt-5-mini","supported_input_modalities":["text"],"supported_output_modalities":["text"]},
				{"id":"openai/gpt-4o","supported_input_modalities":["text","image"],"supported_output_modalities":["text"]},
				{"id":"openai/gpt-4o","supported_input_modalities":["text","image"],"supported_output_modalities":["text"]},
				{"id":"openai/gpt-image","supported_input_modalities":["image"],"supported_output_modalities":["text"]},
				{"id":"audio/model","supported_input_modalities":["text"],"supported_output_modalities":["audio"]},
				{"id":"openai/gpt-5-chat","supported_input_modalities":["text"],"supported_output_modalities":["text"]}
			]`), nil
		}),
	}
	p := NewGitHubModelsProvider(GitHubModelsOptions{
		HTTPClient: client,
	})

	models, err := p.ListModels(context.Background(), core.Account{})
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	want := []string{"openai/gpt-4o", "openai/gpt-5-chat", "openai/gpt-5-mini"}
	if strings.Join(models, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected models: got=%v want=%v", models, want)
	}
}

func TestGitHubModelsContextWindowUsesNamespacedModelIDs(t *testing.T) {
	p := NewGitHubModelsProvider(GitHubModelsOptions{})
	if got := p.ContextWindow("openai/gpt-4.1"); got != 1_000_000 {
		t.Fatalf("ContextWindow(openai/gpt-4.1)=%d want=1000000", got)
	}
	if got := p.ContextWindow("openai/gpt-5-chat"); got != 200_000 {
		t.Fatalf("ContextWindow(openai/gpt-5-chat)=%d want=200000", got)
	}
}

func TestGitHubModelsGenerateRejectsImages(t *testing.T) {
	p := NewGitHubModelsProvider(GitHubModelsOptions{
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected outbound request: %s", req.URL.String())
				return nil, nil
			}),
		},
	})

	_, err := p.Generate(context.Background(), GenerateRequest{
		Model: "default",
		Messages: []core.Message{
			{
				Role:    core.RoleUser,
				Content: "describe this image",
				Images: []core.ImageData{
					{MimeType: "image/png", Data: "ZmFrZQ=="},
				},
			},
		},
		Account: core.Account{
			Provider: "github-models",
			Type:     core.AccountToken,
			Token:    "ghp_test_pat",
		},
	})
	if err == nil {
		t.Fatalf("expected image input to fail")
	}
	var failureErr *FailureError
	if !strings.Contains(err.Error(), "image input") || !strings.Contains(err.Error(), "endpoint=https://models.github.ai") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected bad request status, got %v", err)
	}
	if !strings.Contains(err.Error(), "github models provider") {
		t.Fatalf("expected provider message, got %v", err)
	}
	if !errors.As(err, &failureErr) {
		t.Fatalf("expected FailureError, got %T", err)
	}
	if failureErr.Reason != core.FailureFormat {
		t.Fatalf("unexpected failure reason: %s", failureErr.Reason)
	}
}

func TestClassifyGitHubModelsStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   core.FailureReason
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: "bad credentials", want: core.FailureAuthPermanent},
		{name: "forbidden auth", status: http.StatusForbidden, body: "resource not accessible", want: core.FailureAuthPermanent},
		{name: "forbidden billing", status: http.StatusForbidden, body: "billing quota exceeded", want: core.FailureBilling},
		{name: "bad request", status: http.StatusBadRequest, body: "invalid request", want: core.FailureFormat},
		{name: "not found", status: http.StatusNotFound, body: "model not found", want: core.FailureModelNotFound},
		{name: "rate limit", status: http.StatusTooManyRequests, body: "too many requests", want: core.FailureRateLimit},
		{name: "timeout", status: http.StatusGatewayTimeout, body: "upstream timeout", want: core.FailureTimeout},
		{name: "server error", status: http.StatusInternalServerError, body: "internal error", want: core.FailureUnknown},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyGitHubModelsStatus(tc.status, tc.body); got != tc.want {
				t.Fatalf("classifyGitHubModelsStatus(%d, %q)=%s want=%s", tc.status, tc.body, got, tc.want)
			}
		})
	}
}
