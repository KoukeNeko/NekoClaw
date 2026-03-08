import { useAppStore } from "@/store/appStore";
import { formatTokens, formatCost } from "@/utils/format";

/**
 * Inspector footer in sidebar — uses daisyUI list, progress, badge, divider.
 */
export function InspectorPanel() {
  const provider = useAppStore((s) => s.provider);
  const model = useAppStore((s) => s.model);
  const activePersona = useAppStore((s) => s.activePersona);
  const totalUsage = useAppStore((s) => s.totalUsage);
  const totalCost = useAppStore((s) => s.totalCost);
  const contextPercent = useAppStore((s) => s.contextPercent);
  const messages = useAppStore((s) => s.messages);

  const messageCount = messages.filter(
    (m) => m.role === "user" || m.role === "assistant",
  ).length;

  return (
    <div className="border-t border-base-300 px-1 py-2">
      <ul className="list list-none text-xs">
        {/* Provider & Model */}
        <li className="list-row py-1 px-2 flex justify-between items-center">
          <span className="text-base-content/50">Provider</span>
          <span className="badge badge-ghost badge-sm font-mono truncate max-w-36">
            {provider || "—"} · {model || "default"}
          </span>
        </li>

        {/* Persona */}
        <li className="list-row py-1 px-2 flex justify-between items-center">
          <span className="text-base-content/50">Persona</span>
          {activePersona ? (
            <span className="badge badge-primary badge-sm">{activePersona}</span>
          ) : (
            <span className="badge badge-ghost badge-sm text-base-content/30">未啟用</span>
          )}
        </li>

        {/* Context usage — daisyUI progress */}
        <li className="list-row py-1 px-2">
          <div className="flex justify-between mb-0.5">
            <span className="text-base-content/50">Context</span>
            <span>{contextPercent}%</span>
          </div>
          <progress
            className={`progress progress-sm w-full ${
              contextPercent > 80
                ? "progress-error"
                : contextPercent > 50
                  ? "progress-warning"
                  : "progress-success"
            }`}
            value={Math.min(contextPercent, 100)}
            max={100}
          />
        </li>

        <li className="list-row py-1 px-2 flex justify-between items-center">
          <span className="text-base-content/50">Cost</span>
          <span className="font-mono">{formatCost(totalCost)}</span>
        </li>

        <li className="list-row py-1 px-2 flex justify-between items-center">
          <span className="text-base-content/50">Tokens</span>
          <span className="font-mono">
            ↑{formatTokens(totalUsage.input_tokens)} ↓
            {formatTokens(totalUsage.output_tokens)}
          </span>
        </li>

        <li className="list-row py-1 px-2 flex justify-between items-center">
          <span className="text-base-content/50">Messages</span>
          <span className="badge badge-ghost badge-sm">{messageCount}</span>
        </li>
      </ul>
    </div>
  );
}
