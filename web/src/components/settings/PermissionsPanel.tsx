import { useEffect, useState } from "react";
import { getPermissions, savePermissions } from "@/api/client";
import type { PermissionGrant } from "@/api/types";

export function PermissionsPanel() {
  const [draft, setDraft] = useState("[]");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState("");

  async function reload() {
    setLoading(true);
    setStatus("");
    try {
      const grants = await getPermissions();
      setDraft(JSON.stringify(grants, null, 2));
    } catch (err) {
      setStatus(err instanceof Error ? err.message : "無法載入權限");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  async function handleSave() {
    setSaving(true);
    setStatus("");
    try {
      const parsed = JSON.parse(draft) as PermissionGrant[];
      const saved = await savePermissions(parsed);
      setDraft(JSON.stringify(saved, null, 2));
      setStatus("已儲存權限規則");
    } catch (err) {
      setStatus(err instanceof Error ? err.message : "儲存失敗");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">Permissions</h2>
          <p className="text-sm text-base-content/70">
            管理 workspace + tool 級別的持久化授權。
          </p>
        </div>
        <div className="flex gap-2">
          <button className="btn btn-sm" onClick={() => void reload()}>
            重新整理
          </button>
          <button className="btn btn-sm btn-primary" onClick={() => void handleSave()} disabled={saving || loading}>
            儲存
          </button>
        </div>
      </div>
      {status && <div className="alert alert-info">{status}</div>}
      <textarea
        className="textarea textarea-bordered h-96 w-full font-mono text-sm"
        value={draft}
        disabled={loading}
        onChange={(e) => setDraft(e.target.value)}
      />
    </div>
  );
}
