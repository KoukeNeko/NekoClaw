import { useEffect, useRef } from "react";
import { useAppStore } from "@/store/appStore";
import { MessageBubble } from "./MessageBubble";
import { ThinkingIndicator } from "./ThinkingIndicator";

/**
 * Scrollable message list with auto-scroll to bottom.
 */
export function MessageList() {
  const messages = useAppStore((s) => s.messages);
  const isStreaming = useAppStore((s) => s.isStreaming);
  const sessionID = useAppStore((s) => s.sessionID);
  const bottomRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Scroll to bottom instantly when switching sessions
  useEffect(() => {
    // Use requestAnimationFrame to ensure DOM has rendered the new messages
    requestAnimationFrame(() => {
      bottomRef.current?.scrollIntoView();
    });
  }, [sessionID]);

  // Auto-scroll to bottom on new messages or streaming updates
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    // Only auto-scroll if user is near the bottom (within 200px)
    const isNearBottom =
      container.scrollHeight - container.scrollTop - container.clientHeight < 200;

    if (isNearBottom) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [messages, isStreaming]);

  if (messages.length === 0) {
    return (
      <div className="h-full flex items-center justify-center">
        <div className="text-center text-base-content/30 space-y-2">
          <div className="text-4xl">🐾</div>
          <p className="text-lg">NekoClaw</p>
          <p className="text-sm">輸入訊息開始對話</p>
        </div>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="h-full overflow-y-auto [scrollbar-width:thin] px-2 sm:px-4 py-4"
    >
      <div className="space-y-4">
        {messages.map((msg) =>
          msg.role === "thinking" ? (
            <ThinkingIndicator key={msg.id} />
          ) : (
            <MessageBubble key={msg.id} message={msg} />
          ),
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
