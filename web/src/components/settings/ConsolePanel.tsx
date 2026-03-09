import { useEffect, useRef, useState } from "react";
import { dispatchBrowserAuthEvent } from "@/api/authEvents";
import type {
  ConsoleAppendEvent,
  ConsoleSnapshotEvent,
} from "@/api/types";
import { SelectionList, SelectionListItem } from "@/components/settings/SelectionList";

type ConnectionState =
  | "connecting"
  | "live"
  | "reconnecting"
  | "unavailable"
  | "error";
type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;

const DEFAULT_TAIL_LINES = 400;
const MAX_BUFFER_LINES = 1000;
const RECONNECT_DELAYS_MS = [1000, 2000, 5000];
const STREAM_URL = `/v1/console/stream?tail=${DEFAULT_TAIL_LINES}`;

function statusClass(tone: StatusTone): string {
  switch (tone) {
    case "success":
      return "alert-success";
    case "error":
      return "alert-error";
    default:
      return "alert-info";
  }
}

function connectionBadgeClass(state: ConnectionState): string {
  switch (state) {
    case "live":
      return "badge-primary";
    case "reconnecting":
      return "badge-warning";
    case "unavailable":
    case "error":
      return "badge-error";
    default:
      return "badge-ghost";
  }
}

function connectionLabel(state: ConnectionState): string {
  switch (state) {
    case "live":
      return "Live";
    case "reconnecting":
      return "Reconnecting";
    case "unavailable":
      return "Unavailable";
    case "error":
      return "Error";
    default:
      return "Connecting";
  }
}

function trimBufferedLines(lines: string[]): string[] {
  if (lines.length <= MAX_BUFFER_LINES) return lines;
  return lines.slice(lines.length - MAX_BUFFER_LINES);
}

function parseModuleName(line: string): string {
  const match = line.match(/^\d{2}:\d{2}:\d{2}\s+([^|]+?)\s*\|/);
  const moduleName = match?.[1]?.trim();
  return moduleName || "other";
}

function readErrorPayload(payload: unknown, fallback: string) {
  if (typeof payload === "object" && payload !== null) {
    const root = payload as { error?: unknown };
    if (typeof root.error === "string") {
      return { code: "", message: root.error || fallback };
    }
    if (typeof root.error === "object" && root.error !== null) {
      const error = root.error as { code?: unknown; message?: unknown };
      return {
        code: typeof error.code === "string" ? error.code : "",
        message: typeof error.message === "string" ? error.message : fallback,
      };
    }
  }
  return { code: "", message: fallback };
}

async function readAPIError(
  response: Response,
  fallback: string,
): Promise<{ code: string; message: string }> {
  try {
    const raw = await response.text();
    if (!raw.trim()) {
      return { code: "", message: fallback };
    }
    return readErrorPayload(JSON.parse(raw), fallback);
  } catch {
    return { code: "", message: fallback };
  }
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

export function ConsolePanel() {
  const [connectionState, setConnectionState] = useState<ConnectionState>("connecting");
  const [status, setStatus] = useState<StatusState>(null);
  const [logPath, setLogPath] = useState("");
  const [lines, setLines] = useState<string[]>([]);
  const [selectedModule, setSelectedModule] = useState("all");
  const [reconnectToken, setReconnectToken] = useState(0);

  const viewportRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);

  useEffect(() => {
    let cancelled = false;
    let eventSource: EventSource | null = null;
    let probeController: AbortController | null = null;
    let reconnectTimer = 0;
    let reconnectAttempt = 0;

    const clearPending = () => {
      if (probeController) {
        probeController.abort();
        probeController = null;
      }
      if (eventSource) {
        eventSource.close();
        eventSource = null;
      }
      if (reconnectTimer) {
        window.clearTimeout(reconnectTimer);
        reconnectTimer = 0;
      }
    };

    const scheduleReconnect = (message: string) => {
      if (cancelled) return;
      const delay =
        RECONNECT_DELAYS_MS[Math.min(reconnectAttempt, RECONNECT_DELAYS_MS.length - 1)];
      reconnectAttempt += 1;
      setConnectionState("reconnecting");
      setStatus({
        tone: "info",
        message: `${message} ${delay / 1000} 秒後重試。`,
      });
      reconnectTimer = window.setTimeout(() => {
        reconnectTimer = 0;
        void connect();
      }, delay);
    };

    const handleSnapshot = (event: Event) => {
      const message = event as MessageEvent<string>;
      const payload = JSON.parse(message.data) as ConsoleSnapshotEvent;
      if (cancelled) return;
      setLogPath(payload.log_path ?? "");
      setLines(trimBufferedLines(payload.lines ?? []));
      setConnectionState("live");
      setStatus(null);
      reconnectAttempt = 0;
    };

    const handleAppend = (event: Event) => {
      const message = event as MessageEvent<string>;
      const payload = JSON.parse(message.data) as ConsoleAppendEvent;
      if (cancelled || !payload.line?.trim()) return;
      setLines((current) => trimBufferedLines([...current, payload.line]));
    };

    const connect = async () => {
      if (cancelled) return;
      clearPending();
      setConnectionState(reconnectAttempt > 0 ? "reconnecting" : "connecting");

      probeController = new AbortController();
      try {
        const response = await fetch(STREAM_URL, {
          method: "GET",
          credentials: "same-origin",
          headers: { Accept: "text/event-stream" },
          signal: probeController.signal,
        });
        if (cancelled) {
          await response.body?.cancel();
          return;
        }
        if (!response.ok) {
          const detail = await readAPIError(response, "無法連線到 Console");
          if (
            detail.code === "setup_required" ||
            detail.code === "auth_required" ||
            detail.code === "session_expired"
          ) {
            dispatchBrowserAuthEvent(detail.code);
            return;
          }
          if (response.status === 503 || detail.code === "console_unavailable") {
            setConnectionState("unavailable");
            setStatus({ tone: "error", message: "Console 不可用" });
            return;
          }
          setConnectionState("error");
          scheduleReconnect(detail.message || "Console 連線失敗，正在重試。");
          return;
        }
        await response.body?.cancel();
      } catch (error) {
        if (cancelled || isAbortError(error)) return;
        setConnectionState("error");
        scheduleReconnect("Console 連線失敗，正在重試。");
        return;
      } finally {
        probeController = null;
      }

      if (cancelled) return;

      const source = new EventSource(STREAM_URL);
      eventSource = source;
      source.addEventListener("snapshot", handleSnapshot);
      source.addEventListener("append", handleAppend);
      source.addEventListener("ping", () => {});
      source.onopen = () => {
        if (cancelled) return;
        setConnectionState("live");
      };
      source.onerror = () => {
        if (cancelled || eventSource !== source) return;
        source.close();
        eventSource = null;
        scheduleReconnect("Console 串流中斷，");
      };
    };

    void connect();

    return () => {
      cancelled = true;
      clearPending();
    };
  }, [reconnectToken]);

  useEffect(() => {
    if (selectedModule === "all") return;
    const hasSelectedModule = lines.some((line) => parseModuleName(line) === selectedModule);
    if (!hasSelectedModule) {
      setSelectedModule("all");
    }
  }, [lines, selectedModule]);

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport || !stickToBottomRef.current) return;
    viewport.scrollTop = viewport.scrollHeight;
  }, [lines, selectedModule]);

  const moduleCounts: Record<string, number> = {};
  for (const line of lines) {
    const moduleName = parseModuleName(line);
    moduleCounts[moduleName] = (moduleCounts[moduleName] ?? 0) + 1;
  }

  const modules = Object.keys(moduleCounts).sort((left, right) => {
    if (left === "other") return 1;
    if (right === "other") return -1;
    return left.localeCompare(right, "en", { sensitivity: "base" });
  });

  const filteredLines =
    selectedModule === "all"
      ? lines
      : lines.filter((line) => parseModuleName(line) === selectedModule);

  const filteredText = filteredLines.join("\n");

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <div className="badge badge-outline badge-sm">Console</div>
                <div className={`badge badge-sm ${connectionBadgeClass(connectionState)}`}>
                  {connectionLabel(connectionState)}
                </div>
              </div>
              <div>
                <h2 className="card-title text-2xl">Runtime Console</h2>
                <p className="text-sm text-base-content/60">
                  延續 Persona 的 master/detail 版型，即時查看目前 web runtime 的 log stream。
                </p>
              </div>
            </div>

            <div className="join">
              <button
                className="btn btn-sm join-item"
                onClick={() => setReconnectToken((value) => value + 1)}
              >
                重新連線
              </button>
              <button
                className="btn btn-ghost btn-sm join-item"
                onClick={() => setLines([])}
                disabled={lines.length === 0}
              >
                清空畫面
              </button>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">連線狀態</div>
              <div className="stat-value text-lg">{connectionLabel(connectionState)}</div>
              <div className="stat-desc">SSE live console stream</div>
            </div>
            <div className="stat">
              <div className="stat-title">Log Path</div>
              <div className="stat-value truncate text-lg">{logPath || "未取得"}</div>
              <div className="stat-desc font-mono">{logPath || "等待 snapshot"}</div>
            </div>
            <div className="stat">
              <div className="stat-title">已載入行數</div>
              <div className="stat-value text-lg">{lines.length}</div>
              <div className="stat-desc">buffer 上限 {MAX_BUFFER_LINES}</div>
            </div>
            <div className="stat">
              <div className="stat-title">Module 數量</div>
              <div className="stat-value text-lg">{modules.length}</div>
              <div className="stat-desc">目前畫面可用 filter</div>
            </div>
          </div>

          {status ? (
            <div className={`alert ${statusClass(status.tone)}`}>
              <span>{status.message}</span>
            </div>
          ) : null}
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(260px,0.75fr)_minmax(0,1.25fr)]">
        <div className="card min-h-[32rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">Module Filter</h3>
              <div className="badge badge-ghost badge-sm">{modules.length + 1}</div>
            </div>

            <SelectionList>
              <SelectionListItem
                selected={selectedModule === "all"}
                onClick={() => setSelectedModule("all")}
              >
                <div className="min-w-0 flex-1 text-left">
                  <div className="flex items-center justify-between gap-3">
                    <div className="font-medium">全部</div>
                    <div className="badge badge-outline badge-sm">{lines.length}</div>
                  </div>
                  <div className="text-sm text-base-content/60">
                    顯示目前 buffer 內的所有 log line
                  </div>
                </div>
              </SelectionListItem>

              {modules.map((moduleName) => (
                <SelectionListItem
                  key={moduleName}
                  selected={selectedModule === moduleName}
                  onClick={() => setSelectedModule(moduleName)}
                >
                  <div className="min-w-0 flex-1 text-left">
                    <div className="flex items-center justify-between gap-3">
                      <div className="truncate font-medium">{moduleName}</div>
                      <div className="badge badge-outline badge-sm">
                        {moduleCounts[moduleName] ?? 0}
                      </div>
                    </div>
                    <div className="text-sm text-base-content/60">
                      {moduleName === "other"
                        ? "不符合標準 module 格式的 log line"
                        : `只顯示 ${moduleName} 模組輸出`}
                    </div>
                  </div>
                </SelectionListItem>
              ))}
            </SelectionList>
          </div>
        </div>

        <div className="card min-h-[32rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-5">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
              <div className="space-y-2">
                <div className="flex flex-wrap items-center gap-2">
                  <div className="badge badge-outline badge-sm">Detail</div>
                  <div className="badge badge-ghost badge-sm font-mono">
                    {selectedModule === "all" ? "all" : selectedModule}
                  </div>
                  <div className="badge badge-primary badge-sm">
                    {filteredLines.length} lines
                  </div>
                </div>
                <div>
                  <h3 className="card-title text-xl">Live Console</h3>
                  <p className="text-sm text-base-content/60">
                    {selectedModule === "all"
                      ? "顯示所有 runtime log。"
                      : `目前只顯示 ${selectedModule} 模組。`}
                  </p>
                </div>
              </div>
            </div>

            {filteredLines.length === 0 ? (
              <div className="alert alert-info">
                <span>
                  {lines.length === 0
                    ? "目前尚未收到任何 console log。"
                    : "目前 filter 下沒有符合的 log line。"}
                </span>
              </div>
            ) : (
              <div
                ref={viewportRef}
                className="h-[32rem] overflow-auto rounded-box border border-base-300 bg-base-100 p-4 [scrollbar-width:thin]"
                onScroll={() => {
                  const viewport = viewportRef.current;
                  if (!viewport) return;
                  const distance =
                    viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
                  stickToBottomRef.current = distance <= 24;
                }}
              >
                <pre className="overflow-x-auto whitespace-pre text-xs leading-6 text-base-content/82">
                  {filteredText}
                </pre>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
