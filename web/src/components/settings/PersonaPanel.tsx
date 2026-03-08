import { useEffect, useState } from "react";
import {
  clearPersona,
  getActivePersona,
  listPersonas,
  reloadPersonas,
  usePersona,
} from "@/api/client";
import type { PersonaInfo } from "@/api/types";
import { useAppStore } from "@/store/appStore";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction = "" | "reload" | "activate" | "clear";

function filterPersonas(personas: PersonaInfo[], query: string): PersonaInfo[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return personas;
  return personas.filter((persona) =>
    [
      persona.name,
      persona.dir_name,
      persona.id,
      persona.description,
    ].some((value) => value.toLowerCase().includes(needle)),
  );
}

function sortPersonas(personas: PersonaInfo[], activeDirName: string): PersonaInfo[] {
  return [...personas].sort((left, right) => {
    const leftActive = left.dir_name === activeDirName ? 1 : 0;
    const rightActive = right.dir_name === activeDirName ? 1 : 0;
    if (leftActive !== rightActive) return rightActive - leftActive;
    const byName = left.name.localeCompare(right.name, "zh-Hant", {
      sensitivity: "base",
    });
    if (byName !== 0) return byName;
    return left.dir_name.localeCompare(right.dir_name, "en", {
      sensitivity: "base",
    });
  });
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

export function PersonaPanel() {
  const setActivePersona = useAppStore((s) => s.setActivePersona);

  const [personas, setPersonas] = useState<PersonaInfo[]>([]);
  const [active, setActive] = useState<PersonaInfo | null>(null);
  const [selectedDirName, setSelectedDirName] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);

  async function syncPersonas(showLoading = false) {
    if (showLoading) setLoading(true);

    try {
      const [loadedPersonas, activePersona] = await Promise.all([
        listPersonas(),
        getActivePersona(),
      ]);
      const sorted = sortPersonas(loadedPersonas, activePersona?.dir_name ?? "");
      setPersonas(sorted);
      setActive(activePersona);
      setActivePersona(activePersona?.name ?? "");
      setSelectedDirName((current) => {
        if (current && sorted.some((persona) => persona.dir_name === current)) {
          return current;
        }
        if (
          activePersona &&
          sorted.some((persona) => persona.dir_name === activePersona.dir_name)
        ) {
          return activePersona.dir_name;
        }
        return sorted[0]?.dir_name ?? "";
      });
      return true;
    } catch {
      setStatus({
        tone: "error",
        message: "無法載入 Persona 清單",
      });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncPersonas(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  useEffect(() => {
    if (personas.length === 0) {
      if (selectedDirName !== "") setSelectedDirName("");
      return;
    }

    const visible = filterPersonas(personas, query);
    const candidatePool = visible.length > 0 ? visible : personas;
    if (candidatePool.some((persona) => persona.dir_name === selectedDirName)) {
      return;
    }
    if (
      active &&
      candidatePool.some((persona) => persona.dir_name === active.dir_name)
    ) {
      setSelectedDirName(active.dir_name);
      return;
    }
    if (candidatePool[0]?.dir_name) {
      setSelectedDirName(candidatePool[0].dir_name);
    }
  }, [active, personas, query, selectedDirName]);

  const filteredPersonas = filterPersonas(personas, query);
  const selectedPersona =
    filteredPersonas.find((persona) => persona.dir_name === selectedDirName) ??
    filteredPersonas[0] ??
    null;
  const selectedIsActive =
    !!selectedPersona && active?.dir_name === selectedPersona.dir_name;

  async function handleReload() {
    setBusyAction("reload");
    try {
      await reloadPersonas();
      const ok = await syncPersonas(false);
      if (ok) {
        setStatus({ tone: "success", message: "Persona 清單已重新整理" });
      }
    } catch {
      setStatus({ tone: "error", message: "重新整理 Persona 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleActivate() {
    if (!selectedPersona || selectedIsActive) return;
    setBusyAction("activate");
    try {
      await usePersona(selectedPersona.dir_name);
      const ok = await syncPersonas(false);
      if (ok) {
        setStatus({
          tone: "success",
          message: `已啟用 ${selectedPersona.name}`,
        });
      }
    } catch {
      setStatus({ tone: "error", message: "啟用 Persona 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleClear() {
    if (!active) return;
    setBusyAction("clear");
    try {
      await clearPersona();
      const ok = await syncPersonas(false);
      if (ok) {
        setStatus({ tone: "info", message: "已清除目前 Persona" });
      }
    } catch {
      setStatus({ tone: "error", message: "清除 Persona 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <div className="badge badge-outline badge-sm">Persona</div>
                {active ? (
                  <div className="badge badge-primary badge-sm">Active</div>
                ) : (
                  <div className="badge badge-ghost badge-sm">Inactive</div>
                )}
              </div>
              <div>
                <h2 className="card-title text-2xl">Persona 管理</h2>
                <p className="text-sm text-base-content/60">
                  使用現有 Persona API 切換不同角色設定，套用到後續對話。
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
                重新整理
              </button>
              <button
                className="btn btn-ghost btn-sm join-item"
                onClick={handleClear}
                disabled={!active || busyAction !== ""}
              >
                {busyAction === "clear" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                清除 Persona
              </button>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">目前啟用</div>
              <div className="stat-value text-lg">
                {active?.name ?? "未啟用"}
              </div>
              <div className="stat-desc">
                {active?.dir_name ?? "將使用預設 system prompt"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">Persona 數量</div>
              <div className="stat-value text-lg">{personas.length}</div>
              <div className="stat-desc">已載入的 persona 定義</div>
            </div>
            <div className="stat">
              <div className="stat-title">搜尋結果</div>
              <div className="stat-value text-lg">{filteredPersonas.length}</div>
              <div className="stat-desc">
                {query.trim() ? `查詢: ${query}` : "顯示全部"}
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="join w-full">
            <label className="input input-bordered join-item flex w-full items-center gap-2">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                className="size-4 shrink-0 opacity-60"
              >
                <circle cx="11" cy="11" r="7" />
                <path d="m20 20-3.5-3.5" />
              </svg>
              <input
                type="text"
                className="grow"
                placeholder="搜尋 persona 名稱、資料夾、ID 或描述"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
            <button
              className="btn  join-item"
              onClick={() => setQuery("")}
              disabled={!query}
            >
              清除
            </button>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]">
        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4 p-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">Persona 清單</h3>
              <div className="badge badge-ghost badge-sm">
                {filteredPersonas.length} / {personas.length}
              </div>
            </div>

            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 5 }).map((_, index) => (
                  <div
                    key={index}
                    className="card border border-base-300 bg-base-100 shadow-sm"
                  >
                    <div className="card-body gap-3 p-4">
                      <div className="skeleton h-5 w-28" />
                      <div className="skeleton h-4 w-40" />
                      <div className="skeleton h-4 w-full" />
                    </div>
                  </div>
                ))}
              </div>
            ) : filteredPersonas.length === 0 ? (
              <div className="card border border-dashed border-base-300 bg-base-100 shadow-sm">
                <div className="card-body">
                  <div className="alert alert-info">
                    <span>
                      {personas.length === 0
                        ? "目前沒有載入任何 Persona。"
                        : "找不到符合搜尋條件的 Persona。"}
                    </span>
                  </div>
                  <p className="text-sm text-base-content/60">
                    {personas.length === 0
                      ? "確認 personas 目錄內容後，可用上方按鈕重新整理。"
                      : "調整搜尋條件，或清除搜尋後重新查看完整清單。"}
                  </p>
                  <div className="card-actions justify-end">
                    {personas.length === 0 ? (
                      <button
                        className="btn btn-primary btn-sm"
                        onClick={handleReload}
                        disabled={busyAction !== ""}
                      >
                        重新整理
                      </button>
                    ) : (
                      <button
                        className="btn  btn-sm"
                        onClick={() => setQuery("")}
                      >
                        清除搜尋
                      </button>
                    )}
                  </div>
                </div>
              </div>
            ) : (
              <ul className="menu rounded-box border border-base-300 bg-base-100 p-2">
                {filteredPersonas.map((persona) => {
                  const isActive = active?.dir_name === persona.dir_name;
                  const isSelected = selectedDirName === persona.dir_name;
                  return (
                    <li key={persona.dir_name}>
                      <button
                        className={isSelected ? "active" : ""}
                        onClick={() => setSelectedDirName(persona.dir_name)}
                      >
                        <div className="min-w-0 flex-1 text-left">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="truncate font-semibold">
                              {persona.name}
                            </span>
                            {isActive && (
                              <span className="badge badge-primary badge-xs">
                                Active
                              </span>
                            )}
                          </div>
                          <div className="truncate text-xs font-mono text-base-content/45">
                            {persona.dir_name}
                          </div>
                          <div className="truncate text-xs text-base-content/60">
                            {persona.description || "沒有描述"}
                          </div>
                        </div>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>

        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">Persona 詳情</h3>
              {selectedPersona ? (
                <div className="badge badge-outline badge-sm">
                  {selectedIsActive ? "使用中" : "待啟用"}
                </div>
              ) : (
                <div className="badge badge-ghost badge-sm">未選取</div>
              )}
            </div>

            {!selectedPersona ? (
              <div className="alert alert-info">
                <span>選擇一個 Persona 來查看詳情與操作。</span>
              </div>
            ) : (
              <>
                <div className="space-y-3">
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <h4 className="text-xl font-semibold">
                        {selectedPersona.name}
                      </h4>
                      {selectedIsActive && (
                        <div className="badge badge-primary">Active</div>
                      )}
                    </div>
                    <p className="mt-2 text-sm text-base-content/70">
                      {selectedPersona.description || "此 Persona 沒有描述。"}
                    </p>
                  </div>

                  <div className="flex flex-wrap gap-2">
                    <div className="badge badge-ghost badge-sm font-mono">
                      {selectedPersona.dir_name}
                    </div>
                    <div className="badge badge-outline badge-sm font-mono">
                      {selectedPersona.id}
                    </div>
                  </div>
                </div>

                <ul className="list rounded-box border border-base-300 bg-base-100">
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Name
                      </div>
                      <div className="font-medium">{selectedPersona.name}</div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Directory
                      </div>
                      <div className="font-mono text-sm">
                        {selectedPersona.dir_name}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Identifier
                      </div>
                      <div className="font-mono text-sm">{selectedPersona.id}</div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Description
                      </div>
                      <div className="text-sm text-base-content/75">
                        {selectedPersona.description || "沒有描述"}
                      </div>
                    </div>
                  </li>
                </ul>

                <div className="card-actions justify-end">
                  <button
                    className="btn "
                    onClick={handleClear}
                    disabled={!active || busyAction !== ""}
                  >
                    {busyAction === "clear" && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    清除目前 Persona
                  </button>
                  <button
                    className="btn btn-primary"
                    onClick={handleActivate}
                    disabled={!selectedPersona || selectedIsActive || busyAction !== ""}
                  >
                    {busyAction === "activate" && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    {selectedIsActive ? "目前啟用中" : "啟用 Persona"}
                  </button>
                </div>
              </>
            )}
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
