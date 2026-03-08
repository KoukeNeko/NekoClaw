import { useCallback, useRef } from "react";
import { useAppStore, nextMessageID } from "@/store/appStore";
import { chatStream } from "@/api/sse";
import type { ChatRequest, StreamChunk } from "@/api/types";
import { calculateCost } from "@/utils/pricing";

/**
 * Chat hook — manages sending messages, SSE streaming, and tool approval.
 */
export function useChat() {
  const abortRef = useRef<AbortController | null>(null);
  const startTimeRef = useRef<number>(0);
  const accumulatedTextRef = useRef<string>("");

  const sessionID = useAppStore((s) => s.sessionID);
  const provider = useAppStore((s) => s.provider);
  const model = useAppStore((s) => s.model);
  const isStreaming = useAppStore((s) => s.isStreaming);

  const addMessage = useAppStore((s) => s.addMessage);
  const updateLastAssistant = useAppStore((s) => s.updateLastAssistant);
  const setStreaming = useAppStore((s) => s.setStreaming);
  const setActiveToolName = useAppStore((s) => s.setActiveToolName);
  const setRetryStatus = useAppStore((s) => s.setRetryStatus);
  const setPendingApprovals = useAppStore((s) => s.setPendingApprovals);
  const addUsage = useAppStore((s) => s.addUsage);

  /** Send a user message and start streaming the response. */
  const sendMessage = useCallback(
    (text: string, images?: { mime_type: string; data: string; file_name?: string }[]) => {
      if (!text.trim() && (!images || images.length === 0)) return;
      if (isStreaming) return;

      // Add user message to UI
      addMessage({
        id: nextMessageID(),
        role: "user",
        content: text,
        images,
        createdAt: new Date().toISOString(),
      });

      // Add thinking placeholder
      addMessage({
        id: nextMessageID(),
        role: "thinking",
        content: "",
        createdAt: new Date().toISOString(),
      });

      setStreaming(true);
      setActiveToolName("");
      setRetryStatus("");
      startTimeRef.current = Date.now();
      accumulatedTextRef.current = "";

      const req: ChatRequest = {
        session_id: sessionID,
        surface: "web",
        provider,
        model,
        message: text,
        images,
        enable_tools: true,
      };

      abortRef.current = chatStream(req, (chunk: StreamChunk) => {
        handleChunk(chunk);
      });
    },
    [
      sessionID,
      provider,
      model,
      isStreaming,
      addMessage,
      setStreaming,
      setActiveToolName,
      setRetryStatus,
    ],
  );

  /** Send tool approval decisions and continue streaming. */
  const sendApprovals = useCallback(
    (decisions: { approval_id: string; decision: "allow" | "deny" }[], runID: string) => {
      setStreaming(true);
      startTimeRef.current = Date.now();
      accumulatedTextRef.current = "";

      // Replace last thinking/assistant with new thinking
      addMessage({
        id: nextMessageID(),
        role: "thinking",
        content: "",
        createdAt: new Date().toISOString(),
      });

      const req: ChatRequest = {
        session_id: sessionID,
        surface: "web",
        provider,
        model,
        message: "",
        enable_tools: true,
        run_id: runID,
        tool_approvals: decisions,
      };

      abortRef.current = chatStream(req, (chunk: StreamChunk) => {
        handleChunk(chunk);
      });
    },
    [sessionID, provider, model, setStreaming, addMessage],
  );

  function handleChunk(chunk: StreamChunk) {
    switch (chunk.type) {
      case "text": {
        const delta = chunk.content ?? "";
        accumulatedTextRef.current += delta;
        const text = accumulatedTextRef.current;
        updateLastAssistant((msg) => ({
          ...msg,
          role: "assistant",
          content: text,
        }));
        break;
      }

      case "tool_status":
        setActiveToolName(chunk.tool_name ?? "");
        break;

      case "retry_status":
        setRetryStatus(chunk.retry_status ?? "");
        break;

      case "error":
        setStreaming(false);
        setActiveToolName("");
        setRetryStatus("");
        updateLastAssistant((msg) => ({
          ...msg,
          role: "error" as const,
          content: chunk.error ?? "未知錯誤",
        }));
        break;

      case "done": {
        const elapsed = Date.now() - startTimeRef.current;
        const resp = chunk.response;
        setStreaming(false);
        setActiveToolName("");
        setRetryStatus("");

        if (resp) {
          // Check for approval-required status
          if (
            resp.status === "approval_required" &&
            resp.pending_approvals &&
            resp.pending_approvals.length > 0
          ) {
            setPendingApprovals(resp.pending_approvals, resp.run_id ?? "");
          }

          // Update the last assistant message with metadata
          updateLastAssistant((msg) => ({
            ...msg,
            role: "assistant",
            content: accumulatedTextRef.current || resp.reply || msg.content,
            usage: resp.usage,
            provider: resp.provider,
            model: resp.model,
            elapsed,
            toolEvents: resp.tool_events,
          }));

          // Track usage
          if (resp.usage) {
            const cost = calculateCost(
              resp.usage.input_tokens,
              resp.usage.output_tokens,
              resp.model,
            );
            addUsage(resp.usage, cost);
          }
        }
        break;
      }
    }
  }

  /** Cancel the current streaming request. */
  const cancelStream = useCallback(() => {
    abortRef.current?.abort();
    setStreaming(false);
    setActiveToolName("");
    setRetryStatus("");
  }, [setStreaming, setActiveToolName, setRetryStatus]);

  return { sendMessage, sendApprovals, cancelStream, isStreaming };
}
