import { useEffect, useState } from "react";
import { getTelegramConfig, setTelegramConfig } from "@/api/client";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction = "" | "save" | "reload";

const TELEGRAM_COMMANDS = [
  {
    command: "/reset",
    description: "重開一個新的對話 session。",
  },
  {
    command: "/persona",
    description: "列出目前可用的 Persona。",
  },
  {
    command: "/persona <name>",
    description: "切換到指定 Persona。",
  },
  {
    command: "/persona off",
    description: "停用目前 Persona。",
  },
];

const TELEGRAM_BEHAVIORS = [
  "支援 private chat、@mention、回覆 bot 與 slash commands。",
  "圖片與照片附件會下載後作為 multimodal input 傳入模型。",
  "每個 chat 會維持獨立 session，並保留 chat-session binding。",
  "訊息上限 4096 字，超長回覆會自動分段。",
];

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

function maskToken(token: string): string {
  const trimmed = token.trim();
  if (!trimmed) return "未設定";
  if (trimmed.length <= 8) return `${trimmed.slice(0, 2)}****${trimmed.slice(-2)}`;
  return `${trimmed.slice(0, 6)}****${trimmed.slice(-4)}`;
}

export function TelegramPanel() {
  const [savedToken, setSavedToken] = useState("");
  const [draftToken, setDraftToken] = useState("");
  const [showToken, setShowToken] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);

  async function syncTelegramConfig(showLoading = false) {
    if (showLoading) setLoading(true);

    try {
      const config = await getTelegramConfig();
      const token = config.bot_token ?? "";
      setSavedToken(token);
      setDraftToken(token);
      return true;
    } catch {
      setStatus({ tone: "error", message: "無法載入 Telegram 設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncTelegramConfig(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncTelegramConfig(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步 Telegram 設定" });
      }
    } finally {
      setBusyAction("");
    }
  }

  async function handleSave() {
    const nextToken = draftToken.trim();
    setBusyAction("save");

    try {
      await setTelegramConfig({ bot_token: nextToken });
      setSavedToken(nextToken);
      setDraftToken(nextToken);
      setStatus({
        tone: "success",
        message: nextToken
          ? "Telegram Bot Token 已儲存，重啟後生效"
          : "已清空 Telegram Bot Token，重啟後生效",
      });
    } catch {
      setStatus({ tone: "error", message: "儲存 Telegram 設定失敗" });
    } finally {
      setBusyAction("");
    }
  }

  function handleReset() {
    setDraftToken(savedToken);
    setStatus({ tone: "info", message: "已還原未儲存變更" });
  }

  const trimmedDraftToken = draftToken.trim();
  const trimmedSavedToken = savedToken.trim();
  const hasSavedToken = trimmedSavedToken.length > 0;
  const hasDraftToken = trimmedDraftToken.length > 0;
  const isDirty = draftToken !== savedToken;
  const actionsDisabled = loading || busyAction !== "";
  const saveDisabled = actionsDisabled || trimmedDraftToken === trimmedSavedToken;
  const resetDisabled = actionsDisabled || !isDirty;

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex flex-col gap-3">
              <div className="flex gap-2">
                <div className="skeleton h-5 w-24" />
                <div className="skeleton h-5 w-20" />
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

        <div className="grid gap-6 lg:grid-cols-[minmax(0,1.05fr)_minmax(320px,0.95fr)]">
          {Array.from({ length: 2 }).map((_, index) => (
            <div
              key={index}
              className="card min-h-[26rem] border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                <div className="skeleton h-6 w-36" />
                <div className="skeleton h-4 w-64" />
                <div className="space-y-3">
                  {Array.from({ length: index === 0 ? 3 : 6 }).map(
                    (_, rowIndex) => (
                      <div
                        key={rowIndex}
                        className="card border border-base-300 bg-base-100 shadow-sm"
                      >
                        <div className="card-body gap-3 p-4">
                          <div className="skeleton h-5 w-20" />
                          <div className="skeleton h-11 w-full" />
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
              <div className="flex items-center gap-2">
                <div className="badge badge-outline badge-sm">Telegram</div>
                <div className="badge badge-info badge-sm">Manual Save</div>
              </div>
              <div>
                <h2 className="card-title text-2xl">Telegram Bot 設定</h2>
                <p className="text-sm text-base-content/60">
                  使用與 Persona 頁面一致的管理版型編輯 Telegram Bot Token，儲存後寫回 config.json。
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
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">Bot Token</div>
              <div className="stat-value text-lg">
                {hasSavedToken ? "已設定" : "未設定"}
              </div>
              <div className="stat-desc">{maskToken(savedToken)}</div>
            </div>
            <div className="stat">
              <div className="stat-title">儲存狀態</div>
              <div className="stat-value text-lg">
                {isDirty ? "未儲存" : "已同步"}
              </div>
              <div className="stat-desc">變更後需重啟 Telegram bot 生效</div>
            </div>
            <div className="stat">
              <div className="stat-title">指令支援</div>
              <div className="stat-value text-lg">{TELEGRAM_COMMANDS.length}</div>
              <div className="stat-desc">Long polling、圖片輸入與 per-chat session</div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.05fr)_minmax(320px,0.95fr)]">
        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-5">
            <div className="flex items-center justify-between gap-3">
              <div className="space-y-2">
                <h3 className="card-title text-lg">Bot 憑證</h3>
                <p className="text-sm text-base-content/60">
                  目前 web 端只需設定 `bot_token`。若使用環境變數，`TELEGRAM_BOT_TOKEN` 會優先於 config.json。
                </p>
              </div>
              <div className={`badge badge-sm ${isDirty ? "badge-warning" : "badge-ghost"}`}>
                {isDirty ? "Draft" : "Synced"}
              </div>
            </div>

            <div className="card border border-base-300 bg-base-100 shadow-sm">
              <div className="card-body gap-4 p-4">
                <div className="space-y-2">
                  <div className="flex items-center justify-between gap-3">
                    <label className="label p-0">
                      <span className="label-text font-medium">Bot Token</span>
                    </label>
                    <span className="text-xs text-base-content/55">
                      {hasDraftToken ? `${trimmedDraftToken.length} chars` : "未填寫"}
                    </span>
                  </div>

                  <div className="join w-full">
                    <input
                      type={showToken ? "text" : "password"}
                      className="input input-bordered join-item w-full font-mono focus:outline-0"
                      placeholder="123456789:AA..."
                      value={draftToken}
                      onChange={(e) => setDraftToken(e.target.value)}
                      disabled={actionsDisabled}
                      autoComplete="off"
                      spellCheck={false}
                    />
                    <button
                      className="btn  join-item"
                      onClick={() => setShowToken((current) => !current)}
                      disabled={actionsDisabled}
                    >
                      {showToken ? "隱藏" : "顯示"}
                    </button>
                  </div>

                  <p className="text-xs text-base-content/60">
                    Token 會儲存在 `config.json`；若你在執行環境有設定 `TELEGRAM_BOT_TOKEN`，runtime 仍會以環境變數為主。
                  </p>
                </div>

                <div className="alert alert-info">
                  <span>
                    儲存後不會自動重啟 Telegram bot。請在更新 token 後重新啟動服務，避免仍使用舊憑證。
                  </span>
                </div>

                <div className="card-actions items-center justify-between">
                  <button
                    className="btn btn-ghost"
                    onClick={() => setDraftToken("")}
                    disabled={actionsDisabled || !draftToken}
                  >
                    清空欄位
                  </button>

                  <div className="join">
                    <button
                      className="btn btn-sm join-item"
                      onClick={handleReset}
                      disabled={resetDisabled}
                    >
                      還原
                    </button>
                    <button
                      className="btn btn-primary btn-sm join-item"
                      onClick={handleSave}
                      disabled={saveDisabled}
                    >
                      {busyAction === "save" && (
                        <span className="loading loading-spinner loading-xs" />
                      )}
                      儲存設定
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <ul className="list rounded-box border border-base-300 bg-base-100">
              <li className="list-row">
                <div>
                  <div className="text-xs uppercase tracking-wide text-base-content/50">
                    Config Source
                  </div>
                  <div className="font-medium">Environment &gt; config.json</div>
                </div>
              </li>
              <li className="list-row">
                <div>
                  <div className="text-xs uppercase tracking-wide text-base-content/50">
                    Persisted Value
                  </div>
                  <div className="font-mono text-sm">{maskToken(savedToken)}</div>
                </div>
              </li>
              <li className="list-row">
                <div>
                  <div className="text-xs uppercase tracking-wide text-base-content/50">
                    Runtime
                  </div>
                  <div className="text-sm text-base-content/75">
                    Long polling，typing indicator 每 4 秒更新一次。
                  </div>
                </div>
              </li>
            </ul>
          </div>
        </div>

        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">整合說明</h3>
              <div className="badge badge-outline badge-sm">Telegram Bot</div>
            </div>

            <div className="space-y-3">
              <div className="card border border-base-300 bg-base-100 shadow-sm">
                <div className="card-body gap-3 p-4">
                  <div className="flex items-center justify-between gap-3">
                    <h4 className="font-semibold">可用指令</h4>
                    <div className="badge badge-ghost badge-sm">
                      {TELEGRAM_COMMANDS.length} commands
                    </div>
                  </div>

                  <ul className="list rounded-box border border-base-300 bg-base-200/40">
                    {TELEGRAM_COMMANDS.map((item) => (
                      <li key={item.command} className="list-row">
                        <div>
                          <div className="font-mono text-sm">{item.command}</div>
                          <div className="text-sm text-base-content/65">
                            {item.description}
                          </div>
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
              </div>

              <div className="card border border-base-300 bg-base-100 shadow-sm">
                <div className="card-body gap-3 p-4">
                  <div className="flex items-center justify-between gap-3">
                    <h4 className="font-semibold">行為摘要</h4>
                    <div className="badge badge-ghost badge-sm">README</div>
                  </div>

                  <ul className="list rounded-box border border-base-300 bg-base-200/40">
                    {TELEGRAM_BEHAVIORS.map((behavior) => (
                      <li key={behavior} className="list-row">
                        <div className="text-sm text-base-content/75">{behavior}</div>
                      </li>
                    ))}
                  </ul>
                </div>
              </div>

              <div className="card border border-base-300 bg-base-100 shadow-sm">
                <div className="card-body gap-3 p-4">
                  <h4 className="font-semibold">啟用檢查</h4>
                  <div className="alert alert-warning">
                    <span>
                      這個頁面只負責保存設定，不會驗證 token 是否可用，也不會自動啟動 bot process。
                    </span>
                  </div>
                  <p className="text-sm text-base-content/65">
                    若更新後沒有反應，先確認目前程序是否讀到環境變數覆蓋值，並重新啟動 NekoClaw。
                  </p>
                </div>
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
