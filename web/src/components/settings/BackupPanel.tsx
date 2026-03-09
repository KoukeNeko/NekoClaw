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
import {
  SelectionList,
  SelectionListItem,
} from "@/components/settings/SelectionList";
import { formatDateTime, formatRelativeTime } from "@/utils/format";

const MIN_BACKUP_PASSWORD_LENGTH = 12;
const GENERATED_PASSWORD_LENGTH = 24;

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
type ImportStepStatus = "pending" | "active" | "done" | "error";
type ImportStep = {
  title: string;
  detail: string;
  status: ImportStepStatus;
};
type CreateDialogState = {
  mode: "manual" | "generated";
  password: string;
  confirmPassword: string;
  generatedPassword: string;
  copiedGeneratedPassword: boolean;
  acknowledgedGeneratedPassword: boolean;
  error: string | null;
};
type ImportPasswordDialogState = {
  file: File;
  password: string;
  error: string | null;
};
type ImportDialogState = {
  file: File;
  password: string;
  phase: ImportPhase;
  imported: BackupEntry | null;
  error: ImportErrorState | null;
};
type RestoreDialogState = {
  backup: BackupEntry;
  password: string;
  error: string | null;
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
      backup.encryption,
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

function encryptionBadgeClass(_encryption: BackupEntry["encryption"]): string {
  return "badge-success";
}

function encryptionLabel(_encryption: BackupEntry["encryption"]): string {
  return "已加密";
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
      return "已接收檔案與密碼，準備送出匯入請求。";
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
      detail: "鎖定目前視窗並組裝 multipart request，附帶 archive 與備份密碼。",
    },
    {
      title: "上傳備份檔",
      detail: "把 zip 與密碼送到 /v1/backups/import，等待 upload 完成。",
    },
    {
      title: "驗證 archive",
      detail: "Server 會讀取 manifest.json，並用提供的密碼解密驗證 payload。",
    },
    {
      title: "寫入備份庫",
      detail: "驗證成功後重封裝成備份庫條目，然後回傳新的 backup entry。",
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

function validatePassword(password: string): string | null {
  if (!password) return "備份密碼不能為空";
  if (password.length < MIN_BACKUP_PASSWORD_LENGTH) {
    return `備份密碼至少需要 ${MIN_BACKUP_PASSWORD_LENGTH} 個字元`;
  }
  return null;
}

function generateBackupPassword(): string {
  const alphabet =
    "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_";
  const random = new Uint8Array(GENERATED_PASSWORD_LENGTH);
  window.crypto.getRandomValues(random);
  return Array.from(random, (value) => alphabet[value % alphabet.length]).join(
    "",
  );
}

function importErrorHint(error: ImportErrorState | null): string | null {
  if (!error) return null;
  if (error.statusCode === 403 && error.code === "origin_mismatch") {
    return "browser security same-origin 檢查擋下 request，請確認目前請求是從 NekoClaw Web UI 發出。";
  }
  if (error.code === "invalid_backup_password") {
    return "提供的備份密碼無法解密 payload，請確認這是建立該備份時使用的密碼。";
  }
  return null;
}

function syncDialog(dialog: HTMLDialogElement | null, open: boolean) {
  if (!dialog) return;
  if (open) {
    if (!dialog.open) dialog.showModal();
    return;
  }
  if (dialog.open) dialog.close();
}

function newCreateDialogState(): CreateDialogState {
  return {
    mode: "manual",
    password: "",
    confirmPassword: "",
    generatedPassword: generateBackupPassword(),
    copiedGeneratedPassword: false,
    acknowledgedGeneratedPassword: false,
    error: null,
  };
}

export function BackupPanel() {
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const createDialogRef = useRef<HTMLDialogElement | null>(null);
  const importPasswordDialogRef = useRef<HTMLDialogElement | null>(null);
  const importStatusDialogRef = useRef<HTMLDialogElement | null>(null);
  const restoreDialogRef = useRef<HTMLDialogElement | null>(null);

  const [backups, setBackups] = useState<BackupEntry[]>([]);
  const [selectedBackupID, setSelectedBackupID] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState<BusyAction>("");
  const [status, setStatus] = useState<StatusState>(null);
  const [restoreNotice, setRestoreNotice] = useState<string | null>(null);
  const [createDialog, setCreateDialog] = useState<CreateDialogState | null>(
    null,
  );
  const [importPasswordDialog, setImportPasswordDialog] =
    useState<ImportPasswordDialogState | null>(null);
  const [importDialog, setImportDialog] = useState<ImportDialogState | null>(
    null,
  );
  const [restoreDialog, setRestoreDialog] = useState<RestoreDialogState | null>(
    null,
  );
  const [importProgress, setImportProgress] = useState(0);

  async function syncBackups(showLoading = false, preferredID = "") {
    if (showLoading) setLoading(true);

    try {
      const loaded = sortBackups(await listBackups());
      setBackups(loaded);
      setSelectedBackupID((current) => {
        if (
          preferredID &&
          loaded.some((backup) => backup.backup_id === preferredID)
        ) {
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
    const timer = window.setTimeout(() => setStatus(null), 2400);
    return () => window.clearTimeout(timer);
  }, [status]);

  useEffect(() => {
    syncDialog(createDialogRef.current, createDialog !== null);
  }, [createDialog]);

  useEffect(() => {
    syncDialog(importPasswordDialogRef.current, importPasswordDialog !== null);
  }, [importPasswordDialog]);

  useEffect(() => {
    syncDialog(importStatusDialogRef.current, importDialog !== null);
  }, [importDialog]);

  useEffect(() => {
    syncDialog(restoreDialogRef.current, restoreDialog !== null);
  }, [restoreDialog]);

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
        const imported = await importBackupArchive(
          importDialog.file,
          importDialog.password,
        );
        if (cancelled) return;

        setQuery("");
        const ok = await syncBackups(false, imported.backup_id);
        if (!ok) {
          setBackups((current) =>
            sortBackups([
              imported,
              ...current.filter(
                (backup) => backup.backup_id !== imported.backup_id,
              ),
            ]),
          );
          setSelectedBackupID(imported.backup_id);
        }
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
        if (!cancelled) setBusyAction("");
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
    ? buildImportSteps(
        importDialog.phase,
        importProgress,
        !!importDialog.imported,
      )
    : [];
  const activeImportStep =
    importSteps.find(
      (step) => step.status === "active" || step.status === "error",
    ) ?? null;

  function openCreateDialog() {
    setCreateDialog(newCreateDialogState());
  }

  function closeCreateDialog() {
    if (busyAction === "create") return;
    setCreateDialog(null);
  }

  async function handleCopyGeneratedPassword() {
    if (!createDialog) return;
    try {
      await navigator.clipboard.writeText(createDialog.generatedPassword);
      setCreateDialog((current) =>
        current
          ? {
              ...current,
              copiedGeneratedPassword: true,
              error: null,
            }
          : current,
      );
    } catch {
      setCreateDialog((current) =>
        current
          ? {
              ...current,
              error: "無法複製到剪貼簿，請手動保存這組密碼。",
            }
          : current,
      );
    }
  }

  async function submitCreateDialog() {
    if (!createDialog) return;

    const password =
      createDialog.mode === "manual"
        ? createDialog.password
        : createDialog.generatedPassword;
    const validationError = validatePassword(password);
    if (validationError) {
      setCreateDialog((current) =>
        current ? { ...current, error: validationError } : current,
      );
      return;
    }
    if (
      createDialog.mode === "manual" &&
      createDialog.password !== createDialog.confirmPassword
    ) {
      setCreateDialog((current) =>
        current
          ? { ...current, error: "兩次輸入的備份密碼不一致" }
          : current,
      );
      return;
    }
    if (
      createDialog.mode === "generated" &&
      !createDialog.acknowledgedGeneratedPassword
    ) {
      setCreateDialog((current) =>
        current
          ? { ...current, error: "請先確認你已保存這組自動產生的備份密碼" }
          : current,
      );
      return;
    }

    setBusyAction("create");
    try {
      const created = await createBackup(password);
      const ok = await syncBackups(false, created.backup_id);
      if (ok) {
        setStatus({ tone: "success", message: "已建立新的加密備份" });
      }
      setCreateDialog(null);
    } catch (error) {
      setCreateDialog((current) =>
        current
          ? {
              ...current,
              error: toErrorMessage(error, "建立備份失敗"),
            }
          : current,
      );
    } finally {
      setBusyAction("");
    }
  }

  function handleImport(file: File | null) {
    if (!file) return;
    setImportPasswordDialog({
      file,
      password: "",
      error: null,
    });
  }

  function closeImportPasswordDialog() {
    if (busyAction === "import") return;
    setImportPasswordDialog(null);
  }

  function submitImportPasswordDialog() {
    if (!importPasswordDialog) return;
    const validationError = validatePassword(importPasswordDialog.password);
    if (validationError) {
      setImportPasswordDialog((current) =>
        current ? { ...current, error: validationError } : current,
      );
      return;
    }
    setImportDialog({
      file: importPasswordDialog.file,
      password: importPasswordDialog.password,
      phase: "ready",
      imported: null,
      error: null,
    });
    setImportPasswordDialog(null);
  }

  function closeImportStatusDialog() {
    if (busyAction === "import") return;
    setImportDialog(null);
  }

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

  function openRestoreDialog(backup: BackupEntry) {
    setRestoreDialog({
      backup,
      password: "",
      error: null,
    });
  }

  function closeRestoreDialog() {
    if (busyAction === "restore") return;
    setRestoreDialog(null);
  }

  async function submitRestoreDialog() {
    if (!restoreDialog) return;
    const validationError = validatePassword(restoreDialog.password);
    if (validationError) {
      setRestoreDialog((current) =>
        current ? { ...current, error: validationError } : current,
      );
      return;
    }

    setBusyAction("restore");
    try {
      const result = await restoreBackup(
        restoreDialog.backup.backup_id,
        restoreDialog.password,
      );
      const ok = await syncBackups(false, restoreDialog.backup.backup_id);
      if (ok) {
        setStatus({
          tone: "success",
          message: `已套用 ${restoreDialog.backup.file_name}`,
        });
      }
      if (result.restart_required) {
        setRestoreNotice(
          `備份 ${restoreDialog.backup.file_name} 已寫入磁碟。請手動重啟 NekoClaw，新的 config、auth、memory、sessions、MCP、personas 與 bot bindings 才會生效；目前後台密碼會保留。`,
        );
      }
      setRestoreDialog(null);
    } catch (error) {
      setRestoreDialog((current) =>
        current
          ? {
              ...current,
              error: toErrorMessage(error, "還原備份失敗"),
            }
          : current,
      );
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
                <div className="badge badge-success badge-sm">ZIP AES-256</div>
                <div className="badge badge-ghost badge-sm">整包還原</div>
              </div>
              <div>
                <h2 className="card-title text-2xl">備份 / 還原</h2>
                <p className="text-sm text-base-content/60">
                  建立備份時必須設定 ZIP 密碼；還原與匯入時也需要輸入同一組備份密碼。
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
                onClick={openCreateDialog}
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
                {latestBackup
                  ? formatRelativeTime(latestBackup.created_at)
                  : "先建立第一份備份"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">備份範圍</div>
              <div className="stat-value text-lg">完整狀態</div>
              <div className="stat-desc">
                config / auth / memory / sessions / MCP / personas / bindings
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">還原模式</div>
              <div className="stat-value text-lg">需密碼</div>
              <div className="stat-desc">
                還原需輸入備份密碼，寫入後仍要手動重啟服務
              </div>
            </div>
          </div>
        </div>
      </div>

      {restoreNotice ? (
        <div className="alert alert-warning">
          <span>{restoreNotice}</span>
        </div>
      ) : null}

      {status ? (
        <div className={`alert ${statusClass(status.tone)}`}>
          <span>{status.message}</span>
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
                placeholder="搜尋檔名、備份 ID、來源、加密狀態或 component"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
              />
            </label>
            <button className="btn join-item" onClick={() => setQuery("")} disabled={!query}>
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
                      ? "可以先建立第一份加密備份，或匯入既有的密碼保護 archive。"
                      : "調整搜尋條件，或清除搜尋後重新查看完整清單。"}
                  </p>
                  <div className="card-actions justify-end">
                    {backups.length === 0 ? (
                      <>
                        <button
                          className="btn btn-primary btn-sm"
                          onClick={openCreateDialog}
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
                          <span className="truncate font-semibold">{backup.file_name}</span>
                          <span className={`badge badge-xs ${sourceBadgeClass(backup.source)}`}>
                            {sourceLabel(backup.source)}
                          </span>
                          <span className={`badge badge-xs ${encryptionBadgeClass(backup.encryption)}`}>
                            {encryptionLabel(backup.encryption)}
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
                <div className="flex flex-wrap items-center gap-2">
                  <div className={`badge badge-sm ${sourceBadgeClass(selectedBackup.source)}`}>
                    {sourceLabel(selectedBackup.source)}
                  </div>
                  <div className={`badge badge-sm ${encryptionBadgeClass(selectedBackup.encryption)}`}>
                    {encryptionLabel(selectedBackup.encryption)}
                  </div>
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
                      <h4 className="text-xl font-semibold">{selectedBackup.file_name}</h4>
                      <div className="badge badge-warning">含機密資料</div>
                      <div className={`badge ${encryptionBadgeClass(selectedBackup.encryption)}`}>
                        {encryptionLabel(selectedBackup.encryption)}
                      </div>
                    </div>
                    <p className="mt-2 text-sm text-base-content/70">
                      這份備份的 payload 已用 ZIP AES-256 保護。還原時需要輸入備份密碼，且完成後必須手動重啟服務。
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
                    還原會替換 config、auth、sessions、memory、MCP、personas 與 bot bindings，但會保留目前後台密碼，且需要輸入備份密碼。
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
                        {formatDateTime(selectedBackup.created_at)} (
                        {formatRelativeTime(selectedBackup.created_at)})
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
                        Encryption
                      </div>
                      <div className="text-sm">ZIP AES-256</div>
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
                  <button className="btn" onClick={handleDownload} disabled={busyAction !== ""}>
                    {busyAction === "download" && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    下載備份
                  </button>
                  <button className="btn" onClick={handleDelete} disabled={busyAction !== ""}>
                    {busyAction === "delete" && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    刪除備份
                  </button>
                  <button
                    className="btn btn-primary"
                    onClick={() => openRestoreDialog(selectedBackup)}
                    disabled={busyAction !== ""}
                    title="還原這份備份"
                  >
                    {busyAction === "restore" && (
                      <span className="loading loading-spinner loading-xs" />
                    )}
                    還原備份
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      <dialog
        ref={createDialogRef}
        className="modal"
        onClose={() => {
          if (createDialog) setCreateDialog(null);
        }}
        onCancel={(event) => {
          if (busyAction === "create") event.preventDefault();
        }}
      >
        <div className="modal-box max-w-3xl space-y-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="text-2xl font-semibold">建立加密備份</h3>
              <p className="mt-2 text-sm text-base-content/60">
                建立時會把 payload 用 ZIP AES-256 保護。這組密碼不會儲存在 NekoClaw 內，請自行妥善保存。
              </p>
            </div>
            <div className="badge badge-success badge-lg">ZIP AES-256</div>
          </div>

          {createDialog ? (
            <>
              <div className="tabs tabs-boxed bg-base-100">
                <button
                  type="button"
                  className={`tab ${createDialog.mode === "manual" ? "tab-active" : ""}`}
                  onClick={() =>
                    setCreateDialog((current) =>
                      current
                        ? {
                            ...current,
                            mode: "manual",
                            error: null,
                          }
                        : current,
                    )
                  }
                >
                  手動輸入
                </button>
                <button
                  type="button"
                  className={`tab ${createDialog.mode === "generated" ? "tab-active" : ""}`}
                  onClick={() =>
                    setCreateDialog((current) =>
                      current
                        ? {
                            ...current,
                            mode: "generated",
                            generatedPassword:
                              current.generatedPassword || generateBackupPassword(),
                            error: null,
                          }
                        : current,
                    )
                  }
                >
                  自動產生
                </button>
              </div>

              {createDialog.mode === "manual" ? (
                <div className="grid gap-4 rounded-box border border-base-300 bg-base-100 p-4 md:grid-cols-2">
                  <label className="form-control">
                    <div className="label">
                      <span className="label-text">備份密碼</span>
                    </div>
                    <input
                      type="password"
                      className="input input-bordered"
                      value={createDialog.password}
                      onChange={(event) =>
                        setCreateDialog((current) =>
                          current
                            ? {
                                ...current,
                                password: event.target.value,
                                error: null,
                              }
                            : current,
                        )
                      }
                      placeholder="至少 12 個字元"
                    />
                  </label>
                  <label className="form-control">
                    <div className="label">
                      <span className="label-text">確認密碼</span>
                    </div>
                    <input
                      type="password"
                      className="input input-bordered"
                      value={createDialog.confirmPassword}
                      onChange={(event) =>
                        setCreateDialog((current) =>
                          current
                            ? {
                                ...current,
                                confirmPassword: event.target.value,
                                error: null,
                              }
                            : current,
                        )
                      }
                      placeholder="再次輸入同一組密碼"
                    />
                  </label>
                  <div className="md:col-span-2 text-sm text-base-content/60">
                    這組密碼未來在匯入與還原時都需要重新輸入，遺失後無法找回。
                  </div>
                </div>
              ) : (
                <div className="space-y-4 rounded-box border border-base-300 bg-base-100 p-4">
                  <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                    <div>
                      <div className="text-sm font-medium">自動產生的備份密碼</div>
                      <div className="mt-2 rounded-box border border-base-300 bg-base-200 px-4 py-3 font-mono text-base">
                        {createDialog.generatedPassword}
                      </div>
                      <p className="mt-2 text-sm text-base-content/60">
                        只會在這個 modal 顯示一次。建立完成後不會再回傳，也不會儲存在 server。
                      </p>
                    </div>
                    <div className="flex gap-2">
                      <button
                        type="button"
                        className="btn btn-sm"
                        onClick={() =>
                          setCreateDialog((current) =>
                            current
                              ? {
                                  ...current,
                                  generatedPassword: generateBackupPassword(),
                                  copiedGeneratedPassword: false,
                                  acknowledgedGeneratedPassword: false,
                                  error: null,
                                }
                              : current,
                          )
                        }
                        disabled={busyAction === "create"}
                      >
                        重新產生
                      </button>
                      <button
                        type="button"
                        className="btn btn-primary btn-sm"
                        onClick={handleCopyGeneratedPassword}
                        disabled={busyAction === "create"}
                      >
                        {createDialog.copiedGeneratedPassword ? "已複製" : "複製密碼"}
                      </button>
                    </div>
                  </div>
                  <label className="label cursor-pointer justify-start gap-3">
                    <input
                      type="checkbox"
                      className="checkbox checkbox-sm"
                      checked={createDialog.acknowledgedGeneratedPassword}
                      onChange={(event) =>
                        setCreateDialog((current) =>
                          current
                            ? {
                                ...current,
                                acknowledgedGeneratedPassword: event.target.checked,
                                error: null,
                              }
                            : current,
                        )
                      }
                    />
                    <span className="label-text">
                      我已經保存這組密碼，之後匯入或還原會用到它。
                    </span>
                  </label>
                </div>
              )}

              {createDialog.error ? (
                <div className="alert alert-error">
                  <span>{createDialog.error}</span>
                </div>
              ) : null}
            </>
          ) : null}

          <div className="modal-action">
            <button className="btn" onClick={closeCreateDialog} disabled={busyAction === "create"}>
              取消
            </button>
            <button
              className="btn btn-primary"
              onClick={submitCreateDialog}
              disabled={busyAction === "create"}
            >
              {busyAction === "create" && (
                <span className="loading loading-spinner loading-xs" />
              )}
              建立加密備份
            </button>
          </div>
        </div>
      </dialog>

      <dialog
        ref={importPasswordDialogRef}
        className="modal"
        onClose={() => {
          if (importPasswordDialog) setImportPasswordDialog(null);
        }}
        onCancel={(event) => {
          if (busyAction === "import") event.preventDefault();
        }}
      >
        <div className="modal-box max-w-2xl space-y-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="text-2xl font-semibold">匯入備份檔</h3>
              <p className="mt-2 text-sm text-base-content/60">
                先輸入 archive 密碼，再開始驗證與匯入。只支援新的密碼保護備份格式。
              </p>
            </div>
            <div className="badge badge-warning badge-lg">需要密碼</div>
          </div>

          {importPasswordDialog ? (
            <>
              <div className="rounded-box border border-base-300 bg-base-100 p-4">
                <div className="text-xs uppercase tracking-wide text-base-content/50">File</div>
                <div className="mt-1 font-semibold">{importPasswordDialog.file.name}</div>
                <div className="mt-3 flex flex-wrap gap-4 text-sm text-base-content/60">
                  <span>大小：{formatBytes(importPasswordDialog.file.size)}</span>
                  <span>MIME：{fileMimeType(importPasswordDialog.file)}</span>
                </div>
              </div>

              <label className="form-control">
                <div className="label">
                  <span className="label-text">備份密碼</span>
                </div>
                <input
                  type="password"
                  className="input input-bordered"
                  value={importPasswordDialog.password}
                  onChange={(event) =>
                    setImportPasswordDialog((current) =>
                      current
                        ? {
                            ...current,
                            password: event.target.value,
                            error: null,
                          }
                        : current,
                    )
                  }
                  placeholder="輸入建立這份備份時使用的密碼"
                />
              </label>

              {importPasswordDialog.error ? (
                <div className="alert alert-error">
                  <span>{importPasswordDialog.error}</span>
                </div>
              ) : null}
            </>
          ) : null}

          <div className="modal-action">
            <button className="btn" onClick={closeImportPasswordDialog} disabled={busyAction === "import"}>
              取消
            </button>
            <button
              className="btn btn-primary"
              onClick={submitImportPasswordDialog}
              disabled={busyAction === "import"}
            >
              開始匯入
            </button>
          </div>
        </div>
      </dialog>

      <dialog
        ref={importStatusDialogRef}
        className="modal"
        onClose={() => {
          if (importDialog && busyAction !== "import") setImportDialog(null);
        }}
        onCancel={(event) => {
          if (busyAction === "import") event.preventDefault();
        }}
      >
        <div className="modal-box max-w-5xl space-y-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="text-2xl font-semibold">匯入備份檔</h3>
              <p className="mt-2 text-sm text-base-content/60">
                上傳完成後會先驗證 archive，再寫入目前備份庫。
              </p>
            </div>
            {importDialog ? (
              <div className={`badge badge-lg ${importPhaseBadgeClass(importDialog.phase)}`}>
                {importPhaseLabel(importDialog.phase)}
              </div>
            ) : null}
          </div>

          {importDialog ? (
            <>
              <div className="rounded-box border border-base-300 bg-base-100 p-4">
                <h4 className="text-lg font-semibold">檔案資訊</h4>
                <div className="mt-4 grid gap-4 md:grid-cols-3">
                  <div>
                    <div className="text-xs uppercase tracking-wide text-base-content/50">File</div>
                    <div className="mt-1 font-medium">{importDialog.file.name}</div>
                  </div>
                  <div>
                    <div className="text-xs uppercase tracking-wide text-base-content/50">Size</div>
                    <div className="mt-1 font-medium">{formatBytes(importDialog.file.size)}</div>
                  </div>
                  <div>
                    <div className="text-xs uppercase tracking-wide text-base-content/50">MIME Type</div>
                    <div className="mt-1 font-medium">{fileMimeType(importDialog.file)}</div>
                  </div>
                </div>
              </div>

              <div className="rounded-box border border-base-300 bg-base-100 p-4">
                <div className="flex items-center justify-between gap-3">
                  <h4 className="text-lg font-semibold">目前狀態</h4>
                  <div className="text-sm font-medium">{importProgress}%</div>
                </div>
                <p className="mt-3 text-sm text-base-content/70">
                  {activeImportStep
                    ? `${activeImportStep.title}：${activeImportStep.detail}`
                    : importPhaseDescription(importDialog.phase)}
                </p>
                <progress
                  className={`progress mt-4 h-3 w-full ${importPhaseProgressClass(importDialog.phase)}`}
                  value={importProgress}
                  max={100}
                />
                <div className="mt-4 space-y-3">
                  {importSteps.map((step) => (
                    <div
                      key={step.title}
                      className="rounded-box border border-base-300 bg-base-200 px-4 py-3"
                    >
                      <div className="flex items-center justify-between gap-3">
                        <div className="font-medium">{step.title}</div>
                        <div className={`badge badge-sm ${importStepBadgeClass(step.status)}`}>
                          {importStepLabel(step.status)}
                        </div>
                      </div>
                      <p className="mt-2 text-sm text-base-content/60">{step.detail}</p>
                    </div>
                  ))}
                </div>
              </div>

              {importDialog.imported ? (
                <div className="rounded-box border border-success/30 bg-success/5 p-4">
                  <h4 className="text-lg font-semibold">成功結果</h4>
                  <div className="mt-4 grid gap-4 md:grid-cols-2">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">File Name</div>
                      <div className="mt-1 font-medium">{importDialog.imported.file_name}</div>
                    </div>
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">Backup ID</div>
                      <div className="mt-1 font-mono text-sm">{importDialog.imported.backup_id}</div>
                    </div>
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">Source</div>
                      <div className="mt-1">{sourceLabel(importDialog.imported.source)}</div>
                    </div>
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">Created At</div>
                      <div className="mt-1">{formatDateTime(importDialog.imported.created_at)}</div>
                    </div>
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">Encryption</div>
                      <div className="mt-1">ZIP AES-256</div>
                    </div>
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">Size</div>
                      <div className="mt-1">{formatBytes(importDialog.imported.size_bytes)}</div>
                    </div>
                  </div>
                  <div className="mt-4 flex flex-wrap gap-2">
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
              ) : null}

              {importDialog.error ? (
                <div className="rounded-box border border-error/30 bg-error/5 p-4">
                  <h4 className="text-lg font-semibold">失敗細節</h4>
                  <div className="mt-4 grid gap-4 md:grid-cols-3">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">HTTP Status</div>
                      <div className="mt-1 font-medium">
                        {importDialog.error.statusCode || "-"}
                      </div>
                    </div>
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">Error Code</div>
                      <div className="mt-1 font-mono text-sm">
                        {importDialog.error.code || "-"}
                      </div>
                    </div>
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">Message</div>
                      <div className="mt-1">{importDialog.error.message}</div>
                    </div>
                  </div>
                  {importErrorHint(importDialog.error) ? (
                    <div className="alert alert-warning mt-4">
                      <span>{importErrorHint(importDialog.error)}</span>
                    </div>
                  ) : null}
                </div>
              ) : null}
            </>
          ) : null}

          <div className="modal-action">
            <button className="btn" onClick={closeImportStatusDialog} disabled={busyAction === "import"}>
              {busyAction === "import" ? "處理中" : "關閉"}
            </button>
          </div>
        </div>
      </dialog>

      <dialog
        ref={restoreDialogRef}
        className="modal"
        onClose={() => {
          if (restoreDialog) setRestoreDialog(null);
        }}
        onCancel={(event) => {
          if (busyAction === "restore") event.preventDefault();
        }}
      >
        <div className="modal-box max-w-2xl space-y-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="text-2xl font-semibold">還原備份</h3>
              <p className="mt-2 text-sm text-base-content/60">
                會完整覆蓋目前資料，但不會覆蓋目前後台密碼。還原完成後需要手動重啟服務。
              </p>
            </div>
            <div className="badge badge-warning badge-lg">需要密碼</div>
          </div>

          {restoreDialog ? (
            <>
              <div className="rounded-box border border-base-300 bg-base-100 p-4">
                <div className="text-xs uppercase tracking-wide text-base-content/50">Target Backup</div>
                <div className="mt-1 font-semibold">{restoreDialog.backup.file_name}</div>
                <div className="mt-3 text-sm text-base-content/60">
                  建立於 {formatDateTime(restoreDialog.backup.created_at)}，還原時會替換主要持久化資料。
                </div>
              </div>

              <div className="alert alert-warning">
                <span>
                  這個動作需要輸入建立該備份時使用的密碼；錯誤密碼會直接導致還原失敗。
                </span>
              </div>

              <label className="form-control">
                <div className="label">
                  <span className="label-text">備份密碼</span>
                </div>
                <input
                  type="password"
                  className="input input-bordered"
                  value={restoreDialog.password}
                  onChange={(event) =>
                    setRestoreDialog((current) =>
                      current
                        ? {
                            ...current,
                            password: event.target.value,
                            error: null,
                          }
                        : current,
                    )
                  }
                  placeholder="輸入建立這份備份時使用的密碼"
                />
              </label>

              {restoreDialog.error ? (
                <div className="alert alert-error">
                  <span>{restoreDialog.error}</span>
                </div>
              ) : null}
            </>
          ) : null}

          <div className="modal-action">
            <button className="btn" onClick={closeRestoreDialog} disabled={busyAction === "restore"}>
              取消
            </button>
            <button
              className="btn btn-primary"
              onClick={submitRestoreDialog}
              disabled={busyAction === "restore"}
            >
              {busyAction === "restore" && (
                <span className="loading loading-spinner loading-xs" />
              )}
              確認還原
            </button>
          </div>
        </div>
      </dialog>
    </div>
  );
}
