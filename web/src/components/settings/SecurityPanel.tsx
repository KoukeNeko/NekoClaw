import { useEffect, useMemo, useState } from "react";
import {
  ApiError,
  changeSecurityPassword,
  getSecurityConfig,
  getSecurityStatus,
  logoutAllSecurity,
  logoutSecurity,
  setSecurityConfig,
} from "@/api/client";
import type { SecurityConfig } from "@/api/types";
import { useAppStore } from "@/store/appStore";
import { SelectionList, SelectionListItem } from "./SelectionList";

type DetailSection = "admin" | "session" | "hardening";
type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction =
  | ""
  | "reload"
  | "save"
  | "logout"
  | "logoutAll"
  | "password";

const DEFAULT_SECURITY_CONFIG: SecurityConfig = {
  auth_enabled: true,
  session_idle_minutes: 8 * 60,
  session_absolute_hours: 7 * 24,
  login_max_attempts: 5,
  login_block_minutes: 15,
};

function normalizeSecurityConfig(
  config?: Partial<SecurityConfig> | null,
): SecurityConfig {
  return {
    auth_enabled: config?.auth_enabled ?? DEFAULT_SECURITY_CONFIG.auth_enabled,
    session_idle_minutes:
      config?.session_idle_minutes ?? DEFAULT_SECURITY_CONFIG.session_idle_minutes,
    session_absolute_hours:
      config?.session_absolute_hours ??
      DEFAULT_SECURITY_CONFIG.session_absolute_hours,
    login_max_attempts:
      config?.login_max_attempts ?? DEFAULT_SECURITY_CONFIG.login_max_attempts,
    login_block_minutes:
      config?.login_block_minutes ?? DEFAULT_SECURITY_CONFIG.login_block_minutes,
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

function formatDateTime(value?: string): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return "-";
  return date.toLocaleString("zh-TW", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function parsePositiveInt(value: string, fallback: number): number {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

export function SecurityPanel() {
  const securityStatus = useAppStore((s) => s.securityStatus);
  const setSecurityStatus = useAppStore((s) => s.setSecurityStatus);
  const resetForAuthBoundary = useAppStore((s) => s.resetForAuthBoundary);
  const setRoute = useAppStore((s) => s.setRoute);

  const [savedConfig, setSavedConfig] = useState<SecurityConfig>(
    DEFAULT_SECURITY_CONFIG,
  );
  const [draftConfig, setDraftConfig] = useState<SecurityConfig>(
    DEFAULT_SECURITY_CONFIG,
  );
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [detailSection, setDetailSection] = useState<DetailSection>("admin");
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  async function refresh(showLoading = false) {
    if (showLoading) setLoading(true);
    try {
      const [config, nextStatus] = await Promise.all([
        getSecurityConfig(),
        getSecurityStatus(),
      ]);
      const normalized = normalizeSecurityConfig(config);
      setSavedConfig(normalized);
      setDraftConfig(normalized);
      setSecurityStatus(nextStatus);
      setLoadFailed(false);
      return true;
    } catch {
      setLoadFailed(true);
      setStatus({ tone: "error", message: "無法載入安全設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void refresh(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2400);
    return () => window.clearTimeout(timer);
  }, [status]);

  const isDirty = useMemo(
    () => JSON.stringify(savedConfig) !== JSON.stringify(draftConfig),
    [draftConfig, savedConfig],
  );

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await refresh(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步安全設定" });
      }
    } finally {
      setBusyAction("");
    }
  }

  async function handleSaveConfig() {
    setBusyAction("save");
    try {
      const nextConfig = normalizeSecurityConfig(draftConfig);
      const saved = await setSecurityConfig(nextConfig);
      setSavedConfig(normalizeSecurityConfig(saved));
      setDraftConfig(normalizeSecurityConfig(saved));
      const nextStatus = await getSecurityStatus();
      setSecurityStatus(nextStatus);
      setStatus({ tone: "success", message: "安全設定已儲存" });
    } catch (caught) {
      setStatus({
        tone: "error",
        message: caught instanceof ApiError ? caught.message : "儲存安全設定失敗",
      });
    } finally {
      setBusyAction("");
    }
  }

  async function handleLogout() {
    setBusyAction("logout");
    try {
      const nextStatus = await logoutSecurity();
      setSecurityStatus(nextStatus);
      resetForAuthBoundary();
      setStatus({ tone: "info", message: "已登出目前 session" });
      setRoute(nextStatus.auth_enabled ? "login" : "chat");
    } catch (caught) {
      setStatus({
        tone: "error",
        message: caught instanceof ApiError ? caught.message : "登出失敗",
      });
    } finally {
      setBusyAction("");
    }
  }

  async function handleLogoutAll() {
    setBusyAction("logoutAll");
    try {
      const nextStatus = await logoutAllSecurity();
      setSecurityStatus(nextStatus);
      resetForAuthBoundary();
      setStatus({ tone: "info", message: "已清除全部 session" });
      setRoute(nextStatus.auth_enabled ? "login" : "chat");
    } catch (caught) {
      setStatus({
        tone: "error",
        message: caught instanceof ApiError ? caught.message : "清除全部 session 失敗",
      });
    } finally {
      setBusyAction("");
    }
  }

  async function handleChangePassword() {
    if (!securityStatus?.initialized) {
      setStatus({ tone: "error", message: "尚未初始化管理員密碼" });
      return;
    }
    if (newPassword.length < 12) {
      setStatus({ tone: "error", message: "新密碼至少需要 12 個字元" });
      return;
    }
    if (newPassword !== confirmPassword) {
      setStatus({ tone: "error", message: "兩次輸入的新密碼不一致" });
      return;
    }

    setBusyAction("password");
    try {
      const nextStatus = await changeSecurityPassword({
        current_password: currentPassword,
        new_password: newPassword,
      });
      setSecurityStatus(nextStatus);
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setStatus({ tone: "success", message: "管理員密碼已更新" });
    } catch (caught) {
      setStatus({
        tone: "error",
        message: caught instanceof ApiError ? caught.message : "更新密碼失敗",
      });
    } finally {
      setBusyAction("");
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="skeleton h-8 w-40" />
            <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
              {Array.from({ length: 4 }).map((_, index) => (
                <div key={index} className="stat">
                  <div className="skeleton h-4 w-24" />
                  <div className="mt-3 skeleton h-8 w-28" />
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    );
  }

  const authBadgeClass = draftConfig.auth_enabled
    ? "badge badge-primary badge-sm"
    : "badge badge-warning badge-sm";

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <div className="badge badge-outline badge-sm">Security</div>
                <div className={authBadgeClass}>
                  {draftConfig.auth_enabled ? "Auth Enabled" : "Auth Disabled"}
                </div>
              </div>
              <div>
                <h2 className="card-title text-2xl">後台安全</h2>
                <p className="text-sm text-base-content/60">
                  管理單一管理員登入、session timeout 與後台基礎防禦策略。
                </p>
              </div>
            </div>

            <div className="join">
              <button
                className="btn btn-sm join-item"
                onClick={handleReload}
                disabled={busyAction !== ""}
              >
                {busyAction === "reload" ? (
                  <span className="loading loading-spinner loading-xs" />
                ) : null}
                重新同步
              </button>
              <button
                className="btn btn-primary btn-sm join-item"
                onClick={handleSaveConfig}
                disabled={!isDirty || busyAction !== ""}
              >
                {busyAction === "save" ? (
                  <span className="loading loading-spinner loading-xs" />
                ) : null}
                儲存設定
              </button>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">登入狀態</div>
              <div className="stat-value text-lg">
                {draftConfig.auth_enabled
                  ? securityStatus?.authenticated
                    ? "已登入"
                    : "已保護"
                  : "未保護"}
              </div>
              <div className="stat-desc">
                {securityStatus?.initialized
                  ? "管理員密碼已設定"
                  : "尚未設定管理員密碼"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">目前 Session</div>
              <div className="stat-value text-lg">
                {securityStatus?.current_session ? "Active" : "None"}
              </div>
              <div className="stat-desc">
                {securityStatus?.current_session
                  ? `Idle 到 ${formatDateTime(
                    securityStatus.current_session.idle_expires_at,
                  )}`
                  : "未持有登入 session"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">Session Policy</div>
              <div className="stat-value text-lg">
                {draftConfig.session_idle_minutes}m / {draftConfig.session_absolute_hours}h
              </div>
              <div className="stat-desc">idle timeout / absolute lifetime</div>
            </div>
            <div className="stat">
              <div className="stat-title">Login Throttle</div>
              <div className="stat-value text-lg">
                {draftConfig.login_max_attempts} / {draftConfig.login_block_minutes}m
              </div>
              <div className="stat-desc">
                {securityStatus?.login_status.blocked_until
                  ? `封鎖到 ${formatDateTime(
                    securityStatus.login_status.blocked_until,
                  )}`
                  : "目前來源未被封鎖"}
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(260px,0.8fr)_minmax(0,1.2fr)]">
        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <h3 className="card-title text-lg">安全項目</h3>
            <SelectionList>
              <SelectionListItem
                selected={detailSection === "admin"}
                onClick={() => setDetailSection("admin")}
              >
                <div className="min-w-0">
                  <div className="font-medium">後台登入</div>
                  <div className="text-sm text-base-content/60">
                    管理登入開關與密碼輪替
                  </div>
                </div>
              </SelectionListItem>
              <SelectionListItem
                selected={detailSection === "session"}
                onClick={() => setDetailSection("session")}
              >
                <div className="min-w-0">
                  <div className="font-medium">Session</div>
                  <div className="text-sm text-base-content/60">
                    timeout、登入狀態與 session 清除
                  </div>
                </div>
              </SelectionListItem>
              <SelectionListItem
                selected={detailSection === "hardening"}
                onClick={() => setDetailSection("hardening")}
              >
                <div className="min-w-0">
                  <div className="font-medium">基礎防禦</div>
                  <div className="text-sm text-base-content/60">
                    same-origin、cookie、setup token 與節流
                  </div>
                </div>
              </SelectionListItem>
            </SelectionList>
          </div>
        </div>

        <div className="card border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            {loadFailed ? (
              <div className="alert alert-warning">
                <span>目前無法完整同步安全設定；你仍可重新同步後再編輯。</span>
              </div>
            ) : null}

            {detailSection === "admin" ? (
              <div className="space-y-4">
                <div>
                  <h3 className="card-title text-xl">後台登入</h3>
                  <p className="text-sm text-base-content/60">
                    `web` mode 會以 cookie session 保護整個後台。關閉後，設定與聊天 API 將重新對外開放。
                  </p>
                </div>

                <div className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body gap-4 p-5">
                    <label className="label cursor-pointer justify-start gap-4">
                      <input
                        type="checkbox"
                        className="toggle toggle-primary"
                        checked={draftConfig.auth_enabled}
                        onChange={(event) =>
                          setDraftConfig((current) => ({
                            ...current,
                            auth_enabled: event.target.checked,
                          }))
                        }
                      />
                      <div>
                        <div className="font-medium">啟用後台登入保護</div>
                        <div className="text-sm text-base-content/60">
                          關閉後，整個 browser 後台不再要求登入。
                        </div>
                      </div>
                    </label>

                    {!securityStatus?.initialized ? (
                      <div className="alert alert-warning">
                        <span>尚未初始化管理員密碼。請使用 console 輸出的 setup URL 完成第一次設定。</span>
                      </div>
                    ) : (
                      <div className="rounded-box border border-base-300 bg-base-200 p-4 text-sm text-base-content/70">
                        上次密碼更新時間：{formatDateTime(securityStatus.password_updated_at)}
                      </div>
                    )}
                  </div>
                </div>

                <div className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body gap-4 p-5">
                    <div>
                      <div className="font-medium">變更管理員密碼</div>
                      <div className="text-sm text-base-content/60">
                        成功後會 rotate 目前 session，並清除其他 session。
                      </div>
                    </div>

                    <label className="form-control gap-2">
                      <span className="label-text">目前密碼</span>
                      <input
                        type="password"
                        className="input input-bordered w-full"
                        value={currentPassword}
                        onChange={(event) => setCurrentPassword(event.target.value)}
                        autoComplete="current-password"
                        disabled={!securityStatus?.initialized}
                      />
                    </label>
                    <label className="form-control gap-2">
                      <span className="label-text">新密碼</span>
                      <input
                        type="password"
                        className="input input-bordered w-full"
                        value={newPassword}
                        onChange={(event) => setNewPassword(event.target.value)}
                        autoComplete="new-password"
                        disabled={!securityStatus?.initialized}
                      />
                    </label>
                    <label className="form-control gap-2">
                      <span className="label-text">再次輸入新密碼</span>
                      <input
                        type="password"
                        className="input input-bordered w-full"
                        value={confirmPassword}
                        onChange={(event) => setConfirmPassword(event.target.value)}
                        autoComplete="new-password"
                        disabled={!securityStatus?.initialized}
                      />
                    </label>

                    <div className="card-actions justify-end">
                      <button
                        className="btn btn-primary"
                        onClick={handleChangePassword}
                        disabled={!securityStatus?.initialized || busyAction !== ""}
                      >
                        {busyAction === "password" ? (
                          <span className="loading loading-spinner loading-xs" />
                        ) : null}
                        更新密碼
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            ) : null}

            {detailSection === "session" ? (
              <div className="space-y-4">
                <div>
                  <h3 className="card-title text-xl">Session</h3>
                  <p className="text-sm text-base-content/60">
                    管理目前登入 session 的 idle timeout 與 absolute lifetime。
                  </p>
                </div>

                <div className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body grid gap-4 p-5 md:grid-cols-2">
                    <label className="form-control gap-2">
                      <span className="label-text">Idle Timeout (minutes)</span>
                      <input
                        type="number"
                        min={1}
                        className="input input-bordered w-full"
                        value={draftConfig.session_idle_minutes}
                        onChange={(event) =>
                          setDraftConfig((current) => ({
                            ...current,
                            session_idle_minutes: parsePositiveInt(
                              event.target.value,
                              current.session_idle_minutes,
                            ),
                          }))
                        }
                      />
                    </label>
                    <label className="form-control gap-2">
                      <span className="label-text">Absolute Lifetime (hours)</span>
                      <input
                        type="number"
                        min={1}
                        className="input input-bordered w-full"
                        value={draftConfig.session_absolute_hours}
                        onChange={(event) =>
                          setDraftConfig((current) => ({
                            ...current,
                            session_absolute_hours: parsePositiveInt(
                              event.target.value,
                              current.session_absolute_hours,
                            ),
                          }))
                        }
                      />
                    </label>
                  </div>
                </div>

                <ul className="list rounded-box border border-base-300 bg-base-100">
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Active Sessions
                      </div>
                      <div className="font-medium">{securityStatus?.session_count ?? 0}</div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Current Session Created
                      </div>
                      <div className="font-medium">
                        {formatDateTime(securityStatus?.current_session?.created_at)}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Idle Expires
                      </div>
                      <div className="font-medium">
                        {formatDateTime(securityStatus?.current_session?.idle_expires_at)}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Absolute Expires
                      </div>
                      <div className="font-medium">
                        {formatDateTime(
                          securityStatus?.current_session?.absolute_expires_at,
                        )}
                      </div>
                    </div>
                  </li>
                </ul>

                <div className="card-actions justify-end">
                  <button
                    className="btn btn-ghost"
                    onClick={handleLogout}
                    disabled={busyAction !== ""}
                  >
                    {busyAction === "logout" ? (
                      <span className="loading loading-spinner loading-xs" />
                    ) : null}
                    登出目前 Session
                  </button>
                  <button
                    className="btn btn-warning"
                    onClick={handleLogoutAll}
                    disabled={busyAction !== ""}
                  >
                    {busyAction === "logoutAll" ? (
                      <span className="loading loading-spinner loading-xs" />
                    ) : null}
                    登出全部 Session
                  </button>
                </div>
              </div>
            ) : null}

            {detailSection === "hardening" ? (
              <div className="space-y-4">
                <div>
                  <h3 className="card-title text-xl">基礎防禦</h3>
                  <p className="text-sm text-base-content/60">
                    目前後台固定使用同源檢查、cookie session、一次性 setup token 與安全標頭。
                  </p>
                </div>

                <div className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body grid gap-4 p-5 md:grid-cols-2">
                    <label className="form-control gap-2">
                      <span className="label-text">Login Max Attempts</span>
                      <input
                        type="number"
                        min={1}
                        className="input input-bordered w-full"
                        value={draftConfig.login_max_attempts}
                        onChange={(event) =>
                          setDraftConfig((current) => ({
                            ...current,
                            login_max_attempts: parsePositiveInt(
                              event.target.value,
                              current.login_max_attempts,
                            ),
                          }))
                        }
                      />
                    </label>
                    <label className="form-control gap-2">
                      <span className="label-text">Login Block (minutes)</span>
                      <input
                        type="number"
                        min={1}
                        className="input input-bordered w-full"
                        value={draftConfig.login_block_minutes}
                        onChange={(event) =>
                          setDraftConfig((current) => ({
                            ...current,
                            login_block_minutes: parsePositiveInt(
                              event.target.value,
                              current.login_block_minutes,
                            ),
                          }))
                        }
                      />
                    </label>
                  </div>
                </div>

                <ul className="list rounded-box border border-base-300 bg-base-100">
                  <li className="list-row">
                    <div>
                      <div className="font-medium">Same-origin 檢查</div>
                      <div className="text-sm text-base-content/65">
                        已登入時，寫入 API 需要 `Origin` 或 `Referer` 與目前 host 相符，或由 NekoClaw Web UI 自帶受信任 request header。
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="font-medium">Cookie Session</div>
                      <div className="text-sm text-base-content/65">
                        Session cookie 固定使用 HttpOnly + SameSite=Lax；在 HTTPS 上會自動加上 Secure。
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="font-medium">一次性 Setup Token</div>
                      <div className="text-sm text-base-content/65">
                        首次初始化需使用 console 輸出的 setup URL；setup 完成後 token 立即失效。
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="font-medium">Response Headers</div>
                      <div className="text-sm text-base-content/65">
                        已固定開啟 `nosniff`、`DENY`、`same-origin` 與 `Permissions-Policy`。
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="font-medium">Login Throttle</div>
                      <div className="text-sm text-base-content/65">
                        目前來源失敗 {securityStatus?.login_status.failed_attempts ?? 0} 次，剩餘{" "}
                        {securityStatus?.login_status.remaining_attempts ?? draftConfig.login_max_attempts} 次。
                      </div>
                    </div>
                  </li>
                </ul>
              </div>
            ) : null}
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
    </div>
  );
}
