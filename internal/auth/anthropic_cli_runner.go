//go:build !windows

package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/creack/pty"
)

type anthropicCLIRunner struct{}

func NewAnthropicCLIRunner() AnthropicCLIRunner {
	return anthropicCLIRunner{}
}

func (anthropicCLIRunner) Available(_ context.Context) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("%w: claude binary not found in PATH", ErrAnthropicCLINotFound)
	}
	return nil
}

func (anthropicCLIRunner) RunSetupToken(ctx context.Context, emit func(message string)) (string, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("%w: claude binary not found in PATH", ErrAnthropicCLINotFound)
	}

	cmd := exec.CommandContext(ctx, path, "setup-token")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrAnthropicPTYUnavailable, err)
	}
	defer func() { _ = ptmx.Close() }()

	var token string
	tail := ""
	buffer := make([]byte, 4096)
	for {
		n, readErr := ptmx.Read(buffer)
		if n > 0 {
			chunk := string(buffer[:n])
			if emit != nil {
				for _, line := range strings.Split(chunk, "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					emit(line)
				}
			}

			search := tail + chunk
			matches := extractAnthropicSetupTokens(search)
			for _, candidate := range matches {
				if validateAnthropicSetupTokenRaw(candidate) == nil {
					token = candidate
				}
			}
			if len(search) > 512 {
				tail = search[len(search)-512:]
			} else {
				tail = search
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", readErr
		}
	}

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if waitErr != nil && token == "" {
		return "", waitErr
	}
	if validateAnthropicSetupTokenRaw(token) != nil {
		return "", ErrAnthropicTokenNotDetected
	}
	return token, nil
}
