package tooling

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Policy struct {
	WorkspaceRoot        string
	MaxOutputBytes       int
	MaxFileReadBytes     int
	MaxListEntries       int
	MaxSearchResults     int
	CommandTimeout       time.Duration
	CommandAllowReadOnly map[string]struct{}
	CommandAllowMutating map[string]struct{}
}

func DefaultPolicy(workspaceRoot string) Policy {
	root := strings.TrimSpace(workspaceRoot)
	readOnly := map[string]struct{}{
		"ls":   {},
		"cat":  {},
		"pwd":  {},
		"rg":   {},
		"find": {},
	}
	mutating := map[string]struct{}{
		"git": {},
	}
	return Policy{
		WorkspaceRoot:        root,
		MaxOutputBytes:       64 * 1024,
		MaxFileReadBytes:     256 * 1024,
		MaxListEntries:       200,
		MaxSearchResults:     100,
		CommandTimeout:       30 * time.Second,
		CommandAllowReadOnly: readOnly,
		CommandAllowMutating: mutating,
	}
}

func (p Policy) ResolvePath(path string) (string, error) {
	root := strings.TrimSpace(p.WorkspaceRoot)
	if root == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	if strings.TrimSpace(path) == "" {
		return root, nil
	}
	var full string
	if filepath.IsAbs(path) {
		full = filepath.Clean(path)
	} else {
		full = filepath.Clean(filepath.Join(root, path))
	}
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path outside workspace root")
	}
	return full, nil
}

func (p Policy) NormalizeWorkdir(workdir string) (string, error) {
	if strings.TrimSpace(workdir) == "" {
		return p.ResolvePath(".")
	}
	return p.ResolvePath(workdir)
}

func (p Policy) ValidateCommand(argv []string) (mutating bool, err error) {
	if len(argv) == 0 {
		return false, fmt.Errorf("argv is required")
	}
	cmd := strings.TrimSpace(argv[0])
	if cmd == "" {
		return false, fmt.Errorf("argv[0] is required")
	}
	if _, lookErr := exec.LookPath(cmd); lookErr != nil {
		return false, fmt.Errorf("command not found: %s", cmd)
	}
	switch cmd {
	case "git":
		if len(argv) < 2 {
			return false, fmt.Errorf("git subcommand is required")
		}
		sub := strings.TrimSpace(argv[1])
		switch sub {
		case "status", "diff", "log", "show":
			return false, nil
		case "add", "restore", "commit":
			return true, nil
		case "push", "pull", "fetch", "clone":
			return false, fmt.Errorf("git remote operations are not allowed")
		default:
			return false, fmt.Errorf("git subcommand not allowed: %s", sub)
		}
	default:
		if _, ok := p.CommandAllowReadOnly[cmd]; ok {
			return false, nil
		}
		if _, ok := p.CommandAllowMutating[cmd]; ok {
			return true, nil
		}
		return false, fmt.Errorf("command not allowed: %s", cmd)
	}
}
