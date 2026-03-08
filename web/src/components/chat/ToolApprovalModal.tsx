import { useAppStore } from "@/store/appStore";

interface Props {
  onDecide: (
    decisions: { approval_id: string; decision: "allow" | "deny" }[],
    runID: string,
  ) => void;
}

/**
 * Modal dialog shown when the LLM requests tool approval.
 * Displays pending tool calls and allows allow/deny for each.
 */
export function ToolApprovalModal({ onDecide }: Props) {
  const pendingApprovals = useAppStore((s) => s.pendingApprovals);
  const currentRunID = useAppStore((s) => s.currentRunID);
  const clearApprovals = useAppStore((s) => s.clearApprovals);

  function handleAllowAll() {
    const decisions = pendingApprovals.map((a) => ({
      approval_id: a.approval_id,
      decision: "allow" as const,
    }));
    clearApprovals();
    onDecide(decisions, currentRunID);
  }

  function handleDenyAll() {
    const decisions = pendingApprovals.map((a) => ({
      approval_id: a.approval_id,
      decision: "deny" as const,
    }));
    clearApprovals();
    onDecide(decisions, currentRunID);
  }

  if (pendingApprovals.length === 0) return null;

  return (
    <dialog className="modal modal-open">
      <div className="modal-box max-w-lg">
        <h3 className="font-bold text-lg mb-4">
          🔧 工具授權要求 ({pendingApprovals.length})
        </h3>

        <div className="space-y-2 max-h-60 overflow-y-auto">
          {pendingApprovals.map((approval) => (
            <div
              key={approval.approval_id}
              className="collapse collapse-arrow bg-base-200"
            >
              <input type="checkbox" defaultChecked />
              {/* Collapse title — tool name + risk badge */}
              <div className="collapse-title text-sm font-semibold flex items-center gap-2 min-h-0 py-2">
                <span className="font-mono">{approval.tool_name}</span>
                {approval.risk_level && (
                  <span
                    className={`badge badge-xs ${
                      approval.risk_level === "high"
                        ? "badge-error"
                        : approval.risk_level === "medium"
                          ? "badge-warning"
                          : "badge-info"
                    }`}
                  >
                    {approval.risk_level}
                  </span>
                )}
              </div>
              {/* Collapse content — arguments preview + reason */}
              <div className="collapse-content text-xs">
                {approval.arguments_preview && (
                  <pre className="text-base-content/60 whitespace-pre-wrap break-all bg-base-300 rounded-btn p-2">
                    {approval.arguments_preview}
                  </pre>
                )}
                {approval.reason && (
                  <p className="text-base-content/50 mt-2">
                    {approval.reason}
                  </p>
                )}
              </div>
            </div>
          ))}
        </div>

        <div className="modal-action">
          <button className="btn btn-ghost" onClick={handleDenyAll}>
            全部拒絕
          </button>
          <button className="btn btn-primary" onClick={handleAllowAll}>
            全部允許
          </button>
        </div>
      </div>
      <form method="dialog" className="modal-backdrop">
        <button onClick={handleDenyAll}>close</button>
      </form>
    </dialog>
  );
}
