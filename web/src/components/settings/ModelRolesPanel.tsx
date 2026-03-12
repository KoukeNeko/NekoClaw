import { useEffect, useMemo, useState } from "react";
import {
  getModelRolesConfig,
  listModels,
  listProviders,
  setModelRolesConfig,
} from "@/api/client";
import type {
  ModelRoleConfig,
  ModelRolesConfig,
  ThinkingMode,
} from "@/api/types";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction = "" | "reload" | "save" | "provider";
type EditableRoleKey = "planner" | "compaction" | "title" | "explorer" | "critic" | "automation";
type DraftThinkingMode = ThinkingMode | "";

interface DraftRoleConfig {
  provider: string;
  model: string;
  thinking_mode: DraftThinkingMode;
}

interface DraftModelRolesConfig {
  action: DraftRoleConfig;
  planner: DraftRoleConfig;
  compaction: DraftRoleConfig;
  title: DraftRoleConfig;
  explorer: DraftRoleConfig;
  critic: DraftRoleConfig;
  automation: DraftRoleConfig;
}

const ROLE_ORDER: EditableRoleKey[] = ["planner", "compaction", "title", "explorer", "critic", "automation"];
const THINKING_OPTIONS: ThinkingMode[] = [
  "auto",
  "off",
  "minimal",
  "low",
  "medium",
  "high",
];
const ROLE_LABELS: Record<EditableRoleKey, string> = {
  planner: "Planner",
  compaction: "Compaction",
  title: "Title",
  explorer: "Explorer",
  critic: "Critic",
  automation: "Automation",
};
const ROLE_DESCRIPTIONS: Record<EditableRoleKey, string> = {
  planner: "建立 plan 時使用的模型角色。未設定時會 fallback 到 Action。",
  compaction: "背景 transcript 摘要與 startup catch-up 使用的模型角色。",
  title: "首次回合的 session title generation 使用的模型角色。",
  explorer: "Code Explorer subagent 使用的模型角色。",
  critic: "Critic subagent 與計畫完成前 review 使用的模型角色。",
  automation: "Automation workflow 的 agent_run 預設模型角色。",
};
const EMPTY_DRAFT_ROLE: DraftRoleConfig = {
  provider: "",
  model: "",
  thinking_mode: "",
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

function normalizeThinkingMode(mode?: string | null): DraftThinkingMode {
  const normalized = (mode || "").trim().toLowerCase();
  switch (normalized) {
    case "":
      return "";
    case "auto":
    case "off":
    case "minimal":
    case "low":
    case "medium":
    case "high":
      return normalized as ThinkingMode;
    default:
      return "auto";
  }
}

function normalizeDraftRoleConfig(config?: ModelRoleConfig | null): DraftRoleConfig {
  const provider = (config?.provider || "").trim();
  return {
    provider,
    model: provider ? ((config?.model || "").trim() || "default") : "",
    thinking_mode: normalizeThinkingMode(config?.thinking_mode),
  };
}

function normalizeDraftModelRolesConfig(
  config?: Partial<ModelRolesConfig> | null,
): DraftModelRolesConfig {
  return {
    action: normalizeDraftRoleConfig(config?.action),
    planner: normalizeDraftRoleConfig(config?.planner),
    compaction: normalizeDraftRoleConfig(config?.compaction),
    title: normalizeDraftRoleConfig(config?.title),
    explorer: normalizeDraftRoleConfig(config?.explorer),
    critic: normalizeDraftRoleConfig(config?.critic),
    automation: normalizeDraftRoleConfig(config?.automation),
  };
}

function serializeDraftRoleConfig(config: DraftRoleConfig): ModelRoleConfig {
  const provider = config.provider.trim();
  const thinkingMode = normalizeThinkingMode(config.thinking_mode);
  const next: ModelRoleConfig = {};
  if (provider) {
    next.provider = provider;
    next.model = (config.model || "").trim() || "default";
  }
  if (thinkingMode) {
    next.thinking_mode = thinkingMode as ThinkingMode;
  }
  return next;
}

function serializeDraftModelRolesConfig(
  config: DraftModelRolesConfig,
): ModelRolesConfig {
  return {
    action: serializeDraftRoleConfig(config.action),
    planner: serializeDraftRoleConfig(config.planner),
    compaction: serializeDraftRoleConfig(config.compaction),
    title: serializeDraftRoleConfig(config.title),
    explorer: serializeDraftRoleConfig(config.explorer),
    critic: serializeDraftRoleConfig(config.critic),
    automation: serializeDraftRoleConfig(config.automation),
  };
}

function configsEqual(left: DraftModelRolesConfig, right: DraftModelRolesConfig): boolean {
  return JSON.stringify(serializeDraftModelRolesConfig(left)) === JSON.stringify(serializeDraftModelRolesConfig(right));
}

async function loadRoleModelOptions(
  config: DraftModelRolesConfig,
): Promise<Record<EditableRoleKey, string[]>> {
  const providerNames = new Set<string>();
  ROLE_ORDER.forEach((role) => {
    if (config[role].provider) {
      providerNames.add(config[role].provider);
    }
  });

  const providerModels = new Map<string, string[]>();
  await Promise.all(
    Array.from(providerNames).map(async (providerName) => {
      try {
        const response = await listModels(providerName);
        providerModels.set(providerName, response.models || []);
      } catch {
        providerModels.set(providerName, []);
      }
    }),
  );

  return {
    planner: providerModels.get(config.planner.provider) ?? [],
    compaction: providerModels.get(config.compaction.provider) ?? [],
    title: providerModels.get(config.title.provider) ?? [],
    explorer: providerModels.get(config.explorer.provider) ?? [],
    critic: providerModels.get(config.critic.provider) ?? [],
    automation: providerModels.get(config.automation.provider) ?? [],
  };
}

function roleSummary(config: DraftRoleConfig): string {
  if (!config.provider) {
    return config.thinking_mode
      ? `Provider / model 跟隨 Action，thinking 使用 ${config.thinking_mode}`
      : "完全跟隨 Action role";
  }
  const thinking = config.thinking_mode || "follow action";
  return `${config.provider}/${config.model || "default"} · thinking ${thinking}`;
}

export function ModelRolesPanel() {
  const [providers, setProviders] = useState<string[]>([]);
  const [savedConfig, setSavedConfig] = useState<DraftModelRolesConfig>({
    action: EMPTY_DRAFT_ROLE,
    planner: EMPTY_DRAFT_ROLE,
    compaction: EMPTY_DRAFT_ROLE,
    title: EMPTY_DRAFT_ROLE,
    explorer: EMPTY_DRAFT_ROLE,
    critic: EMPTY_DRAFT_ROLE,
    automation: EMPTY_DRAFT_ROLE,
  });
  const [draftConfig, setDraftConfig] = useState<DraftModelRolesConfig>({
    action: EMPTY_DRAFT_ROLE,
    planner: EMPTY_DRAFT_ROLE,
    compaction: EMPTY_DRAFT_ROLE,
    title: EMPTY_DRAFT_ROLE,
    explorer: EMPTY_DRAFT_ROLE,
    critic: EMPTY_DRAFT_ROLE,
    automation: EMPTY_DRAFT_ROLE,
  });
  const [modelOptions, setModelOptions] = useState<Record<EditableRoleKey, string[]>>({
    planner: [],
    compaction: [],
    title: [],
    explorer: [],
    critic: [],
    automation: [],
  });
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);

  async function syncConfig(showLoading = false) {
    if (showLoading) setLoading(true);
    try {
      const [roleConfig, availableProviders] = await Promise.all([
        getModelRolesConfig(),
        listProviders(),
      ]);
      const normalized = normalizeDraftModelRolesConfig(roleConfig);
      const nextModels = await loadRoleModelOptions(normalized);
      setProviders(availableProviders);
      setSavedConfig(normalized);
      setDraftConfig(normalized);
      setModelOptions(nextModels);
      return true;
    } catch {
      setStatus({ tone: "error", message: "無法載入 Model Roles 設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncConfig(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  const isDirty = !configsEqual(savedConfig, draftConfig);
  const actionSummary = useMemo(() => roleSummary(savedConfig.action), [savedConfig.action]);

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncConfig(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步 Model Roles 設定" });
      }
    } finally {
      setBusyAction("");
    }
  }

  function handleReset() {
    setDraftConfig(savedConfig);
    setStatus({ tone: "info", message: "已還原未儲存變更" });
  }

  async function handleSave() {
    setBusyAction("save");
    try {
      const payload = serializeDraftModelRolesConfig(draftConfig);
      await setModelRolesConfig(payload);
      await syncConfig(false);
      setStatus({ tone: "success", message: "Model Roles 設定已儲存" });
    } catch (error) {
      const message =
        error instanceof Error && error.message
          ? error.message
          : "儲存 Model Roles 設定失敗";
      setStatus({ tone: "error", message });
    } finally {
      setBusyAction("");
    }
  }

  async function handleProviderChange(role: EditableRoleKey, provider: string) {
    setBusyAction("provider");
    try {
      const models = provider ? (await listModels(provider)).models || [] : [];
      setModelOptions((current) => ({
        ...current,
        [role]: models,
      }));
      setDraftConfig((current) => ({
        ...current,
        [role]: {
          ...current[role],
          provider,
          model: provider ? "default" : "",
        },
      }));
    } catch {
      setStatus({ tone: "error", message: `${ROLE_LABELS[role]} model 清單載入失敗` });
    } finally {
      setBusyAction("");
    }
  }

  function handleModelChange(role: EditableRoleKey, model: string) {
    setDraftConfig((current) => ({
      ...current,
      [role]: {
        ...current[role],
        model,
      },
    }));
  }

  function handleThinkingChange(role: EditableRoleKey, thinkingMode: DraftThinkingMode) {
    setDraftConfig((current) => ({
      ...current,
      [role]: {
        ...current[role],
        thinking_mode: normalizeThinkingMode(thinkingMode),
      },
    }));
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="skeleton h-6 w-36" />
            <div className="skeleton h-4 w-80 max-w-full" />
            <div className="grid gap-4 lg:grid-cols-3">
              {Array.from({ length: 3 }).map((_, index) => (
                <div key={index} className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body gap-3 p-4">
                    <div className="skeleton h-5 w-24" />
                    <div className="skeleton h-4 w-44" />
                    <div className="skeleton h-10 w-full" />
                    <div className="skeleton h-10 w-full" />
                    <div className="skeleton h-10 w-full" />
                  </div>
                </div>
              ))}
            </div>
          </div>
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
              <div className="flex items-center gap-2">
                <div className="badge badge-outline badge-sm">Model Roles</div>
                <div className="badge badge-primary badge-sm">planner / compaction / title</div>
              </div>
              <div>
                <h2 className="card-title text-2xl">Workflow Model Roles</h2>
                <p className="text-sm text-base-content/60">
                  `action` role 仍由 Provider 設定管理；這裡只調整 planner、compaction、title。
                </p>
              </div>
            </div>

            <div className="join">
              <button
                className="btn btn-sm join-item"
                onClick={handleReload}
                disabled={busyAction !== ""}
              >
                {busyAction === "reload" ? "同步中..." : "重新載入"}
              </button>
              <button
                className="btn btn-sm join-item"
                onClick={handleReset}
                disabled={busyAction !== "" || !isDirty}
              >
                還原
              </button>
              <button
                className="btn btn-primary btn-sm join-item"
                onClick={handleSave}
                disabled={busyAction !== "" || !isDirty}
              >
                {busyAction === "save" ? "儲存中..." : "儲存"}
              </button>
            </div>
          </div>

          {status && (
            <div className={`alert ${statusClass(status.tone)}`}>
              <span>{status.message}</span>
            </div>
          )}

          <div className="card border border-base-300 bg-base-100 shadow-sm">
            <div className="card-body gap-3 p-4">
              <div className="flex items-center gap-2">
                <div className="badge badge-secondary badge-sm">Action</div>
                <span className="text-sm font-medium">由 Provider panel 管理</span>
              </div>
              <p className="text-sm text-base-content/65">{actionSummary}</p>
              <p className="text-xs text-base-content/50">
                其它 roles 若 provider / model / thinking_mode 留空，就會回退到 Action。
              </p>
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-6 xl:grid-cols-3">
        {ROLE_ORDER.map((role) => {
          const config = draftConfig[role];
          const availableModels = config.provider
            ? Array.from(new Set(["default", ...(modelOptions[role] || [])]))
            : [];

          return (
            <div
              key={role}
              className="card border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <h3 className="card-title text-lg">{ROLE_LABELS[role]}</h3>
                    {!config.provider && (
                      <div className="badge badge-ghost badge-sm">Fallback</div>
                    )}
                  </div>
                  <p className="text-sm text-base-content/60">
                    {ROLE_DESCRIPTIONS[role]}
                  </p>
                </div>

                <label className="form-control gap-2">
                  <span className="label-text text-sm font-medium">Provider</span>
                  <select
                    className="select select-bordered"
                    value={config.provider}
                    disabled={busyAction !== ""}
                    onChange={(event) => {
                      void handleProviderChange(role, event.target.value);
                    }}
                  >
                    <option value="">跟隨 Action</option>
                    {providers.map((provider) => (
                      <option key={provider} value={provider}>
                        {provider}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="form-control gap-2">
                  <span className="label-text text-sm font-medium">Model</span>
                  <select
                    className="select select-bordered"
                    value={config.provider ? config.model || "default" : ""}
                    disabled={busyAction !== "" || !config.provider}
                    onChange={(event) => handleModelChange(role, event.target.value)}
                  >
                    {!config.provider ? (
                      <option value="">跟隨 Action</option>
                    ) : (
                      availableModels.map((model) => (
                        <option key={model} value={model}>
                          {model}
                        </option>
                      ))
                    )}
                  </select>
                  <span className="text-xs text-base-content/50">
                    provider 已指定但 model 留空時，後端會自動正規化成 `default`。
                  </span>
                </label>

                <label className="form-control gap-2">
                  <span className="label-text text-sm font-medium">Thinking</span>
                  <select
                    className="select select-bordered"
                    value={config.thinking_mode}
                    disabled={busyAction !== ""}
                    onChange={(event) => handleThinkingChange(role, event.target.value as DraftThinkingMode)}
                  >
                    <option value="">跟隨 Action</option>
                    {THINKING_OPTIONS.map((mode) => (
                      <option key={mode} value={mode}>
                        {mode}
                      </option>
                    ))}
                  </select>
                  <span className="text-xs text-base-content/50">
                    非 Gemini provider 目前會忽略 thinking mode，但設定仍會保留。
                  </span>
                </label>

                <div className="rounded-box border border-dashed border-base-300 bg-base-100/70 p-3 text-xs text-base-content/60">
                  目前摘要：{roleSummary(config)}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
