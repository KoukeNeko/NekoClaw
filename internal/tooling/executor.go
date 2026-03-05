package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/provider"
)

type RuntimeExecutor struct {
	backend        Backend
	policy         Policy
	specs          map[string]ToolSpec
	httpClient     *http.Client // shared by web_search, web_fetch
	braveSearchKey string       // Brave Search API key (empty = web_search disabled)
}

// ExecutorConfig holds optional configuration for tools that need external
// services (e.g. web search API keys).
type ExecutorConfig struct {
	BraveSearchAPIKey string
}

func NewRuntimeExecutor(backend Backend, policy Policy, cfg ExecutorConfig) *RuntimeExecutor {
	specs := map[string]ToolSpec{
		"file_list":      {Definition: toolDef("file_list", "List files in the workspace.", `{"type":"object","properties":{"path":{"type":"string"},"recursive":{"type":"boolean"},"max_entries":{"type":"integer","minimum":1}}}`)},
		"file_read":      {Definition: toolDef("file_read", "Read file content.", `{"type":"object","required":["path"],"properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}}}`)},
		"file_search":    {Definition: toolDef("file_search", "Search text in files.", `{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"},"max_results":{"type":"integer","minimum":1}}}`)},
		"sessions_list":  {Definition: toolDef("sessions_list", "List chat sessions.", `{"type":"object","properties":{"limit":{"type":"integer","minimum":1}}}`)},
		"memory_search":  {Definition: toolDef("memory_search", "Search memory index.", `{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1}}}`)},
		"providers_list": {Definition: toolDef("providers_list", "List providers.", `{"type":"object","properties":{}}`)},
		"accounts_list":  {Definition: toolDef("accounts_list", "List provider accounts.", `{"type":"object","required":["provider"],"properties":{"provider":{"type":"string"}}}`)},
		"git_status":     {Definition: toolDef("git_status", "Run git status.", `{"type":"object","properties":{}}`)},
		"git_diff":       {Definition: toolDef("git_diff", "Run git diff.", `{"type":"object","properties":{"pathspec":{"type":"string"},"staged":{"type":"boolean"}}}`)},
		"git_log":        {Definition: toolDef("git_log", "Run git log.", `{"type":"object","properties":{"limit":{"type":"integer","minimum":1}}}`)},
		"git_show":       {Definition: toolDef("git_show", "Run git show.", `{"type":"object","properties":{"rev":{"type":"string"}}}`)},
		"exec_command":   {Definition: toolDef("exec_command", "Execute an allowlisted command.", `{"type":"object","required":["argv"],"properties":{"argv":{"type":"array","items":{"type":"string"}},"workdir":{"type":"string"},"timeout_sec":{"type":"integer","minimum":1}}}`)},
		"file_write":     {Definition: toolDef("file_write", "Write file content.", `{"type":"object","required":["path","content"],"properties":{"path":{"type":"string"},"content":{"type":"string"},"mode":{"type":"string","enum":["overwrite","append"]}}}`), Mutating: true},
		"file_replace":   {Definition: toolDef("file_replace", "Replace text in a file.", `{"type":"object","required":["path","old","new"],"properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"replace_all":{"type":"boolean"}}}`), Mutating: true},
		"git_add":        {Definition: toolDef("git_add", "Run git add.", `{"type":"object","required":["pathspecs"],"properties":{"pathspecs":{"type":"array","items":{"type":"string"}}}}`), Mutating: true},
		"git_restore":    {Definition: toolDef("git_restore", "Run git restore.", `{"type":"object","required":["pathspecs"],"properties":{"pathspecs":{"type":"array","items":{"type":"string"}},"staged":{"type":"boolean"}}}`), Mutating: true},
		"git_commit":     {Definition: toolDef("git_commit", "Run git commit.", `{"type":"object","required":["message"],"properties":{"message":{"type":"string"}}}`), Mutating: true},
		// AI assistant tools — always available, no external dependencies.
		"datetime":  {Definition: toolDef("datetime", "Get current date, time, timezone, and unix timestamp.", `{"type":"object","properties":{}}`)},
		"web_fetch": {Definition: toolDef("web_fetch", "Fetch a web page and return its text content. Useful for reading articles, documentation, or URLs shared by the user.", `{"type":"object","required":["url"],"properties":{"url":{"type":"string"},"max_chars":{"type":"integer","minimum":100}}}`)},
	}

	// web_search requires an API key; only register when configured.
	braveKey := strings.TrimSpace(cfg.BraveSearchAPIKey)
	if braveKey != "" {
		specs["web_search"] = ToolSpec{Definition: toolDef("web_search",
			"Search the web using Brave Search. Returns titles, URLs, and snippets.",
			`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"count":{"type":"integer","minimum":1,"maximum":20}}}`)}
	}

	return &RuntimeExecutor{
		backend:        backend,
		policy:         policy,
		specs:          specs,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		braveSearchKey: braveKey,
	}
}

func toolDef(name, description, schema string) provider.ToolDefinition {
	return provider.ToolDefinition{
		Name:        name,
		Description: description,
		InputSchema: json.RawMessage(schema),
	}
}

func (e *RuntimeExecutor) Definitions() []provider.ToolDefinition {
	out := make([]provider.ToolDefinition, 0, len(e.specs))
	for _, spec := range e.specs {
		out = append(out, spec.Definition)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (e *RuntimeExecutor) HasTool(toolName string) bool {
	_, ok := e.specs[strings.TrimSpace(toolName)]
	return ok
}

func (e *RuntimeExecutor) IsMutating(toolName string) bool {
	spec, ok := e.specs[strings.TrimSpace(toolName)]
	return ok && spec.Mutating
}

func (e *RuntimeExecutor) IsCallMutating(call provider.ToolCall) bool {
	name := strings.TrimSpace(call.Name)
	if name == "exec_command" {
		argv := struct {
			Argv []string `json:"argv"`
		}{}
		if err := json.Unmarshal(call.Arguments, &argv); err != nil {
			return true
		}
		mutating, err := e.policy.ValidateCommand(argv.Argv)
		if err != nil {
			return true
		}
		return mutating
	}
	return e.IsMutating(name)
}

func (e *RuntimeExecutor) ArgumentPreview(call provider.ToolCall) string {
	return trimPreview(string(call.Arguments), 220)
}

func (e *RuntimeExecutor) Run(ctx context.Context, call provider.ToolCall) (string, error) {
	switch strings.TrimSpace(call.Name) {
	case "file_list":
		return e.runFileList(call.Arguments)
	case "file_read":
		return e.runFileRead(call.Arguments)
	case "file_search":
		return e.runFileSearch(call.Arguments)
	case "sessions_list":
		return e.runSessionsList(call.Arguments)
	case "memory_search":
		return e.runMemorySearch(call.Arguments)
	case "providers_list":
		return e.runProvidersList()
	case "accounts_list":
		return e.runAccountsList(call.Arguments)
	case "git_status":
		return e.runCommand(ctx, []string{"git", "status", "--short", "--branch"}, ".", 0)
	case "git_diff":
		return e.runGitDiff(ctx, call.Arguments)
	case "git_log":
		return e.runGitLog(ctx, call.Arguments)
	case "git_show":
		return e.runGitShow(ctx, call.Arguments)
	case "exec_command":
		return e.runExecCommand(ctx, call.Arguments)
	case "file_write":
		return e.runFileWrite(call.Arguments)
	case "file_replace":
		return e.runFileReplace(call.Arguments)
	case "git_add":
		return e.runGitAdd(ctx, call.Arguments)
	case "git_restore":
		return e.runGitRestore(ctx, call.Arguments)
	case "git_commit":
		return e.runGitCommit(ctx, call.Arguments)
	case "datetime":
		return e.runDatetime()
	case "web_fetch":
		return e.runWebFetch(call.Arguments)
	case "web_search":
		return e.runWebSearch(call.Arguments)
	default:
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
}

func (e *RuntimeExecutor) runFileList(raw json.RawMessage) (string, error) {
	var args struct {
		Path       string `json:"path"`
		Recursive  bool   `json:"recursive"`
		MaxEntries int    `json:"max_entries"`
	}
	_ = json.Unmarshal(raw, &args)
	base, err := e.policy.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	maxEntries := args.MaxEntries
	if maxEntries <= 0 || maxEntries > e.policy.MaxListEntries {
		maxEntries = e.policy.MaxListEntries
	}
	results := make([]string, 0, maxEntries)
	if !args.Recursive {
		entries, err := os.ReadDir(base)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			results = append(results, name)
			if len(results) >= maxEntries {
				break
			}
		}
		sort.Strings(results)
		return strings.Join(results, "\n"), nil
	}
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			rel += "/"
		}
		results = append(results, rel)
		if len(results) >= maxEntries {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(results)
	return strings.Join(results, "\n"), nil
}

func (e *RuntimeExecutor) runFileRead(raw json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	full, err := e.policy.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if len(content) > e.policy.MaxFileReadBytes {
		content = content[:e.policy.MaxFileReadBytes]
	}
	lines := strings.Split(string(content), "\n")
	start := args.StartLine
	if start <= 0 {
		start = 1
	}
	end := args.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return "", fmt.Errorf("start_line must be <= end_line")
	}
	out := strings.Join(lines[start-1:end], "\n")
	return trimPreview(out, e.policy.MaxOutputBytes), nil
}

func (e *RuntimeExecutor) runFileSearch(raw json.RawMessage) (string, error) {
	var args struct {
		Query      string `json:"query"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	base, err := e.policy.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	maxResults := args.MaxResults
	if maxResults <= 0 || maxResults > e.policy.MaxSearchResults {
		maxResults = e.policy.MaxSearchResults
	}
	results := make([]string, 0, maxResults)
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if args.Glob != "" {
			ok, _ := filepath.Match(args.Glob, filepath.Base(path))
			if !ok {
				return nil
			}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(content), "\n")
		for idx, line := range lines {
			if !strings.Contains(line, query) {
				continue
			}
			rel, _ := filepath.Rel(base, path)
			results = append(results, fmt.Sprintf("%s:%d:%s", rel, idx+1, strings.TrimSpace(line)))
			if len(results) >= maxResults {
				return fs.SkipAll
			}
		}
		return nil
	})
	return strings.Join(results, "\n"), nil
}

func (e *RuntimeExecutor) runSessionsList(raw json.RawMessage) (string, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(raw, &args)
	sessions := e.backend.ListSessions()
	limit := args.Limit
	if limit > 0 && limit < len(sessions) {
		sessions = sessions[:limit]
	}
	payload, _ := json.MarshalIndent(sessions, "", "  ")
	return string(payload), nil
}

func (e *RuntimeExecutor) runMemorySearch(raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
	results, err := e.backend.SearchMemory(strings.TrimSpace(args.Query), limit)
	if err != nil {
		return "", err
	}
	payload, _ := json.MarshalIndent(results, "", "  ")
	return string(payload), nil
}

func (e *RuntimeExecutor) runProvidersList() (string, error) {
	providers := e.backend.Providers()
	sort.Strings(providers)
	payload, _ := json.MarshalIndent(providers, "", "  ")
	return string(payload), nil
}

func (e *RuntimeExecutor) runAccountsList(raw json.RawMessage) (string, error) {
	var args struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	accounts := e.backend.Accounts(strings.TrimSpace(args.Provider))
	payload, _ := json.MarshalIndent(accounts, "", "  ")
	return string(payload), nil
}

func (e *RuntimeExecutor) runGitDiff(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Pathspec string `json:"pathspec"`
		Staged   bool   `json:"staged"`
	}
	_ = json.Unmarshal(raw, &args)
	argv := []string{"git", "diff"}
	if args.Staged {
		argv = append(argv, "--staged")
	}
	if strings.TrimSpace(args.Pathspec) != "" {
		argv = append(argv, "--", strings.TrimSpace(args.Pathspec))
	}
	return e.runCommand(ctx, argv, ".", 0)
}

func (e *RuntimeExecutor) runGitLog(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(raw, &args)
	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}
	argv := []string{"git", "log", "--oneline", fmt.Sprintf("-%d", limit)}
	return e.runCommand(ctx, argv, ".", 0)
}

func (e *RuntimeExecutor) runGitShow(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Rev string `json:"rev"`
	}
	_ = json.Unmarshal(raw, &args)
	rev := strings.TrimSpace(args.Rev)
	if rev == "" {
		rev = "HEAD"
	}
	return e.runCommand(ctx, []string{"git", "show", rev}, ".", 0)
}

func (e *RuntimeExecutor) runExecCommand(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Argv       []string `json:"argv"`
		Workdir    string   `json:"workdir"`
		TimeoutSec int      `json:"timeout_sec"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	timeout := time.Duration(args.TimeoutSec) * time.Second
	return e.runCommand(ctx, args.Argv, args.Workdir, timeout)
}

func (e *RuntimeExecutor) runFileWrite(raw json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	full, err := e.policy.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	switch strings.TrimSpace(args.Mode) {
	case "", "overwrite":
		if err := os.WriteFile(full, []byte(args.Content), 0o644); err != nil {
			return "", err
		}
	case "append":
		f, err := os.OpenFile(full, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err := f.WriteString(args.Content); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported mode: %s", args.Mode)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), full), nil
}

func (e *RuntimeExecutor) runFileReplace(raw json.RawMessage) (string, error) {
	var args struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Old == "" {
		return "", fmt.Errorf("old is required")
	}
	full, err := e.policy.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	before := string(content)
	n := 1
	if args.ReplaceAll {
		n = -1
	}
	after := strings.Replace(before, args.Old, args.New, n)
	if after == before {
		return "no changes", nil
	}
	if err := os.WriteFile(full, []byte(after), 0o644); err != nil {
		return "", err
	}
	return "replace complete", nil
}

func (e *RuntimeExecutor) runGitAdd(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Pathspecs []string `json:"pathspecs"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if len(args.Pathspecs) == 0 {
		return "", fmt.Errorf("pathspecs is required")
	}
	argv := []string{"git", "add"}
	argv = append(argv, args.Pathspecs...)
	return e.runCommand(ctx, argv, ".", 0)
}

func (e *RuntimeExecutor) runGitRestore(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Pathspecs []string `json:"pathspecs"`
		Staged    bool     `json:"staged"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if len(args.Pathspecs) == 0 {
		return "", fmt.Errorf("pathspecs is required")
	}
	argv := []string{"git", "restore"}
	if args.Staged {
		argv = append(argv, "--staged")
	}
	argv = append(argv, args.Pathspecs...)
	return e.runCommand(ctx, argv, ".", 0)
}

func (e *RuntimeExecutor) runGitCommit(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Message) == "" {
		return "", fmt.Errorf("message is required")
	}
	return e.runCommand(ctx, []string{"git", "commit", "-m", strings.TrimSpace(args.Message)}, ".", 0)
}

func (e *RuntimeExecutor) runCommand(ctx context.Context, argv []string, workdir string, timeout time.Duration) (string, error) {
	mutating, err := e.policy.ValidateCommand(argv)
	if err != nil {
		return "", err
	}
	if mutating {
		// This path is allowed but should always require approval at runtime.
	}
	cwd, err := e.policy.NormalizeWorkdir(workdir)
	if err != nil {
		return "", err
	}
	cmdTimeout := timeout
	if cmdTimeout <= 0 {
		cmdTimeout = e.policy.CommandTimeout
	}
	if cmdTimeout <= 0 {
		cmdTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	output, runErr := cmd.CombinedOutput()
	if len(output) > e.policy.MaxOutputBytes && e.policy.MaxOutputBytes > 0 {
		output = output[:e.policy.MaxOutputBytes]
	}
	text := string(output)
	if runErr != nil {
		if strings.TrimSpace(text) == "" {
			text = runErr.Error()
		}
		return "", fmt.Errorf("%s", trimPreview(text, e.policy.MaxOutputBytes))
	}
	if strings.TrimSpace(text) == "" {
		text = "(ok)"
	}
	return trimPreview(text, e.policy.MaxOutputBytes), nil
}
