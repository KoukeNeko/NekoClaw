//go:build windows

package auth

import (
	"context"
	"fmt"
)

type openAICodexCLIRunner struct{}

func NewOpenAICodexCLIRunner() OpenAICodexCLIRunner {
	return openAICodexCLIRunner{}
}

func (openAICodexCLIRunner) Available(_ context.Context) error {
	return fmt.Errorf("%w: pty bridge is not supported on windows", ErrOpenAICodexPTYUnavailable)
}

func (openAICodexCLIRunner) RunLogin(_ context.Context, _ func(message string)) (string, error) {
	return "", fmt.Errorf("%w: pty bridge is not supported on windows", ErrOpenAICodexPTYUnavailable)
}
