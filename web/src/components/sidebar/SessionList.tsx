import { useEffect, useCallback } from "react";
import { useAppStore } from "@/store/appStore";
import { listSessions } from "@/api/client";
import { formatRelativeTime } from "@/utils/format";
import { openSessionConversation } from "@/utils/sessionNavigation";
import {
  sidebarItemActiveClass,
  sidebarListItemClass,
  sidebarSectionClass,
  sidebarSectionLabelClass,
} from "./sidebarItemStyles";

/**
 * Scrollable session list showing all persisted sessions.
 * Clicking a session loads its transcript and switches to it.
 */
export function SessionList() {
  const sessions = useAppStore((s) => s.sessions);
  const setSessions = useAppStore((s) => s.setSessions);
  const sessionID = useAppStore((s) => s.sessionID);

  const loadSessions = useCallback(async () => {
    try {
      const list = await listSessions();
      setSessions(list);
    } catch {
      // Silently fail — server might not be ready
    }
  }, [setSessions]);

  useEffect(() => {
    loadSessions();
    // Poll every 10 seconds for session updates
    const timer = setInterval(loadSessions, 10_000);
    return () => clearInterval(timer);
  }, [loadSessions]);

  async function handleSelect(id: string) {
    if (id === sessionID) return;
    try {
      await openSessionConversation(id);
    } catch {
      // Keep the current conversation intact when the transcript fails to load.
    }
  }

  return (
    <div className={sidebarSectionClass}>
      <div className={sidebarSectionLabelClass}>
        對話紀錄
      </div>
      {sessions.length === 0 && (
        <div className="px-3 py-4 text-center text-sm text-base-content/40">
          尚無對話
        </div>
      )}
      <ul className="space-y-1">
        {sessions.map((s) => (
          <li key={s.session_id}>
            <button
              type="button"
              className={`${sidebarListItemClass} ${s.session_id === sessionID ? sidebarItemActiveClass : ""}`}
              onClick={() => handleSelect(s.session_id)}
            >
              <span className="flex min-w-0 flex-1 items-start justify-between gap-2">
                <span className="truncate min-w-0 font-medium">
                  {s.title || s.session_id}
                </span>
                <span className="mt-0.5 shrink-0 whitespace-nowrap text-[10px] text-base-content/40">
                  {formatRelativeTime(s.updated_at)}
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
