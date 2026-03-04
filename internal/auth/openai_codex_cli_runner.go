//go:build !windows

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/creack/pty"
)

type openAICodexCLIRunner struct{}

func NewOpenAICodexCLIRunner() OpenAICodexCLIRunner {
	return openAICodexCLIRunner{}
}

func (openAICodexCLIRunner) Available(_ context.Context) error {
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("%w: codex binary not found in PATH", ErrOpenAICodexCLINotFound)
	}
	return nil
}

func (openAICodexCLIRunner) RunLogin(ctx context.Context, emit func(message string)) (string, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("%w: codex binary not found in PATH", ErrOpenAICodexCLINotFound)
	}

	cmd := exec.CommandContext(ctx, path, "login", "--device-auth")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOpenAICodexPTYUnavailable, err)
	}
	defer func() { _ = ptmx.Close() }()

	var token string
	tail := ""
	emitCarry := ""
	buffer := make([]byte, 4096)
	for {
		n, readErr := ptmx.Read(buffer)
		if n > 0 {
			chunk := string(buffer[:n])
			if emit != nil {
				lines, nextCarry := extractDisplayLinesFromCLIChunk(emitCarry, chunk)
				emitCarry = nextCarry
				for _, line := range lines {
					emit(line)
				}
			}

			search := tail + chunk
			matches := extractOpenAICodexTokens(search)
			for _, candidate := range matches {
				if validateOpenAICodexTokenRaw(candidate) == nil {
					token = strings.TrimSpace(candidate)
				}
			}
			if len(search) > 1024 {
				tail = search[len(search)-1024:]
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

	if emit != nil {
		if line := flushDisplayCLIEventCarry(emitCarry); line != "" {
			emit(line)
		}
	}

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if validateOpenAICodexTokenRaw(token) != nil {
		fileToken, fileErr := loadOpenAICodexAccessTokenFromAuthFile()
		if fileErr == nil && validateOpenAICodexTokenRaw(fileToken) == nil {
			token = strings.TrimSpace(fileToken)
		}
	}

	if waitErr != nil && validateOpenAICodexTokenRaw(token) != nil {
		return "", waitErr
	}
	if err := validateOpenAICodexTokenRaw(token); err != nil {
		return "", ErrOpenAICodexTokenNotDetected
	}
	return strings.TrimSpace(token), nil
}

func loadOpenAICodexAccessTokenFromAuthFile() (string, error) {
	path, err := openAICodexAuthFilePath()
	if err != nil {
		return "", err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var typed struct {
		AccessToken string `json:"access_token"`
		Tokens      struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(payload, &typed); err == nil {
		if token := strings.TrimSpace(typed.Tokens.AccessToken); token != "" {
			return token, nil
		}
		if token := strings.TrimSpace(typed.AccessToken); token != "" {
			return token, nil
		}
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return "", err
	}
	if token := strings.TrimSpace(anyToString(raw["access_token"])); token != "" {
		return token, nil
	}
	if nested, ok := raw["tokens"].(map[string]any); ok {
		if token := strings.TrimSpace(anyToString(nested["access_token"])); token != "" {
			return token, nil
		}
	}
	return "", ErrOpenAICodexTokenNotDetected
}

func openAICodexAuthFilePath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("OPENAI_CODEX_AUTH_FILE")); custom != "" {
		return custom, nil
	}
	home := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if home == "" {
		resolvedHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(resolvedHome, ".codex")
	}
	return filepath.Join(home, "auth.json"), nil
}

func anyToString(value any) string {
	switch cast := value.(type) {
	case string:
		return cast
	default:
		return ""
	}
}
