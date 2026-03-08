import { useAppStore } from "@/store/appStore";
import { SessionList } from "./SessionList";
import { ThemeDropdown } from "./ThemeDropdown";

/**
 * Sidebar — daisyUI menu with is-drawer-close/is-drawer-open collapsible pattern.
 * Collapsed: icon-only (w-14) with tooltips.
 * Expanded: full sidebar with text labels, session list, inspector.
 */
/** Close sidebar only on mobile where it overlays content */
const MOBILE_BREAKPOINT = 1024;
function closeSidebarOnMobile() {
  if (window.innerWidth < MOBILE_BREAKPOINT) {
    useAppStore.getState().setSidebarOpen(false);
  }
}

export function Sidebar() {
  const route = useAppStore((s) => s.route);
  const setRoute = useAppStore((s) => s.setRoute);

  function handleNewChat() {
    const now = new Date();
    const pad = (n: number) => String(n).padStart(2, "0");
    const id = `chat-${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
    useAppStore.getState().setSessionID(id);
    useAppStore.getState().clearMessages();
    useAppStore.getState().resetUsage();
    setRoute("chat");
    closeSidebarOnMobile();
  }

  function handleNavSettings() {
    setRoute("settings/provider");
    closeSidebarOnMobile();
  }

  return (
    <div className="flex flex-col h-full w-full">
      {/* Header — logo (always visible) + title (hidden when collapsed) */}
      <div className="border-b border-base-300 p-2">
        <button
          className="btn btn-ghost btn-sm w-full is-drawer-close:btn-square is-drawer-open:justify-start gap-2"
          onClick={() => {
            setRoute("chat");
            closeSidebarOnMobile();
          }}
        >
          <span className="text-lg shrink-0">🐾</span>
          <span className="font-bold is-drawer-close:hidden">NekoClaw</span>
        </button>
      </div>

      {/* New chat — daisyUI menu with collapsible tooltip */}
      <ul className="menu w-full p-1">
        <li>
          <button
            className="is-drawer-close:tooltip is-drawer-close:tooltip-right"
            data-tip="新對話 (Ctrl+N)"
            onClick={handleNewChat}
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={2}
              stroke="currentColor"
              className="size-4 shrink-0"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M12 4.5v15m7.5-7.5h-15"
              />
            </svg>
            <span className="is-drawer-close:hidden">新對話</span>
          </button>
        </li>
      </ul>

      {/* Session list (scrollable) — hidden when collapsed */}
      <div className="flex-1 overflow-y-auto [scrollbar-width:thin] is-drawer-close:hidden">
        <SessionList />
      </div>

      {/* Bottom section — pushed to bottom via mt-auto */}
      <div className="mt-auto w-full">
        {/* Settings — daisyUI menu with collapsible tooltip */}
        <ul className="menu w-full px-2 py-0">
          <li>
            <button
              className={`py-2.5 is-drawer-close:tooltip is-drawer-close:tooltip-right ${route.startsWith("settings") ? "active" : ""
                }`}
              data-tip="設定"
              onClick={handleNavSettings}
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.5}
                stroke="currentColor"
                className="size-4 shrink-0"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z"
                />
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"
                />
              </svg>
              <span className="is-drawer-close:hidden">設定</span>
            </button>
          </li>
        </ul>

        {/* Theme switcher */}
        <div className="px-2 mb-2 w-full">
          <ThemeDropdown />
        </div>

        {/* Keyboard shortcuts — hidden when collapsed */}
        <div className="px-4 pb-2 flex flex-wrap gap-1 text-[10px] text-base-content/30 is-drawer-close:hidden">
          <span>
            <kbd className="kbd kbd-xs mb-1">Ctrl</kbd>+
            <kbd className="kbd kbd-xs mb-1">N</kbd> 新對話
          </span>
          <span>
            <kbd className="kbd kbd-xs mb-1">Ctrl</kbd>+
            <kbd className="kbd kbd-xs mb-1">B</kbd> 側欄
          </span>
          <span>
            <kbd className="kbd kbd-xs">Esc</kbd> 設定
          </span>
        </div>
      </div>
    </div>
  );
}
