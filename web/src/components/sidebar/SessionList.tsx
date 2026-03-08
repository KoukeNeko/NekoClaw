import { useEffect, useCallback } from "react";
import { useAppStore } from "@/store/appStore";
import { listSessions, getTranscript } from "@/api/client";
import { formatRelativeTime } from "@/utils/format";
import type { TranscriptEntry } from "@/api/types";
import type { ChatMessage } from "@/store/appStore";

/**
 * Scrollable session list showing all persisted sessions.
 * Clicking a session loads its transcript and switches to it.
 */
export function SessionList() {
  const sessions = useAppStore((s) => s.sessions);
  const setSessions = useAppStore((s) => s.setSessions);
  const sessionID = useAppStore((s) => s.sessionID);
  const setSessionID = useAppStore((s) => s.setSessionID);
  const setMessages = useAppStore((s) => s.setMessages);
  const setRoute = useAppStore((s) => s.setRoute);
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);
  const resetUsage = useAppStore((s) => s.resetUsage);

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
    setSessionID(id);
    resetUsage();
    setRoute("chat");
    // Only close sidebar on mobile where it overlays content
    if (window.innerWidth < 1024) {
      setSidebarOpen(false);
    }

    // Load transcript
    try {
      const entries = await getTranscript(id);
      const msgs = transcriptToMessages(entries);
      setMessages(msgs);
    } catch {
      setMessages([]);
    }
  }

  return (
    <div className="p-2">
      <div className="text-xs font-semibold text-base-content/50 uppercase tracking-wider px-2 py-1">
        對話紀錄
      </div>
      {sessions.length === 0 && (
        <div className="text-sm text-base-content/40 px-2 py-4 text-center">
          尚無對話
        </div>
      )}
      <ul className="menu menu-sm p-0 gap-0.5">
        {sessions.map((s) => (
          <li key={s.session_id}>
            <button
              className={`flex justify-between items-center w-full min-w-0 ${
                s.session_id === sessionID ? "active" : ""
              }`}
              onClick={() => handleSelect(s.session_id)}
            >
              <span className="truncate flex-1 text-left min-w-0">
                {s.title || s.session_id}
              </span>
              <span className="text-xs text-base-content/40 shrink-0 ml-2">
                {formatRelativeTime(s.updated_at)}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Convert JSONL transcript entries to ChatMessages for display */
function transcriptToMessages(entries: TranscriptEntry[]): ChatMessage[] {
  const msgs: ChatMessage[] = [];
  let counter = 0;
  for (const entry of entries) {
    if (entry.type !== "message") continue;
    if (!entry.role || entry.role === "system" || entry.role === "tool")
      continue;
    msgs.push({
      id: entry.id || `tx-${++counter}`,
      role: entry.role as ChatMessage["role"],
      content: entry.content ?? "",
      images: entry.images,
      createdAt: entry.timestamp,
    });
  }
  return msgs;
}
