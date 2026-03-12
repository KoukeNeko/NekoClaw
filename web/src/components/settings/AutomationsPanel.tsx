import { useEffect, useMemo, useState } from "react";
import { deleteAutomation, listAutomations, runAutomation, saveAutomation } from "@/api/client";
import type { WorkflowRecord } from "@/api/types";

const EMPTY_WORKFLOW: WorkflowRecord = {
  id: "",
  name: "",
  enabled: true,
  trigger_kind: "schedule",
  schedule_interval_mins: 60,
  steps: [],
  created_at: "",
  updated_at: "",
};

export function AutomationsPanel() {
  const [workflows, setWorkflows] = useState<WorkflowRecord[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [draft, setDraft] = useState(JSON.stringify(EMPTY_WORKFLOW, null, 2));
  const [status, setStatus] = useState("");

  async function reload() {
    setStatus("");
    try {
      const items = await listAutomations();
      setWorkflows(items);
      if (!selectedID && items[0]) {
        setSelectedID(items[0].id);
      }
    } catch (err) {
      setStatus(err instanceof Error ? err.message : "無法載入 Automations");
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  const selected = useMemo(
    () => workflows.find((workflow) => workflow.id === selectedID) ?? null,
    [workflows, selectedID],
  );

  useEffect(() => {
    setDraft(JSON.stringify(selected ?? EMPTY_WORKFLOW, null, 2));
  }, [selected]);

  async function handleSave() {
    try {
      const parsed = JSON.parse(draft) as WorkflowRecord;
      const saved = await saveAutomation(parsed);
      setSelectedID(saved.id);
      setStatus("已儲存 automation");
      await reload();
    } catch (err) {
      setStatus(err instanceof Error ? err.message : "儲存失敗");
    }
  }

  async function handleRun() {
    if (!selectedID) return;
    try {
      await runAutomation(selectedID, {});
      setStatus("已觸發 automation run");
      await reload();
    } catch (err) {
      setStatus(err instanceof Error ? err.message : "執行失敗");
    }
  }

  async function handleDelete() {
    if (!selectedID) return;
    try {
      await deleteAutomation(selectedID);
      setSelectedID("");
      setDraft(JSON.stringify(EMPTY_WORKFLOW, null, 2));
      setStatus("已刪除 automation");
      await reload();
    } catch (err) {
      setStatus(err instanceof Error ? err.message : "刪除失敗");
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[320px_minmax(0,1fr)]">
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold">Automations</h2>
          <button
            className="btn btn-sm"
            onClick={() => {
              setSelectedID("");
              setDraft(JSON.stringify(EMPTY_WORKFLOW, null, 2));
            }}
          >
            新增
          </button>
        </div>
        <div className="space-y-2">
          {workflows.map((workflow) => (
            <button
              key={workflow.id}
              className={`w-full rounded-box border p-3 text-left ${
                workflow.id === selectedID ? "border-primary bg-base-200/60" : "border-base-300"
              }`}
              onClick={() => setSelectedID(workflow.id)}
            >
              <div className="font-medium">{workflow.name || workflow.id}</div>
              <div className="text-xs text-base-content/60">
                {workflow.trigger_kind} · {workflow.enabled ? "enabled" : "disabled"}
              </div>
            </button>
          ))}
        </div>
      </div>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-lg font-semibold">Workflow JSON</h3>
            <p className="text-sm text-base-content/70">
              目前用 JSON 直接編輯 linear workflow steps。
            </p>
          </div>
          <div className="flex gap-2">
            {selectedID && <button className="btn btn-sm" onClick={() => void handleRun()}>執行</button>}
            {selectedID && <button className="btn btn-sm" onClick={() => void handleDelete()}>刪除</button>}
            <button className="btn btn-sm btn-primary" onClick={() => void handleSave()}>儲存</button>
          </div>
        </div>
        {status && <div className="alert alert-info">{status}</div>}
        <textarea
          className="textarea textarea-bordered h-[32rem] w-full font-mono text-sm"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      </div>
    </div>
  );
}
