import { useMemo } from "react";
import type { ChatMessage } from "@/store/appStore";
import { renderMarkdown } from "@/utils/markdown";
import { formatTokens, formatDuration } from "@/utils/format";

interface Props {
  message: ChatMessage;
}

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
      {message.role === "assistant" && message.usage && (
        <div className="chat-footer text-xs text-base-content/40 mt-1 space-y-0.5 flex-col">
          <div className="flex flex-wrap items-center gap-x-1">
            {message.elapsed != null && (
              <>
                <span>⏱ {formatDuration(message.elapsed)}</span>
                <span className="opacity-40">·</span>
              </>
            )}
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
              <>
                <span className="opacity-40">·</span>
                <span>{message.provider}/{message.model}</span>
              </>
            )}
          </div>
          {message.toolEvents && message.toolEvents.length > 0 && (() => {
            const executedTools = message.toolEvents
              .filter((e) => e.phase === "executed")
              .map((e) => e.tool_name)
              .filter(Boolean);
            if (executedTools.length === 0) return null;
            return (
              <div>
                <div>🔧 使用的工具：</div>
                {executedTools.map((name, i) => (
                  <div key={i}>{i + 1}. {name}</div>
                ))}
              </div>
            );
          })()}
        </div>
      )}
    </div>
  );
}
