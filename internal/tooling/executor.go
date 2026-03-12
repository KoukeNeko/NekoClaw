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

	"github.com/doeshing/nekoclaw/internal/core"
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
	cfg = normalizeExecutorConfig(cfg)
	specs := builtinRuntimeSpecs(cfg)
	braveKey := cfg.BraveSearchAPIKey

	return &RuntimeExecutor{
		backend:        backend,
		policy:         policy,
		specs:          specs,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		braveSearchKey: braveKey,
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
	case "memory_get":
		return e.runMemoryGet(call.Arguments)
	case "memory_save":
		return e.runMemorySave(call.Arguments)
	case "task_list":
		return e.runTaskList(call.Arguments)
	case "task_update":
		return e.runTaskUpdate(call.Arguments)
	case "tool_catalog_search":
		return e.runToolCatalogSearch(call.Arguments)
	case "tool_catalog_describe":
		return e.runToolCatalogDescribe(call.Arguments)
	case "spawn_subagent":
		return e.runSpawnSubagent(ctx, call.Arguments)
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
	return truncateHeadTail(out, e.policy.MaxOutputBytes), nil
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

func (e *RuntimeExecutor) runMemoryGet(raw json.RawMessage) (string, error) {
	var args struct {
		Path  string `json:"path"`
		From  int    `json:"from"`
		Lines int    `json:"lines"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	path := strings.TrimSpace(args.Path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	content, err := e.backend.ReadMemoryFile(path, args.From, args.Lines)
	if err != nil {
		return "", err
	}
	return content, nil
}

func (e *RuntimeExecutor) runMemorySave(raw json.RawMessage) (string, error) {
	var args struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	if err := e.backend.SaveMemory(content); err != nil {
		return "", err
	}
	return fmt.Sprintf("Saved to memory: %s", content), nil
}

func (e *RuntimeExecutor) runTaskList(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(raw, &args)
	if strings.TrimSpace(args.SessionID) == "" {
		args.SessionID = "main"
	}
	list := e.backend.TaskList(args.SessionID)
	payload, _ := json.MarshalIndent(list.Items, "", "  ")
	return string(payload), nil
}

func (e *RuntimeExecutor) runTaskUpdate(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
		Items     []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.SessionID) == "" {
		args.SessionID = "main"
	}
	list := core.TaskList{
		SessionID: args.SessionID,
		Items:     make([]core.TaskItem, 0, len(args.Items)),
	}
	for i, item := range args.Items {
		list.Items = append(list.Items, core.TaskItem{
			ID:      strings.TrimSpace(item.ID),
			Content: strings.TrimSpace(item.Content),
			Status:  core.TaskStatus(strings.TrimSpace(item.Status)),
			Order:   i,
		})
	}
	saved, err := e.backend.SaveTaskList(list)
	if err != nil {
		return "", err
	}
	payload, _ := json.MarshalIndent(saved.Items, "", "  ")
	return string(payload), nil
}

func (e *RuntimeExecutor) runToolCatalogSearch(raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(raw, &args)
	query := strings.ToLower(strings.TrimSpace(args.Query))
	limit := args.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	type summary struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Mutating    bool   `json:"mutating"`
	}
	out := make([]summary, 0, limit)
	for _, entry := range e.backend.ToolCatalog() {
		if query != "" {
			haystack := strings.ToLower(entry.Name + " " + entry.Description)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		out = append(out, summary{
			Name:        entry.Name,
			Description: entry.Description,
			Mutating:    entry.Mutating,
		})
		if len(out) >= limit {
			break
		}
	}
	payload, _ := json.MarshalIndent(out, "", "  ")
	return string(payload), nil
}

func (e *RuntimeExecutor) runToolCatalogDescribe(raw json.RawMessage) (string, error) {
	var args struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	requested := map[string]struct{}{}
	for _, name := range args.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		requested[name] = struct{}{}
	}
	entries := make([]ToolCatalogEntry, 0, len(requested))
	for _, entry := range e.backend.ToolCatalog() {
		if _, ok := requested[entry.Name]; ok {
			entries = append(entries, entry)
		}
	}
	payload, _ := json.MarshalIndent(entries, "", "  ")
	return string(payload), nil
}

func (e *RuntimeExecutor) runSpawnSubagent(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		SessionID   string `json:"session_id"`
		Surface     string `json:"surface"`
		Type        string `json:"type"`
		Goal        string `json:"goal"`
		ContextHint string `json:"context_hint"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	artifact, err := e.backend.SpawnSubagent(
		ctx,
		strings.TrimSpace(args.SessionID),
		core.Surface(strings.TrimSpace(args.Surface)),
		args.Type,
		args.Goal,
		args.ContextHint,
	)
	if err != nil {
		return "", err
	}
	return artifact.Markdown, nil
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
	text := string(output)
	if runErr != nil {
		if strings.TrimSpace(text) == "" {
			text = runErr.Error()
		}
		return "", fmt.Errorf("%s", truncateHeadTail(text, e.policy.MaxOutputBytes))
	}
	if strings.TrimSpace(text) == "" {
		text = "(ok)"
	}
	return truncateHeadTail(text, e.policy.MaxOutputBytes), nil
}
