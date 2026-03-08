import { useEffect, useState } from "react";
import { useAppStore } from "@/store/appStore";
import {
  getDefaultProvider,
  getFallbacks,
  listModels,
  listProviders,
  setDefaultProvider,
  setFallbacks,
} from "@/api/client";
import type { FallbackEntry } from "@/api/types";

const MAX_FALLBACKS = 5;

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;

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

async function loadModelOptions(
  primaryProvider: string,
  fallbackEntries: FallbackEntry[],
): Promise<{
  primaryModels: string[];
  fallbackModelMap: Record<number, string[]>;
}> {
  const providerNames = new Set<string>();
  if (primaryProvider) providerNames.add(primaryProvider);
  fallbackEntries.forEach((entry) => {
    if (entry.provider) providerNames.add(entry.provider);
  });

  const modelMap = new Map<string, string[]>();
  await Promise.all(
    Array.from(providerNames).map(async (providerName) => {
      try {
        const resp = await listModels(providerName);
        modelMap.set(providerName, resp.models || []);
      } catch {
        modelMap.set(providerName, []);
      }
    }),
  );

  const fallbackModelMap: Record<number, string[]> = {};
  fallbackEntries.forEach((entry, index) => {
    fallbackModelMap[index] = modelMap.get(entry.provider) ?? [];
  });

  return {
    primaryModels: modelMap.get(primaryProvider) ?? [],
    fallbackModelMap,
  };
}

export function ProviderPanel() {
  const provider = useAppStore((s) => s.provider);
  const model = useAppStore((s) => s.model);
  const storeSetProvider = useAppStore((s) => s.setProvider);
  const storeSetModel = useAppStore((s) => s.setModel);

  const [providers, setProviders] = useState<string[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const [fallbacks, setFallbacksList] = useState<FallbackEntry[]>([]);
  const [fallbackModels, setFallbackModels] = useState<
    Record<number, string[]>
  >({});
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState("");
  const [status, setStatus] = useState<StatusState>(null);

  async function syncProviderConfig(showLoading = false) {
    if (showLoading) setLoading(true);

    try {
      const [loadedProviders, defaultProvider, fallbackResp] = await Promise.all(
        [listProviders(), getDefaultProvider(), getFallbacks()],
      );
      const loadedFallbacks = fallbackResp.fallbacks || [];
      const loadedModels = await loadModelOptions(
        defaultProvider.provider,
        loadedFallbacks,
      );

      setProviders(loadedProviders);
      storeSetProvider(defaultProvider.provider);
      storeSetModel(defaultProvider.model);
      setModels(loadedModels.primaryModels);
      setFallbacksList(loadedFallbacks);
      setFallbackModels(loadedModels.fallbackModelMap);
      return true;
    } catch {
      setStatus({ tone: "error", message: "無法載入 Provider 設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncProviderConfig(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  async function handleProviderChange(newProvider: string) {
    setBusyAction("provider");
    storeSetProvider(newProvider);
    storeSetModel("default");
    setModels([]);

    try {
      await setDefaultProvider({ provider: newProvider, model: "default" });
      const { primaryModels } = await loadModelOptions(newProvider, []);
      setModels(primaryModels);
      setStatus({ tone: "success", message: "已切換 Provider" });
    } catch {
      setStatus({ tone: "error", message: "切換 Provider 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleModelChange(newModel: string) {
    setBusyAction("model");
    storeSetModel(newModel);

    try {
      await setDefaultProvider({ provider, model: newModel });
      setStatus({ tone: "success", message: "已切換 Model" });
    } catch {
      setStatus({ tone: "error", message: "切換 Model 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleFallbackProviderChange(
    index: number,
    newProvider: string,
  ) {
    const updated = [...fallbacks];
    if (!updated[index]) {
      updated[index] = { provider: newProvider, model: "default" };
    } else {
      updated[index] = {
        provider: newProvider,
        model: newProvider ? "default" : "",
      };
    }

    setBusyAction(`fallback-${index}`);
    setFallbacksList(updated);

    try {
      if (newProvider) {
        const resp = await listModels(newProvider);
        setFallbackModels((prev) => ({
          ...prev,
          [index]: resp.models || [],
        }));
      } else {
        setFallbackModels((prev) => ({ ...prev, [index]: [] }));
      }

      await saveFallbacks(updated, "Fallback 已更新");
    } finally {
      setBusyAction("");
    }
  }

  async function handleFallbackModelChange(index: number, newModel: string) {
    const updated = [...fallbacks];
    if (!updated[index]) return;

    updated[index] = { ...updated[index], model: newModel };
    setBusyAction(`fallback-${index}`);
    setFallbacksList(updated);

    try {
      await saveFallbacks(updated, "Fallback 已更新");
    } finally {
      setBusyAction("");
    }
  }

  async function handleClearFallback(index: number) {
    const updated = fallbacks.filter((_, currentIndex) => currentIndex !== index);
    setBusyAction(`fallback-${index}`);
    setFallbacksList(updated);

    try {
      const { fallbackModelMap } = await loadModelOptions(provider, updated);
      setFallbackModels(fallbackModelMap);
      await saveFallbacks(updated, "已清除備援項目");
    } finally {
      setBusyAction("");
    }
  }

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncProviderConfig(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步 Provider 設定" });
      }
    } finally {
      setBusyAction("");
    }
  }

  async function saveFallbacks(entries: FallbackEntry[], successMessage: string) {
    try {
      await setFallbacks({ fallbacks: entries.filter((entry) => entry.provider) });
      setStatus({ tone: "success", message: successMessage });
    } catch {
      setStatus({ tone: "error", message: "Fallback 更新失敗" });
    }
  }

  const configuredFallbacks = fallbacks.filter((entry) => entry.provider).length;
  const providerDisabled = loading || providers.length === 0 || busyAction !== "";
  const modelDisabled = providerDisabled || !provider;

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex flex-col gap-3">
              <div className="flex gap-2">
                <div className="skeleton h-5 w-20" />
                <div className="skeleton h-5 w-16" />
              </div>
              <div className="skeleton h-8 w-56" />
              <div className="skeleton h-4 w-80 max-w-full" />
            </div>
            <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
              {Array.from({ length: 3 }).map((_, index) => (
                <div key={index} className="stat">
                  <div className="skeleton h-4 w-20" />
                  <div className="mt-3 skeleton h-8 w-28" />
                  <div className="mt-3 skeleton h-4 w-40" />
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="grid gap-6 lg:grid-cols-[minmax(0,0.9fr)_minmax(320px,1.1fr)]">
          {Array.from({ length: 2 }).map((_, index) => (
            <div
              key={index}
              className="card min-h-[26rem] border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                <div className="skeleton h-6 w-32" />
                <div className="skeleton h-4 w-56" />
                <div className="space-y-3">
                  {Array.from({ length: index === 0 ? 2 : 5 }).map(
                    (_, rowIndex) => (
                      <div
                        key={rowIndex}
                        className="card border border-base-300 bg-base-100 shadow-sm"
                      >
                        <div className="card-body gap-3 p-4">
                          <div className="skeleton h-5 w-20" />
                          <div className="skeleton h-11 w-full" />
                          <div className="skeleton h-11 w-full" />
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

  if (providers.length === 0) {
    return (
      <div className="space-y-6">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center gap-2">
              <div className="badge badge-outline badge-sm">Provider</div>
              <div className="badge badge-ghost badge-sm">Unavailable</div>
            </div>
            <div>
              <h2 className="card-title text-2xl">模型提供者設定</h2>
              <p className="text-sm text-base-content/60">
                目前沒有可用的 Provider，請先確認伺服器設定或憑證狀態。
              </p>
            </div>
            <div className="alert alert-warning">
              <span>找不到可用 Provider，暫時無法編輯預設模型或 fallback chain。</span>
            </div>
            <div className="card-actions justify-end">
              <button
                className="btn  btn-sm"
                onClick={handleReload}
                disabled={busyAction !== ""}
              >
                {busyAction === "reload" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                重新同步
              </button>
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

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <div className="badge badge-outline badge-sm">Provider</div>
                <div className="badge badge-secondary badge-sm">Auto Save</div>
              </div>
              <div>
                <h2 className="card-title text-2xl">模型提供者設定</h2>
                <p className="text-sm text-base-content/60">
                  管理預設 Provider / Model，並設定主模型失敗時依序接手的 fallback chain。
                </p>
              </div>
            </div>

            <div className="join">
              <button
                className="btn  btn-sm join-item"
                onClick={handleReload}
                disabled={busyAction !== ""}
              >
                {busyAction === "reload" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                重新同步
              </button>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">主 Provider</div>
              <div className="stat-value text-lg">{provider || "未設定"}</div>
              <div className="stat-desc">目前對話的預設提供者</div>
            </div>
            <div className="stat">
              <div className="stat-title">目前 Model</div>
              <div className="stat-value text-lg">{model || "default"}</div>
              <div className="stat-desc">
                {models.length > 0 ? `${models.length} 個可選模型` : "使用 default"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">Fallback Chain</div>
              <div className="stat-value text-lg">{configuredFallbacks}</div>
              <div className="stat-desc">
                最多 {MAX_FALLBACKS} 組備援，已載入 {providers.length} 個 Provider
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,0.9fr)_minmax(320px,1.1fr)]">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-5">
            <div className="space-y-2">
              <h3 className="card-title text-lg">主要 Provider</h3>
              <p className="text-sm text-base-content/60">
                這組設定會直接寫回預設 Provider 與 Model，變更後立即生效。
              </p>
            </div>

            <div className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body gap-4 p-4">
                <div className="space-y-2">
                  <label className="label p-0">
                    <span className="label-text font-medium">Provider</span>
                  </label>
                  <select
                    className="select select-bordered w-full focus:outline-0"
                    value={provider}
                    onChange={(e) => void handleProviderChange(e.target.value)}
                    disabled={providerDisabled}
                  >
                    <option value="">選擇 Provider</option>
                    {providers.map((providerName) => (
                      <option key={providerName} value={providerName}>
                        {providerName}
                      </option>
                    ))}
                  </select>
                  <p className="text-xs text-base-content/60">
                    選擇新的 Provider 後，Model 會先重設為 `default`。
                  </p>
                </div>

                <div className="space-y-2">
                  <label className="label p-0">
                    <span className="label-text font-medium">Model</span>
                  </label>
                  <select
                    className="select select-bordered w-full focus:outline-0"
                    value={model || "default"}
                    onChange={(e) => void handleModelChange(e.target.value)}
                    disabled={modelDisabled}
                  >
                    <option value="default">default</option>
                    {models.map((modelName) => (
                      <option key={modelName} value={modelName}>
                        {modelName}
                      </option>
                    ))}
                  </select>
                  {provider && models.length === 0 ? (
                    <p className="text-xs text-base-content/60">
                      這個 Provider 沒有額外模型清單，將使用 `default`。
                    </p>
                  ) : (
                    <p className="text-xs text-base-content/60">
                      使用模型清單中的項目，或保留 `default` 交由 provider 端決定。
                    </p>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>

        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-3">
              <div className="space-y-2">
                <h3 className="card-title text-lg">Fallback Chain</h3>
                <p className="text-sm text-base-content/60">
                  主 Provider 失敗時依序嘗試這些備援設定，最多 {MAX_FALLBACKS} 組。
                </p>
              </div>
              <div className="badge badge-ghost badge-sm">
                {configuredFallbacks} / {MAX_FALLBACKS}
              </div>
            </div>

            <div className="space-y-3">
              {Array.from({ length: MAX_FALLBACKS }).map((_, index) => {
                const fallback = fallbacks[index];
                const rowBusy = busyAction === `fallback-${index}`;
                const fallbackProvider = fallback?.provider || "";
                const fallbackModel = fallback?.model || "default";

                return (
                  <div
                    key={index}
                    className="card border border-base-300 bg-base-100 shadow-sm"
                  >
                    <div className="card-body gap-3 p-4">
                      <div className="flex items-center justify-between gap-3">
                        <div className="flex items-center gap-2">
                          <div className="badge badge-ghost badge-sm">
                            {index + 1}
                          </div>
                          {fallbackProvider ? (
                            <div className="badge badge-primary badge-sm">
                              Configured
                            </div>
                          ) : (
                            <div className="badge badge-ghost badge-sm">Empty</div>
                          )}
                        </div>
                        {fallbackProvider && (
                          <button
                            className="btn btn-ghost btn-xs"
                            onClick={() => void handleClearFallback(index)}
                            disabled={busyAction !== ""}
                          >
                            {rowBusy && (
                              <span className="loading loading-spinner loading-xs" />
                            )}
                            清除
                          </button>
                        )}
                      </div>

                      <div className="join join-vertical w-full md:join-horizontal">
                        <select
                          className="select select-bordered join-item w-full focus:outline-0"
                          value={fallbackProvider}
                          onChange={(e) =>
                            void handleFallbackProviderChange(index, e.target.value)
                          }
                          disabled={busyAction !== ""}
                        >
                          <option value="">選擇備援 Provider</option>
                          {providers.map((providerName) => (
                            <option key={providerName} value={providerName}>
                              {providerName}
                            </option>
                          ))}
                        </select>
                        <select
                          className="select select-bordered join-item w-full focus:outline-0"
                          value={fallbackModel}
                          onChange={(e) =>
                            void handleFallbackModelChange(index, e.target.value)
                          }
                          disabled={!fallbackProvider || busyAction !== ""}
                        >
                          <option value="default">default</option>
                          {(fallbackModels[index] || []).map((modelName) => (
                            <option key={modelName} value={modelName}>
                              {modelName}
                            </option>
                          ))}
                        </select>
                      </div>

                      <p className="text-xs text-base-content/60">
                        {fallbackProvider
                          ? "這組備援會在前一個 provider 失敗後自動接手。"
                          : "留空代表這個順位不啟用。"}
                      </p>
                    </div>
                  </div>
                );
              })}
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
