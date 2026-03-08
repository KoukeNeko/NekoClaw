import { useEffect, useState } from "react";
import { getTranscript, getUsageSummary } from "@/api/client";
import type {
  TranscriptEntry,
  UsageSummary,
  UsageSummaryModel,
  UsageSummaryProvider,
  UsageSummarySession,
  UsageSummaryTrendPoint,
} from "@/api/types";
import { useAppStore } from "@/store/appStore";
import {
  formatCost,
  formatDateTime,
  formatDuration,
  formatRelativeTime,
  formatTokens,
} from "@/utils/format";
import { calculateCost } from "@/utils/pricing";
import { openSessionConversation } from "@/utils/sessionNavigation";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type SessionSortMode = "recent" | "tokens" | "cost";

interface RequestDetail {
  id: string;
  created_at: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  estimated_cost_usd: number;
  elapsed_ms: number;
}

interface ShareDatum {
  label: string;
  total_tokens: number;
  request_count: number;
  estimated_cost_usd: number;
  color: string;
}

const SHARE_COLORS = [
  "var(--color-primary)",
  "var(--color-secondary)",
  "var(--color-accent)",
  "var(--color-info)",
  "var(--color-success)",
  "var(--color-warning)",
  "var(--color-error)",
];

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

function readErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) {
    return error.message.trim();
  }
  return fallback;
}

function sortSessions(
  sessions: UsageSummarySession[],
  mode: SessionSortMode,
): UsageSummarySession[] {
  return [...sessions].sort((left, right) => {
    switch (mode) {
      case "tokens":
        if (left.total_tokens !== right.total_tokens) {
          return right.total_tokens - left.total_tokens;
        }
        break;
      case "cost":
        if (left.estimated_cost_usd !== right.estimated_cost_usd) {
          return right.estimated_cost_usd - left.estimated_cost_usd;
        }
        break;
      default:
        break;
    }

    const leftTime = new Date(left.updated_at).getTime();
    const rightTime = new Date(right.updated_at).getTime();
    if (leftTime !== rightTime) {
      return rightTime - leftTime;
    }
    return left.session_id.localeCompare(right.session_id, "en", {
      sensitivity: "base",
    });
  });
}

function normalizeTotalTokens(
  inputTokens: number,
  outputTokens: number,
  totalTokens: number,
): number {
  if (totalTokens > 0) return totalTokens;
  if (inputTokens > 0 || outputTokens > 0) {
    return inputTokens + outputTokens;
  }
  return 0;
}

function toRequestDetails(entries: TranscriptEntry[]): RequestDetail[] {
  return entries
    .filter((entry) => entry.role === "assistant")
    .map((entry, index) => {
      const inputTokens = entry.usage?.input_tokens ?? 0;
      const outputTokens = entry.usage?.output_tokens ?? 0;
      const totalTokens = normalizeTotalTokens(
        inputTokens,
        outputTokens,
        entry.usage?.total_tokens ?? 0,
      );
      return {
        id: `${entry.created_at}-${index}`,
        created_at: entry.created_at,
        model: entry.model ?? "",
        input_tokens: inputTokens,
        output_tokens: outputTokens,
        total_tokens: totalTokens,
        estimated_cost_usd: calculateCost(
          inputTokens,
          outputTokens,
          entry.model ?? "",
        ),
        elapsed_ms: entry.elapsed_ms ?? 0,
      };
    })
    .sort(
      (left, right) =>
        new Date(right.created_at).getTime() - new Date(left.created_at).getTime(),
    )
    .slice(0, 10);
}

function buildProviderShareData(providers: UsageSummaryProvider[]): ShareDatum[] {
  const topProviders = providers.slice(0, 5).map((provider, index) => ({
    label: provider.provider,
    total_tokens: provider.total_tokens,
    request_count: provider.request_count,
    estimated_cost_usd: provider.estimated_cost_usd,
    color: SHARE_COLORS[index % SHARE_COLORS.length],
  }));

  if (providers.length <= 5) {
    return topProviders;
  }

  const remainder = providers.slice(5).reduce(
    (acc, provider) => ({
      total_tokens: acc.total_tokens + provider.total_tokens,
      request_count: acc.request_count + provider.request_count,
      estimated_cost_usd: acc.estimated_cost_usd + provider.estimated_cost_usd,
    }),
    {
      total_tokens: 0,
      request_count: 0,
      estimated_cost_usd: 0,
    },
  );

  return [
    ...topProviders,
    {
      label: "other",
      total_tokens: remainder.total_tokens,
      request_count: remainder.request_count,
      estimated_cost_usd: remainder.estimated_cost_usd,
      color: SHARE_COLORS[topProviders.length % SHARE_COLORS.length],
    },
  ];
}

function buildModelShareData(models: UsageSummaryModel[]): ShareDatum[] {
  return models.map((model, index) => ({
    label: model.model,
    total_tokens: model.total_tokens,
    request_count: model.request_count,
    estimated_cost_usd: model.estimated_cost_usd,
    color: SHARE_COLORS[index % SHARE_COLORS.length],
  }));
}

function sessionLabel(session: UsageSummarySession): string {
  return session.title?.trim() || session.session_id;
}

function UsageStatsGrid({
  summary,
  currentSession,
  currentSessionID,
  contextPercent,
}: {
  summary: UsageSummary;
  currentSession: UsageSummarySession | null;
  currentSessionID: string;
  contextPercent: number;
}) {
  const items = [
    {
      label: "Sessions",
      value: formatTokens(summary.totals.session_count),
      desc: `${formatTokens(summary.totals.message_count)} messages`,
    },
    {
      label: "Requests",
      value: formatTokens(summary.totals.request_count),
      desc: `${formatTokens(summary.totals.input_tokens)} in / ${formatTokens(summary.totals.output_tokens)} out`,
    },
    {
      label: "Total Tokens",
      value: formatTokens(summary.totals.total_tokens),
      desc: `Generated ${formatDateTime(summary.generated_at)}`,
    },
    {
      label: "Estimated Cost",
      value: formatCost(summary.totals.estimated_cost_usd),
      desc: "Unknown models count as $0",
    },
    {
      label: "Compactions",
      value: formatTokens(summary.totals.compaction_count),
      desc: "Session compression events",
    },
    {
      label: "Current Session",
      value: currentSession ? sessionLabel(currentSession) : currentSessionID || "未選擇",
      desc: currentSession
        ? `${formatTokens(currentSession.total_tokens)} tok · ${formatCost(currentSession.estimated_cost_usd)} · Context ${contextPercent}%`
        : `Context ${contextPercent}%`,
      compact: true,
    },
  ];

  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-6">
      {items.map((item) => (
        <div
          key={item.label}
          className="card border border-base-300 bg-base-100 shadow-sm"
        >
          <div className="card-body gap-2 p-4">
            <div className="text-xs uppercase tracking-[0.18em] text-base-content/45">
              {item.label}
            </div>
            <div className={item.compact ? "min-h-[3.25rem]" : ""}>
              <div
                className={`font-semibold tracking-tight ${item.compact ? "line-clamp-2 text-lg" : "text-2xl"
                  }`}
              >
                {item.value}
              </div>
            </div>
            <div className="text-xs text-base-content/60">{item.desc}</div>
          </div>
        </div>
      ))}
    </div>
  );
}

function TrendChart({ points }: { points: UsageSummaryTrendPoint[] }) {
  const maxTotal = Math.max(...points.map((point) => point.total_tokens), 0);
  const recentRequests = points.reduce(
    (sum, point) => sum + point.request_count,
    0,
  );
  const recentTokens = points.reduce((sum, point) => sum + point.total_tokens, 0);
  const axisLabel = (date: string) => date.slice(5).replace("-", "/");
  const inputColor = "var(--color-primary)";
  const outputColor = "var(--color-accent)";
  const statItems = [
    {
      label: "Recent Requests",
      value: `${formatTokens(recentRequests)} 筆`,
    },
    {
      label: "Recent Tokens",
      value: formatTokens(recentTokens),
    },
    {
      label: "Peak Day",
      value: formatTokens(maxTotal),
    },
  ];

  return (
    <div className="card h-full border border-base-300 bg-base-100 shadow-sm">
      <div className="card-body flex h-full flex-col gap-5">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="space-y-2">
            <h3 className="card-title text-lg">14 天 Token 趨勢</h3>
            <p className="max-w-2xl text-sm leading-relaxed text-base-content/60">
              顯示最近兩週 assistant request 的 input/output 堆疊分布。
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <div className="badge badge-primary badge-outline">Input</div>
            <div className="badge badge-accent badge-outline">Output</div>
          </div>
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          {statItems.map((item) => (
            <div
              key={item.label}
              className="rounded-box flex min-h-[8.5rem] flex-col justify-between border border-base-300 bg-base-200/60 px-4 py-4"
            >
              <div className="text-[11px] font-medium uppercase tracking-[0.24em] text-base-content/45">
                {item.label}
              </div>
              <div className="text-xl font-semibold tracking-tight text-base-content/88 tabular-nums">
                {item.value}
              </div>
            </div>
          ))}
        </div>

        {maxTotal === 0 ? (
          <div className="alert alert-info">
            <span>最近 14 天沒有 assistant usage 資料。</span>
          </div>
        ) : (
          <div className="rounded-box flex min-h-[24rem] flex-1 flex-col border border-base-300 bg-base-200/50 p-5">
            <div className="flex h-full flex-col gap-4">
              <div className="grid min-h-[18rem] flex-1 grid-cols-[repeat(14,minmax(0,1fr))] items-end gap-3">
                {points.map((point) => {
                  const inputHeight = (point.input_tokens / maxTotal) * 100;
                  const outputHeight = (point.output_tokens / maxTotal) * 100;
                  return (
                    <div
                      key={point.date}
                      className="flex h-full items-end"
                    >
                      <div
                        className="flex h-full w-full items-end rounded-[1.15rem] bg-base-300/28 p-1.5"
                        title={`${point.date} · ${formatTokens(point.total_tokens)} tok · ${point.request_count} req`}
                      >
                        <div className="flex h-full w-full flex-col justify-end overflow-hidden rounded-[0.9rem] bg-base-300/18">
                          <div
                            style={{
                              height: `${Math.max(outputHeight, point.output_tokens > 0 ? 6 : 0)}%`,
                              backgroundColor: outputColor,
                            }}
                          />
                          <div
                            style={{
                              height: `${Math.max(inputHeight, point.input_tokens > 0 ? 6 : 0)}%`,
                              backgroundColor: inputColor,
                            }}
                          />
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>

              <div className="grid grid-cols-[repeat(14,minmax(0,1fr))] gap-2">
                {points.map((point, index) => {
                  const showLabel =
                    index === 0 ||
                    index === points.length - 1 ||
                    (index % 3 === 0 && index < points.length - 2);
                  return (
                    <div key={point.date} className="text-center text-[10px]">
                      {showLabel ? (
                        <span className="inline-block whitespace-nowrap font-medium text-base-content/72">
                          {axisLabel(point.date)}
                        </span>
                      ) : (
                        <span className="inline-block text-base-content/28">·</span>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function ShareChart({
  title,
  description,
  items,
}: {
  title: string;
  description: string;
  items: ShareDatum[];
}) {
  const totalTokens = items.reduce((sum, item) => sum + item.total_tokens, 0);
  const totalCost = items.reduce(
    (sum, item) => sum + item.estimated_cost_usd,
    0,
  );
  const circumference = 2 * Math.PI * 42;
  let offset = 0;

  return (
    <div className="card h-full border border-base-300 bg-base-100 shadow-sm">
      <div className="card-body h-full gap-5">
        <div className="min-h-[6.5rem] space-y-2">
          <h3 className="card-title text-lg">{title}</h3>
          <p className="text-sm leading-relaxed text-base-content/60">
            {description}
          </p>
        </div>

        {totalTokens === 0 ? (
          <div className="alert alert-info">
            <span>目前沒有可繪製的 token 占比資料。</span>
          </div>
        ) : (
          <div className="flex h-full flex-col gap-5">
            <div className="flex flex-col items-center gap-4 pt-1 text-center">
              <div className="flex h-44 w-44 items-center justify-center">
                <svg viewBox="0 0 120 120" className="h-full w-full">
                  <circle
                    cx="60"
                    cy="60"
                    r="42"
                    fill="none"
                    stroke="var(--color-base-300)"
                    strokeOpacity="0.35"
                    strokeWidth="14"
                  />
                  {items.map((item) => {
                    const ratio = item.total_tokens / totalTokens;
                    const dash = circumference * ratio;
                    const segment = (
                      <circle
                        key={item.label}
                        cx="60"
                        cy="60"
                        r="42"
                        fill="none"
                        stroke={item.color}
                        strokeWidth="14"
                        strokeDasharray={`${dash} ${circumference - dash}`}
                        strokeDashoffset={-offset}
                        transform="rotate(-90 60 60)"
                      />
                    );
                    offset += dash;
                    return segment;
                  })}
                  <text
                    x="60"
                    y="56"
                    textAnchor="middle"
                    className="fill-current text-[8px] font-semibold uppercase tracking-[0.28em]"
                  >
                    Share
                  </text>
                  <text
                    x="60"
                    y="70"
                    textAnchor="middle"
                    className="fill-current text-[12px] font-semibold"
                  >
                    {formatTokens(totalTokens)}
                  </text>
                </svg>
              </div>

              <div className="rounded-box border border-base-300 bg-base-200/60 px-3 py-2 text-center text-xs text-base-content/60">
                Top share {items[0] ? `${((items[0].total_tokens / totalTokens) * 100).toFixed(1)}%` : "0%"}
              </div>

              <div className="grid w-full max-w-md grid-cols-2 gap-4 border-t border-base-300/70 pt-4">
                <div className="flex flex-col items-center text-center">
                  <div className="whitespace-nowrap text-[10px] font-medium uppercase tracking-[0.22em] text-base-content/42">
                    Total Tokens
                  </div>
                  <div className="mt-2 text-2xl font-semibold tabular-nums">
                    {formatTokens(totalTokens)}
                  </div>
                  <div className="text-xs text-base-content/60">
                    {items.length} segments
                  </div>
                </div>
                <div className="flex flex-col items-center text-center">
                  <div className="whitespace-nowrap text-[10px] font-medium uppercase tracking-[0.22em] text-base-content/42">
                    Total Cost
                  </div>
                  <div className="mt-2 text-2xl font-semibold tabular-nums">
                    {formatCost(totalCost)}
                  </div>
                  <div className="max-w-[12rem] text-xs leading-snug text-base-content/60">
                    Unknown models remain $0
                  </div>
                </div>
              </div>
            </div>

            <div className="space-y-3">
              {items.map((item) => {
                const percent =
                  totalTokens === 0 ? 0 : (item.total_tokens / totalTokens) * 100;
                return (
                  <div
                    key={item.label}
                    className="rounded-box border border-base-300 bg-base-200/55 px-4 py-3"
                  >
                    <div className="flex items-center justify-between gap-3">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span
                            className="inline-block size-2.5 rounded-full"
                            style={{ backgroundColor: item.color }}
                          />
                          <span
                            className="truncate font-medium"
                            title={item.label}
                          >
                            {item.label}
                          </span>
                        </div>
                      </div>
                      <div className="shrink-0 text-right">
                        <div className="font-semibold text-base-content/85">
                          {percent.toFixed(1)}%
                        </div>
                      </div>
                    </div>

                    <div className="mt-3 h-2 rounded-full bg-base-300/30">
                      <div
                        className="h-full rounded-full"
                        style={{
                          width: `${Math.max(percent, item.total_tokens > 0 ? 6 : 0)}%`,
                          backgroundColor: item.color,
                        }}
                      />
                    </div>

                    <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-base-content/60">
                      <span>{formatTokens(item.total_tokens)} tok</span>
                      <span>{item.request_count} req</span>
                      <span>{formatCost(item.estimated_cost_usd)}</span>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function SummarySkeleton() {
  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="space-y-2">
              <div className="flex gap-2">
                <div className="skeleton h-5 w-14" />
                <div className="skeleton h-5 w-20" />
              </div>
              <div className="skeleton h-8 w-48" />
              <div className="skeleton h-4 w-80 max-w-full" />
            </div>
            <div className="skeleton h-10 w-28" />
          </div>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-6">
        {Array.from({ length: 6 }).map((_, index) => (
          <div
            key={index}
            className="card border border-base-300 bg-base-100 shadow-sm"
          >
            <div className="card-body gap-3 p-4">
              <div className="skeleton h-3 w-16" />
              <div className="skeleton h-8 w-24" />
              <div className="skeleton h-3 w-28" />
            </div>
          </div>
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.6fr)_minmax(320px,0.7fr)_minmax(320px,0.7fr)]">
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-4">
            <div className="skeleton h-6 w-32" />
            <div className="skeleton h-52 w-full" />
          </div>
        </div>
        {Array.from({ length: 2 }).map((_, index) => (
          <div
            key={index}
            className="card border border-base-300 bg-base-100 shadow-sm"
          >
            <div className="card-body gap-4">
              <div className="skeleton h-6 w-28" />
              <div className="skeleton h-36 w-full rounded-full" />
              <div className="space-y-2">
                <div className="skeleton h-12 w-full" />
                <div className="skeleton h-12 w-full" />
                <div className="skeleton h-12 w-full" />
              </div>
            </div>
          </div>
        ))}
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(320px,0.9fr)_minmax(0,1.3fr)]">
        {Array.from({ length: 2 }).map((_, index) => (
          <div
            key={index}
            className="card border border-base-300 bg-base-200 shadow-sm"
          >
            <div className="card-body gap-4">
              <div className="skeleton h-6 w-40" />
              <div className="space-y-3">
                <div className="skeleton h-16 w-full" />
                <div className="skeleton h-16 w-full" />
                <div className="skeleton h-16 w-full" />
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function UsagePanel() {
  const sessionID = useAppStore((s) => s.sessionID);
  const contextPercent = useAppStore((s) => s.contextPercent);

  const [summary, setSummary] = useState<UsageSummary | null>(null);
  const [selectedSessionID, setSelectedSessionID] = useState("");
  const [sortMode, setSortMode] = useState<SessionSortMode>("recent");
  const [requestDetails, setRequestDetails] = useState<RequestDetail[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [detailReloadToken, setDetailReloadToken] = useState(0);
  const [status, setStatus] = useState<StatusState>(null);

  const sessions = sortSessions(summary?.sessions ?? [], sortMode);
  const selectedSession =
    sessions.find((session) => session.session_id === selectedSessionID) ?? null;
  const currentSession =
    summary?.sessions.find((session) => session.session_id === sessionID) ?? null;
  const providerShare = buildProviderShareData(summary?.providers ?? []);
  const modelShare = buildModelShareData(summary?.models ?? []);

  async function loadSummary(showLoading: boolean, announce: boolean) {
    if (showLoading) {
      setLoading(true);
    } else {
      setRefreshing(true);
    }

    try {
      const loadedSummary = await getUsageSummary();
      setSummary(loadedSummary);
      setDetailReloadToken((value) => value + 1);
      if (announce) {
        setStatus({ tone: "success", message: "用量摘要已重新整理" });
      }
    } catch (error) {
      setStatus({
        tone: "error",
        message: readErrorMessage(error, "無法載入用量摘要"),
      });
    } finally {
      if (showLoading) {
        setLoading(false);
      } else {
        setRefreshing(false);
      }
    }
  }

  useEffect(() => {
    void loadSummary(true, false);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2200);
    return () => window.clearTimeout(timer);
  }, [status]);

  useEffect(() => {
    if (!sessions.length) {
      if (selectedSessionID !== "") {
        setSelectedSessionID("");
      }
      return;
    }

    if (selectedSessionID && sessions.some((session) => session.session_id === selectedSessionID)) {
      return;
    }

    const preferred =
      sessions.find((session) => session.session_id === sessionID) ?? sessions[0];
    setSelectedSessionID(preferred.session_id);
  }, [sessionID, selectedSessionID, sessions]);

  useEffect(() => {
    if (!selectedSessionID) {
      setRequestDetails([]);
      setDetailError("");
      setDetailLoading(false);
      return;
    }

    let cancelled = false;
    setDetailLoading(true);
    setDetailError("");

    void (async () => {
      try {
        const transcript = await getTranscript(selectedSessionID);
        if (cancelled) return;
        setRequestDetails(toRequestDetails(transcript));
      } catch (error) {
        if (cancelled) return;
        setRequestDetails([]);
        setDetailError(readErrorMessage(error, "無法載入 session 明細"));
      } finally {
        if (!cancelled) {
          setDetailLoading(false);
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [detailReloadToken, selectedSessionID]);

  async function handleRefresh() {
    if (refreshing || loading) return;
    await loadSummary(false, true);
  }

  async function handleOpenConversation() {
    if (!selectedSession) return;
    try {
      await openSessionConversation(selectedSession.session_id);
    } catch (error) {
      setStatus({
        tone: "error",
        message: readErrorMessage(error, "載入對話失敗"),
      });
    }
  }

  if (loading) {
    return <SummarySkeleton />;
  }

  if (!summary) {
    return (
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="alert alert-error">
            <span>目前無法取得用量摘要。</span>
          </div>
          <div className="card-actions justify-end">
            <button className="btn btn-primary btn-sm" onClick={handleRefresh}>
              重新整理
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <div className="badge badge-outline badge-sm">Usage</div>
                <div className="badge badge-primary badge-sm">All-time</div>
              </div>
              <div>
                <h2 className="card-title text-2xl">用量總覽</h2>
                <p className="text-sm text-base-content/60">
                  集中查看歷史 session token、成本與最近請求明細。
                </p>
              </div>
            </div>

            <div className="join">
              <button
                className="btn btn-sm join-item"
                onClick={handleRefresh}
                disabled={refreshing}
              >
                {refreshing && (
                  <span className="loading loading-spinner loading-xs" />
                )}
                重新整理
              </button>
            </div>
          </div>

          <div className="rounded-box border border-base-300 bg-base-100 px-4 py-3 text-sm text-base-content/60">
            Summary generated at {formatDateTime(summary.generated_at)}，最近趨勢固定顯示 14 天。
          </div>
        </div>
      </div>

      <UsageStatsGrid
        summary={summary}
        currentSession={currentSession}
        currentSessionID={sessionID}
        contextPercent={contextPercent}
      />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(360px,0.82fr)_minmax(360px,0.82fr)]">
        <TrendChart points={summary.trend} />
        <ShareChart
          title="Provider 占比"
          description="依總 token 排列，超過 5 個 provider 併入 other。"
          items={providerShare}
        />
        <ShareChart
          title="Model 占比"
          description="後端已合併 top 6 之外的 model 成 other。"
          items={modelShare}
        />
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(320px,0.9fr)_minmax(0,1.3fr)]">
        <div className="card min-h-[34rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4 p-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h3 className="card-title text-lg">Session Explorer</h3>
                <p className="text-sm text-base-content/60">
                  可依最近更新、總 tokens 或成本排序。
                </p>
              </div>
              <div className="badge badge-ghost badge-sm">
                {sessions.length} sessions
              </div>
            </div>

            <div className="join">
              <button
                className={`btn btn-sm join-item ${sortMode === "recent" ? "btn-primary" : ""}`}
                onClick={() => setSortMode("recent")}
              >
                recent
              </button>
              <button
                className={`btn btn-sm join-item ${sortMode === "tokens" ? "btn-primary" : ""}`}
                onClick={() => setSortMode("tokens")}
              >
                tokens
              </button>
              <button
                className={`btn btn-sm join-item ${sortMode === "cost" ? "btn-primary" : ""}`}
                onClick={() => setSortMode("cost")}
              >
                cost
              </button>
            </div>

            {sessions.length === 0 ? (
              <div className="card border border-dashed border-base-300 bg-base-100 shadow-sm">
                <div className="card-body">
                  <div className="alert alert-info">
                    <span>目前沒有可分析的 session。</span>
                  </div>
                </div>
              </div>
            ) : (
              <div className="rounded-box border border-base-300 bg-base-100 p-3">
                <div className="space-y-2">
                  {sessions.map((session) => {
                    const isActive = session.session_id === selectedSessionID;
                    return (
                      <button
                        key={session.session_id}
                        className={`w-full rounded-box border px-4 py-4 text-left transition-colors ${isActive
                            ? "border-base-300 bg-base-300/40"
                            : "border-transparent bg-transparent hover:border-base-300 hover:bg-base-200/50"
                          }`}
                        onClick={() => setSelectedSessionID(session.session_id)}
                      >
                        <div className="space-y-3">
                          <div className="flex items-start gap-3">
                            <div className="min-w-0 flex-1">
                              <div className="truncate text-xl font-semibold">
                                {sessionLabel(session)}
                              </div>
                              {(session.top_provider || session.top_model) && (
                                <div className="mt-2 flex flex-wrap gap-2">
                                  {session.top_provider && (
                                    <span className="badge badge-outline badge-sm">
                                      {session.top_provider}
                                    </span>
                                  )}
                                  {session.top_model && (
                                    <span className="badge badge-ghost badge-sm">
                                      {session.top_model}
                                    </span>
                                  )}
                                </div>
                              )}
                            </div>
                            <span className="shrink-0 pt-1 text-xs text-base-content/45">
                              {formatRelativeTime(session.updated_at)}
                            </span>
                          </div>

                          <div className="grid grid-cols-3 gap-2 text-xs">
                            <div className="rounded-box bg-base-200/70 px-3 py-2">
                              <div className="text-[10px] uppercase tracking-[0.16em] text-base-content/40">
                                Tokens
                              </div>
                              <div className="mt-1 font-mono text-sm text-base-content/70">
                                {formatTokens(session.total_tokens)} tok
                              </div>
                            </div>
                            <div className="rounded-box bg-base-200/70 px-3 py-2">
                              <div className="text-[10px] uppercase tracking-[0.16em] text-base-content/40">
                                Cost
                              </div>
                              <div className="mt-1 font-mono text-sm text-base-content/70">
                                {formatCost(session.estimated_cost_usd)}
                              </div>
                            </div>
                            <div className="rounded-box bg-base-200/70 px-3 py-2">
                              <div className="text-[10px] uppercase tracking-[0.16em] text-base-content/40">
                                Requests
                              </div>
                              <div className="mt-1 font-mono text-sm text-base-content/70">
                                {session.request_count} req
                              </div>
                            </div>
                          </div>
                        </div>
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
        </div>

        <div className="card min-h-[34rem] border border-base-300 bg-base-200 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h3 className="card-title text-lg">Session Detail</h3>
                <p className="text-sm text-base-content/60">
                  顯示選取 session 的摘要與最近 10 筆 assistant requests。
                </p>
              </div>
              <div className="join">
                <button
                  className="btn btn-primary btn-sm join-item"
                  onClick={handleOpenConversation}
                  disabled={!selectedSession}
                >
                  打開對話
                </button>
              </div>
            </div>

            {!selectedSession ? (
              <div className="alert alert-info">
                <span>選擇一個 session 來查看明細。</span>
              </div>
            ) : (
              <>
                <div className="space-y-3">
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <h4 className="text-xl font-semibold">
                        {sessionLabel(selectedSession)}
                      </h4>
                      {selectedSession.session_id === sessionID && (
                        <div className="badge badge-primary">Current</div>
                      )}
                    </div>
                    <div className="mt-2 flex flex-wrap gap-2">
                      <div className="badge badge-outline badge-sm font-mono">
                        {selectedSession.session_id}
                      </div>
                      {selectedSession.top_provider && (
                        <div className="badge badge-secondary badge-sm">
                          {selectedSession.top_provider}
                        </div>
                      )}
                      {selectedSession.top_model && (
                        <div className="badge badge-accent badge-sm">
                          {selectedSession.top_model}
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                    <div className="rounded-box border border-base-300 bg-base-100 px-3 py-3">
                      <div className="text-xs uppercase tracking-[0.18em] text-base-content/45">
                        Requests
                      </div>
                      <div className="mt-2 text-xl font-semibold">
                        {formatTokens(selectedSession.request_count)}
                      </div>
                    </div>
                    <div className="rounded-box border border-base-300 bg-base-100 px-3 py-3">
                      <div className="text-xs uppercase tracking-[0.18em] text-base-content/45">
                        Tokens
                      </div>
                      <div className="mt-2 text-xl font-semibold">
                        {formatTokens(selectedSession.total_tokens)}
                      </div>
                    </div>
                    <div className="rounded-box border border-base-300 bg-base-100 px-3 py-3">
                      <div className="text-xs uppercase tracking-[0.18em] text-base-content/45">
                        Cost
                      </div>
                      <div className="mt-2 text-xl font-semibold">
                        {formatCost(selectedSession.estimated_cost_usd)}
                      </div>
                    </div>
                    <div className="rounded-box border border-base-300 bg-base-100 px-3 py-3">
                      <div className="text-xs uppercase tracking-[0.18em] text-base-content/45">
                        Compactions
                      </div>
                      <div className="mt-2 text-xl font-semibold">
                        {formatTokens(selectedSession.compaction_count)}
                      </div>
                    </div>
                  </div>

                  <ul className="list rounded-box border border-base-300 bg-base-100">
                    <li className="list-row">
                      <div>
                        <div className="text-xs uppercase tracking-wide text-base-content/50">
                          Created
                        </div>
                        <div className="font-medium">
                          {formatDateTime(selectedSession.created_at)}
                        </div>
                      </div>
                    </li>
                    <li className="list-row">
                      <div>
                        <div className="text-xs uppercase tracking-wide text-base-content/50">
                          Updated
                        </div>
                        <div className="font-medium">
                          {formatDateTime(selectedSession.updated_at)} ·{" "}
                          <span className="text-base-content/60">
                            {formatRelativeTime(selectedSession.updated_at)}
                          </span>
                        </div>
                      </div>
                    </li>
                    <li className="list-row">
                      <div>
                        <div className="text-xs uppercase tracking-wide text-base-content/50">
                          Messages
                        </div>
                        <div className="font-medium">
                          {formatTokens(selectedSession.message_count)}
                        </div>
                      </div>
                    </li>
                  </ul>
                </div>

                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <h4 className="text-base font-semibold">最近 10 筆 Requests</h4>
                    <div className="badge badge-ghost badge-sm">
                      {requestDetails.length} rows
                    </div>
                  </div>

                  {detailLoading ? (
                    <div className="space-y-2">
                      {Array.from({ length: 5 }).map((_, index) => (
                        <div
                          key={index}
                          className="skeleton h-12 w-full rounded-box"
                        />
                      ))}
                    </div>
                  ) : detailError ? (
                    <div className="alert alert-error">
                      <span>{detailError}</span>
                    </div>
                  ) : requestDetails.length === 0 ? (
                    <div className="alert alert-info">
                      <span>這個 session 目前沒有 assistant request 明細。</span>
                    </div>
                  ) : (
                    <div className="overflow-x-auto rounded-box border border-base-300 bg-base-100">
                      <table className="table table-sm">
                        <thead>
                          <tr>
                            <th>time</th>
                            <th>model</th>
                            <th className="text-right">input</th>
                            <th className="text-right">output</th>
                            <th className="text-right">total</th>
                            <th className="text-right">cost</th>
                            <th className="text-right">elapsed</th>
                          </tr>
                        </thead>
                        <tbody>
                          {requestDetails.map((detail) => (
                            <tr key={detail.id}>
                              <td className="whitespace-nowrap text-xs text-base-content/70">
                                {formatDateTime(detail.created_at)}
                              </td>
                              <td className="max-w-56 truncate font-mono text-xs">
                                {detail.model || "—"}
                              </td>
                              <td className="text-right font-mono text-xs">
                                {formatTokens(detail.input_tokens)}
                              </td>
                              <td className="text-right font-mono text-xs">
                                {formatTokens(detail.output_tokens)}
                              </td>
                              <td className="text-right font-mono text-xs">
                                {formatTokens(detail.total_tokens)}
                              </td>
                              <td className="text-right font-mono text-xs">
                                {formatCost(detail.estimated_cost_usd)}
                              </td>
                              <td className="text-right font-mono text-xs">
                                {detail.elapsed_ms > 0
                                  ? formatDuration(detail.elapsed_ms)
                                  : "—"}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>
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
