import { useAppStore } from "@/store/appStore";

/**
 * Chat header bar — shows provider · model and sidebar toggle.
 * Toggle is visible on ALL screens: mobile (overlay) + desktop (collapse/expand).
 */
export function ChatHeader() {
  const provider = useAppStore((s) => s.provider);
  const model = useAppStore((s) => s.model);

  return (
    <header className="navbar bg-base-200/50 backdrop-blur-sm border-b border-base-300 min-h-12 px-3">
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

      {/* Spacer for symmetry */}
      <div className="flex-none w-8" />
    </header>
  );
}
