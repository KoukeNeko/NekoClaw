import { useEffect, useRef, useState } from "react";
import { searchMemory } from "@/api/client";
import type { MemorySearchResult } from "@/api/types";
import { useAppStore } from "@/store/appStore";
import { formatDateTime, formatRelativeTime } from "@/utils/format";
import { openSessionConversation } from "@/utils/sessionNavigation";

const SEARCH_LIMIT = 10;
const SEARCH_DEBOUNCE_MS = 300;

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;

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

function resultKey(result: MemorySearchResult): string {
  if (result.entry_id) return `entry:${result.entry_id}`;
  if (result.path) {
    return `path:${result.path}:${result.start_line ?? 0}:${result.end_line ?? 0}:${result.timestamp}`;
  }
  return `fallback:${result.source}:${result.session_id}:${result.content}`;
}

function pickSelectedKey(
  currentKey: string,
  nextResults: MemorySearchResult[],
): string {
  if (
    currentKey &&
    nextResults.some((result) => resultKey(result) === currentKey)
  ) {
    return currentKey;
  }
  return nextResults[0] ? resultKey(nextResults[0]) : "";
}

function readErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) {
    return error.message.trim();
  }
  return fallback;
}

function previewText(content: string, maxLength = 160): string {
  const compact = content.replace(/\s+/g, " ").trim();
  if (compact.length <= maxLength) return compact;
  return `${compact.slice(0, maxLength).trimEnd()}…`;
}

function lineRangeLabel(result: MemorySearchResult): string {
  if (!result.start_line && !result.end_line) return "未提供";
  if (result.start_line && result.end_line && result.start_line !== result.end_line) {
    return `L${result.start_line}-${result.end_line}`;
  }
  return `L${result.start_line || result.end_line}`;
}

function formatSourceLabel(source: string): string {
  switch (source) {
    case "sessions":
      return "Session";
    case "memory":
      return "Memory";
    default:
      return source || "Unknown";
  }
}

function formatRoleLabel(role: string): string {
  switch (role) {
    case "assistant":
      return "Assistant";
    case "user":
      return "User";
    case "system":
      return "System";
    case "tool":
      return "Tool";
    default:
      return role || "未分類";
  }
}

function resolveSessionTitle(
  result: MemorySearchResult,
  sessions: ReturnType<typeof useAppStore.getState>["sessions"],
): string {
  const matched = sessions.find((session) => session.session_id === result.session_id);
  return matched?.title?.trim() || result.session_id || "未知對話";
}

function resultTitle(
  result: MemorySearchResult,
  sessions: ReturnType<typeof useAppStore.getState>["sessions"],
): string {
  if (result.source === "sessions") return resolveSessionTitle(result, sessions);
  if (result.path) {
    const parts = result.path.split("/");
    return parts[parts.length - 1] || result.path;
  }
  return "記憶片段";
}

export function MemoryPanel() {
  const sessions = useAppStore((s) => s.sessions);

  const [query, setQuery] = useState("");
  const [results, setResults] = useState<MemorySearchResult[]>([]);
  const [selectedKey, setSelectedKey] = useState("");
  const [searching, setSearching] = useState(false);
  const [errorMessage, setErrorMessage] = useState("");
  const [openTargetKey, setOpenTargetKey] = useState("");
  const [status, setStatus] = useState<StatusState>(null);
  const requestSeq = useRef(0);

  const trimmedQuery = query.trim();
  const sessionResultCount = results.filter((result) => result.source === "sessions").length;
  const memoryResultCount = results.filter((result) => result.source === "memory").length;
  const selectedResult =
    results.find((result) => resultKey(result) === selectedKey) ?? results[0] ?? null;

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  useEffect(() => {
    if (!trimmedQuery) {
      requestSeq.current += 1;
      setSearching(false);
      setErrorMessage("");
      setResults([]);
      setSelectedKey("");
      return;
    }

    const seq = ++requestSeq.current;
    setSearching(true);
    setErrorMessage("");

    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const response = await searchMemory({
            query: trimmedQuery,
            limit: SEARCH_LIMIT,
          });
          if (requestSeq.current !== seq) return;
          const loadedResults = response.results ?? [];
          setResults(loadedResults);
          setSelectedKey((currentKey) => pickSelectedKey(currentKey, loadedResults));
        } catch (error) {
          if (requestSeq.current !== seq) return;
          setResults([]);
          setSelectedKey("");
          setErrorMessage(readErrorMessage(error, "記憶搜尋失敗"));
        } finally {
          if (requestSeq.current === seq) {
            setSearching(false);
          }
        }
      })();
    }, SEARCH_DEBOUNCE_MS);

    return () => window.clearTimeout(timer);
  }, [trimmedQuery]);

  async function handleOpenSession() {
    if (!selectedResult || selectedResult.source !== "sessions" || !selectedResult.session_id) {
      return;
    }

    const currentKey = resultKey(selectedResult);
    setOpenTargetKey(currentKey);
    try {
      await openSessionConversation(selectedResult.session_id);
    } catch (error) {
      setStatus({
        tone: "error",
        message: readErrorMessage(error, "載入對話失敗"),
      });
    } finally {
      setOpenTargetKey("");
    }
  }

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <div className="badge badge-outline badge-sm">Memory</div>
                <div className="badge badge-info badge-sm">Auto Search</div>
              </div>
              <div>
                <h2 className="card-title text-2xl">記憶搜尋</h2>
                <p className="text-sm text-base-content/60">
                  使用既有 memory search API 搜尋 session 與持久化記憶片段，並在同一版型內檢視詳情。
                </p>
              </div>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">目前查詢</div>
              <div className="stat-value break-all text-lg">
                {trimmedQuery || "未搜尋"}
              </div>
              <div className="stat-desc">
                {trimmedQuery ? "停止輸入後 300ms 自動搜尋" : "輸入關鍵字開始搜尋"}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">搜尋結果</div>
              <div className="stat-value text-lg">{results.length}</div>
              <div className="stat-desc">
                {searching ? "搜尋中..." : `最多顯示 ${SEARCH_LIMIT} 筆結果`}
              </div>
            </div>
            <div className="stat">
              <div className="stat-title">來源分布</div>
              <div className="stat-value text-lg">
                {sessionResultCount} / {memoryResultCount}
              </div>
              <div className="stat-desc">Session / Memory</div>
            </div>
          </div>
        </div>
      </div>

      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="join w-full">
            <label className="input input-bordered join-item flex w-full items-center gap-2 focus-within:outline-none">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                className="size-4 shrink-0 opacity-60"
              >
                <circle cx="11" cy="11" r="7" />
                <path d="m20 20-3.5-3.5" />
              </svg>
              <input
                type="text"
                className="grow"
                placeholder="搜尋 session 內容、MEMORY.md 或 daily log"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                spellCheck={false}
              />
            </label>
            <button
              className="btn join-item"
              onClick={() => setQuery("")}
              disabled={!query}
            >
              清除
            </button>
          </div>
          <p className="text-sm text-base-content/60">
            不需按送出。輸入後會自動查詢，結果維持 server 排序，不在前端重排。
          </p>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]">
        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4 p-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">搜尋結果</h3>
              <div className="badge badge-ghost badge-sm">{results.length}</div>
            </div>

            {!trimmedQuery ? (
              <div className="card border border-dashed border-base-300 bg-base-100 shadow-sm">
                <div className="card-body">
                  <div className="alert alert-info">
                    <span>輸入關鍵字開始搜尋記憶內容。</span>
                  </div>
                  <p className="text-sm text-base-content/60">
                    會同時搜尋對話紀錄、`MEMORY.md`、daily logs 與 `memory/*.md`。
                  </p>
                </div>
              </div>
            ) : searching ? (
              <div className="space-y-3">
                <div className="alert alert-info">
                  <span className="flex items-center gap-2">
                    <span className="loading loading-spinner loading-sm" />
                    搜尋中...
                  </span>
                </div>
                {Array.from({ length: 3 }).map((_, index) => (
                  <div
                    key={index}
                    className="card border border-base-300 bg-base-100 shadow-sm"
                  >
                    <div className="card-body gap-3 p-4">
                      <div className="skeleton h-5 w-36" />
                      <div className="skeleton h-4 w-48" />
                      <div className="skeleton h-4 w-full" />
                    </div>
                  </div>
                ))}
              </div>
            ) : errorMessage ? (
              <div className="card border border-base-300 bg-base-100 shadow-sm">
                <div className="card-body gap-4">
                  <div className="alert alert-error">
                    <span>{errorMessage}</span>
                  </div>
                  <p className="text-sm text-base-content/60">
                    請調整查詢內容後再試一次。
                  </p>
                </div>
              </div>
            ) : results.length === 0 ? (
              <div className="card border border-base-300 bg-base-100 shadow-sm">
                <div className="card-body gap-4">
                  <div className="alert alert-info">
                    <span>找不到符合條件的記憶片段。</span>
                  </div>
                  <p className="text-sm text-base-content/60">
                    可以換個關鍵字，或縮短查詢字串再試一次。
                  </p>
                </div>
              </div>
            ) : (
              <ul className="menu rounded-box border border-base-300 bg-base-100 p-2">
                {results.map((result) => {
                  const key = resultKey(result);
                  const isSelected = selectedResult ? resultKey(selectedResult) === key : false;
                  const isSessionSource = result.source === "sessions";

                  return (
                    <li key={key}>
                      <button
                        className={isSelected ? "active" : ""}
                        onClick={() => setSelectedKey(key)}
                      >
                        <div className="min-w-0 flex-1 text-left">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="truncate font-semibold">
                              {resultTitle(result, sessions)}
                            </span>
                            <span className="badge badge-outline badge-xs">
                              {formatSourceLabel(result.source)}
                            </span>
                            {result.role && (
                              <span className="badge badge-ghost badge-xs">
                                {formatRoleLabel(result.role)}
                              </span>
                            )}
                          </div>
                          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-base-content/45">
                            <span>
                              {isSessionSource
                                ? result.session_id
                                : result.path || "未知檔案"}
                            </span>
                            {!isSessionSource && <span>{lineRangeLabel(result)}</span>}
                            <span>{formatRelativeTime(result.timestamp)}</span>
                            <span>score {result.score.toFixed(2)}</span>
                          </div>
                          <div className="mt-1 line-clamp-2 text-xs text-base-content/65">
                            {previewText(result.content)}
                          </div>
                        </div>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>

        <div className="card min-h-[28rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-3">
              <h3 className="card-title text-lg">結果詳情</h3>
              {selectedResult ? (
                <div className="badge badge-outline badge-sm">
                  {formatSourceLabel(selectedResult.source)}
                </div>
              ) : (
                <div className="badge badge-ghost badge-sm">未選取</div>
              )}
            </div>

            {!selectedResult ? (
              <div className="alert alert-info">
                <span>選擇一筆搜尋結果來檢視完整內容與 metadata。</span>
              </div>
            ) : (
              <>
                <div className="space-y-3">
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <h4 className="text-xl font-semibold">
                        {resultTitle(selectedResult, sessions)}
                      </h4>
                      {selectedResult.role && (
                        <div className="badge badge-ghost">
                          {formatRoleLabel(selectedResult.role)}
                        </div>
                      )}
                    </div>
                    <p className="mt-2 text-sm text-base-content/70">
                      {selectedResult.source === "sessions"
                        ? `來自對話 ${resolveSessionTitle(selectedResult, sessions)} 的搜尋命中。`
                        : `來自 ${selectedResult.path || "memory file"} 的搜尋命中。`}
                    </p>
                  </div>

                  <div className="flex flex-wrap gap-2">
                    <div className="badge badge-outline badge-sm">
                      {formatSourceLabel(selectedResult.source)}
                    </div>
                    <div className="badge badge-ghost badge-sm">
                      {formatDateTime(selectedResult.timestamp)}
                    </div>
                    <div className="badge badge-ghost badge-sm">
                      score {selectedResult.score.toFixed(2)}
                    </div>
                    {selectedResult.source !== "sessions" && (
                      <div className="badge badge-ghost badge-sm">
                        {lineRangeLabel(selectedResult)}
                      </div>
                    )}
                  </div>
                </div>

                <ul className="list rounded-box border border-base-300 bg-base-100">
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Source
                      </div>
                      <div className="font-medium">
                        {formatSourceLabel(selectedResult.source)}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        {selectedResult.source === "sessions" ? "Session" : "Path"}
                      </div>
                      <div className="font-mono text-sm">
                        {selectedResult.source === "sessions"
                          ? selectedResult.session_id
                          : selectedResult.path || "未提供"}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Timestamp
                      </div>
                      <div className="text-sm text-base-content/75">
                        {formatDateTime(selectedResult.timestamp)} (
                        {formatRelativeTime(selectedResult.timestamp)})
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Score
                      </div>
                      <div className="font-medium">{selectedResult.score.toFixed(4)}</div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Role
                      </div>
                      <div className="text-sm text-base-content/75">
                        {formatRoleLabel(selectedResult.role)}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Line Range
                      </div>
                      <div className="font-mono text-sm">
                        {selectedResult.source === "sessions"
                          ? "不適用"
                          : lineRangeLabel(selectedResult)}
                      </div>
                    </div>
                  </li>
                  <li className="list-row">
                    <div>
                      <div className="text-xs uppercase tracking-wide text-base-content/50">
                        Entry ID
                      </div>
                      <div className="font-mono text-sm">
                        {selectedResult.entry_id || "未提供"}
                      </div>
                    </div>
                  </li>
                </ul>

                <div className="card border border-base-300 bg-base-100 shadow-sm">
                  <div className="card-body gap-3 p-4">
                    <div className="flex items-center justify-between gap-3">
                      <h4 className="font-semibold">內容片段</h4>
                      <div className="text-xs text-base-content/55">
                        {selectedResult.content.trim().length} chars
                      </div>
                    </div>
                    <pre className="whitespace-pre-wrap break-words text-sm text-base-content/80">
                      {selectedResult.content.trim() || "沒有內容"}
                    </pre>
                  </div>
                </div>

                {selectedResult.source === "sessions" && (
                  <div className="card-actions justify-end">
                    <button
                      className="btn btn-primary"
                      onClick={handleOpenSession}
                      disabled={
                        !selectedResult.session_id || openTargetKey !== ""
                      }
                    >
                      {openTargetKey === resultKey(selectedResult) && (
                        <span className="loading loading-spinner loading-xs" />
                      )}
                      開啟對話
                    </button>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      </div>

      {status && (
        <div className="toast toast-end toast-bottom z-50">
          <div className={`alert shadow-lg ${statusClass(status.tone)}`}>
            <span>{status.message}</span>
          </div>
        </div>
      )}
    </div>
  );
}
