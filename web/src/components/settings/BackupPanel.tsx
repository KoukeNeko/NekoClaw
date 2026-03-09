import { useEffect, useRef, useState } from "react";
import {
  ApiError,
  createBackup,
  deleteBackup,
  downloadBackupArchive,
  importBackupArchive,
  listBackups,
  restoreBackup,
} from "@/api/client";
import type { BackupEntry } from "@/api/types";
import { SelectionList, SelectionListItem } from "@/components/settings/SelectionList";
import { formatDateTime, formatRelativeTime } from "@/utils/format";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type BusyAction =
  | ""
  | "reload"
  | "create"
  | "import"
  | "download"
  | "delete"
  | "restore";
type ImportPhase = "ready" | "running" | "success" | "error";
type ImportErrorState = {
  statusCode: number;
  code: string;
  message: string;
};
type ImportDialogState = {
  file: File;
  phase: ImportPhase;
  imported: BackupEntry | null;
  error: ImportErrorState | null;
};
type ImportStepStatus = "pending" | "active" | "done" | "error";
type ImportStep = {
  title: string;
  detail: string;
  status: ImportStepStatus;
};

function sortBackups(backups: BackupEntry[]): BackupEntry[] {
  return [...backups].sort((left, right) => {
    const leftTime = new Date(left.created_at).getTime();
    const rightTime = new Date(right.created_at).getTime();
    if (leftTime !== rightTime) return rightTime - leftTime;
    return right.file_name.localeCompare(left.file_name, "en", {
      sensitivity: "base",
    });
  });
}

function filterBackups(backups: BackupEntry[], query: string): BackupEntry[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return backups;
  return backups.filter((backup) =>
    [
      backup.file_name,
      backup.backup_id,
      backup.source,
      backup.restore_mode,
      ...backup.components.map((component) => component.label),
    ].some((value) => value.toLowerCase().includes(needle)),
  );
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

function sourceBadgeClass(source: BackupEntry["source"]): string {
  return source === "imported" ? "badge-secondary" : "badge-primary";
}

function sourceLabel(source: BackupEntry["source"]): string {
  return source === "imported" ? "匯入" : "建立";
}

function formatBytes(size: number): string {
  if (size <= 0) return "0 B";
  if (size >= 1024 * 1024 * 1024) return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  if (size >= 1024 * 1024) return `${(size / (1024 * 1024)).toFixed(1)} MB`;
  if (size >= 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${size} B`;
}

function toErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) {
    return error.message.trim();
  }
  return fallback;
}

function toImportErrorState(error: unknown, fallback: string): ImportErrorState {
  if (error instanceof ApiError) {
    return {
      statusCode: error.statusCode,
      code: error.code,
      message: error.message.trim() || fallback,
    };
  }
  if (error instanceof Error) {
    return {
      statusCode: 0,
      code: "",
      message: error.message.trim() || fallback,
    };
  }
  return {
    statusCode: 0,
    code: "",
    message: fallback,
  };
}

function importPhaseLabel(phase: ImportPhase): string {
  switch (phase) {
    case "ready":
      return "準備匯入";
    case "running":
      return "匯入中";
    case "success":
      return "匯入成功";
    case "error":
      return "匯入失敗";
  }
}

function importPhaseBadgeClass(phase: ImportPhase): string {
  switch (phase) {
    case "success":
      return "badge-success";
    case "error":
      return "badge-error";
    default:
      return "badge-warning";
  }
}

function importPhaseProgressClass(phase: ImportPhase): string {
  switch (phase) {
    case "success":
      return "progress-success";
    case "error":
      return "progress-error";
    default:
      return "progress-warning";
  }
}

function importPhaseDescription(phase: ImportPhase): string {
  switch (phase) {
    case "ready":
      return "已接收檔案，準備送出匯入請求。";
    case "running":
      return "正在上傳、驗證 archive，並寫入目前備份庫。";
    case "success":
      return "備份檔已通過驗證並寫入備份庫。";
    case "error":
      return "匯入在驗證或寫入階段失敗。";
  }
}

function importStageIndex(progress: number): number {
  if (progress >= 82) return 3;
  if (progress >= 56) return 2;
  if (progress >= 28) return 1;
  return 0;
}

function buildImportSteps(
  phase: ImportPhase,
  progress: number,
  hasImportedEntry: boolean,
): ImportStep[] {
  const steps = [
    {
      title: "建立匯入請求",
      detail: "鎖定目前視窗、準備把選取的 zip 送往 /v1/backups/import。",
    },
    {
      title: "上傳備份檔",
      detail: "將 archive 內容送到 server，等待 multipart upload 完成。",
    },
    {
      title: "驗證 archive",
      detail: "Server 會解壓、檢查 manifest.json，並確認 payload 結構可匯入。",
    },
    {
      title: "寫入備份庫",
      detail: "把驗證成功的內容寫進 backups library，然後回傳新的 backup entry。",
    },
  ];

  if (phase === "success" && hasImportedEntry) {
    return steps.map((step) => ({ ...step, status: "done" as const }));
  }

  const activeIndex = importStageIndex(progress);
  return steps.map((step, index) => {
    let status: ImportStepStatus = "pending";
    if (index < activeIndex) status = "done";
    if (index === activeIndex) status = phase === "error" ? "error" : "active";
    if (phase === "ready" && index === 0) status = "active";
    return { ...step, status };
  });
}

function importStepBadgeClass(status: ImportStepStatus): string {
  switch (status) {
    case "done":
      return "badge-success";
    case "active":
      return "badge-warning";
    case "error":
      return "badge-error";
    default:
      return "badge-ghost";
  }
}

function importStepLabel(status: ImportStepStatus): string {
  switch (status) {
    case "done":
      return "完成";
    case "active":
      return "進行中";
    case "error":
      return "中斷";
    default:
      return "待命";
  }
}

function fileMimeType(file: File): string {
  return file.type.trim() || "application/octet-stream";
}

export function BackupPanel() {
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const [backups, setBackups] = useState<BackupEntry[]>([]);
  const [selectedBackupID, setSelectedBackupID] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);
  const [restorePrompt, setRestorePrompt] = useState<BackupEntry | null>(null);
  const [restoreNotice, setRestoreNotice] = useState<string | null>(null);
  const [importDialog, setImportDialog] = useState<ImportDialogState | null>(null);
  const [importProgress, setImportProgress] = useState(0);

  async function syncBackups(showLoading = false, preferredID = "") {
    if (showLoading) setLoading(true);

    try {
      const loaded = sortBackups(await listBackups());
      setBackups(loaded);
      setSelectedBackupID((current) => {
        if (preferredID && loaded.some((backup) => backup.backup_id === preferredID)) {
          return preferredID;
        }
        if (current && loaded.some((backup) => backup.backup_id === current)) {
          return current;
        }
        return loaded[0]?.backup_id ?? "";
      });
      return true;
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "無法載入備份清單"),
      });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncBackups(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  useEffect(() => {
    if (!importDialog) {
      setImportProgress(0);
      return undefined;
    }

    if (importDialog.phase === "ready") {
      setImportProgress(12);
      return undefined;
    }

    if (importDialog.phase === "running") {
      setImportProgress((current) => Math.max(current, 18));
      const timer = window.setInterval(() => {
        setImportProgress((current) => {
          if (current >= 92) return current;
          if (current < 48) return Math.min(92, current + 8);
          if (current < 76) return Math.min(92, current + 4);
          return Math.min(92, current + 2);
        });
      }, 240);
      return () => window.clearInterval(timer);
    }

    setImportProgress(100);
    return undefined;
  }, [importDialog]);

  useEffect(() => {
    if (!importDialog || importDialog.phase !== "ready") return undefined;

    let cancelled = false;
    void (async () => {
      setBusyAction("import");
      setImportDialog((current) =>
        current && current.phase === "ready"
          ? { ...current, phase: "running" }
          : current,
      );
      try {
        const imported = await importBackupArchive(importDialog.file);
        if (cancelled) return;
        setQuery("");
        setBackups((current) =>
          sortBackups([
            imported,
            ...current.filter((backup) => backup.backup_id !== imported.backup_id),
          ]),
        );
        setSelectedBackupID(imported.backup_id);
        setImportDialog((current) =>
          current
            ? {
                ...current,
                phase: "success",
                imported,
                error: null,
              }
            : current,
        );
      } catch (error) {
        if (cancelled) return;
        setImportDialog((current) =>
          current
            ? {
                ...current,
                phase: "error",
                imported: null,
                error: toImportErrorState(error, "匯入備份檔失敗"),
              }
            : current,
        );
      } finally {
        if (!cancelled) {
          setBusyAction("");
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [importDialog]);

  useEffect(() => {
    if (backups.length === 0) {
      if (selectedBackupID !== "") setSelectedBackupID("");
      return;
    }

    const visible = filterBackups(backups, query);
    const candidatePool = visible.length > 0 ? visible : backups;
    if (candidatePool.some((backup) => backup.backup_id === selectedBackupID)) {
      return;
    }
    if (candidatePool[0]?.backup_id) {
      setSelectedBackupID(candidatePool[0].backup_id);
    }
  }, [backups, query, selectedBackupID]);

  const filteredBackups = filterBackups(backups, query);
  const selectedBackup =
    filteredBackups.find((backup) => backup.backup_id === selectedBackupID) ??
    filteredBackups[0] ??
    null;
  const latestBackup = backups[0] ?? null;
  const importSteps = importDialog
    ? buildImportSteps(importDialog.phase, importProgress, !!importDialog.imported)
    : [];
  const activeImportStep =
    importSteps.find((step) => step.status === "active" || step.status === "error") ??
    null;

  async function handleReload() {
    setBusyAction("reload");
    try {
      const ok = await syncBackups(false, selectedBackupID);
      if (ok) {
        setStatus({ tone: "info", message: "備份清單已重新同步" });
      }
    } finally {
      setBusyAction("");
    }
  }

  async function handleCreate() {
    setBusyAction("create");
    try {
      const created = await createBackup();
      const ok = await syncBackups(false, created.backup_id);
      if (ok) {
        setStatus({ tone: "success", message: "已建立新備份" });
      }
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "建立備份失敗"),
      });
    } finally {
      setBusyAction("");
    }
  }

  function handleImport(file: File | null) {
    if (!file) return;
    setImportDialog({
      file,
      phase: "ready",
      imported: null,
      error: null,
    });
  }

  function closeImportDialog() {
    if (busyAction === "import") return;
    setImportDialog(null);
  }

  async function handleDownload() {
    if (!selectedBackup) return;
    setBusyAction("download");
    try {
      await downloadBackupArchive(selectedBackup.backup_id);
      setStatus({
        tone: "info",
        message: `已開始下載 ${selectedBackup.file_name}`,
      });
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "下載備份失敗"),
      });
    } finally {
      setBusyAction("");
    }
  }

  async function handleDelete() {
    if (!selectedBackup) return;
    setBusyAction("delete");
    try {
      const deletedName = selectedBackup.file_name;
      await deleteBackup(selectedBackup.backup_id);
      const ok = await syncBackups(false);
      if (ok) {
        setStatus({
          tone: "info",
          message: `已刪除 ${deletedName}`,
        });
      }
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "刪除備份失敗"),
      });
    } finally {
      setBusyAction("");
    }
  }

  async function confirmRestore() {
    if (!restorePrompt) return;
    setBusyAction("restore");
    try {
      const result = await restoreBackup(restorePrompt.backup_id);
      const ok = await syncBackups(false, restorePrompt.backup_id);
      if (ok) {
        setStatus({ tone: "success", message: `已套用 ${restorePrompt.file_name}` });
      }
      if (result.restart_required) {
        setRestoreNotice(
          `備份 ${restorePrompt.file_name} 已寫入磁碟。請手動重啟 NekoClaw，讓 config、auth、memory、sessions 與 bot bindings 全部切換到這份備份。`,
        );
      }
      setRestorePrompt(null);
    } catch (error) {
      setStatus({
        tone: "error",
        message: toErrorMessage(error, "還原備份失敗"),
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
            <div className="flex flex-col gap-3">
              <div className="flex gap-2">
                <div className="skeleton h-5 w-16" />
                <div className="skeleton h-5 w-24" />
              </div>
              <div className="skeleton h-8 w-48" />
              <div className="skeleton h-4 w-80 max-w-full" />
            </div>
            <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
              {Array.from({ length: 4 }).map((_, index) => (
                <div key={index} className="stat">
                  <div className="skeleton h-4 w-24" />
                  <div className="mt-3 skeleton h-8 w-28" />
                  <div className="mt-3 skeleton h-4 w-36" />
                </div>
              ))}
            </div>
          </div>
        </div>
        <div className="grid gap-6 lg:grid-cols-[minmax(320px,0.78fr)_minmax(420px,1.22fr)]">
          {Array.from({ length: 2 }).map((_, index) => (
            <div
              key={index}
              className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4">
                {Array.from({ length: 6 }).map((_, itemIndex) => (
                  <div key={itemIndex} className="skeleton h-10 w-full" />
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <input
        ref={fileInputRef}
        type="file"
        className="hidden"
        accept=".zip,application/zip"
        onChange={(event) => {
          const file = event.target.files?.[0] ?? null;
          event.currentTarget.value = "";
          handleImport(file);
        }}
      />

      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <div className="badge badge-outline badge-sm">Backup</div>
                <div className="badge badge-warning badge-sm">含機密資料</div>
                <div className="badge badge-ghost badge-sm">整包還原</div>
              </div>
              <div>
                <h2 className="card-title text-2xl">備份 / 還原</h2>
                <p className="text-sm text-base-content/60">
                  使用與 Persona 頁面一致的管理版型，集中建立、匯入、下載、刪除與還原完整狀態備份。
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
                className="btn btn-primary btn-sm join-item"
                onClick={handleCreate}
                disabled={busyAction !== ""}
              >
                {busyAction === "create" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                立即建立備份
              </button>
              <button
                className="btn btn-sm join-item"
                onClick={() => fileInputRef.current?.click()}
                disabled={busyAction !== ""}
              >
                {busyAction === "import" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                匯入備份檔
              </button>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">備份數量</div>
              <div className="stat-value text-lg">{backups.length}</div>
              <div className="stat-desc">目前備份庫內的 archive</div>
            </div>
            <div className="stat">
              <div className="stat-title">最近建立</div>
              <div className="stat-value text-lg">
                {latestBackup ? formatDateTime(latestBackup.created_at) : "尚無"}
              </div>
              <div className="stat-desc">
                {latestBackup ? formatRelativeTime(latestBackup.created_at) : "先建立第一份備份"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">備份範圍</div>
              <div className="stat-value text-lg">完整狀態</div>
              <div className="stat-desc">config / auth / memory / sessions / MCP / personas / bindings</div>
            </div>
            <div className="stat">
              <div className="stat-title">還原模式</div>
              <div className="stat-value text-lg">需重啟</div>
              <div className="stat-desc">寫入後不熱套用，避免半套 runtime 狀態</div>
            </div>
          </div>
        </div>
      </div>

      {restoreNotice ? (
        <div className="alert alert-warning">
          <span>{restoreNotice}</span>
        </div>
      ) : null}

      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="join w-full">
            <label className="input input-bordered join-item flex w-full items-center gap-2 focus-within:outline-none focus-within:ring-0 focus-within:shadow-none">
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
                className="grow outline-none focus:outline-none focus:ring-0 focus-visible:outline-none"
                placeholder="搜尋檔名、備份 ID、來源或 component"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
              />
            </label>
            <button
              className="btn join-item"
              onClick={() => setQuery("")}
              disabled={!query}
            >
              清除
            </button>
          </div>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(320px,0.78fr)_minmax(420px,1.22fr)]">
        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4 p-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">備份清單</h3>
              <div className="badge badge-ghost badge-sm">
                {filteredBackups.length} / {backups.length}
              </div>
            </div>

            {filteredBackups.length === 0 ? (
              <div className="card border border-dashed border-base-300 bg-base-100 shadow-sm">
                <div className="card-body">
                  <div className="alert alert-info">
                    <span>
                      {backups.length === 0
                        ? "目前還沒有任何備份。"
                        : "找不到符合搜尋條件的備份。"}
                    </span>
                  </div>
                  <p className="text-sm text-base-content/60">
                    {backups.length === 0
                      ? "可以先建立第一份完整備份，或匯入既有 archive。"
                      : "調整搜尋條件，或清除搜尋後重新查看完整清單。"}
                  </p>
                  <div className="card-actions justify-end">
                    {backups.length === 0 ? (
                      <>
                        <button
                          className="btn btn-primary btn-sm"
                          onClick={handleCreate}
                          disabled={busyAction !== ""}
                        >
                          建立備份
                        </button>
                        <button
                          className="btn btn-sm"
                          onClick={() => fileInputRef.current?.click()}
                          disabled={busyAction !== ""}
                        >
                          匯入備份檔
                        </button>
                      </>
                    ) : (
                      <button className="btn btn-sm" onClick={() => setQuery("")}>
                        清除搜尋
                      </button>
                    )}
                  </div>
                </div>
              </div>
            ) : (
              <SelectionList>
                {filteredBackups.map((backup) => {
                  const isSelected = selectedBackup?.backup_id === backup.backup_id;
                  return (
                    <SelectionListItem
                      key={backup.backup_id}
                      selected={isSelected}
                      onClick={() => setSelectedBackupID(backup.backup_id)}
                    >
                        <div className="min-w-0 flex-1 text-left">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="truncate font-semibold">
                              {backup.file_name}
                            </span>
                            <span className={`badge badge-xs ${sourceBadgeClass(backup.source)}`}>
                              {sourceLabel(backup.source)}
                            </span>
                          </div>
                          <div className="truncate text-xs font-mono text-base-content/45">
                            {backup.backup_id}
                          </div>
                          <div className="flex flex-wrap items-center gap-2 text-xs text-base-content/60">
                            <span>{formatRelativeTime(backup.created_at)}</span>
                            <span>{formatBytes(backup.size_bytes)}</span>
                            <span>{backup.components.length} 個 component</span>
                          </div>
                        </div>
                    </SelectionListItem>
                  );
                })}
              </SelectionList>
            )}
          </div>
        </div>

        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">備份詳情</h3>
              {selectedBackup ? (
                <div className={`badge badge-sm ${sourceBadgeClass(selectedBackup.source)}`}>
                  {sourceLabel(selectedBackup.source)}
                </div>
              ) : (
                <div className="badge badge-ghost badge-sm">未選取</div>
              )}
            </div>

            {!selectedBackup ? (
              <div className="alert alert-info">
                <span>從左側選一份備份來查看詳情與操作。</span>
              </div>
            ) : (
              <>
                <div className="space-y-3">
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <h4 className="text-xl font-semibold">
                        {selectedBackup.file_name}
                      </h4>
                      <div className="badge badge-warning">含機密資料</div>
                    </div>
                    <p className="mt-2 text-sm text-base-content/70">
                      這份備份會覆蓋目前磁碟上的主要持久化狀態，但不會覆蓋目前 Web 後台密碼；還原完成後必須手動重啟服務。
                    </p>
                  </div>

                  <div className="flex flex-wrap gap-2">
                    <div className="badge badge-outline badge-sm font-mono">
                      {selectedBackup.backup_id}
                    </div>
                    <div className="badge badge-ghost badge-sm">
                      {selectedBackup.restore_mode}
                    </div>
                  </div>
                </div>

                <div className="alert alert-warning">
                  <span>
                    還原會替換 config、auth、sessions、memory、MCP、personas 與 bot bindings，但會保留目前後台密碼。
                  </span>
                </div>

                <div className="flex flex-wrap gap-2">
                  {selectedBackup.components.map((component) => (
                    <div key={component.key} className="badge badge-outline gap-1 px-3 py-3">
                      <span>{component.label}</span>
                      <span className="font-mono text-[11px] opacity-70">
                        {component.item_count}
                      </span>
                    </div>
                  ))}
                </div>

                <ul className="list rounded-box border border-base-300 bg-base-100">
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        File
                      </div>
                      <div className="font-medium">{selectedBackup.file_name}</div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Created
                      </div>
                      <div className="text-sm">
                        {formatDateTime(selectedBackup.created_at)} ({formatRelativeTime(selectedBackup.created_at)})
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Source
                      </div>
                      <div className="text-sm">{sourceLabel(selectedBackup.source)}</div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Size
                      </div>
                      <div className="font-mono text-sm">
                        {formatBytes(selectedBackup.size_bytes)}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Restart Required
                      </div>
                      <div className="text-sm">
                        {selectedBackup.restart_required ? "Yes" : "No"}
                      </div>
                    </div>
                  </li>
                </ul>

                <div className="card-actions justify-end">
                  <button
                    className="btn"
                    onClick={handleDownload}
                    disabled={busyAction !== ""}
                  >
                    {busyAction === "download" && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    下載備份
                  </button>
                  <button
                    className="btn"
                    onClick={handleDelete}
                    disabled={busyAction !== ""}
                  >
                    {busyAction === "delete" && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    刪除備份
                  </button>
                  <button
                    className="btn btn-primary"
                    onClick={() => setRestorePrompt(selectedBackup)}
                    disabled={busyAction !== ""}
                  >
                    還原這份備份
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {restorePrompt ? (
        <dialog className="modal modal-open">
          <div className="modal-box max-w-lg">
            <h3 className="text-lg font-semibold">確認還原備份</h3>
            <div className="mt-3 space-y-3 text-sm text-base-content/75">
              <p>
                你即將還原 <span className="font-medium">{restorePrompt.file_name}</span>。
              </p>
              <p>這會覆蓋目前的持久化資料，包含 auth、memory、sessions、MCP 與 bot bindings。</p>
              <p>目前 Web 後台密碼會保留，不會被這份備份覆蓋回舊值。</p>
              <p>還原完成後不會熱套用；你需要手動重啟 NekoClaw。</p>
            </div>
            <div className="modal-action">
              <button
                className="btn"
                onClick={() => setRestorePrompt(null)}
                disabled={busyAction === "restore"}
              >
                取消
              </button>
              <button
                className="btn btn-primary"
                onClick={() => void confirmRestore()}
                disabled={busyAction === "restore"}
              >
                {busyAction === "restore" && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                確認還原
              </button>
            </div>
          </div>
          <form method="dialog" className="modal-backdrop">
            <button onClick={() => setRestorePrompt(null)}>close</button>
          </form>
        </dialog>
      ) : null}

      {importDialog ? (
        <dialog className="modal modal-open">
          <div className="modal-box max-w-2xl">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h3 className="text-lg font-semibold">匯入備份檔</h3>
                <p className="mt-1 text-sm text-base-content/65">
                  上傳完成後會先驗證 archive，再寫入目前備份庫。
                </p>
              </div>
              <div className={`badge badge-sm ${importPhaseBadgeClass(importDialog.phase)}`}>
                {importPhaseLabel(importDialog.phase)}
              </div>
            </div>

            <div className="mt-5 space-y-4">
              <div className="card border border-base-300 bg-base-100 shadow-sm">
                <div className="card-body gap-3">
                  <h4 className="text-sm font-semibold uppercase tracking-wide text-base-content/55">
                    檔案資訊
                  </h4>
                  <ul className="list rounded-box border border-base-300 bg-base-100">
                    <li className="list-row">
                      <div>
                        <div className="text-xs uppercase tracking-wide text-base-content/50">
                          File
                        </div>
                        <div className="font-medium">{importDialog.file.name}</div>
                      </div>
                    </li>
                    <li className="list-row">
                      <div>
                        <div className="text-xs uppercase tracking-wide text-base-content/50">
                          Size
                        </div>
                        <div className="font-mono text-sm">
                          {formatBytes(importDialog.file.size)}
                        </div>
                      </div>
                    </li>
                    <li className="list-row">
                      <div>
                        <div className="text-xs uppercase tracking-wide text-base-content/50">
                          MIME Type
                        </div>
                        <div className="font-mono text-sm">
                          {fileMimeType(importDialog.file)}
                        </div>
                      </div>
                    </li>
                  </ul>
                </div>
              </div>

              <div className="card border border-base-300 bg-base-100 shadow-sm">
                <div className="card-body gap-3">
                  <h4 className="text-sm font-semibold uppercase tracking-wide text-base-content/55">
                    目前狀態
                  </h4>
                  <div className="space-y-2">
                    <div className="flex items-center justify-between gap-3 text-xs text-base-content/60">
                      <span>
                        {activeImportStep?.title
                          ? `目前步驟：${activeImportStep.title}`
                          : importPhaseDescription(importDialog.phase)}
                      </span>
                      <span className="font-mono">{importProgress}%</span>
                    </div>
                    <progress
                      className={`progress w-full ${importPhaseProgressClass(importDialog.phase)}`}
                      value={importProgress}
                      max="100"
                    />
                  </div>
                  <div className="text-xs text-base-content/60">
                    {activeImportStep?.detail ?? importPhaseDescription(importDialog.phase)}
                  </div>
                  {importDialog.phase === "running" || importDialog.phase === "ready" ? (
                    <div className="alert alert-warning">
                      <span className="loading loading-spinner loading-sm" />
                      <span>正在上傳與驗證備份檔，請勿關閉此視窗。</span>
                    </div>
                  ) : null}
                  {importDialog.phase === "success" ? (
                    <div className="alert alert-success">
                      <span>備份檔已通過驗證並寫入備份庫，左側清單已切到這份新匯入的 archive。</span>
                    </div>
                  ) : null}
                  {importDialog.phase === "error" ? (
                    <div className="alert alert-error">
                      <span>匯入未完成，備份庫沒有套用這次上傳結果。</span>
                    </div>
                  ) : null}

                  <div className="card border border-base-300 bg-base-200/60 shadow-sm">
                    <div className="card-body gap-3 p-4">
                      <div className="text-sm font-semibold">目前在做什麼</div>
                      <ul className="space-y-3">
                        {importSteps.map((step, index) => (
                          <li
                            key={step.title}
                            className="flex items-start gap-3 rounded-box border border-base-300 bg-base-100 px-3 py-3"
                          >
                            <div className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border border-base-300 text-xs font-semibold text-base-content/70">
                              {index + 1}
                            </div>
                            <div className="min-w-0 flex-1 space-y-1">
                              <div className="flex flex-wrap items-center gap-2">
                                <div className="font-medium">{step.title}</div>
                                <div className={`badge badge-xs ${importStepBadgeClass(step.status)}`}>
                                  {importStepLabel(step.status)}
                                </div>
                              </div>
                              <div className="text-sm text-base-content/65">{step.detail}</div>
                            </div>
                          </li>
                        ))}
                      </ul>
                    </div>
                  </div>
                </div>
              </div>

              {importDialog.imported ? (
                <div className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body gap-3">
                    <h4 className="text-sm font-semibold uppercase tracking-wide text-base-content/55">
                      成功結果
                    </h4>
                    <ul className="list rounded-box border border-base-300 bg-base-100">
                      <li className="list-row">
                        <div>
                          <div className="text-xs uppercase tracking-wide text-base-content/50">
                            File
                          </div>
                          <div className="font-medium">{importDialog.imported.file_name}</div>
                        </div>
                      </li>
                      <li className="list-row">
                        <div>
                          <div className="text-xs uppercase tracking-wide text-base-content/50">
                            Backup ID
                          </div>
                          <div className="font-mono text-sm">{importDialog.imported.backup_id}</div>
                        </div>
                      </li>
                      <li className="list-row">
                        <div>
                          <div className="text-xs uppercase tracking-wide text-base-content/50">
                            Source
                          </div>
                          <div className="text-sm">{sourceLabel(importDialog.imported.source)}</div>
                        </div>
                      </li>
                      <li className="list-row">
                        <div>
                          <div className="text-xs uppercase tracking-wide text-base-content/50">
                            Created
                          </div>
                          <div className="text-sm">
                            {formatDateTime(importDialog.imported.created_at)}
                          </div>
                        </div>
                      </li>
                      <li className="list-row">
                        <div>
                          <div className="text-xs uppercase tracking-wide text-base-content/50">
                            Size
                          </div>
                          <div className="font-mono text-sm">
                            {formatBytes(importDialog.imported.size_bytes)}
                          </div>
                        </div>
                      </li>
                    </ul>

                    <div className="flex flex-wrap gap-2">
                      {importDialog.imported.components.map((component) => (
                        <div key={component.key} className="badge badge-outline gap-1 px-3 py-3">
                          <span>{component.label}</span>
                          <span className="font-mono text-[11px] opacity-70">
                            {component.item_count}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              ) : null}

              {importDialog.error ? (
                <div className="card border border-error/40 bg-base-100 shadow-sm">
                  <div className="card-body gap-3">
                    <h4 className="text-sm font-semibold uppercase tracking-wide text-base-content/55">
                      失敗細節
                    </h4>
                    <ul className="list rounded-box border border-base-300 bg-base-100">
                      <li className="list-row">
                        <div>
                          <div className="text-xs uppercase tracking-wide text-base-content/50">
                            HTTP Status
                          </div>
                          <div className="font-mono text-sm">
                            {importDialog.error.statusCode || "n/a"}
                          </div>
                        </div>
                      </li>
                      <li className="list-row">
                        <div>
                          <div className="text-xs uppercase tracking-wide text-base-content/50">
                            Error Code
                          </div>
                          <div className="font-mono text-sm">
                            {importDialog.error.code || "n/a"}
                          </div>
                        </div>
                      </li>
                      <li className="list-row">
                        <div>
                          <div className="text-xs uppercase tracking-wide text-base-content/50">
                            Message
                          </div>
                          <div className="text-sm">{importDialog.error.message}</div>
                        </div>
                      </li>
                    </ul>

                    {importDialog.error.statusCode === 403 &&
                    importDialog.error.code === "origin_mismatch" ? (
                      <div className="alert alert-warning">
                        <span>Browser security 的 same-origin 檢查擋下了這次 request。</span>
                      </div>
                    ) : null}
                  </div>
                </div>
              ) : null}
            </div>

            <div className="modal-action">
              <button
                className="btn"
                onClick={closeImportDialog}
                disabled={busyAction === "import"}
              >
                {importDialog.phase === "success" || importDialog.phase === "error"
                  ? "關閉"
                  : "處理中"}
              </button>
            </div>
          </div>
          <div className="modal-backdrop bg-black/40" />
        </dialog>
      ) : null}

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
