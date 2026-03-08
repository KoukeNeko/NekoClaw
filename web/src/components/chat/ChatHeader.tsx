import { useAppStore } from "@/store/appStore";
import { formatTokens, formatCost } from "@/utils/format";

/**
 * Chat header bar — shows provider · model, sidebar toggle, and inspector dropdown.
 * Toggle is visible on ALL screens: mobile (overlay) + desktop (collapse/expand).
 */
export function ChatHeader() {
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
    <header className="navbar bg-base-200/50 backdrop-blur-sm border-b border-base-300 min-h-12 px-3 relative z-10">
      {/* Sidebar toggle — always visible */}
      <div className="flex-none">
        <label
          htmlFor="app-drawer"
          aria-label="toggle sidebar"
          className="btn btn-ghost btn-sm btn-square"
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 24 24"
            strokeLinejoin="round"
            strokeLinecap="round"
            strokeWidth="2"
            fill="none"
            stroke="currentColor"
            className="size-4"
          >
            <path d="M4 4m0 2a2 2 0 0 1 2 -2h12a2 2 0 0 1 2 2v12a2 2 0 0 1 -2 2h-12a2 2 0 0 1 -2 -2z" />
            <path d="M9 4v16" />
            <path d="M14 10l2 2l-2 2" />
          </svg>
        </label>
      </div>

      {/* Provider · Model centered */}
      <div className="flex-1 flex justify-center">
        <span className="text-sm font-mono text-base-content/60">
          {provider || "—"} · {model || "default"}
        </span>
      </div>

      {/* Inspector dropdown */}
      <div className="flex-none">
        <div className="dropdown dropdown-end">
          <div
            tabIndex={0}
            role="button"
            className="btn btn-ghost btn-sm btn-square"
            aria-label="session info"
          >
            {/* Info circle icon */}
            <svg
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
              className="size-4"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="m11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z"
              />
            </svg>
          </div>
          <div
            tabIndex={0}
            className="dropdown-content z-50 bg-base-200 border border-base-300 rounded-box shadow-lg p-3 w-64 mt-2"
          >
            <ul className="text-xs space-y-1.5">
              <li className="flex justify-between items-center">
                <span className="text-base-content/50">Provider</span>
                <span className="font-mono truncate max-w-36 text-right">
                  {provider || "—"} · {model || "default"}
                </span>
              </li>
              <li className="flex justify-between items-center">
                <span className="text-base-content/50">Persona</span>
                {activePersona ? (
                  <span className="badge badge-primary badge-xs">{activePersona}</span>
                ) : (
                  <span className="text-base-content/30">未啟用</span>
                )}
              </li>
              <li>
                <div className="flex justify-between mb-0.5">
                  <span className="text-base-content/50">Context</span>
                  <span>{contextPercent}%</span>
                </div>
                <progress
                  className={`progress progress-xs w-full ${
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
              <li className="flex justify-between items-center">
                <span className="text-base-content/50">Cost</span>
                <span className="font-mono">{formatCost(totalCost)}</span>
              </li>
              <li className="flex justify-between items-center">
                <span className="text-base-content/50">Tokens</span>
                <span className="font-mono">
                  ↑{formatTokens(totalUsage.input_tokens)} ↓
                  {formatTokens(totalUsage.output_tokens)}
                </span>
              </li>
              <li className="flex justify-between items-center">
                <span className="text-base-content/50">Messages</span>
                <span>{messageCount}</span>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </header>
  );
}
