import { useEffect, useMemo, useState } from "react";
import { getDiscordConfig, setDiscordConfig } from "@/api/client";
import type { DiscordConfig } from "@/api/types";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction = "" | "save" | "reload";

type DiscordFormState = {
  bot_token: string;
  reply_to_original: boolean;
  console_channel: string;
};

const DEFAULT_FORM: DiscordFormState = {
  bot_token: "",
  reply_to_original: true,
  console_channel: "",
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

function normalizeDiscordConfig(
  config?: Partial<DiscordConfig> | null,
): DiscordFormState {
  return {
    bot_token: config?.bot_token?.trim() ?? "",
    reply_to_original: config?.reply_to_original ?? true,
    console_channel: config?.console_channel?.trim() ?? "",
  };
}

function areConfigsEqual(left: DiscordFormState, right: DiscordFormState): boolean {
  return (
    left.bot_token === right.bot_token &&
    left.reply_to_original === right.reply_to_original &&
    left.console_channel === right.console_channel
  );
}

function maskToken(token: string): string {
  if (!token) return "未設定";
  if (token.length <= 10) return `${token.slice(0, 2)}••••${token.slice(-2)}`;
  return `${token.slice(0, 4)}••••••${token.slice(-4)}`;
}

function formatReplyMode(replyToOriginal: boolean): string {
  return replyToOriginal ? "回覆原始訊息" : "直接送出訊息";
}

function formatConsoleChannel(channelID: string): string {
  return channelID || "未設定";
}

export function DiscordPanel() {
  const [savedConfig, setSavedConfig] = useState<DiscordFormState>(DEFAULT_FORM);
  const [form, setForm] = useState<DiscordFormState>(DEFAULT_FORM);
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);
  const [tokenVisible, setTokenVisible] = useState(false);

  async function syncDiscordConfig(showLoading = false) {
    if (showLoading) setLoading(true);

    try {
      const config = normalizeDiscordConfig(await getDiscordConfig());
      setSavedConfig(config);
      setForm(config);
      return true;
    } catch {
      setStatus({ tone: "error", message: "無法載入 Discord 設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncDiscordConfig(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  const isDirty = useMemo(
    () => !areConfigsEqual(form, savedConfig),
    [form, savedConfig],
  );

  const hasToken = savedConfig.bot_token !== "";
  const hasConsoleChannel = savedConfig.console_channel !== "";

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncDiscordConfig(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步 Discord 設定" });
      }
    } finally {
      setBusyAction("");
    }
  }

  function handleReset() {
    setForm(savedConfig);
    setStatus({ tone: "info", message: "表單已還原為已儲存內容" });
  }

  async function handleSave() {
    setBusyAction("save");
    const nextConfig = normalizeDiscordConfig(form);

    try {
      await setDiscordConfig(nextConfig);
      setSavedConfig(nextConfig);
      setForm(nextConfig);
      setStatus({ tone: "success", message: "Discord 設定已儲存" });
    } catch {
      setStatus({ tone: "error", message: "儲存 Discord 設定失敗" });
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
                <div className="skeleton h-5 w-20" />
                <div className="skeleton h-5 w-20" />
              </div>
              <div className="skeleton h-8 w-56" />
              <div className="skeleton h-4 w-96 max-w-full" />
            </div>
            <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
              {Array.from({ length: 4 }).map((_, index) => (
                <div key={index} className="stat">
                  <div className="skeleton h-4 w-20" />
                  <div className="mt-3 skeleton h-8 w-28" />
                  <div className="mt-3 skeleton h-4 w-36" />
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="grid gap-6 lg:grid-cols-[minmax(0,1.05fr)_minmax(320px,0.95fr)]">
          {Array.from({ length: 2 }).map((_, index) => (
            <div
              key={index}
              className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                <div className="skeleton h-6 w-36" />
                <div className="skeleton h-4 w-56" />
                <div className="space-y-3">
                  {Array.from({ length: index === 0 ? 4 : 5 }).map(
                    (_, rowIndex) => (
                      <div
                        key={rowIndex}
                        className="card border border-base-300 bg-base-100 shadow-sm"
                      >
                        <div className="card-body gap-3 p-4">
                          <div className="skeleton h-5 w-24" />
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

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <div className="badge badge-outline badge-sm">Discord</div>
                <div className={`badge badge-sm ${hasToken ? "badge-primary" : "badge-ghost"}`}>
                  {hasToken ? "Configured" : "Inactive"}
                </div>
                {isDirty && <div className="badge badge-warning badge-sm">未儲存</div>}
              </div>
              <div>
                <h2 className="card-title text-2xl">Discord Bot 設定</h2>
                <p className="text-sm text-base-content/60">
                  使用和 Persona 頁相同的設定版型，集中管理 bot token、回覆模式與 console
                  channel。
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
              <div className="stat-title">Bot Token</div>
              <div className="stat-value text-lg">{maskToken(savedConfig.bot_token)}</div>
              <div className="stat-desc">
                {hasToken ? "已寫入 config.json" : "尚未設定 Discord bot token"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">回覆模式</div>
              <div className="stat-value text-lg">
                {savedConfig.reply_to_original ? "Reply" : "Direct"}
              </div>
              <div className="stat-desc">
                {formatReplyMode(savedConfig.reply_to_original)}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">Console Channel</div>
              <div className="stat-value text-lg">
                {hasConsoleChannel ? "Enabled" : "Optional"}
              </div>
              <div className="stat-desc font-mono">
                {formatConsoleChannel(savedConfig.console_channel)}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">套用方式</div>
              <div className="stat-value text-lg">Restart</div>
              <div className="stat-desc">儲存後需重新啟動 bot 程序</div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.05fr)_minmax(320px,0.95fr)]">
        <div className="card min-h-[30rem] border border-base-300 bg-base-200 shadow-sm">
          <form
            className="card-body gap-5"
            onSubmit={(event) => {
              event.preventDefault();
              if (!isDirty || busyAction !== "") return;
              void handleSave();
            }}
          >
            <div className="flex items-center justify-between gap-3">
              <div>
                <h3 className="card-title text-lg">連線與回覆</h3>
                <p className="text-sm text-base-content/60">
                  這些欄位會直接對應 `/v1/discord/config`。
                </p>
              </div>
              <div className="badge badge-outline badge-sm">
                {isDirty ? "Editing" : "Saved"}
              </div>
            </div>

            <label className="fieldset">
              <legend className="fieldset-legend">Bot Token</legend>
              <label className="input input-bordered flex w-full items-center gap-2 focus-within:outline-none focus-within:ring-0 focus-within:shadow-none">
                <input
                  type={tokenVisible ? "text" : "password"}
                  className="min-w-0 grow outline-none focus:outline-none focus:ring-0 focus-visible:outline-none"
                  placeholder="Discord Bot Token"
                  autoComplete="current-password"
                  spellCheck={false}
                  value={form.bot_token}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      bot_token: event.target.value,
                    }))
                  }
                />
                <button
                  type="button"
                  className="btn btn-ghost btn-xs"
                  onClick={() => setTokenVisible((current) => !current)}
                >
                  {tokenVisible ? "隱藏" : "顯示"}
                </button>
              </label>
              <p className="label text-base-content/55">
                若同時設定環境變數 `DISCORD_BOT_TOKEN`，環境變數會優先。
              </p>
            </label>

            <div className="fieldset">
              <legend className="fieldset-legend">Reply Mode</legend>
              <div className="grid gap-3 sm:grid-cols-2">
                <button
                  type="button"
                  className={`card border text-left shadow-sm transition-colors ${
                    form.reply_to_original
                      ? "border-primary bg-primary/8"
                      : "border-base-300 bg-base-100"
                  }`}
                  onClick={() =>
                    setForm((current) => ({
                      ...current,
                      reply_to_original: true,
                    }))
                  }
                >
                  <div className="card-body gap-2 p-4">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-semibold">回覆原始訊息</span>
                      {form.reply_to_original && (
                        <span className="badge badge-primary badge-sm">目前選擇</span>
                      )}
                    </div>
                    <p className="text-sm text-base-content/65">
                      Bot 會以 reply 形式回應，保留原始訊息脈絡。
                    </p>
                  </div>
                </button>

                <button
                  type="button"
                  className={`card border text-left shadow-sm transition-colors ${
                    !form.reply_to_original
                      ? "border-primary bg-primary/8"
                      : "border-base-300 bg-base-100"
                  }`}
                  onClick={() =>
                    setForm((current) => ({
                      ...current,
                      reply_to_original: false,
                    }))
                  }
                >
                  <div className="card-body gap-2 p-4">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-semibold">直接送出訊息</span>
                      {!form.reply_to_original && (
                        <span className="badge badge-primary badge-sm">目前選擇</span>
                      )}
                    </div>
                    <p className="text-sm text-base-content/65">
                      Bot 直接在頻道發言，不綁定原始訊息的 reply UI。
                    </p>
                  </div>
                </button>
              </div>
            </div>

            <label className="fieldset">
              <legend className="fieldset-legend">Console Channel</legend>
              <input
                type="text"
                className="input input-bordered w-full font-mono outline-none focus:outline-none focus:ring-0 focus-visible:outline-none focus:shadow-none"
                placeholder="123456789012345678"
                value={form.console_channel}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    console_channel: event.target.value,
                  }))
                }
              />
              <p className="label text-base-content/55">
                可留空。設定後會把啟動、錯誤、session reset、persona 切換等 log 發到該頻道。
              </p>
            </label>

            <div className="alert alert-warning">
              <span>Web 頁面只負責儲存設定；正在執行中的 Discord bot 需要重啟才會套用新值。</span>
            </div>

            <div className="card-actions justify-end">
                <button
                  className="btn"
                  type="button"
                  onClick={handleReset}
                  disabled={!isDirty || busyAction !== ""}
                >
                  還原已儲存內容
                </button>
                <button
                  className="btn btn-primary"
                  type="submit"
                  disabled={!isDirty || busyAction !== ""}
                >
                  {busyAction === "save" && (
                    <span className="loading loading-spinner loading-xs" />
                  )}
                  儲存 Discord 設定
                </button>
              </div>
          </form>
        </div>

        <div className="space-y-6">
          <div className="card border border-base-300 bg-base-200 shadow-sm">
            <div className="card-body gap-4">
              <div className="flex items-center justify-between gap-3">
                <h3 className="card-title text-lg">設定摘要</h3>
                <div className="badge badge-ghost badge-sm">
                  {hasToken ? "Ready" : "Setup required"}
                </div>
              </div>

              <ul className="list rounded-box border border-base-300 bg-base-100">
                <li className="list-row">
                  <div>
                    <div className="text-xs uppercase tracking-wide text-base-content/50">
                      Token Preview
                    </div>
                    <div className="font-mono text-sm">{maskToken(form.bot_token)}</div>
                  </div>
                </li>
                <li className="list-row">
                  <div>
                    <div className="text-xs uppercase tracking-wide text-base-content/50">
                      Current Reply Mode
                    </div>
                    <div className="font-medium">
                      {formatReplyMode(form.reply_to_original)}
                    </div>
                  </div>
                </li>
                <li className="list-row">
                  <div>
                    <div className="text-xs uppercase tracking-wide text-base-content/50">
                      Console Output
                    </div>
                    <div className="font-mono text-sm">
                      {formatConsoleChannel(form.console_channel)}
                    </div>
                  </div>
                </li>
                <li className="list-row">
                  <div>
                    <div className="text-xs uppercase tracking-wide text-base-content/50">
                      Save State
                    </div>
                    <div className="font-medium">
                      {isDirty ? "尚有未儲存變更" : "表單與已儲存內容一致"}
                    </div>
                  </div>
                </li>
              </ul>
            </div>
          </div>

          <div className="card border border-base-300 bg-base-200 shadow-sm">
            <div className="card-body gap-4">
              <div>
                <h3 className="card-title text-lg">行為說明</h3>
                <p className="text-sm text-base-content/60">
                  這裡整理目前 Discord bot 的主要行為，方便在同一頁確認設定影響。
                </p>
              </div>

              <ul className="list rounded-box border border-base-300 bg-base-100">
                <li className="list-row">
                  <div>
                    <div className="font-medium">觸發條件</div>
                    <div className="text-sm text-base-content/65">
                      支援 `@mention`、回覆 bot 訊息，以及 DM。
                    </div>
                  </div>
                </li>
                <li className="list-row">
                  <div>
                    <div className="font-medium">內建指令</div>
                    <div className="text-sm text-base-content/65">
                      `/reset`、`/persona`、`/persona off` 與 `/persona &lt;name&gt;`。
                    </div>
                  </div>
                </li>
                <li className="list-row">
                  <div>
                    <div className="font-medium">Console 頻道用途</div>
                    <div className="text-sm text-base-content/65">
                      啟動訊息、錯誤、session reset、persona 切換與流量摘要都會送往指定 channel。
                    </div>
                  </div>
                </li>
                <li className="list-row">
                  <div>
                    <div className="font-medium">套用時機</div>
                    <div className="text-sm text-base-content/65">
                      設定寫入 `config.json` 後，新的 bot 程序才會讀到更新內容。
                    </div>
                  </div>
                </li>
              </ul>
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
