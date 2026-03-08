/**
 * POST-based SSE streaming for /v1/chat/stream.
 *
 * Browser EventSource only supports GET. We use fetch() + ReadableStream
 * to manually parse the SSE "data: {...}\n\n" protocol.
 */
import type { ChatRequest, StreamChunk } from "./types";

export type StreamCallback = (chunk: StreamChunk) => void;

/**
 * Start a streaming chat request. Returns an AbortController so the
 * caller can cancel. Invokes `onChunk` for every parsed SSE event.
 */
export function chatStream(
  req: ChatRequest,
  onChunk: StreamCallback,
): AbortController {
  const controller = new AbortController();

  (async () => {
    try {
      const resp = await fetch("/v1/chat/stream", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "text/event-stream",
        },
        body: JSON.stringify(req),
        signal: controller.signal,
      });

      if (!resp.ok) {
        let errorMsg = `HTTP ${resp.status}`;
        try {
          const body = await resp.text();
          if (body) errorMsg = body;
        } catch {
          /* ignore */
        }
        onChunk({ type: "error", error: errorMsg });
        return;
      }

      const reader = resp.body?.getReader();
      if (!reader) {
        onChunk({ type: "error", error: "No response body" });
        return;
      }

      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // Process complete lines
        const lines = buffer.split("\n");
        // Keep the last (possibly incomplete) line in buffer
        buffer = lines.pop() ?? "";

        for (const line of lines) {
          if (!line.startsWith("data: ")) continue;
          const data = line.slice(6); // remove "data: " prefix
          try {
            const chunk: StreamChunk = JSON.parse(data);
            onChunk(chunk);
          } catch {
            // Skip malformed events
          }
        }
      }

      // Process any remaining buffer
      if (buffer.startsWith("data: ")) {
        try {
          const chunk: StreamChunk = JSON.parse(buffer.slice(6));
          onChunk(chunk);
        } catch {
          /* ignore */
        }
      }
    } catch (err) {
      if (controller.signal.aborted) return;
      onChunk({
        type: "error",
        error: err instanceof Error ? err.message : String(err),
      });
    }
  })();

  return controller;
}
