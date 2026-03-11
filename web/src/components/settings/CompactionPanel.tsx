import { useEffect, useMemo, useState } from "react";
import {
  getCompactionConfig,
  setCompactionConfig,
} from "@/api/client";
import type { CompactionConfig } from "@/api/types";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction = "" | "reload" | "save";

const MIN_TRIGGER_THRESHOLD_RATIO = 0.5;
const MAX_TRIGGER_THRESHOLD_RATIO = 0.95;
const MIN_KEEP_RECENT_TOKENS = 1024;
const DEFAULT_COMPACTION_CONFIG: CompactionConfig = {
  enabled: true,
  background_enabled: true,
  llm_summary_enabled: true,
  trigger_threshold_ratio: 0.8,
  keep_recent_tokens: 20000,
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

function normalizeCompactionConfig(
  config?: Partial<CompactionConfig> | null,
): CompactionConfig {
  return {
    enabled: config?.enabled ?? DEFAULT_COMPACTION_CONFIG.enabled,
    background_enabled:
      config?.background_enabled ?? DEFAULT_COMPACTION_CONFIG.background_enabled,
    llm_summary_enabled:
      config?.llm_summary_enabled ?? DEFAULT_COMPACTION_CONFIG.llm_summary_enabled,
    trigger_threshold_ratio:
      config?.trigger_threshold_ratio ??
      DEFAULT_COMPACTION_CONFIG.trigger_threshold_ratio,
    keep_recent_tokens:
      config?.keep_recent_tokens ?? DEFAULT_COMPACTION_CONFIG.keep_recent_tokens,
  };
}

function areConfigsEqual(
  left: CompactionConfig,
  right: CompactionConfig,
): boolean {
  return (
    left.enabled === right.enabled &&
    left.background_enabled === right.background_enabled &&
    left.llm_summary_enabled === right.llm_summary_enabled &&
    left.trigger_threshold_ratio === right.trigger_threshold_ratio &&
    left.keep_recent_tokens === right.keep_recent_tokens
  );
}

export function CompactionPanel() {
  const [savedConfig, setSavedConfig] = useState<CompactionConfig>(
    DEFAULT_COMPACTION_CONFIG,
  );
  const [draftConfig, setDraftConfig] = useState<CompactionConfig>(
    DEFAULT_COMPACTION_CONFIG,
  );
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);

  async function syncCompactionConfig(showLoading = false) {
    if (showLoading) setLoading(true);

    try {
      const config = normalizeCompactionConfig(await getCompactionConfig());
      setSavedConfig(config);
      setDraftConfig(config);
      return true;
    } catch {
      setStatus({ tone: "error", message: "無法載入壓縮設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncCompactionConfig(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  const isDirty = !areConfigsEqual(savedConfig, draftConfig);
  const ratioValid =
    draftConfig.trigger_threshold_ratio >= MIN_TRIGGER_THRESHOLD_RATIO &&
    draftConfig.trigger_threshold_ratio <= MAX_TRIGGER_THRESHOLD_RATIO;
  const keepRecentValid =
    Number.isInteger(draftConfig.keep_recent_tokens) &&
    draftConfig.keep_recent_tokens >= MIN_KEEP_RECENT_TOKENS;
  const validationMessage = !ratioValid
    ? `觸發比例必須介於 ${MIN_TRIGGER_THRESHOLD_RATIO.toFixed(2)} 和 ${MAX_TRIGGER_THRESHOLD_RATIO.toFixed(2)} 之間`
    : !keepRecentValid
      ? `保留 token 數必須至少為 ${MIN_KEEP_RECENT_TOKENS}`
      : "";
  const saveDisabled = busyAction !== "" || !isDirty || !ratioValid || !keepRecentValid;
  const triggerPercent = Math.round(draftConfig.trigger_threshold_ratio * 100);
  const modeSummary = useMemo(() => {
    if (!draftConfig.enabled) {
      return "請求路徑與背景持久化摘要都停用。";
    }
    if (!draftConfig.background_enabled) {
      return "只保留請求路徑的 deterministic guardrail，不回寫 transcript。";
    }
    if (!draftConfig.llm_summary_enabled) {
      return "背景 worker 只使用 deterministic summary，完全不依賴模型摘要。";
    }
    return "請求路徑走 deterministic-first，背景 worker 會嘗試用 LLM 增強摘要。";
  }, [
    draftConfig.background_enabled,
    draftConfig.enabled,
    draftConfig.llm_summary_enabled,
  ]);

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncCompactionConfig(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步壓縮設定" });
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
    if (validationMessage) {
      setStatus({ tone: "error", message: validationMessage });
      return;
    }

    const nextConfig = normalizeCompactionConfig(draftConfig);
    setBusyAction("save");
    try {
      await setCompactionConfig(nextConfig);
      setSavedConfig(nextConfig);
      setDraftConfig(nextConfig);
      setStatus({ tone: "success", message: "壓縮設定已儲存" });
    } catch (error) {
      const message =
        error instanceof Error && error.message
          ? error.message
          : "儲存壓縮設定失敗";
      setStatus({ tone: "error", message });
    } finally {
      setBusyAction("");
    }
  }

  function setFlag<K extends keyof CompactionConfig>(key: K, value: CompactionConfig[K]) {
    setDraftConfig((current) => ({
      ...current,
      [key]: value,
    }));
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
              <div className="skeleton h-8 w-60" />
              <div className="skeleton h-4 w-96 max-w-full" />
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
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1.1fr)_minmax(320px,0.9fr)]">
          {Array.from({ length: 2 }).map((_, index) => (
            <div
              key={index}
              className="card min-h-[24rem] border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                <div className="skeleton h-6 w-36" />
                <div className="skeleton h-4 w-64" />
                <div className="space-y-3">
                  {Array.from({ length: index === 0 ? 5 : 4 }).map((__, rowIndex) => (
                    <div
                      key={rowIndex}
                      className="card border border-base-300 bg-base-100 shadow-sm"
                    >
                      <div className="card-body gap-3 p-4">
                        <div className="skeleton h-5 w-28" />
                        <div className="skeleton h-11 w-full" />
                        <div className="skeleton h-4 w-52" />
                      </div>
                    </div>
                  ))}
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
              <div className="flex items-center gap-2">
                <div className="badge badge-outline badge-sm">Compaction</div>
                <div className={draftConfig.enabled ? "badge badge-primary badge-sm" : "badge badge-ghost badge-sm"}>
                  {draftConfig.enabled ? "Enabled" : "Disabled"}
                </div>
              </div>
              <div>
                <h2 className="card-title text-2xl">壓縮設定</h2>
                <p className="text-sm text-base-content/60">
                  控制 request path 的 deterministic 壓縮與背景 transcript 持久化摘要。
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
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">觸發比例</div>
              <div className="stat-value text-lg">{triggerPercent}%</div>
              <div className="stat-desc">超過 context window 或檔案大小比例時排入背景壓縮。</div>
            </div>
            <div className="stat">
              <div className="stat-title">尾段保留</div>
              <div className="stat-value text-lg">{draftConfig.keep_recent_tokens.toLocaleString()}</div>
              <div className="stat-desc">背景 rewrite 時至少保留最新尾段 token 預算。</div>
            </div>
            <div className="stat">
              <div className="stat-title">背景 worker</div>
              <div className="stat-value text-lg">
                {draftConfig.enabled && draftConfig.background_enabled ? "On" : "Off"}
              </div>
              <div className="stat-desc">單工 FIFO，startup 會先 catch up 超門檻 session。</div>
            </div>
            <div className="stat">
              <div className="stat-title">摘要模式</div>
              <div className="stat-value text-lg">
                {draftConfig.llm_summary_enabled ? "LLM + Fallback" : "Deterministic"}
              </div>
              <div className="stat-desc">{modeSummary}</div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.1fr)_minmax(320px,0.9fr)]">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-5">
            <div>
              <h3 className="card-title text-lg">Runtime 行為</h3>
              <p className="text-sm text-base-content/60">
                request path 永遠不等待背景 worker；這裡只控制何時 enqueue 與背景摘要策略。
              </p>
            </div>

            <label className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body flex-row items-center justify-between gap-4 p-4">
                <div className="space-y-1">
                  <div className="font-medium">啟用壓縮</div>
                  <p className="text-sm text-base-content/60">
                    關閉後請求路徑與背景 transcript compact 都停用。
                  </p>
                </div>
                <input
                  type="checkbox"
                  className="toggle toggle-primary"
                  checked={draftConfig.enabled}
                  onChange={(event) => setFlag("enabled", event.target.checked)}
                />
              </div>
            </label>

            <label className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body flex-row items-center justify-between gap-4 p-4">
                <div className="space-y-1">
                  <div className="font-medium">背景持久化摘要</div>
                  <p className="text-sm text-base-content/60">
                    append 後超門檻 session 會排進單工 worker，完成後 rewrite transcript。
                  </p>
                </div>
                <input
                  type="checkbox"
                  className="toggle toggle-primary"
                  checked={draftConfig.background_enabled}
                  disabled={!draftConfig.enabled}
                  onChange={(event) =>
                    setFlag("background_enabled", event.target.checked)
                  }
                />
              </div>
            </label>

            <label className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body flex-row items-center justify-between gap-4 p-4">
                <div className="space-y-1">
                  <div className="font-medium">LLM 摘要增強</div>
                  <p className="text-sm text-base-content/60">
                    背景 compact 時優先嘗試模型摘要；失敗或停用時自動降級 deterministic summary。
                  </p>
                </div>
                <input
                  type="checkbox"
                  className="toggle toggle-primary"
                  checked={draftConfig.llm_summary_enabled}
                  disabled={!draftConfig.enabled || !draftConfig.background_enabled}
                  onChange={(event) =>
                    setFlag("llm_summary_enabled", event.target.checked)
                  }
                />
              </div>
            </label>

            <div className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body gap-3 p-4">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <div className="font-medium">觸發比例</div>
                    <p className="text-sm text-base-content/60">
                      合法範圍 {MIN_TRIGGER_THRESHOLD_RATIO.toFixed(2)}-
                      {MAX_TRIGGER_THRESHOLD_RATIO.toFixed(2)}。
                    </p>
                  </div>
                  <span className="badge badge-outline badge-sm">
                    {draftConfig.trigger_threshold_ratio.toFixed(2)}
                  </span>
                </div>
                <input
                  type="range"
                  min={MIN_TRIGGER_THRESHOLD_RATIO}
                  max={MAX_TRIGGER_THRESHOLD_RATIO}
                  step="0.01"
                  className="range range-primary range-sm"
                  value={draftConfig.trigger_threshold_ratio}
                  onChange={(event) =>
                    setFlag(
                      "trigger_threshold_ratio",
                      Number.parseFloat(event.target.value),
                    )
                  }
                />
                <input
                  type="number"
                  className={`input input-bordered w-full ${
                    ratioValid ? "" : "input-error"
                  }`}
                  min={MIN_TRIGGER_THRESHOLD_RATIO}
                  max={MAX_TRIGGER_THRESHOLD_RATIO}
                  step="0.01"
                  value={draftConfig.trigger_threshold_ratio}
                  onChange={(event) =>
                    setFlag(
                      "trigger_threshold_ratio",
                      Number.parseFloat(event.target.value),
                    )
                  }
                />
              </div>
            </div>

            <div className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body gap-3 p-4">
                <div>
                  <div className="font-medium">保留最新 token 數</div>
                  <p className="text-sm text-base-content/60">
                    背景 rewrite 時保留 transcript 尾段的最低 token 預算。
                  </p>
                </div>
                <input
                  type="number"
                  className={`input input-bordered w-full ${
                    keepRecentValid ? "" : "input-error"
                  }`}
                  min={MIN_KEEP_RECENT_TOKENS}
                  step="256"
                  value={draftConfig.keep_recent_tokens}
                  onChange={(event) =>
                    setFlag(
                      "keep_recent_tokens",
                      Number.parseInt(event.target.value, 10),
                    )
                  }
                />
              </div>
            </div>
          </div>
        </div>

        <div className="space-y-6">
          <div className="card border border-base-300 bg-base-200 shadow-sm">
            <div className="card-body gap-4">
              <div>
                <h3 className="card-title text-lg">摘要</h3>
                <p className="text-sm text-base-content/60">
                  這些設定只影響 compaction，不改動 provider fallback、memory 或 session routing。
                </p>
              </div>
              <div className="space-y-3">
                <div className="alert alert-info">
                  <span>Request path 會先裁切超長 tool result，再清理 orphaned tool pairs，最後才補 dropped-history summary。</span>
                </div>
                <div className="alert alert-info">
                  <span>背景 worker 採 event-driven queue，不做固定週期全量掃描。</span>
                </div>
                {validationMessage ? (
                  <div className="alert alert-error">
                    <span>{validationMessage}</span>
                  </div>
                ) : (
                  <div className="alert alert-success">
                    <span>目前輸入有效，可以直接儲存。</span>
                  </div>
                )}
              </div>
            </div>
          </div>

          <div className="card border border-base-300 bg-base-200 shadow-sm">
            <div className="card-body gap-4">
              <div>
                <h3 className="card-title text-lg">儲存</h3>
                <p className="text-sm text-base-content/60">
                  設定會寫進 `config.json`，worker 不提供即時狀態頁或手動 compact 入口。
                </p>
              </div>
              <button
                className="btn btn-primary"
                onClick={handleSave}
                disabled={saveDisabled}
              >
                {busyAction === "save" && (
                  <span className="loading loading-spinner loading-sm" />
                )}
                儲存壓縮設定
              </button>
              <div className="text-sm text-base-content/60">
                Dirty state: {isDirty ? "有未儲存變更" : "已同步"}
              </div>
            </div>
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
