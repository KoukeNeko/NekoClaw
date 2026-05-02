import { useMemo } from "react";
import type { ChatMessage } from "@/store/appStore";
import { renderMarkdown } from "@/utils/markdown";
import { formatTokens, formatDuration } from "@/utils/format";

interface Props {
  message: ChatMessage;
}

const reminderLabels: Record<string, string> = {
  respect_tool_denial: "遵守工具拒絕",
  active_plan_in_progress: "Plan 仍在進行",
  repeat_failure_change_strategy: "改變策略",
  read_only_loop_breakout: "停止只讀迴圈",
};

/**
 * Single message bubble. User messages use chat-end, assistant uses chat-start.
 * Renders markdown for assistant messages. Shows metadata footer for responses.
 *
 * Note: dangerouslySetInnerHTML is used intentionally here because:
 * 1. Content comes from our own LLM responses (not user-submitted HTML)
 * 2. marked.js escapes HTML entities in the markdown source by default
 * 3. The only HTML generated is from marked's own rendering pipeline
 */
export function MessageBubble({ message }: Props) {
  const isUser = message.role === "user";
  const isError = message.role === "error";
  const hasGrounding =
    message.role === "assistant" &&
    Boolean(
      message.grounding &&
        ((message.grounding.sources?.length ?? 0) > 0 ||
          (message.grounding.search_queries?.length ?? 0) > 0),
    );
  const hasFooter =
    message.role === "assistant" &&
    (Boolean(message.usage) ||
      (message.toolEvents?.length ?? 0) > 0 ||
      (message.reminders?.length ?? 0) > 0 ||
      message.elapsed != null ||
      hasGrounding ||
      Boolean(message.provider && message.model));

  const htmlContent = useMemo(() => {
    if (isUser || isError) return null;
    if (!message.content) return "";
    return renderMarkdown(message.content);
  }, [message.content, isUser, isError]);

  return (
    <div className={`chat ${isUser ? "chat-end" : "chat-start"}`}>
      <div
        className={`chat-bubble max-w-full ${isUser
          ? "chat-bubble-primary"
          : isError
            ? "chat-bubble-error"
            : ""
          }`}
      >
        {/* Image attachments */}
        {message.images && message.images.length > 0 && (
          <div className="flex flex-wrap gap-2 mb-2">
            {message.images.map((img, i) => (
              <div
                key={i}
                className="badge badge-outline badge-sm gap-1"
              >
                📎 {img.file_name || `image-${i + 1}`}
              </div>
            ))}
          </div>
        )}

        {/* Message content */}
        {isUser || isError ? (
          <div className="whitespace-pre-wrap break-words">
            {message.content}
          </div>
        ) : (
          <div
            className="prose prose-sm max-w-none break-words text-inherit prose-headings:text-inherit prose-strong:text-inherit prose-a:text-primary prose-pre:bg-base-200 prose-pre:rounded-box prose-code:bg-base-200 prose-code:rounded-btn prose-code:px-1.5 prose-code:py-0.5 prose-code:text-[0.875em]"
            // Content is from our LLM pipeline via marked.js which escapes
            // HTML entities. This is safe server-generated content.
            dangerouslySetInnerHTML={{ __html: htmlContent ?? "" }}
          />
        )}
      </div>

      {/* Metadata footer for assistant messages */}
      {hasFooter && (
        <div className="chat-footer text-xs text-base-content/40 mt-1 space-y-0.5 flex-col">
          {(message.elapsed != null || message.usage || (message.provider && message.model)) && (
            <div className="flex flex-wrap items-center gap-x-1">
              {message.elapsed != null && (
                <>
                  <span>⏱ {formatDuration(message.elapsed)}</span>
                  {(message.usage || (message.provider && message.model)) && (
                    <span className="opacity-40">·</span>
                  )}
                </>
              )}
              {message.usage && (
                <>
                  <span>
                    ↑{formatTokens(message.usage.input_tokens)} ↓
                    {formatTokens(message.usage.output_tokens)}
                    {message.usage.total_tokens > 0 && (
                      <> ({formatTokens(message.usage.total_tokens)})</>
                    )}
                  </span>
                  {message.elapsed != null && message.elapsed > 0 && (
                    <>
                      <span className="opacity-40">·</span>
                      <span>
                        {Math.round(message.usage.output_tokens / (message.elapsed / 1000))} tok/s
                      </span>
                    </>
                  )}
                  {message.provider && message.model && (
                    <span className="opacity-40">·</span>
                  )}
                </>
              )}
              {message.provider && message.model && (
                <span>{message.provider}/{message.model}</span>
              )}
            </div>
          )}
          {hasGrounding && message.grounding && (
            <div className="space-y-1" data-testid="grounding-block">
              {message.grounding.search_queries && message.grounding.search_queries.length > 0 && (
                <div className="flex flex-wrap items-center gap-1">
                  <span className="opacity-60">🔍</span>
                  {message.grounding.search_queries.map((query, i) => (
                    <span
                      key={`q-${i}`}
                      className="badge badge-ghost badge-xs"
                      data-testid="grounding-query"
                    >
                      {query}
                    </span>
                  ))}
                </div>
              )}
              {message.grounding.sources && message.grounding.sources.length > 0 && (
                <div className="flex flex-wrap items-center gap-1">
                  <span className="opacity-60">Sources:</span>
                  {message.grounding.sources.map((source, i) => {
                    const host = (() => {
                      try {
                        return source.uri ? new URL(source.uri).hostname : source.title || "source";
                      } catch {
                        return source.title || source.uri || "source";
                      }
                    })();
                    return source.uri ? (
                      <a
                        key={`s-${i}`}
                        href={source.uri}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="badge badge-outline badge-xs hover:badge-primary"
                        title={source.title || source.uri}
                        data-testid="grounding-chip"
                      >
                        {i + 1}. {host}
                      </a>
                    ) : (
                      <span
                        key={`s-${i}`}
                        className="badge badge-outline badge-xs"
                        data-testid="grounding-chip"
                      >
                        {i + 1}. {host}
                      </span>
                    );
                  })}
                </div>
              )}
            </div>
          )}
          {message.reminders && message.reminders.length > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              {message.reminders.map((reminder) => {
                const label = reminderLabels[reminder.kind] ?? reminder.kind;
                const countSuffix = reminder.count > 1 ? ` x${reminder.count}` : "";
                return (
                  <span
                    key={`${reminder.kind}-${reminder.at}`}
                    className="badge badge-outline badge-xs"
                    title={reminder.message}
                  >
                    {label}{countSuffix}
                  </span>
                );
              })}
            </div>
          )}
          {message.toolEvents && message.toolEvents.length > 0 && (() => {
            const executedTools = message.toolEvents
              .filter((e) => e.phase === "executed")
              .map((e) => e.tool_name)
              .filter(Boolean) as string[];

            if (executedTools.length === 0) return null;
            // Group consecutive identical tool calls (e.g. web_search (×5))
            const groupedTools: { name: string; count: number }[] = [];
            for (const name of executedTools) {
              const last = groupedTools[groupedTools.length - 1];
              if (last && last.name === name) {
                last.count++;
              } else {
                groupedTools.push({ name, count: 1 });
              }
            }

            return (
              <div>
                <div>🔧 使用的工具：</div>
                {groupedTools.map((tool, i) => (
                  <div key={i}>
                    {i + 1}. {tool.name}{tool.count > 1 ? ` (×${tool.count})` : ""}
                  </div>
                ))}
              </div>
            );
          })()}
        </div>
      )}
    </div>
  );
}
