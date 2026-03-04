package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/doeshing/nekoclaw/internal/core"
)

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (m *MockProvider) ID() string {
	return "mock"
}

func (m *MockProvider) ContextWindow(_ string) int {
	return 32000
}

func (m *MockProvider) ToolCapabilities() ToolCapabilities {
	return ToolCapabilities{SupportsTools: false}
}

func (m *MockProvider) Generate(_ context.Context, req GenerateRequest) (GenerateResponse, error) {
	prompt := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == core.RoleUser {
			prompt = strings.TrimSpace(req.Messages[i].Content)
			break
		}
	}
	if prompt == "" {
		prompt = "(empty prompt)"
	}
	return GenerateResponse{
		Text:     fmt.Sprintf("mock(%s): %s", req.Model, prompt),
		Endpoint: "local://mock",
	}, nil
}

func (m *MockProvider) GenerateToolTurn(_ context.Context, _ ToolTurnRequest) (ToolTurnResponse, error) {
	return ToolTurnResponse{}, &FailureError{
		Reason:   core.FailureFormat,
		Message:  "provider mock does not support tool calling",
		Endpoint: "local://mock",
	}
}
