import { useEffect, useState } from "react";
import {
  getToolsCatalog,
  getToolsConfig,
  setToolsConfig,
} from "@/api/client";
import { SelectionList, SelectionListItem } from "@/components/settings/SelectionList";
import type {
  ToolCatalogEntry,
  ToolConfigField,
  ToolsConfig,
} from "@/api/types";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction = "" | "reload" | "save";

const EMPTY_TOOLS_CONFIG: ToolsConfig = {
  brave_search_api_key: "",
};

function statusClass(tone: StatusTone): string {
  switch (tone) {
    case "success":
      return "alert-success";
    case "error":
      return "alert-error";
    default:
      return "alert-info";
  }
}

function normalizeToolsConfig(
  config?: Partial<ToolsConfig> | null,
): ToolsConfig {
  return {
    brave_search_api_key: config?.brave_search_api_key?.trim() ?? "",
  };
}

function areToolsConfigsEqual(left: ToolsConfig, right: ToolsConfig): boolean {
  return left.brave_search_api_key === right.brave_search_api_key;
}

function filterTools(tools: ToolCatalogEntry[], query: string): ToolCatalogEntry[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return tools;

  return tools.filter((tool) =>
    [tool.name, tool.description].some((value) =>
      value.toLowerCase().includes(needle),
    ),
  );
}

function sortTools(tools: ToolCatalogEntry[]): ToolCatalogEntry[] {
  return [...tools].sort((left, right) => {
    const leftConfigurable = left.configurable ? 1 : 0;
    const rightConfigurable = right.configurable ? 1 : 0;
    if (leftConfigurable !== rightConfigurable) {
      return rightConfigurable - leftConfigurable;
    }
    return left.name.localeCompare(right.name, "en", {
      sensitivity: "base",
    });
  });
}

function readConfigFieldValue(config: ToolsConfig, key: string): string {
  switch (key) {
    case "brave_search_api_key":
      return config.brave_search_api_key ?? "";
    default:
      return "";
  }
}

function writeConfigFieldValue(
  config: ToolsConfig,
  key: string,
  value: string,
): ToolsConfig {
  switch (key) {
    case "brave_search_api_key":
      return {
        ...config,
        brave_search_api_key: value,
      };
    default:
      return config;
  }
}

function isToolConfigured(tool: ToolCatalogEntry, config: ToolsConfig): boolean {
  if (!tool.configurable) return true;

  const configFields = tool.config_fields ?? [];
  if (configFields.length === 0) return tool.configured;

  return configFields.every(
    (field) => readConfigFieldValue(config, field.key).trim() !== "",
  );
}

function applyConfiguredState(
  tools: ToolCatalogEntry[],
  config: ToolsConfig,
): ToolCatalogEntry[] {
  return sortTools(
    tools.map((tool) => ({
      ...tool,
      configured: isToolConfigured(tool, config),
    })),
  );
}

function formatSchema(schema: unknown): string {
  const formatted = JSON.stringify(schema ?? {}, null, 2);
  return formatted || "{}";
}

function configBadgeClass(tool: ToolCatalogEntry): string {
  if (!tool.configurable) return "badge-ghost";
  return tool.configured ? "badge-success" : "badge-warning";
}

function configBadgeLabel(tool: ToolCatalogEntry): string {
  return tool.configured ? "Configured" : "Needs config";
}

function toolFieldHelp(field: ToolConfigField): string {
  if (field.key === "brave_search_api_key") {
    return "寫入 config.json 後需重啟服務，`web_search` 才會重新載入 Brave Search 憑證。";
  }
  if (field.placeholder) {
    return `寫入 config.json；可參考格式 ${field.placeholder}`;
  }
  return "寫入 config.json 後需重啟服務生效。";
}

export function ToolsPanel() {
  const [tools, setTools] = useState<ToolCatalogEntry[]>([]);
  const [savedConfig, setSavedConfig] = useState<ToolsConfig>(EMPTY_TOOLS_CONFIG);
  const [draftConfig, setDraftConfig] = useState<ToolsConfig>(EMPTY_TOOLS_CONFIG);
  const [selectedToolName, setSelectedToolName] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);
  const [visibleSecrets, setVisibleSecrets] = useState<Record<string, boolean>>(
    {},
  );

  async function syncTools(showLoading = false) {
    if (showLoading) setLoading(true);

    try {
      const [catalog, config] = await Promise.all([
        getToolsCatalog(),
        getToolsConfig(),
      ]);
      const normalizedConfig = normalizeToolsConfig(config);
      const nextTools = applyConfiguredState(catalog, normalizedConfig);

      setTools(nextTools);
      setSavedConfig(normalizedConfig);
      setDraftConfig(normalizedConfig);
      setVisibleSecrets({});
      setSelectedToolName((current) => {
        if (current && nextTools.some((tool) => tool.name === current)) {
          return current;
        }
        if (nextTools.some((tool) => tool.name === "web_search")) {
          return "web_search";
        }
        return nextTools[0]?.name ?? "";
      });
      return true;
    } catch {
      setStatus({ tone: "error", message: "無法載入工具設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncTools(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  useEffect(() => {
    if (tools.length === 0) {
      if (selectedToolName !== "") setSelectedToolName("");
      return;
    }

    const visible = filterTools(tools, query);
    const candidatePool = visible.length > 0 ? visible : tools;
    if (candidatePool.some((tool) => tool.name === selectedToolName)) {
      return;
    }
    if (candidatePool.some((tool) => tool.name === "web_search")) {
      setSelectedToolName("web_search");
      return;
    }
    if (candidatePool[0]?.name) {
      setSelectedToolName(candidatePool[0].name);
    }
  }, [query, selectedToolName, tools]);

  const filteredTools = filterTools(tools, query);
  const selectedTool =
    filteredTools.find((tool) => tool.name === selectedToolName) ??
    filteredTools[0] ??
    null;
  const isDirty = !areToolsConfigsEqual(savedConfig, draftConfig);
  const configurableTools = tools.filter((tool) => tool.configurable);
  const configuredIntegrations = configurableTools.filter(
    (tool) => tool.configured,
  ).length;
  const mutatingTools = tools.filter((tool) => tool.mutating).length;
  const selectedToolConfigurable = !!selectedTool?.configurable;

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncTools(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步工具設定" });
      }
    } finally {
      setBusyAction("");
    }
  }

  function handleReset() {
    setDraftConfig(savedConfig);
    setVisibleSecrets({});
    setStatus({ tone: "info", message: "已還原未儲存變更" });
  }

  async function handleSave() {
    const nextConfig = normalizeToolsConfig(draftConfig);
    setBusyAction("save");

    try {
      await setToolsConfig(nextConfig);
      setSavedConfig(nextConfig);
      setDraftConfig(nextConfig);
      setTools((current) => applyConfiguredState(current, nextConfig));
      setStatus({ tone: "success", message: "工具設定已儲存，重啟後生效" });
    } catch {
      setStatus({ tone: "error", message: "儲存工具設定失敗" });
    } finally {
      setBusyAction("");
    }
  }

  function toggleSecretVisibility(key: string) {
    setVisibleSecrets((current) => ({
      ...current,
      [key]: !current[key],
    }));
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex flex-col gap-3">
              <div className="flex gap-2">
                <div className="skeleton h-5 w-20" />
                <div className="skeleton h-5 w-20" />
              </div>
              <div className="skeleton h-8 w-60" />
              <div className="skeleton h-4 w-96 max-w-full" />
            </div>
            <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
              {Array.from({ length: 4 }).map((_, index) => (
                <div key={index} className="stat">
                  <div className="skeleton h-4 w-24" />
                  <div className="mt-3 skeleton h-8 w-28" />
                  <div className="mt-3 skeleton h-4 w-40" />
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="skeleton h-11 w-full" />
          </div>
        </div>

        <div className="grid gap-6 lg:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]">
          {Array.from({ length: 2 }).map((_, index) => (
            <div
              key={index}
              className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                <div className="skeleton h-6 w-40" />
                <div className="skeleton h-4 w-56" />
                <div className="space-y-3">
                  {Array.from({ length: index === 0 ? 6 : 5 }).map(
                    (_, rowIndex) => (
                      <div
                        key={rowIndex}
                        className="card border border-base-300 bg-base-100 shadow-sm"
                      >
                        <div className="card-body gap-3 p-4">
                          <div className="skeleton h-5 w-28" />
                          <div className="skeleton h-4 w-full" />
                          <div className="skeleton h-4 w-3/4" />
                        </div>
                      </div>
                    ),
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <div className="badge badge-outline badge-sm">Tools</div>
                <div className="badge badge-info badge-sm">Built-in</div>
                {isDirty && <div className="badge badge-warning badge-sm">未儲存</div>}
              </div>
              <div>
                <h2 className="card-title text-2xl">工具設定</h2>
                <p className="text-sm text-base-content/60">
                  使用與 Persona 頁面一致的管理版型，檢視 built-in tools、搜尋條目，並編輯需要憑證的工具設定。
                </p>
              </div>
            </div>

            <div className="join">
              <button
                className="btn btn-sm join-item"
                onClick={handleReload}
                disabled={busyAction !== ""}
              >
                {busyAction === "reload" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                重新同步
              </button>
              <button
                className="btn btn-ghost btn-sm join-item"
                onClick={handleReset}
                disabled={!isDirty || busyAction !== ""}
              >
                還原
              </button>
              <button
                className="btn btn-primary btn-sm join-item"
                onClick={handleSave}
                disabled={!isDirty || busyAction !== ""}
              >
                {busyAction === "save" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                儲存設定
              </button>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">Built-in Tools</div>
              <div className="stat-value text-lg">{tools.length}</div>
              <div className="stat-desc">不包含 MCP 工具</div>
            </div>
            <div className="stat">
              <div className="stat-title">可設定工具</div>
              <div className="stat-value text-lg">{configurableTools.length}</div>
              <div className="stat-desc">目前可寫入 config.json 的 built-in tools</div>
            </div>
            <div className="stat">
              <div className="stat-title">Mutating</div>
              <div className="stat-value text-lg">{mutatingTools}</div>
              <div className="stat-desc">會修改檔案或 git 狀態的工具</div>
            </div>
            <div className="stat">
              <div className="stat-title">Configured</div>
              <div className="stat-value text-lg">{configuredIntegrations}</div>
              <div className="stat-desc">已設定完成的工具整合</div>
            </div>
          </div>
        </div>
      </div>

      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="join w-full">
            <label className="input input-bordered join-item flex w-full items-center gap-2 focus-within:outline-none focus-within:ring-0 focus-within:shadow-none">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                className="size-4 shrink-0 opacity-60"
              >
                <circle cx="11" cy="11" r="7" />
                <path d="m20 20-3.5-3.5" />
              </svg>
              <input
                type="text"
                className="grow outline-none focus:outline-none focus:ring-0 focus-visible:outline-none"
                placeholder="搜尋工具名稱或描述"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
              />
            </label>
            <button
              className="btn join-item"
              onClick={() => setQuery("")}
              disabled={!query}
            >
              清除
            </button>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]">
        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4 p-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">工具清單</h3>
              <div className="badge badge-ghost badge-sm">
                {filteredTools.length} / {tools.length}
              </div>
            </div>

            {filteredTools.length === 0 ? (
              <div className="card border border-dashed border-base-300 bg-base-100 shadow-sm">
                <div className="card-body">
                  <div className="alert alert-info">
                    <span>找不到符合搜尋條件的工具。</span>
                  </div>
                  <p className="text-sm text-base-content/60">
                    清除搜尋條件後，可回到完整工具清單。
                  </p>
                  <div className="card-actions justify-end">
                    <button
                      className="btn btn-sm"
                      onClick={() => setQuery("")}
                    >
                      清除搜尋
                    </button>
                  </div>
                </div>
              </div>
            ) : (
              <SelectionList>
                {filteredTools.map((tool) => {
                  const isSelected = selectedToolName === tool.name;
                  return (
                    <SelectionListItem
                      key={tool.name}
                      selected={isSelected}
                        onClick={() => setSelectedToolName(tool.name)}
                    >
                        <div className="min-w-0 flex-1 text-left">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="truncate font-semibold">{tool.name}</span>
                            {tool.configurable && (
                              <span className="badge badge-info badge-xs">
                                Configurable
                              </span>
                            )}
                            {tool.mutating && (
                              <span className="badge badge-warning badge-xs">
                                Mutating
                              </span>
                            )}
                            <span
                              className={`badge badge-xs ${configBadgeClass(tool)}`}
                            >
                              {configBadgeLabel(tool)}
                            </span>
                          </div>
                          <div className="mt-1 truncate text-xs text-base-content/65">
                            {tool.description}
                          </div>
                        </div>
                    </SelectionListItem>
                  );
                })}
              </SelectionList>
            )}
          </div>
        </div>

        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-5">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">工具詳情</h3>
              {selectedTool ? (
                <div className="flex flex-wrap gap-2">
                  {selectedTool.configurable && (
                    <div className="badge badge-info badge-sm">Configurable</div>
                  )}
                  {selectedTool.mutating && (
                    <div className="badge badge-warning badge-sm">Mutating</div>
                  )}
                  <div className={`badge badge-sm ${configBadgeClass(selectedTool)}`}>
                    {configBadgeLabel(selectedTool)}
                  </div>
                  {isDirty && selectedToolConfigurable && (
                    <div className="badge badge-warning badge-sm">Draft</div>
                  )}
                </div>
              ) : (
                <div className="badge badge-ghost badge-sm">未選取</div>
              )}
            </div>

            {!selectedTool ? (
              <div className="alert alert-info">
                <span>選擇一個工具來查看設定與 schema。</span>
              </div>
            ) : (
              <>
                <div className="space-y-3">
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <h4 className="text-xl font-semibold">{selectedTool.name}</h4>
                      {selectedTool.configurable ? (
                        <div className="badge badge-info">需要設定</div>
                      ) : (
                        <div className="badge badge-ghost">唯讀資訊</div>
                      )}
                    </div>
                    <p className="mt-2 text-sm text-base-content/70">
                      {selectedTool.description}
                    </p>
                  </div>

                  <div className="flex flex-wrap gap-2">
                    <div className="badge badge-outline badge-sm font-mono">
                      {selectedTool.name}
                    </div>
                    <div className="badge badge-ghost badge-sm">
                      {selectedTool.mutating ? "Mutating" : "Non-mutating"}
                    </div>
                    <div className={`badge badge-sm ${configBadgeClass(selectedTool)}`}>
                      {configBadgeLabel(selectedTool)}
                    </div>
                  </div>
                </div>

                {selectedTool.configurable ? (
                  <>
                    <div className="alert alert-info">
                      <span>
                        這個工具的設定只會寫入 <code>config.json</code>。儲存後請重啟服務，讓 runtime
                        重新載入工具設定。
                      </span>
                    </div>

                    <form
                      className="space-y-4"
                      onSubmit={(event) => {
                        event.preventDefault();
                        if (!isDirty || busyAction !== "") return;
                        void handleSave();
                      }}
                    >
                      {(selectedTool.config_fields ?? []).map((field) => {
                        const value = readConfigFieldValue(draftConfig, field.key);
                        const visible = visibleSecrets[field.key] ?? false;
                        return (
                          <label key={field.key} className="fieldset">
                            <legend className="fieldset-legend">{field.label}</legend>
                            <label className="input input-bordered flex w-full items-center gap-2 focus-within:outline-none focus-within:ring-0 focus-within:shadow-none">
                              <input
                                type={field.secret && !visible ? "password" : "text"}
                                className="min-w-0 grow outline-none focus:outline-none focus:ring-0 focus-visible:outline-none"
                                autoComplete="off"
                                spellCheck={false}
                                placeholder={field.placeholder}
                                value={value}
                                onChange={(event) =>
                                  setDraftConfig((current) =>
                                    writeConfigFieldValue(
                                      current,
                                      field.key,
                                      event.target.value,
                                    ),
                                  )
                                }
                              />
                              {field.secret && (
                                <button
                                  type="button"
                                  className="btn btn-ghost btn-xs"
                                  onClick={() => toggleSecretVisibility(field.key)}
                                >
                                  {visible ? "隱藏" : "顯示"}
                                </button>
                              )}
                            </label>
                            <span className="fieldset-label">
                              {toolFieldHelp(field)}
                            </span>
                          </label>
                        );
                      })}

                      <div className="card-actions justify-end">
                        <button
                          type="button"
                          className="btn btn-ghost"
                          onClick={handleReset}
                          disabled={!isDirty || busyAction !== ""}
                        >
                          還原
                        </button>
                        <button
                          type="submit"
                          className="btn btn-primary"
                          disabled={!isDirty || busyAction !== ""}
                        >
                          {busyAction === "save" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          儲存設定
                        </button>
                      </div>
                    </form>
                  </>
                ) : (
                  <div className="alert alert-info">
                    <span>這個 built-in tool 目前沒有可調整的設定欄位。</span>
                  </div>
                )}

                <ul className="list rounded-box border border-base-300 bg-base-100">
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Identifier
                      </div>
                      <div className="font-mono text-sm">{selectedTool.name}</div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Config Status
                      </div>
                      <div className="font-medium">
                        {configBadgeLabel(selectedTool)}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Settings Fields
                      </div>
                      <div className="font-medium">
                        {selectedTool.config_fields?.length ?? 0}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Safety
                      </div>
                      <div className="text-sm text-base-content/75">
                        {selectedTool.mutating
                          ? "可能改動 workspace 或 git 狀態。"
                          : "執行時不應修改 workspace 狀態。"}
                      </div>
                    </div>
                  </li>
                </ul>

                <div className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body gap-3 p-4">
                    <div className="flex items-center justify-between gap-3">
                      <h5 className="card-title text-base">Input Schema</h5>
                      <div className="badge badge-outline badge-sm">JSON</div>
                    </div>
                    <pre className="overflow-x-auto rounded-box bg-base-200 p-4 text-xs leading-6 text-base-content/80">
                      {formatSchema(selectedTool.input_schema)}
                    </pre>
                  </div>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {status && (
        <div className="toast toast-end toast-bottom z-50">
          <div className={`alert shadow-lg ${statusClass(status.tone)}`}>
            <span>{status.message}</span>
          </div>
        </div>
      )}
    </div>
  );
}
