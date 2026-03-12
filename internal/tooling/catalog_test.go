package tooling

import (
	"context"
	"sort"
	"testing"

	"github.com/doeshing/nekoclaw/internal/core"
)

type stubToolBackend struct{}

func (stubToolBackend) ListSessions() []core.SessionMetadata { return nil }

func (stubToolBackend) SearchMemory(string, int) ([]MemoryResult, error) { return nil, nil }

func (stubToolBackend) ReadMemoryFile(string, int, int) (string, error) { return "", nil }

func (stubToolBackend) SaveMemory(string) error { return nil }

func (stubToolBackend) TaskList(string) core.TaskList { return core.TaskList{} }

func (stubToolBackend) SaveTaskList(list core.TaskList) (core.TaskList, error) { return list, nil }

func (stubToolBackend) ToolCatalog() []ToolCatalogEntry { return nil }

func (stubToolBackend) SpawnSubagent(context.Context, string, core.Surface, string, string, string) (core.SubagentArtifact, error) {
	return core.SubagentArtifact{}, nil
}

func (stubToolBackend) Providers() []string { return nil }

func (stubToolBackend) Accounts(string) []core.AccountSnapshot { return nil }

func TestBuiltinToolCatalogIncludesConditionalTools(t *testing.T) {
	tools := BuiltinToolCatalog(ExecutorConfig{})

	webSearch := findCatalogTool(tools, "web_search")
	if webSearch == nil {
		t.Fatalf("expected web_search in catalog")
	}
	if !webSearch.Configurable {
		t.Fatalf("expected web_search configurable")
	}
	if webSearch.Configured {
		t.Fatalf("expected web_search unconfigured without Brave key")
	}
	if len(webSearch.ConfigFields) != 1 {
		t.Fatalf("expected web_search config field, got %d", len(webSearch.ConfigFields))
	}
	if webSearch.ConfigFields[0].Key != "brave_search_api_key" {
		t.Fatalf("unexpected config field key: %q", webSearch.ConfigFields[0].Key)
	}

	memorySave := findCatalogTool(tools, "memory_save")
	if memorySave == nil {
		t.Fatalf("expected memory_save in catalog")
	}
	if !memorySave.Mutating {
		t.Fatalf("expected memory_save mutating")
	}
	if memorySave.ReadOnlySafe {
		t.Fatalf("expected memory_save not read-only safe")
	}
	if memorySave.RequiresApproval {
		t.Fatalf("expected memory_save to skip approval")
	}

	taskUpdate := findCatalogTool(tools, "task_update")
	if taskUpdate == nil {
		t.Fatalf("expected task_update in catalog")
	}
	if !taskUpdate.Mutating {
		t.Fatalf("expected task_update mutating")
	}
	if taskUpdate.ReadOnlySafe {
		t.Fatalf("expected task_update not read-only safe")
	}
	if taskUpdate.RequiresApproval {
		t.Fatalf("expected task_update to skip approval")
	}
}

func TestRuntimeDefinitionsMatchCatalogRegistration(t *testing.T) {
	tests := []struct {
		name string
		cfg  ExecutorConfig
	}{
		{name: "without brave key", cfg: ExecutorConfig{}},
		{name: "with brave key", cfg: ExecutorConfig{BraveSearchAPIKey: "BSA-test-key"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			executor := NewRuntimeExecutor(stubToolBackend{}, DefaultPolicy(t.TempDir()), tc.cfg)

			var got []string
			for _, def := range executor.Definitions() {
				got = append(got, def.Name)
			}
			sort.Strings(got)

			want := enabledCatalogToolNames(tc.cfg)
			if len(got) != len(want) {
				t.Fatalf("definition count mismatch: got=%d want=%d names=%v", len(got), len(want), got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("definition mismatch at %d: got=%q want=%q", i, got[i], want[i])
				}
			}
		})
	}
}

func enabledCatalogToolNames(cfg ExecutorConfig) []string {
	cfg = normalizeExecutorConfig(cfg)

	names := make([]string, 0, len(builtinToolCatalogSpecs()))
	for _, spec := range builtinToolCatalogSpecs() {
		if spec.RuntimeEnabled != nil && !spec.RuntimeEnabled(cfg) {
			continue
		}
		names = append(names, spec.Definition.Name)
	}
	sort.Strings(names)
	return names
}

func findCatalogTool(tools []ToolCatalogEntry, name string) *ToolCatalogEntry {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}
