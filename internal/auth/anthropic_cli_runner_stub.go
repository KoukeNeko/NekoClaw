//go:build windows

package auth

import (
	"context"
	"fmt"
)

type anthropicCLIRunner struct{}

func NewAnthropicCLIRunner() AnthropicCLIRunner {
	return anthropicCLIRunner{}
}

func (anthropicCLIRunner) Available(_ context.Context) error {
	return fmt.Errorf("%w: pty bridge is not supported on windows", ErrAnthropicPTYUnavailable)
}

func (anthropicCLIRunner) RunSetupToken(_ context.Context, _ func(message string)) (string, error) {
	return "", fmt.Errorf("%w: pty bridge is not supported on windows", ErrAnthropicPTYUnavailable)
}
