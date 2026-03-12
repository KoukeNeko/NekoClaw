package tooling

import (
	"encoding/json"
	"strings"

	"github.com/doeshing/nekoclaw/internal/provider"
)

// ToolConfigField describes an editable settings field for a built-in tool.
type ToolConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
}

// ToolCatalogEntry is the API-facing metadata for a built-in tool.
type ToolCatalogEntry struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	InputSchema  json.RawMessage   `json:"input_schema"`
	Mutating     bool              `json:"mutating"`
	Configurable bool              `json:"configurable"`
	Configured   bool              `json:"configured"`
	ConfigFields []ToolConfigField `json:"config_fields,omitempty"`
}

type builtinToolCatalogSpec struct {
	Definition     provider.ToolDefinition
	Mutating       bool
	ConfigFields   []ToolConfigField
	IsConfigured   func(ExecutorConfig) bool
	RuntimeEnabled func(ExecutorConfig) bool
}

func newToolDefinition(name, description, schema string) provider.ToolDefinition {
	return provider.ToolDefinition{
		Name:        name,
		Description: description,
		InputSchema: json.RawMessage(schema),
	}
}

func normalizeExecutorConfig(cfg ExecutorConfig) ExecutorConfig {
	cfg.BraveSearchAPIKey = strings.TrimSpace(cfg.BraveSearchAPIKey)
	return cfg
}

func builtinToolCatalogSpecs() []builtinToolCatalogSpec {
	braveConfigured := func(cfg ExecutorConfig) bool {
		return strings.TrimSpace(cfg.BraveSearchAPIKey) != ""
	}

	return []builtinToolCatalogSpec{
		{Definition: newToolDefinition("file_list", "List files in the workspace.", `{"type":"object","properties":{"path":{"type":"string"},"recursive":{"type":"boolean"},"max_entries":{"type":"integer","minimum":1}}}`)},
		{Definition: newToolDefinition("file_read", "Read file content.", `{"type":"object","required":["path"],"properties":{"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}}}`)},
		{Definition: newToolDefinition("file_search", "Search text in files.", `{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"},"max_results":{"type":"integer","minimum":1}}}`)},
		{Definition: newToolDefinition("sessions_list", "List chat sessions.", `{"type":"object","properties":{"limit":{"type":"integer","minimum":1}}}`)},
		{Definition: newToolDefinition("memory_search", "Search memory index.", `{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1}}}`)},
		{Definition: newToolDefinition("memory_get", "Read a memory file by relative path. Optionally specify line range.", `{"type":"object","required":["path"],"properties":{"path":{"type":"string","description":"Relative path to memory file (e.g. MEMORY.md, memory/2026-03-06-auth.md)"},"from":{"type":"integer","description":"Starting line number (1-based)","minimum":1},"lines":{"type":"integer","description":"Number of lines to read","minimum":1}}}`)},
		{Definition: newToolDefinition("memory_save", "Save a note to the daily memory log. Use this when the user asks you to remember something.", `{"type":"object","required":["content"],"properties":{"content":{"type":"string","description":"The content to save to memory"}}}`)},
		{Definition: newToolDefinition("task_list", "List active tasks for the current session.", `{"type":"object","properties":{}}`)},
		{Definition: newToolDefinition("task_update", "Replace the current session task list.", `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":{"type":"object","required":["content"],"properties":{"id":{"type":"string"},"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed","blocked"]}}}}}}`)},
		{Definition: newToolDefinition("tool_catalog_search", "Search the available tool catalog and return concise summaries.", `{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}}}`)},
		{Definition: newToolDefinition("tool_catalog_describe", "Describe one or more tools and request that their full schemas be exposed for the current run.", `{"type":"object","required":["names"],"properties":{"names":{"type":"array","items":{"type":"string"}}}}`)},
		{Definition: newToolDefinition("spawn_subagent", "Run a specialized read-only subagent such as planner, code_explorer, or critic.", `{"type":"object","required":["type","goal"],"properties":{"type":{"type":"string","enum":["planner","code_explorer","critic"]},"goal":{"type":"string"},"context_hint":{"type":"string"}}}`)},
		{Definition: newToolDefinition("providers_list", "List providers.", `{"type":"object","properties":{}}`)},
		{Definition: newToolDefinition("accounts_list", "List provider accounts.", `{"type":"object","required":["provider"],"properties":{"provider":{"type":"string"}}}`)},
		{Definition: newToolDefinition("git_status", "Run git status.", `{"type":"object","properties":{}}`)},
		{Definition: newToolDefinition("git_diff", "Run git diff.", `{"type":"object","properties":{"pathspec":{"type":"string"},"staged":{"type":"boolean"}}}`)},
		{Definition: newToolDefinition("git_log", "Run git log.", `{"type":"object","properties":{"limit":{"type":"integer","minimum":1}}}`)},
		{Definition: newToolDefinition("git_show", "Run git show.", `{"type":"object","properties":{"rev":{"type":"string"}}}`)},
		{Definition: newToolDefinition("exec_command", "Execute an allowlisted command.", `{"type":"object","required":["argv"],"properties":{"argv":{"type":"array","items":{"type":"string"}},"workdir":{"type":"string"},"timeout_sec":{"type":"integer","minimum":1}}}`)},
		{Definition: newToolDefinition("file_write", "Write file content.", `{"type":"object","required":["path","content"],"properties":{"path":{"type":"string"},"content":{"type":"string"},"mode":{"type":"string","enum":["overwrite","append"]}}}`), Mutating: true},
		{Definition: newToolDefinition("file_replace", "Replace text in a file.", `{"type":"object","required":["path","old","new"],"properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"replace_all":{"type":"boolean"}}}`), Mutating: true},
		{Definition: newToolDefinition("git_add", "Run git add.", `{"type":"object","required":["pathspecs"],"properties":{"pathspecs":{"type":"array","items":{"type":"string"}}}}`), Mutating: true},
		{Definition: newToolDefinition("git_restore", "Run git restore.", `{"type":"object","required":["pathspecs"],"properties":{"pathspecs":{"type":"array","items":{"type":"string"}},"staged":{"type":"boolean"}}}`), Mutating: true},
		{Definition: newToolDefinition("git_commit", "Run git commit.", `{"type":"object","required":["message"],"properties":{"message":{"type":"string"}}}`), Mutating: true},
		{Definition: newToolDefinition("datetime", "Get current date, time, timezone, and unix timestamp.", `{"type":"object","properties":{}}`)},
		{Definition: newToolDefinition("web_fetch", "Fetch a web page and return its text content. Useful for reading articles, documentation, or URLs shared by the user.", `{"type":"object","required":["url"],"properties":{"url":{"type":"string"},"max_chars":{"type":"integer","minimum":100}}}`)},
		{
			Definition: newToolDefinition("web_search", "Search the web using Brave Search. Returns titles, URLs, and snippets.", `{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"count":{"type":"integer","minimum":1,"maximum":20}}}`),
			ConfigFields: []ToolConfigField{
				{
					Key:         "brave_search_api_key",
					Label:       "Brave Search API Key",
					Placeholder: "BSA...",
					Secret:      true,
				},
			},
			IsConfigured:   braveConfigured,
			RuntimeEnabled: braveConfigured,
		},
	}
}

func builtinRuntimeSpecs(cfg ExecutorConfig) map[string]ToolSpec {
	cfg = normalizeExecutorConfig(cfg)

	specs := make(map[string]ToolSpec)
	for _, spec := range builtinToolCatalogSpecs() {
		if spec.RuntimeEnabled != nil && !spec.RuntimeEnabled(cfg) {
			continue
		}
		specs[spec.Definition.Name] = ToolSpec{
			Definition: spec.Definition,
			Mutating:   spec.Mutating,
		}
	}
	return specs
}

// BuiltinToolCatalog returns API-facing metadata for all built-in tools,
// including conditionally enabled tools such as web_search.
func BuiltinToolCatalog(cfg ExecutorConfig) []ToolCatalogEntry {
	cfg = normalizeExecutorConfig(cfg)

	tools := make([]ToolCatalogEntry, 0, len(builtinToolCatalogSpecs()))
	for _, spec := range builtinToolCatalogSpecs() {
		configurable := len(spec.ConfigFields) > 0
		configured := true
		if spec.IsConfigured != nil {
			configured = spec.IsConfigured(cfg)
		}
		if configurable && spec.IsConfigured == nil {
			configured = false
		}
		tools = append(tools, ToolCatalogEntry{
			Name:         spec.Definition.Name,
			Description:  spec.Definition.Description,
			InputSchema:  spec.Definition.InputSchema,
			Mutating:     spec.Mutating,
			Configurable: configurable,
			Configured:   configured,
			ConfigFields: append([]ToolConfigField(nil), spec.ConfigFields...),
		})
	}
	return tools
}
