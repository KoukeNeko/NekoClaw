import { useEffect, useState } from "react";
import { ApiError, setupSecurity } from "@/api/client";
import { useAppStore } from "@/store/appStore";

function readSetupTokenFromHash(): string {
  const raw = window.location.hash.split("?")[1] ?? "";
  const params = new URLSearchParams(raw);
  return params.get("token")?.trim() ?? "";
}

export function SetupPage() {
  const setSecurityStatus = useAppStore((s) => s.setSecurityStatus);
  const setRoute = useAppStore((s) => s.setRoute);

  const [token, setToken] = useState(() => readSetupTokenFromHash());
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    function handleHashChange() {
      setToken(readSetupTokenFromHash());
    }
    window.addEventListener("hashchange", handleHashChange);
    return () => window.removeEventListener("hashchange", handleHashChange);
  }, []);

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) {
      setError("缺少 setup token，請使用 console 輸出的完整 setup URL。");
      return;
    }
    if (password.length < 12) {
      setError("管理員密碼至少需要 12 個字元。");
      return;
    }
    if (password !== confirmPassword) {
      setError("兩次輸入的密碼不一致。");
      return;
    }

    setSubmitting(true);
    setError("");
    try {
      const status = await setupSecurity({ token, password });
      setSecurityStatus(status);
      setPassword("");
      setConfirmPassword("");
      setRoute("chat");
    } catch (caught) {
      if (caught instanceof ApiError) {
        setError(caught.message);
      } else {
        setError("初始化管理員密碼失敗");
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
                  <div className="badge badge-warning badge-sm">Setup Required</div>
                </div>
                <div>
                  <h1 className="text-3xl font-semibold">初始化管理員密碼</h1>
                  <p className="mt-2 text-sm text-base-content/65">
                    這是第一次啟動後台。完成 setup 後，server 會立即建立目前 session 並保護整個後台。
                  </p>
                </div>
              </div>

              <form className="space-y-5" onSubmit={handleSubmit}>
                <label className="fieldset">
                  <legend className="fieldset-legend">Setup Token</legend>
                  <input
                    type="text"
                    className="input input-bordered w-full font-mono text-sm outline-none focus:outline-none focus:ring-0 focus-visible:outline-none focus:shadow-none"
                    value={token}
                    readOnly
                    spellCheck={false}
                  />
                  <p className="label text-base-content/55">
                    此 token 來自 console 輸出的 setup URL，初始化完成後會立即失效。
                  </p>
                </label>

                <label className="fieldset">
                  <legend className="fieldset-legend">管理員密碼</legend>
                  <input
                    type="password"
                    className="input input-bordered w-full outline-none focus:outline-none focus:ring-0 focus-visible:outline-none focus:shadow-none"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    autoComplete="new-password"
                    placeholder="至少 12 個字元"
                  />
                  <p className="label text-base-content/55">
                    只會儲存 bcrypt hash，不會寫進 config.json。
                  </p>
                </label>

                <label className="fieldset">
                  <legend className="fieldset-legend">再次輸入密碼</legend>
                  <input
                    type="password"
                    className="input input-bordered w-full outline-none focus:outline-none focus:ring-0 focus-visible:outline-none focus:shadow-none"
                    value={confirmPassword}
                    onChange={(event) => setConfirmPassword(event.target.value)}
                    autoComplete="new-password"
                    placeholder="再次輸入密碼"
                  />
                  <p className="label text-base-content/55">再次確認，避免 setup 後立刻被鎖在外面。</p>
                </label>

                {error ? (
                  <div className="alert alert-error">
                    <span>{error}</span>
                  </div>
                ) : null}

                <div className="card-actions justify-end">
                  <button
                    type="submit"
                    className="btn btn-primary"
                    disabled={submitting}
                  >
                    {submitting ? (
                      <span className="loading loading-spinner loading-xs" />
                    ) : null}
                    完成初始化
                  </button>
                </div>
              </form>
            </div>
          </div>

          <div className="card border border-base-300 bg-base-100 shadow-xl">
            <div className="card-body gap-4">
              <h2 className="card-title text-xl">注意事項</h2>
              <ul className="list rounded-box border border-base-300 bg-base-200">
                <li className="list-row">
                  <div>
                    <div className="font-medium">Setup token 僅能使用一次</div>
                    <div className="text-sm text-base-content/65">
                      完成 setup 後 token 立即失效，重新啟動時會重新產生。
                    </div>
                  </div>
                </li>
                <li className="list-row">
                  <div>
                    <div className="font-medium">密碼不會寫進 config.json</div>
                    <div className="text-sm text-base-content/65">
                      後端只會在 auth 目錄中保存 bcrypt hash。
                    </div>
                  </div>
                </li>
                <li className="list-row">
                  <div>
                    <div className="font-medium">setup URL 來自 console</div>
                    <div className="text-sm text-base-content/65">
                      若 token 缺失，請回到啟動該服務的終端查看輸出的 setup URL。
                    </div>
                  </div>
                </li>
              </ul>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
