import { useMemo, useState } from "react";
import { ApiError, loginSecurity } from "@/api/client";
import { useAppStore } from "@/store/appStore";

function formatBlockedUntil(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return "";
  return date.toLocaleString("zh-TW", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function LoginPage() {
  const securityStatus = useAppStore((s) => s.securityStatus);
  const setSecurityStatus = useAppStore((s) => s.setSecurityStatus);
  const setRoute = useAppStore((s) => s.setRoute);

  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const blockedUntilLabel = useMemo(
    () => formatBlockedUntil(securityStatus?.login_status.blocked_until),
    [securityStatus?.login_status.blocked_until],
  );

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!password.trim()) {
      setError("請輸入管理員密碼");
      return;
    }

    setSubmitting(true);
    setError("");
    try {
      const status = await loginSecurity({ password });
      setSecurityStatus(status);
      setPassword("");
      setRoute("chat");
    } catch (caught) {
      if (caught instanceof ApiError) {
        setError(caught.message);
      } else {
        setError("登入失敗");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen bg-base-200 p-6">
      <div className="mx-auto flex min-h-screen max-w-5xl items-center justify-center">
        <div className="grid w-full gap-6 lg:grid-cols-[minmax(0,0.95fr)_minmax(320px,1.05fr)]">
          <div className="card border border-base-300 bg-base-100 shadow-xl">
            <div className="card-body gap-6">
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <div className="badge badge-outline badge-sm">Security</div>
                  <div className="badge badge-primary badge-sm">Protected</div>
                </div>
                <div>
                  <h1 className="text-3xl font-semibold">管理員登入</h1>
                  <p className="mt-2 text-sm text-base-content/65">
                    後台已啟用單一管理員保護。登入後才能使用聊天、設定與敏感 API。
                  </p>
                </div>
              </div>

              <form className="space-y-5" onSubmit={handleSubmit}>
                <label className="fieldset">
                  <legend className="fieldset-legend">管理員密碼</legend>
                  <input
                    type="password"
                    className="input input-bordered w-full outline-none focus:outline-none focus:ring-0 focus-visible:outline-none focus:shadow-none"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    autoComplete="current-password"
                    placeholder="請輸入密碼"
                  />
                  <p className="label text-base-content/55">
                    登入後才能使用聊天、設定與受保護的管理 API。
                  </p>
                </label>

                {error ? (
                  <div className="alert alert-error">
                    <span>{error}</span>
                  </div>
                ) : null}

                <div className="pt-1">
                  <button
                    type="submit"
                    className="btn btn-primary w-full sm:w-auto sm:min-w-28"
                    disabled={submitting}
                  >
                    {submitting ? (
                      <span className="loading loading-spinner loading-xs" />
                    ) : null}
                    登入
                  </button>
                </div>
              </form>
            </div>
          </div>

          <div className="card border border-base-300 bg-base-100 shadow-xl">
            <div className="card-body gap-4">
              <h2 className="card-title text-xl">登入狀態</h2>

              <div className="stats stats-vertical border border-base-300 bg-base-200 shadow-sm">
                <div className="stat">
                  <div className="stat-title">剩餘嘗試</div>
                  <div className="stat-value text-lg">
                    {securityStatus?.login_status.remaining_attempts ?? "-"}
                  </div>
                  <div className="stat-desc">失敗次數過多會暫時封鎖登入</div>
                </div>
                <div className="stat">
                  <div className="stat-title">目前失敗次數</div>
                  <div className="stat-value text-lg">
                    {securityStatus?.login_status.failed_attempts ?? 0}
                  </div>
                  <div className="stat-desc">以目前來源位址計算</div>
                </div>
                <div className="stat">
                  <div className="stat-title">封鎖到期</div>
                  <div className="stat-value text-lg">
                    {blockedUntilLabel || "未封鎖"}
                  </div>
                  <div className="stat-desc">
                    {blockedUntilLabel
                      ? "到期後可再次嘗試登入"
                      : "尚未進入 rate limit"}
                  </div>
                </div>
              </div>

              <div className="alert alert-info">
                <span>若這是第一次啟動，請使用 console 輸出的 setup URL 完成初始化。</span>
              </div>

              <div className="rounded-box border border-warning/35 bg-warning/12 p-4 text-base-content shadow-sm">
                <div className="space-y-2 text-sm leading-relaxed">
                  <div className="font-semibold">忘記密碼時如何重設</div>
                  <div>
                    到服務主機刪除 <span className="font-mono">security-state.json</span> 後重啟{" "}
                    <span className="font-mono">web</span> mode，server 會重新輸出 setup URL。
                  </div>
                  <div className="text-base-content/80">
                    預設路徑：
                    <span className="font-mono"> ~/.nekoclaw/auth/security-state.json</span>。
                    若有設定 <span className="font-mono">--auth-dir</span> 或{" "}
                    <span className="font-mono">NEKOCLAW_AUTH_DIR</span>，請改刪除該目錄下的{" "}
                    <span className="font-mono">security-state.json</span>。
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
