import { useState, useEffect } from "react";
import { useAppStore } from "@/store/appStore";
import {
  listProviders,
  listModels,
  getDefaultProvider,
  setDefaultProvider,
  getFallbacks,
  setFallbacks,
} from "@/api/client";
import type { FallbackEntry } from "@/api/types";

const MAX_FALLBACKS = 5;

/**
 * Provider settings panel — select provider, model, and configure fallbacks.
 */
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
  const [status, setStatus] = useState("");

  // Load providers on mount
  useEffect(() => {
    listProviders()
      .then(setProviders)
      .catch(() => {});
    getDefaultProvider()
      .then((dp) => {
        storeSetProvider(dp.provider);
        storeSetModel(dp.model);
      })
      .catch(() => {});
    getFallbacks()
      .then((fb) => setFallbacksList(fb.fallbacks || []))
      .catch(() => {});
  }, [storeSetProvider, storeSetModel]);

  // Load models when provider changes
  useEffect(() => {
    if (!provider) return;
    listModels(provider)
      .then((resp) => setModels(resp.models || []))
      .catch(() => setModels([]));
  }, [provider]);

  // Load models for existing fallback providers on mount
  useEffect(() => {
    fallbacks.forEach((fb, i) => {
      if (!fb?.provider) return;
      // Skip if already loaded
      if (fallbackModels[i]?.length) return;
      listModels(fb.provider)
        .then((resp) =>
          setFallbackModels((prev) => ({
            ...prev,
            [i]: resp.models || [],
          })),
        )
        .catch(() => {});
    });
  }, [fallbacks]); // eslint-disable-line react-hooks/exhaustive-deps

  async function handleProviderChange(newProvider: string) {
    storeSetProvider(newProvider);
    storeSetModel("default");
    try {
      await setDefaultProvider({ provider: newProvider, model: "default" });
      setStatus("已切換 Provider");
      setTimeout(() => setStatus(""), 2000);
    } catch {
      setStatus("切換失敗");
    }
  }

  async function handleModelChange(newModel: string) {
    storeSetModel(newModel);
    try {
      await setDefaultProvider({ provider, model: newModel });
      setStatus("已切換 Model");
      setTimeout(() => setStatus(""), 2000);
    } catch {
      setStatus("切換失敗");
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
      updated[index] = { ...updated[index], provider: newProvider };
    }
    setFallbacksList(updated);

    // Load models for fallback
    try {
      const resp = await listModels(newProvider);
      setFallbackModels((prev) => ({ ...prev, [index]: resp.models || [] }));
    } catch {
      /* ignore */
    }

    await saveFallbacks(updated);
  }

  async function handleFallbackModelChange(index: number, newModel: string) {
    const updated = [...fallbacks];
    if (updated[index]) {
      updated[index] = { ...updated[index], model: newModel };
      setFallbacksList(updated);
      await saveFallbacks(updated);
    }
  }

  async function handleClearFallback(index: number) {
    const updated = fallbacks.filter((_, i) => i !== index);
    setFallbacksList(updated);
    await saveFallbacks(updated);
  }

  async function saveFallbacks(entries: FallbackEntry[]) {
    try {
      await setFallbacks({ fallbacks: entries.filter((e) => e.provider) });
      setStatus("Fallback 已更新");
      setTimeout(() => setStatus(""), 2000);
    } catch {
      setStatus("Fallback 更新失敗");
    }
  }

  return (
    <div className="space-y-6">
      {/* Primary provider — daisyUI fieldset + label */}
      <fieldset className="fieldset">
        <legend className="fieldset-legend">Provider</legend>
        <label className="label">
          <span className="label-text">主要 Provider</span>
        </label>
        <select
          className="select select-bordered w-full focus:outline-0"
          value={provider}
          onChange={(e) => handleProviderChange(e.target.value)}
        >
          <option value="">選擇 Provider</option>
          {providers.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      </fieldset>

      {/* Model — daisyUI fieldset + label */}
      <fieldset className="fieldset">
        <legend className="fieldset-legend">Model</legend>
        <label className="label">
          <span className="label-text">選擇模型</span>
        </label>
        <select
          className="select select-bordered w-full focus:outline-0"
          value={model}
          onChange={(e) => handleModelChange(e.target.value)}
        >
          <option value="default">default</option>
          {models.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
      </fieldset>

      {/* Fallbacks — daisyUI fieldset + join for row grouping */}
      <fieldset className="fieldset">
        <legend className="fieldset-legend">
          Fallback Chain (最多 {MAX_FALLBACKS})
        </legend>
        <label className="label">
          <span className="label-text">依序嘗試的備援 Provider</span>
        </label>
        <div className="space-y-2">
          {Array.from({ length: MAX_FALLBACKS }).map((_, i) => {
            const fb = fallbacks[i];
            return (
              <div key={i} className="flex gap-2 items-center">
                <span className="badge badge-ghost badge-sm font-mono w-6 shrink-0">
                  {i + 1}
                </span>
                <div className="join flex-1">
                  <select
                    className="select select-bordered select-sm join-item flex-1 focus:outline-0"
                    value={fb?.provider || ""}
                    onChange={(e) =>
                      handleFallbackProviderChange(i, e.target.value)
                    }
                  >
                    <option value="">—</option>
                    {providers.map((p) => (
                      <option key={p} value={p}>
                        {p}
                      </option>
                    ))}
                  </select>
                  <select
                    className="select select-bordered select-sm join-item flex-1 focus:outline-0"
                    value={fb?.model || "default"}
                    onChange={(e) =>
                      handleFallbackModelChange(i, e.target.value)
                    }
                    disabled={!fb?.provider}
                  >
                    <option value="default">default</option>
                    {(fallbackModels[i] || []).map((m) => (
                      <option key={m} value={m}>
                        {m}
                      </option>
                    ))}
                  </select>
                </div>
                {fb?.provider && (
                  <div className="tooltip" data-tip="清除">
                    <button
                      className="btn btn-ghost btn-xs btn-square"
                      onClick={() => handleClearFallback(i)}
                    >
                      ✕
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </fieldset>

      {/* Status toast — daisyUI toast (fixed bottom-right) */}
      {status && (
        <div className="toast toast-end toast-bottom z-50">
          <div className="alert alert-info alert-sm shadow-lg">
            <span>{status}</span>
          </div>
        </div>
      )}
    </div>
  );
}
