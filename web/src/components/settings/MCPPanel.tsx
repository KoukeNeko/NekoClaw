import { useEffect, useState } from "react";
import {
  ApiError,
  applyMCPConfigs,
  deleteMCPConfig,
  getMCPConfigs,
  listMCPBuiltin,
  listMCPServers,
  listMCPTools,
  saveMCPConfig,
  toggleMCPBuiltin,
} from "@/api/client";
import { SelectionList, SelectionListItem } from "@/components/settings/SelectionList";
import type {
  MCPBuiltinServer,
  MCPConfigDocument,
  MCPServer,
  MCPServerConfig,
  MCPTool,
} from "@/api/types";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type EditorMode = "form" | "json";
type BusyAction = "" | "reload" | "save" | "delete" | "apply" | `builtin:${string}`;
type SelectedItem =
  | { kind: "builtin"; name: string }
  | { kind: "custom"; fileName: string }
  | { kind: "new" }
  | null;
type ApplyPromptState = { action: "save" | "delete"; fileName?: string } | null;

type ConfigFormState = {
  name: string;
  transport: MCPServerConfig["transport"];
  trust: MCPServerConfig["trust"];
  command: string;
  url: string;
  argsText: string;
  envText: string;
  headersText: string;
};

const EMPTY_CONFIG: MCPServerConfig = {
  name: "",
  transport: "stdio",
  trust: "untrusted",
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

function toErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.message.trim()) return error.message;
  if (error instanceof Error && error.message.trim()) return error.message;
  return fallback;
}

function matchesSearch(values: Array<string | undefined>, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  return values.some((value) => value?.toLowerCase().includes(needle));
}

function statusBadge(status: string): string {
  switch (status) {
    case "ready":
      return "badge-success";
    case "connecting":
      return "badge-info";
    case "error":
      return "badge-error";
    default:
      return "badge-ghost";
  }
}

function runtimeStatusText(status: string): string {
  switch (status) {
    case "ready":
      return "Ready";
    case "connecting":
      return "Connecting";
    case "error":
      return "Error";
    default:
      return "Disconnected";
  }
}

function normalizeText(value: string): string {
  return value.replace(/\r\n/g, "\n").trim();
}

function configToForm(config: MCPServerConfig): ConfigFormState {
  return {
    name: config.name ?? "",
    transport: config.transport ?? "stdio",
    trust: config.trust ?? "untrusted",
    command: config.command ?? "",
    url: config.url ?? "",
    argsText: (config.args ?? []).join("\n"),
    envText: mapToLines(config.env),
    headersText: mapToLines(config.headers),
  };
}

function mapToLines(value?: Record<string, string>): string {
  if (!value) return "";
  return Object.entries(value)
    .map(([key, entryValue]) => `${key}=${entryValue}`)
    .join("\n");
}

function linesToList(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function linesToMap(
  text: string,
  label: "env" | "headers",
): { value?: Record<string, string>; error?: string } {
  const lines = text.split(/\r?\n/);
  const next: Record<string, string> = {};

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index]?.trim() ?? "";
    if (!line) continue;
    const separator = line.indexOf("=");
    if (separator <= 0) {
      return {
        error: `${label} 第 ${index + 1} 行格式錯誤，請使用 KEY=VALUE`,
      };
    }
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (!key) {
      return {
        error: `${label} 第 ${index + 1} 行缺少 key`,
      };
    }
    next[key] = value;
  }

  return {
    value: Object.keys(next).length > 0 ? next : undefined,
  };
}

function cleanConfig(config: MCPServerConfig): MCPServerConfig {
  const next: MCPServerConfig = {
    name: config.name.trim(),
    transport: config.transport,
    trust: config.trust,
  };

  if (config.transport === "stdio") {
    if (config.command?.trim()) next.command = config.command.trim();
    if (config.args && config.args.length > 0) next.args = config.args;
    if (config.env && Object.keys(config.env).length > 0) next.env = config.env;
  } else {
    if (config.url?.trim()) next.url = config.url.trim();
    if (config.headers && Object.keys(config.headers).length > 0) {
      next.headers = config.headers;
    }
  }

  return next;
}

function validateConfig(config: MCPServerConfig): string | null {
  const name = config.name.trim();
  if (!name) return "name is required";
  if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]*$/.test(name)) {
    return "name 只能使用英數、單一底線或連字號";
  }
  if (name.includes("__")) {
    return "name 不能包含雙底線";
  }

  if (config.transport === "stdio") {
    if (!config.command?.trim()) return "stdio transport 需要 command";
  } else if (config.transport === "sse" || config.transport === "streamable-http") {
    if (!config.url?.trim()) return `${config.transport} transport 需要 url`;
  } else {
    return "transport 必須是 stdio、sse 或 streamable-http";
  }

  if (config.trust !== "trusted" && config.trust !== "untrusted") {
    return "trust 必須是 trusted 或 untrusted";
  }

  return null;
}

function formToConfig(form: ConfigFormState): {
  config?: MCPServerConfig;
  error?: string;
} {
  const env = linesToMap(form.envText, "env");
  if (env.error) return { error: env.error };
  const headers = linesToMap(form.headersText, "headers");
  if (headers.error) return { error: headers.error };

  const next = cleanConfig({
    name: form.name,
    transport: form.transport,
    trust: form.trust,
    command: form.command,
    url: form.url,
    args: linesToList(form.argsText),
    env: env.value,
    headers: headers.value,
  });
  const validationError = validateConfig(next);
  if (validationError) return { error: validationError };
  return { config: next };
}

function serializeConfig(config: MCPServerConfig): string {
  return JSON.stringify(cleanConfig(config), null, 2);
}

function parseJSONDraft(text: string): { config?: MCPServerConfig; error?: string } {
  try {
    const parsed: unknown = JSON.parse(text);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return { error: "JSON 必須是物件" };
    }
    const value = parsed as Record<string, unknown>;
    if (
      value.transport !== undefined &&
      value.transport !== "stdio" &&
      value.transport !== "sse" &&
      value.transport !== "streamable-http"
    ) {
      return { error: "transport 必須是 stdio、sse 或 streamable-http" };
    }
    if (
      value.trust !== undefined &&
      value.trust !== "trusted" &&
      value.trust !== "untrusted"
    ) {
      return { error: "trust 必須是 trusted 或 untrusted" };
    }
    const config: MCPServerConfig = cleanConfig({
      name: typeof value.name === "string" ? value.name : "",
      transport:
        value.transport === "sse" || value.transport === "streamable-http"
          ? value.transport
          : "stdio",
      trust: value.trust === "trusted" ? "trusted" : "untrusted",
      command: typeof value.command === "string" ? value.command : undefined,
      url: typeof value.url === "string" ? value.url : undefined,
      args: Array.isArray(value.args)
        ? value.args.filter((item): item is string => typeof item === "string")
        : undefined,
      env: objectToStringMap(value.env),
      headers: objectToStringMap(value.headers),
    });
    const validationError = validateConfig(config);
    if (validationError) return { error: validationError };
    return { config };
  } catch (error) {
    return {
      error: error instanceof Error ? error.message : "JSON 解析失敗",
    };
  }
}

function objectToStringMap(value: unknown): Record<string, string> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return undefined;
  }
  const entries = Object.entries(value);
  const next: Record<string, string> = {};
  for (const [key, entryValue] of entries) {
    if (typeof entryValue !== "string") continue;
    next[key] = entryValue;
  }
  return Object.keys(next).length > 0 ? next : undefined;
}

function resolveSelection(
  current: SelectedItem,
  docs: MCPConfigDocument[],
  builtins: MCPBuiltinServer[],
  preferred?: SelectedItem,
): SelectedItem {
  if (preferred?.kind === "custom" && docs.some((doc) => doc.file_name === preferred.fileName)) {
    return preferred;
  }
  if (preferred?.kind === "builtin" && builtins.some((item) => item.name === preferred.name)) {
    return preferred;
  }
  if (preferred?.kind === "new") return preferred;

  if (current?.kind === "custom" && docs.some((doc) => doc.file_name === current.fileName)) {
    return current;
  }
  if (current?.kind === "builtin" && builtins.some((item) => item.name === current.name)) {
    return current;
  }
  if (current?.kind === "new") return current;

  if (docs[0]) return { kind: "custom", fileName: docs[0].file_name };
  if (builtins[0]) return { kind: "builtin", name: builtins[0].name };
  return null;
}

export function MCPPanel() {
  const [configDir, setConfigDir] = useState("");
  const [documents, setDocuments] = useState<MCPConfigDocument[]>([]);
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [tools, setTools] = useState<MCPTool[]>([]);
  const [builtins, setBuiltins] = useState<MCPBuiltinServer[]>([]);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<SelectedItem>(null);
  const [editorMode, setEditorMode] = useState<EditorMode>("form");
  const [form, setForm] = useState<ConfigFormState>(configToForm(EMPTY_CONFIG));
  const [jsonDraft, setJSONDraft] = useState(serializeConfig(EMPTY_CONFIG));
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);
  const [applyPrompt, setApplyPrompt] = useState<ApplyPromptState>(null);

  const selectedDocument =
    selected?.kind === "custom"
      ? documents.find((doc) => doc.file_name === selected.fileName) ?? null
      : null;
  const selectedBuiltin =
    selected?.kind === "builtin"
      ? builtins.find((item) => item.name === selected.name) ?? null
      : null;
  const baselineConfig =
    selected?.kind === "new"
      ? EMPTY_CONFIG
      : selectedDocument?.config ?? null;
  const baselineRawJSON =
    selected?.kind === "new"
      ? serializeConfig(EMPTY_CONFIG)
      : selectedDocument?.raw_json ?? "";

  const customRuntimeServers = servers.filter((server) => !server.builtin);
  const enabledBuiltinCount = builtins.filter((item) => item.enabled).length;
  const readyServerCount = servers.filter((server) => server.status === "ready").length;

  const filteredBuiltins = builtins.filter((item) =>
    matchesSearch([item.name, item.description, item.error], query),
  );
  const filteredDocuments = documents.filter((doc) =>
    matchesSearch(
      [
        doc.file_name,
        doc.config?.name,
        doc.config?.transport,
        doc.config?.trust,
        doc.parse_error,
        doc.validation_error,
      ],
      query,
    ),
  );

  const formResult = formToConfig(form);
  const jsonResult = parseJSONDraft(jsonDraft);
  const canUseForm = !jsonResult.error && !!jsonResult.config;

  const selectedRuntime =
    selectedDocument?.config?.name
      ? customRuntimeServers.find(
          (server) => server.name === selectedDocument.config?.name,
        ) ?? null
      : null;
  const selectedBuiltinTools = selectedBuiltin
    ? tools.filter((tool) => tool.server === selectedBuiltin.name)
    : [];
  const selectedCustomTools =
    selectedDocument?.config?.name
      ? tools.filter((tool) => tool.server === selectedDocument.config?.name)
      : [];

  const isDirty = (() => {
    if (selected?.kind !== "custom" && selected?.kind !== "new") return false;
    if (editorMode === "form") {
      if (!formResult.config) return true;
      const baseline = baselineConfig ? serializeConfig(baselineConfig) : "";
      return serializeConfig(formResult.config) !== baseline;
    }
    if (jsonResult.config && baselineConfig) {
      return serializeConfig(jsonResult.config) !== serializeConfig(baselineConfig);
    }
    return normalizeText(jsonDraft) !== normalizeText(baselineRawJSON);
  })();

  async function syncAll(showLoading = false, preferred?: SelectedItem) {
    if (showLoading) setLoading(true);
    try {
      const [configResp, serverResp, builtinResp, toolResp] = await Promise.all([
        getMCPConfigs(),
        listMCPServers(),
        listMCPBuiltin(),
        listMCPTools(),
      ]);
      setConfigDir(configResp.config_dir ?? "");
      setDocuments(configResp.documents ?? []);
      setServers(serverResp ?? []);
      setBuiltins(builtinResp ?? []);
      setTools(toolResp ?? []);
      setSelected((current) =>
        resolveSelection(current, configResp.documents ?? [], builtinResp ?? [], preferred),
      );
      return true;
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "無法載入 MCP 設定"),
      });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  function resetEditorState(nextSelected: SelectedItem, nextDocuments: MCPConfigDocument[]) {
    if (nextSelected?.kind === "new") {
      setEditorMode("form");
      setForm(configToForm(EMPTY_CONFIG));
      setJSONDraft(serializeConfig(EMPTY_CONFIG));
      return;
    }

    if (nextSelected?.kind !== "custom") return;
    const doc = nextDocuments.find((item) => item.file_name === nextSelected.fileName);
    if (!doc) return;
    const nextConfig = doc.config ?? EMPTY_CONFIG;
    setForm(configToForm(nextConfig));
    setJSONDraft(doc.raw_json || serializeConfig(nextConfig));
    setEditorMode(doc.parse_error || doc.validation_error ? "json" : "form");
  }

  useEffect(() => {
    void syncAll(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2600);
    return () => window.clearTimeout(timer);
  }, [status]);

  useEffect(() => {
    resetEditorState(selected, documents);
  }, [documents, selected]);

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncAll(false);
      if (ok) {
        setStatus({ tone: "info", message: "MCP 設定已重新同步" });
      }
    } finally {
      setBusyAction("");
    }
  }

  async function handleBuiltinToggle(item: MCPBuiltinServer) {
    setBusyAction(`builtin:${item.name}`);
    try {
      await toggleMCPBuiltin(item.name, !item.enabled);
      const ok = await syncAll(false, { kind: "builtin", name: item.name });
      if (ok) {
        setStatus({
          tone: "success",
          message: item.enabled ? `已停用 ${item.name}` : `已啟用 ${item.name}`,
        });
      }
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "切換 builtin MCP 失敗"),
      });
    } finally {
      setBusyAction("");
    }
  }

  function switchToJSON() {
    const next = formToConfig(form);
    if (!next.config) {
      setStatus({
        tone: "error",
        message: next.error ?? "表單內容無法轉成 JSON",
      });
      return;
    }
    setJSONDraft(serializeConfig(next.config));
    setEditorMode("json");
  }

  function switchToForm() {
    if (!jsonResult.config) {
      setStatus({
        tone: "error",
        message: jsonResult.error ?? "JSON 尚未修正",
      });
      return;
    }
    setForm(configToForm(jsonResult.config));
    setEditorMode("form");
  }

  async function handleSave() {
    if (selected?.kind !== "custom" && selected?.kind !== "new") return;

    setBusyAction("save");
    try {
      const payload =
        editorMode === "form"
          ? formResult.config
            ? {
                file_name:
                  selected.kind === "custom" ? selected.fileName : undefined,
                config: formResult.config,
              }
            : null
          : jsonResult.config
            ? {
                file_name:
                  selected.kind === "custom" ? selected.fileName : undefined,
                raw_json: jsonDraft,
              }
            : null;

      if (!payload) {
        setStatus({
          tone: "error",
          message:
            editorMode === "form"
              ? formResult.error ?? "表單驗證失敗"
              : jsonResult.error ?? "JSON 尚未修正",
        });
        return;
      }

      const resp = await saveMCPConfig(payload);
      const preferred = { kind: "custom", fileName: resp.file_name } as const;
      const ok = await syncAll(false, preferred);
      if (ok) {
        setSelected(preferred);
        setApplyPrompt({ action: "save", fileName: resp.file_name });
        setStatus({ tone: "success", message: "MCP 設定已儲存到磁碟" });
      }
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "儲存 MCP 設定失敗"),
      });
    } finally {
      setBusyAction("");
    }
  }

  async function handleDelete() {
    if (selected?.kind !== "custom") return;
    setBusyAction("delete");
    try {
      await deleteMCPConfig(selected.fileName);
      const deletedFile = selected.fileName;
      const ok = await syncAll(false);
      if (ok) {
        setApplyPrompt({ action: "delete", fileName: deletedFile });
        setStatus({ tone: "info", message: "MCP 設定檔已刪除" });
      }
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "刪除 MCP 設定失敗"),
      });
    } finally {
      setBusyAction("");
    }
  }

  async function confirmApply() {
    setBusyAction("apply");
    try {
      await applyMCPConfigs();
      const ok = await syncAll(false, selected);
      if (ok) {
        setStatus({ tone: "success", message: "已將 custom MCP 設定套用到 runtime" });
      }
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "套用 MCP 設定失敗"),
      });
    } finally {
      setBusyAction("");
      setApplyPrompt(null);
    }
  }

  function resetCurrentDraft() {
    resetEditorState(selected, documents);
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex flex-col gap-3">
              <div className="flex gap-2">
                <div className="skeleton h-5 w-16" />
                <div className="skeleton h-5 w-20" />
              </div>
              <div className="skeleton h-8 w-48" />
              <div className="skeleton h-4 w-80 max-w-full" />
            </div>
            <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
              {Array.from({ length: 4 }).map((_, index) => (
                <div key={index} className="stat">
                  <div className="skeleton h-4 w-20" />
                  <div className="mt-3 skeleton h-8 w-24" />
                  <div className="mt-3 skeleton h-4 w-36" />
                </div>
              ))}
            </div>
          </div>
        </div>
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1.05fr)_minmax(360px,0.95fr)]">
          {Array.from({ length: 2 }).map((_, index) => (
            <div
              key={index}
              className="card min-h-[30rem] border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                {Array.from({ length: 6 }).map((_, itemIndex) => (
                  <div key={itemIndex} className="skeleton h-10 w-full" />
                ))}
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
                <div className="badge badge-outline badge-sm">MCP</div>
                <div className={`badge badge-sm ${readyServerCount > 0 ? "badge-primary" : "badge-ghost"}`}>
                  {readyServerCount > 0 ? "Connected" : "Idle"}
                </div>
                {selected?.kind === "custom" || selected?.kind === "new" ? (
                  <div className={`badge badge-sm ${isDirty ? "badge-warning" : "badge-ghost"}`}>
                    {isDirty ? "未儲存" : "Saved"}
                  </div>
                ) : null}
              </div>
              <div>
                <h2 className="card-title text-2xl">MCP 設定</h2>
                <p className="text-sm text-base-content/60">
                  使用和 Persona 頁一致的版型管理 builtin 開關、custom config 檔案與 runtime tool 狀態。
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
                重新整理
              </button>
              <button
                className="btn btn-primary btn-sm join-item"
                onClick={() => setSelected({ kind: "new" })}
                disabled={busyAction !== ""}
              >
                新增 Custom Server
              </button>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">Builtin 啟用數</div>
              <div className="stat-value text-lg">{enabledBuiltinCount}</div>
              <div className="stat-desc">{builtins.length} 個 builtin 定義</div>
            </div>
            <div className="stat">
              <div className="stat-title">Custom 檔案</div>
              <div className="stat-value text-lg">{documents.length}</div>
              <div className="stat-desc">config dir 內的 `.json` 文件</div>
            </div>
            <div className="stat">
              <div className="stat-title">Ready Servers</div>
              <div className="stat-value text-lg">{readyServerCount}</div>
              <div className="stat-desc">{servers.length} 個 runtime 連線</div>
            </div>
            <div className="stat">
              <div className="stat-title">Tools</div>
              <div className="stat-value text-lg">{tools.length}</div>
              <div className="stat-desc">來自 ready servers 的工具數</div>
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
                placeholder="搜尋 builtin、custom file、server name 或錯誤訊息"
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
          <div className="alert alert-info">
            <span className="font-mono text-xs break-all">
              config dir: {configDir || "未設定"}
            </span>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.05fr)_minmax(360px,0.95fr)]">
        <div className="card min-h-[30rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-5 p-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">MCP 清單</h3>
              <div className="badge badge-ghost badge-sm">
                {filteredBuiltins.length + filteredDocuments.length}
              </div>
            </div>

            {selected?.kind === "new" ? (
              <div className="card border border-dashed border-primary/40 bg-base-100 shadow-sm">
                <div className="card-body gap-2 p-4">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-semibold">新增中的 Custom Server</span>
                    <div className="badge badge-primary badge-sm">New</div>
                  </div>
                  <p className="text-sm text-base-content/60">
                    尚未寫入磁碟，儲存後會取得正式 file name。
                  </p>
                </div>
              </div>
            ) : null}

            <div className="space-y-4 overflow-y-auto [scrollbar-width:thin]">
              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <h4 className="text-sm font-semibold uppercase tracking-wide text-base-content/55">
                    Builtin
                  </h4>
                  <div className="badge badge-outline badge-sm">{filteredBuiltins.length}</div>
                </div>
                {filteredBuiltins.length === 0 ? (
                  <div className="alert alert-info">
                    <span>沒有符合搜尋條件的 builtin server。</span>
                  </div>
                ) : (
                  <SelectionList>
                    {filteredBuiltins.map((item) => {
                      const isSelected =
                        selected?.kind === "builtin" && selected.name === item.name;
                      return (
                        <SelectionListItem
                          key={item.name}
                          selected={isSelected}
                            onClick={() => setSelected({ kind: "builtin", name: item.name })}
                        >
                            <div className="min-w-0 flex-1 text-left">
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="truncate font-semibold">{item.name}</span>
                                <span className={`badge badge-xs ${statusBadge(item.status)}`}>
                                  {runtimeStatusText(item.status)}
                                </span>
                                <span className={`badge badge-xs ${item.enabled ? "badge-primary" : "badge-ghost"}`}>
                                  {item.enabled ? "Enabled" : "Disabled"}
                                </span>
                              </div>
                              <div className="truncate text-xs text-base-content/60">
                                {item.description || "沒有描述"}
                              </div>
                              <div className="truncate text-xs font-mono text-base-content/45">
                                tools: {item.tool_count}
                              </div>
                            </div>
                        </SelectionListItem>
                      );
                    })}
                  </SelectionList>
                )}
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <h4 className="text-sm font-semibold uppercase tracking-wide text-base-content/55">
                    Custom
                  </h4>
                  <div className="badge badge-outline badge-sm">{filteredDocuments.length}</div>
                </div>
                {filteredDocuments.length === 0 ? (
                  <div className="alert alert-info">
                    <span>
                      {documents.length === 0
                        ? "目前還沒有 custom MCP 設定檔。"
                        : "沒有符合搜尋條件的 custom 設定。"}
                    </span>
                  </div>
                ) : (
                  <SelectionList>
                    {filteredDocuments.map((doc) => {
                      const runtime = doc.config?.name
                        ? customRuntimeServers.find((server) => server.name === doc.config?.name)
                        : null;
                      const invalidReason = doc.parse_error || doc.validation_error;
                      const isSelected =
                        selected?.kind === "custom" && selected.fileName === doc.file_name;
                      return (
                        <SelectionListItem
                          key={doc.file_name}
                          selected={isSelected}
                            onClick={() =>
                              setSelected({ kind: "custom", fileName: doc.file_name })
                            }
                        >
                            <div className="min-w-0 flex-1 text-left">
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="truncate font-semibold">
                                  {doc.config?.name || doc.file_name}
                                </span>
                                {invalidReason ? (
                                  <span className="badge badge-error badge-xs">Invalid</span>
                                ) : runtime ? (
                                  <span className={`badge badge-xs ${statusBadge(runtime.status)}`}>
                                    {runtimeStatusText(runtime.status)}
                                  </span>
                                ) : (
                                  <span className="badge badge-ghost badge-xs">Not Applied</span>
                                )}
                              </div>
                              <div className="truncate text-xs font-mono text-base-content/45">
                                {doc.file_name}
                              </div>
                              <div className="truncate text-xs text-base-content/60">
                                {invalidReason ||
                                  `${doc.config?.transport ?? "unknown"} · ${doc.config?.trust ?? "unknown"}`}
                              </div>
                            </div>
                        </SelectionListItem>
                      );
                    })}
                  </SelectionList>
                )}
              </div>
            </div>
          </div>
        </div>

        <div className="card min-h-[30rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-5">
            {selectedBuiltin ? (
              <>
                <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                  <div className="space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <div className="badge badge-outline badge-sm">Builtin</div>
                      <div className={`badge badge-sm ${selectedBuiltin.enabled ? "badge-primary" : "badge-ghost"}`}>
                        {selectedBuiltin.enabled ? "Enabled" : "Disabled"}
                      </div>
                      <div className={`badge badge-sm ${statusBadge(selectedBuiltin.status)}`}>
                        {runtimeStatusText(selectedBuiltin.status)}
                      </div>
                    </div>
                    <div>
                      <h3 className="card-title text-xl">{selectedBuiltin.name}</h3>
                      <p className="text-sm text-base-content/65">
                        {selectedBuiltin.description || "沒有描述"}
                      </p>
                    </div>
                  </div>
                  <button
                    className={`btn btn-sm ${selectedBuiltin.enabled ? "btn-ghost" : "btn-primary"}`}
                    onClick={() => void handleBuiltinToggle(selectedBuiltin)}
                    disabled={busyAction !== ""}
                  >
                    {busyAction === `builtin:${selectedBuiltin.name}` && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    {selectedBuiltin.enabled ? "停用 Builtin" : "啟用 Builtin"}
                  </button>
                </div>

                <ul className="list rounded-box border border-base-300 bg-base-100">
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Runtime Status
                      </div>
                      <div className="font-medium">{runtimeStatusText(selectedBuiltin.status)}</div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Tool Count
                      </div>
                      <div className="font-medium">{selectedBuiltin.tool_count}</div>
                    </div>
                  </li>
                  {selectedBuiltin.error ? (
                    <li className="list-row">
                      <div>
                        <div className="text-xs uppercase tracking-wide text-base-content/50">
                          Last Error
                        </div>
                        <div className="text-sm text-error whitespace-pre-wrap break-words">
                          {selectedBuiltin.error}
                        </div>
                      </div>
                    </li>
                  ) : null}
                </ul>

                <ToolListCard title="Tools" tools={selectedBuiltinTools} />
              </>
            ) : selected?.kind === "custom" || selected?.kind === "new" ? (
              <>
                <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                  <div className="space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <div className="badge badge-outline badge-sm">Custom</div>
                      <div className={`badge badge-sm ${isDirty ? "badge-warning" : "badge-ghost"}`}>
                        {isDirty ? "Editing" : "Saved"}
                      </div>
                      {selectedDocument?.parse_error || selectedDocument?.validation_error ? (
                        <div className="badge badge-error badge-sm">Invalid</div>
                      ) : null}
                    </div>
                    <div>
                      <h3 className="card-title text-xl">
                        {selected?.kind === "new"
                          ? "新增 Custom MCP Server"
                          : selectedDocument?.config?.name || selectedDocument?.file_name}
                      </h3>
                      <p className="text-sm text-base-content/65">
                        {selected?.kind === "new"
                          ? "先填寫表單或 JSON，儲存後再選擇是否立即套用到 runtime。"
                          : "custom config 寫入磁碟後，可選擇是否立刻對 runtime 進行整批 apply。"}
                      </p>
                    </div>
                  </div>
                  <div className="join">
                    <button
                      className={`btn btn-sm join-item ${editorMode === "form" ? "btn-primary" : ""}`}
                      onClick={switchToForm}
                      disabled={editorMode === "form" || !canUseForm || busyAction !== ""}
                    >
                      Form
                    </button>
                    <button
                      className={`btn btn-sm join-item ${editorMode === "json" ? "btn-primary" : ""}`}
                      onClick={switchToJSON}
                      disabled={editorMode === "json" || busyAction !== ""}
                    >
                      JSON
                    </button>
                  </div>
                </div>

                <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
                  <div className="stat">
                    <div className="stat-title">File</div>
                    <div className="stat-value text-lg">
                      {selected?.kind === "new" ? "未儲存" : selectedDocument?.file_name || "—"}
                    </div>
                    <div className="stat-desc font-mono">
                      {selectedDocument?.file_name || "儲存後會產生 file name"}
                    </div>
                  </div>
                  <div className="stat">
                    <div className="stat-title">Runtime</div>
                    <div className="stat-value text-lg">
                      {selectedRuntime ? runtimeStatusText(selectedRuntime.status) : "Not Applied"}
                    </div>
                    <div className="stat-desc">
                      {selectedRuntime
                        ? `${selectedRuntime.transport} · ${selectedRuntime.trust}`
                        : "尚未連到 runtime"}
                    </div>
                  </div>
                  <div className="stat">
                    <div className="stat-title">Tools</div>
                    <div className="stat-value text-lg">
                      {selectedRuntime?.tool_count ?? selectedCustomTools.length}
                    </div>
                    <div className="stat-desc">
                      {selectedRuntime ? "runtime 已發現的工具數" : "套用後才會出現工具"}
                    </div>
                  </div>
                </div>

                {selectedDocument?.parse_error ? (
                  <div className="alert alert-error">
                    <span>目前檔案無法解析 JSON：{selectedDocument.parse_error}</span>
                  </div>
                ) : null}
                {selectedDocument?.validation_error ? (
                  <div className="alert alert-warning">
                    <span>目前檔案 validation 失敗：{selectedDocument.validation_error}</span>
                  </div>
                ) : null}
                {selectedRuntime?.error ? (
                  <div className="alert alert-warning">
                    <span>runtime 最後一次錯誤：{selectedRuntime.error}</span>
                  </div>
                ) : null}

                {editorMode === "form" ? (
                  <form
                    className="space-y-4"
                    onSubmit={(event) => {
                      event.preventDefault();
                      if (!isDirty || busyAction !== "") return;
                      void handleSave();
                    }}
                  >
                    <div className="grid gap-4 md:grid-cols-2">
                      <label className="fieldset">
                        <legend className="fieldset-legend">Name</legend>
                        <input
                          className="input input-bordered w-full"
                          value={form.name}
                          onChange={(event) =>
                            setForm((current) => ({ ...current, name: event.target.value }))
                          }
                          placeholder="filesystem"
                        />
                      </label>
                      <label className="fieldset">
                        <legend className="fieldset-legend">Trust</legend>
                        <select
                          className="select select-bordered w-full"
                          value={form.trust}
                          onChange={(event) =>
                            setForm((current) => ({
                              ...current,
                              trust: event.target.value as MCPServerConfig["trust"],
                            }))
                          }
                        >
                          <option value="untrusted">untrusted</option>
                          <option value="trusted">trusted</option>
                        </select>
                      </label>
                      <label className="fieldset">
                        <legend className="fieldset-legend">Transport</legend>
                        <select
                          className="select select-bordered w-full"
                          value={form.transport}
                          onChange={(event) =>
                            setForm((current) => ({
                              ...current,
                              transport: event.target.value as MCPServerConfig["transport"],
                            }))
                          }
                        >
                          <option value="stdio">stdio</option>
                          <option value="sse">sse</option>
                          <option value="streamable-http">streamable-http</option>
                        </select>
                      </label>
                      {form.transport === "stdio" ? (
                        <label className="fieldset">
                          <legend className="fieldset-legend">Command</legend>
                          <input
                            className="input input-bordered w-full"
                            value={form.command}
                            onChange={(event) =>
                              setForm((current) => ({
                                ...current,
                                command: event.target.value,
                              }))
                            }
                            placeholder="npx"
                          />
                        </label>
                      ) : (
                        <label className="fieldset">
                          <legend className="fieldset-legend">URL</legend>
                          <input
                            className="input input-bordered w-full"
                            value={form.url}
                            onChange={(event) =>
                              setForm((current) => ({ ...current, url: event.target.value }))
                            }
                            placeholder="http://localhost:3000/mcp"
                          />
                        </label>
                      )}
                    </div>

                    {form.transport === "stdio" ? (
                      <>
                        <label className="fieldset">
                          <legend className="fieldset-legend">Args</legend>
                          <textarea
                            className="textarea textarea-bordered min-h-24 w-full font-mono text-sm"
                            value={form.argsText}
                            onChange={(event) =>
                              setForm((current) => ({
                                ...current,
                                argsText: event.target.value,
                              }))
                            }
                            placeholder={"每行一個參數\n-y\n@playwright/mcp@latest"}
                          />
                        </label>
                        <label className="fieldset">
                          <legend className="fieldset-legend">Env</legend>
                          <textarea
                            className="textarea textarea-bordered min-h-24 w-full font-mono text-sm"
                            value={form.envText}
                            onChange={(event) =>
                              setForm((current) => ({
                                ...current,
                                envText: event.target.value,
                              }))
                            }
                            placeholder={"每行一個 KEY=VALUE\nPLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1"}
                          />
                        </label>
                      </>
                    ) : (
                      <label className="fieldset">
                        <legend className="fieldset-legend">Headers</legend>
                        <textarea
                          className="textarea textarea-bordered min-h-24 w-full font-mono text-sm"
                          value={form.headersText}
                          onChange={(event) =>
                            setForm((current) => ({
                              ...current,
                              headersText: event.target.value,
                            }))
                          }
                          placeholder={"每行一個 KEY=VALUE\nAuthorization=Bearer <token>"}
                        />
                      </label>
                    )}

                    {formResult.error ? (
                      <div className="alert alert-warning">
                        <span>{formResult.error}</span>
                      </div>
                    ) : null}

                    <div className="card-actions justify-end">
                      {selected?.kind === "new" ? (
                        <button
                          type="button"
                          className="btn btn-ghost"
                          onClick={() => setSelected(resolveSelection(null, documents, builtins))}
                          disabled={busyAction !== ""}
                        >
                          取消新增
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className="btn"
                        onClick={resetCurrentDraft}
                        disabled={!isDirty || busyAction !== ""}
                      >
                        還原
                      </button>
                      {selected?.kind === "custom" ? (
                        <button
                          type="button"
                          className="btn btn-error btn-soft"
                          onClick={() => void handleDelete()}
                          disabled={busyAction !== ""}
                        >
                          {busyAction === "delete" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          刪除
                        </button>
                      ) : null}
                      <button
                        type="submit"
                        className="btn btn-primary"
                        disabled={!isDirty || !!formResult.error || busyAction !== ""}
                      >
                        {busyAction === "save" && (
                          <span className="loading loading-spinner loading-xs" />
                        )}
                        儲存設定
                      </button>
                    </div>
                  </form>
                ) : (
                  <div className="space-y-4">
                    <label className="fieldset">
                      <legend className="fieldset-legend">Raw JSON</legend>
                      <textarea
                        className="textarea textarea-bordered min-h-[20rem] w-full font-mono text-sm"
                        value={jsonDraft}
                        onChange={(event) => setJSONDraft(event.target.value)}
                        spellCheck={false}
                      />
                    </label>
                    {jsonResult.error ? (
                      <div className="alert alert-warning">
                        <span>{jsonResult.error}</span>
                      </div>
                    ) : null}
                    {!jsonResult.error ? (
                      <div className="alert alert-info">
                        <span>JSON 可切回 Form，並可直接儲存。</span>
                      </div>
                    ) : null}

                    <div className="card-actions justify-end">
                      {selected?.kind === "new" ? (
                        <button
                          type="button"
                          className="btn btn-ghost"
                          onClick={() => setSelected(resolveSelection(null, documents, builtins))}
                          disabled={busyAction !== ""}
                        >
                          取消新增
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className="btn"
                        onClick={resetCurrentDraft}
                        disabled={!isDirty || busyAction !== ""}
                      >
                        還原
                      </button>
                      {selected?.kind === "custom" ? (
                        <button
                          type="button"
                          className="btn btn-error btn-soft"
                          onClick={() => void handleDelete()}
                          disabled={busyAction !== ""}
                        >
                          {busyAction === "delete" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          刪除
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className="btn btn-primary"
                        onClick={() => void handleSave()}
                        disabled={!isDirty || !!jsonResult.error || busyAction !== ""}
                      >
                        {busyAction === "save" && (
                          <span className="loading loading-spinner loading-xs" />
                        )}
                        儲存設定
                      </button>
                    </div>
                  </div>
                )}

                <ToolListCard title="Runtime Tools" tools={selectedCustomTools} />
              </>
            ) : (
              <div className="alert alert-info">
                <span>從左側選一個 builtin 或 custom server 來查看詳細設定。</span>
              </div>
            )}
          </div>
        </div>
      </div>

      {status ? (
        <div className="toast toast-end toast-bottom z-50">
          <div className={`alert shadow-lg ${statusClass(status.tone)}`}>
            <span>{status.message}</span>
          </div>
        </div>
      ) : null}

      {applyPrompt ? (
        <dialog className="modal modal-open">
          <div className="modal-box max-w-lg">
            <h3 className="text-lg font-bold">
              {applyPrompt.action === "save" ? "設定已寫入磁碟" : "設定檔已刪除"}
            </h3>
            <p className="mt-3 text-sm text-base-content/70">
              {applyPrompt.action === "save"
                ? "要不要立刻對 custom MCP configs 執行 apply，讓 runtime 連線與工具列表同步更新？"
                : "要不要立刻 apply custom MCP configs，讓 runtime 移除被刪除的 server？"}
            </p>
            {applyPrompt.fileName ? (
              <div className="alert alert-info mt-4">
                <span className="font-mono text-xs">{applyPrompt.fileName}</span>
              </div>
            ) : null}
            <div className="modal-action">
              <button
                className="btn btn-ghost"
                onClick={() => setApplyPrompt(null)}
                disabled={busyAction === "apply"}
              >
                稍後
              </button>
              <button
                className="btn btn-primary"
                onClick={() => void confirmApply()}
                disabled={busyAction === "apply"}
              >
                {busyAction === "apply" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                立即套用
              </button>
            </div>
          </div>
          <form method="dialog" className="modal-backdrop">
            <button onClick={() => setApplyPrompt(null)}>close</button>
          </form>
        </dialog>
      ) : null}
    </div>
  );
}

function ToolListCard({ title, tools }: { title: string; tools: MCPTool[] }) {
  return (
    <div className="card border border-base-300 bg-base-100 shadow-sm">
      <div className="card-body gap-3 p-4">
        <div className="flex items-center justify-between gap-3">
          <h4 className="card-title text-base">{title}</h4>
          <div className="badge badge-outline badge-sm">{tools.length}</div>
        </div>
        {tools.length === 0 ? (
          <div className="alert alert-info">
            <span>目前沒有可顯示的 tool。</span>
          </div>
        ) : (
          <ul className="list rounded-box border border-base-300 bg-base-200">
            {tools.map((tool) => (
              <li key={`${tool.server}:${tool.name}`} className="list-row">
                <div>
                  <div className="font-mono text-sm">{tool.name}</div>
                  <div className="text-sm text-base-content/60">
                    {tool.description || "沒有描述"}
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
