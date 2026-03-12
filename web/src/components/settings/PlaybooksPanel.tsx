import { useEffect, useState } from "react";
import { approvePlaybook, archivePlaybook, listPlaybooks } from "@/api/client";
import type { PlaybookEntry } from "@/api/types";

export function PlaybooksPanel() {
  const [entries, setEntries] = useState<PlaybookEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  async function reload() {
    setLoading(true);
    setError("");
    try {
      setEntries(await listPlaybooks());
    } catch (err) {
      setError(err instanceof Error ? err.message : "無法載入 Playbooks");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  async function handleApprove(id: string) {
    await approvePlaybook(id);
    await reload();
  }

  async function handleArchive(id: string) {
    await archivePlaybook(id);
    await reload();
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">Playbooks</h2>
          <p className="text-sm text-base-content/70">
            檢視候選策略記憶並手動核准成 active playbooks。
          </p>
        </div>
        <button className="btn btn-sm" onClick={() => void reload()}>
          重新整理
        </button>
      </div>

      {error && <div className="alert alert-error">{error}</div>}
      {loading ? (
        <div className="loading loading-spinner" />
      ) : (
        <div className="space-y-3">
          {entries.length === 0 && (
            <div className="rounded-box border border-base-300 p-4 text-sm text-base-content/70">
              目前沒有 playbook entries。
            </div>
          )}
          {entries.map((entry) => (
            <div key={entry.id} className="rounded-box border border-base-300 bg-base-100 p-4">
              <div className="mb-2 flex items-center justify-between gap-3">
                <div className="flex items-center gap-2 text-sm">
                  <span className="badge badge-outline">{entry.kind}</span>
                  <span className="badge badge-outline">{entry.scope}</span>
                  <span className="badge badge-primary badge-outline">{entry.status}</span>
                </div>
                <div className="flex gap-2">
                  {entry.status !== "active" && (
                    <button className="btn btn-xs btn-primary" onClick={() => void handleApprove(entry.id)}>
                      核准
                    </button>
                  )}
                  {entry.status !== "archived" && (
                    <button className="btn btn-xs" onClick={() => void handleArchive(entry.id)}>
                      封存
                    </button>
                  )}
                </div>
              </div>
              <pre className="whitespace-pre-wrap text-sm">{entry.content}</pre>
              <div className="mt-3 text-xs text-base-content/60">
                source: {entry.source || "manual"} · updated: {entry.updated_at}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
