import { useEffect, useMemo, useState } from "react";
import { getGeneralConfig, setGeneralConfig } from "@/api/client";
import type { GeneralConfig } from "@/api/types";
import { useAppStore } from "@/store/appStore";
import {
  formatRFC3339MinuteInTimeZone,
  isValidTimeZone,
} from "@/utils/timezone";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction = "" | "reload" | "save" | "clear";

const EMPTY_GENERAL_CONFIG: GeneralConfig = {
  timezone: "",
};

function normalizeGeneralConfig(
  config?: Partial<GeneralConfig> | null,
): GeneralConfig {
  return {
    timezone: config?.timezone?.trim() ?? "",
  };
}

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

export function GeneralPanel() {
  const browserTimezone = useAppStore((s) => s.browserTimezone);
  const effectiveTimezone = useAppStore((s) => s.effectiveTimezone);
  const setGeneralTimezoneOverride = useAppStore(
    (s) => s.setGeneralTimezoneOverride,
  );

  const [savedConfig, setSavedConfig] = useState<GeneralConfig>(EMPTY_GENERAL_CONFIG);
  const [draftConfig, setDraftConfig] = useState<GeneralConfig>(EMPTY_GENERAL_CONFIG);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);

  async function syncGeneralConfig(showLoading = false) {
    if (showLoading) setLoading(true);

    try {
      const config = normalizeGeneralConfig(await getGeneralConfig());
      setSavedConfig(config);
      setDraftConfig(config);
      setGeneralTimezoneOverride(config.timezone);
      setLoadFailed(false);
      return true;
    } catch {
      setLoadFailed(true);
      setStatus({ tone: "error", message: "無法載入一般設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncGeneralConfig(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  const draftTimezone = draftConfig.timezone.trim();
  const draftTimezoneValid = draftTimezone === "" || isValidTimeZone(draftTimezone);
  const previewTimezone =
    draftTimezoneValid && draftTimezone ? draftTimezone : browserTimezone;
  const previewValue = useMemo(
    () => formatRFC3339MinuteInTimeZone(new Date(), previewTimezone),
    [previewTimezone],
  );
  const sourceLabel = savedConfig.timezone ? "Manual Override" : "Auto Detect";
  const sourceBadgeClass = savedConfig.timezone
    ? "badge badge-primary badge-sm"
    : "badge badge-ghost badge-sm";
  const isDirty = draftTimezone !== savedConfig.timezone;

  async function handleSave() {
    if (!draftTimezoneValid) {
      setStatus({
        tone: "error",
        message: "請輸入有效的 IANA timezone，例如 Asia/Taipei",
      });
      return;
    }

    const nextConfig = normalizeGeneralConfig({ timezone: draftTimezone });
    setBusyAction("save");
    try {
      await setGeneralConfig(nextConfig);
      setSavedConfig(nextConfig);
      setDraftConfig(nextConfig);
      setGeneralTimezoneOverride(nextConfig.timezone);
      setLoadFailed(false);
      setStatus({
        tone: "success",
        message: nextConfig.timezone
          ? `已儲存 timezone override: ${nextConfig.timezone}`
          : "已切回瀏覽器自動偵測時區",
      });
    } catch {
      setStatus({ tone: "error", message: "儲存一般設定失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleClearOverride() {
    setBusyAction("clear");
    try {
      await setGeneralConfig({ timezone: "" });
      setSavedConfig(EMPTY_GENERAL_CONFIG);
      setDraftConfig(EMPTY_GENERAL_CONFIG);
      setGeneralTimezoneOverride("");
      setLoadFailed(false);
      setStatus({ tone: "info", message: "已切回瀏覽器自動偵測時區" });
    } catch {
      setStatus({ tone: "error", message: "清除 timezone override 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncGeneralConfig(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步一般設定" });
      }
    } finally {
      setBusyAction("");
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex flex-col gap-3">
              <div className="flex gap-2">
                <div className="skeleton h-5 w-16" />
                <div className="skeleton h-5 w-24" />
              </div>
              <div className="skeleton h-8 w-56" />
              <div className="skeleton h-4 w-80 max-w-full" />
            </div>
            <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
              {Array.from({ length: 3 }).map((_, index) => (
                <div key={index} className="stat">
                  <div className="skeleton h-4 w-24" />
                  <div className="mt-3 skeleton h-8 w-28" />
                  <div className="mt-3 skeleton h-4 w-44" />
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="grid gap-6 lg:grid-cols-[minmax(0,0.95fr)_minmax(320px,1.05fr)]">
          {Array.from({ length: 2 }).map((_, index) => (
            <div
              key={index}
              className="card min-h-[24rem] border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                <div className="skeleton h-6 w-32" />
                <div className="skeleton h-4 w-64" />
                <div className="space-y-3">
                  {Array.from({ length: index === 0 ? 2 : 3 }).map((__, rowIndex) => (
                    <div
                      key={rowIndex}
                      className="card border border-base-300 bg-base-100 shadow-sm"
                    >
                      <div className="card-body gap-3 p-4">
                        <div className="skeleton h-5 w-24" />
                        <div className="skeleton h-11 w-full" />
                        <div className="skeleton h-4 w-48" />
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
                <div className="badge badge-outline badge-sm">一般</div>
                <div className={sourceBadgeClass}>{sourceLabel}</div>
              </div>
              <div>
                <h2 className="card-title text-2xl">一般設定</h2>
                <p className="text-sm text-base-content/60">
                  管理送給模型時使用的對話時區。空值代表 web 端沿用瀏覽器自動偵測。
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
                onClick={handleClearOverride}
                disabled={!savedConfig.timezone || busyAction !== ""}
              >
                {busyAction === "clear" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                清除 override
              </button>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">目前有效時區</div>
              <div className="stat-value text-lg">{effectiveTimezone}</div>
              <div className="stat-desc">
                {savedConfig.timezone
                  ? "後端與 web chat 會優先使用 override"
                  : "尚未設定 override，沿用瀏覽器時區"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">設定來源</div>
              <div className="stat-value text-lg">{sourceLabel}</div>
              <div className="stat-desc">
                {savedConfig.timezone || "Browser detected timezone"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">瀏覽器偵測</div>
              <div className="stat-value text-lg">{browserTimezone}</div>
              <div className="stat-desc">供 auto mode 與快速填入使用</div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,0.95fr)_minmax(320px,1.05fr)]">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="space-y-2">
              <h3 className="card-title text-xl">時區設定</h3>
              <p className="text-sm text-base-content/60">
                儲存後，web chat request 會帶上 `client_timezone` 與 `client_sent_at`，
                後端再用這個時區格式化送給模型的訊息時間。
              </p>
            </div>

            {loadFailed ? (
              <div className="alert alert-warning">
                <span>目前無法讀取一般設定；你仍可重新同步後再編輯。</span>
              </div>
            ) : null}

            <div className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body gap-4 p-5">
                <label className="form-control gap-2">
                  <span className="label-text font-medium">Timezone Override</span>
                  <input
                    className={`input input-bordered w-full outline-none focus:outline-none focus:ring-0 focus-visible:outline-none focus:shadow-none ${
                      draftTimezone && !draftTimezoneValid ? "input-error" : ""
                    }`}
                    value={draftConfig.timezone}
                    onChange={(event) =>
                      setDraftConfig(
                        normalizeGeneralConfig({ timezone: event.target.value }),
                      )
                    }
                    placeholder="Asia/Taipei"
                    autoComplete="off"
                  />
                  <span className="text-xs text-base-content/60">
                    使用 IANA timezone 名稱，例如 `Asia/Taipei`、`America/Los_Angeles`。
                  </span>
                </label>

                {draftTimezone && !draftTimezoneValid ? (
                  <div className="alert alert-error">
                    <span>這不是有效的 IANA timezone，請修正後再儲存。</span>
                  </div>
                ) : (
                  <div className="alert alert-info">
                    <span>
                      {draftTimezone
                        ? "儲存後會覆蓋瀏覽器 auto detect。"
                        : "目前為 auto mode，會沿用瀏覽器偵測時區。"}
                    </span>
                  </div>
                )}

                <div className="join flex-wrap">
                  <button
                    className="btn btn-sm join-item"
                    onClick={() =>
                      setDraftConfig(
                        normalizeGeneralConfig({ timezone: browserTimezone }),
                      )
                    }
                    disabled={busyAction !== ""}
                  >
                    使用瀏覽器時區
                  </button>
                  <button
                    className="btn btn-ghost btn-sm join-item"
                    onClick={handleSave}
                    disabled={busyAction !== "" || !isDirty}
                  >
                    {busyAction === "save" && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    儲存 override
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div className="space-y-6">
          <div className="card border border-base-300 bg-base-200 shadow-sm">
            <div className="card-body gap-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <h3 className="card-title text-xl">RFC3339 預覽</h3>
                  <p className="text-sm text-base-content/60">
                    這是目前 draft 時區下，模型會看到的分鐘級時間格式。
                  </p>
                </div>
                <div className="badge badge-secondary badge-sm">{previewTimezone}</div>
              </div>

              <div className="card border border-base-300 bg-base-100 shadow-sm">
                <div className="card-body gap-2 p-5">
                  <div className="text-xs uppercase tracking-wide text-base-content/50">
                    sent_at preview
                  </div>
                  <code className="text-sm break-all">{previewValue}</code>
                  <div className="text-xs text-base-content/60">
                    chat request 仍會送 `client_sent_at = new Date().toISOString()`；
                    後端會再依有效時區重格式化 prefix。
                  </div>
                </div>
              </div>
            </div>
          </div>

          {[
            {
              title: "日期顯示偏好",
              message: "之後可在這裡集中管理 transcript / usage 等畫面的日期顯示格式。",
            },
            {
              title: "全域語言偏好",
              message: "之後可放置 web 專用的 locale / wording 設定，不與 persona 混在一起。",
            },
          ].map((item) => (
            <div
              key={item.title}
              className="card border border-dashed border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-3">
                <div className="flex items-center justify-between gap-3">
                  <h3 className="card-title text-lg">{item.title}</h3>
                  <div className="badge badge-ghost badge-sm">Coming Soon</div>
                </div>
                <p className="text-sm text-base-content/60">{item.message}</p>
                <div className="card border border-base-300 bg-base-100 shadow-sm opacity-60">
                  <div className="card-body gap-3 p-4">
                    <div className="skeleton h-4 w-24" />
                    <div className="skeleton h-10 w-full" />
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {status ? (
        <div className="toast toast-end toast-bottom z-50">
          <div className={`alert shadow-lg ${statusClass(status.tone)}`}>
            <span>{status.message}</span>
          </div>
        </div>
      ) : null}
    </div>
  );
}
