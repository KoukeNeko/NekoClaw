import { useCallback, useRef } from "react";
import { useAppStore, nextMessageID } from "@/store/appStore";
import { approvePlan as approvePlanRequest, createPlan as createPlanRequest, getPlan, listPlans, rejectPlan as rejectPlanRequest } from "@/api/client";
import { chatStream, planExecuteStream } from "@/api/sse";
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
  const effectiveTimezone = useAppStore((s) => s.effectiveTimezone);
  const isStreaming = useAppStore((s) => s.isStreaming);
  const googleSearchEnabled = useAppStore((s) => s.googleSearchEnabled);

  const addMessage = useAppStore((s) => s.addMessage);
  const updateLastAssistant = useAppStore((s) => s.updateLastAssistant);
  const setStreaming = useAppStore((s) => s.setStreaming);
  const setActiveToolName = useAppStore((s) => s.setActiveToolName);
  const setRetryStatus = useAppStore((s) => s.setRetryStatus);
  const setPendingApprovals = useAppStore((s) => s.setPendingApprovals);
  const setPlans = useAppStore((s) => s.setPlans);
  const upsertPlan = useAppStore((s) => s.upsertPlan);
  const currentRunProvider = useAppStore((s) => s.currentRunProvider);
  const currentRunModel = useAppStore((s) => s.currentRunModel);
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
        client_timezone: effectiveTimezone,
        client_sent_at: new Date().toISOString(),
        enable_tools: true,
        enable_google_search: googleSearchEnabled,
      };

      abortRef.current = chatStream(req, (chunk: StreamChunk) => {
        handleChunk(chunk);
      });
    },
    [
      sessionID,
      provider,
      model,
      effectiveTimezone,
      isStreaming,
      googleSearchEnabled,
      addMessage,
      setStreaming,
      setActiveToolName,
      setRetryStatus,
    ],
  );

  /** Send tool approval decisions and continue streaming. */
  const sendApprovals = useCallback(
    (
      decisions: {
        approval_id: string;
        decision: "allow_once" | "allow_for_session" | "allow_for_workspace" | "deny";
      }[],
      runID: string,
    ) => {
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
        provider: currentRunProvider || provider,
        model: currentRunModel || model,
        message: "",
        client_timezone: effectiveTimezone,
        enable_tools: true,
        enable_google_search: googleSearchEnabled,
        run_id: runID,
        tool_approvals: decisions,
      };

      abortRef.current = chatStream(req, (chunk: StreamChunk) => {
        handleChunk(chunk);
      });
    },
    [sessionID, provider, model, currentRunProvider, currentRunModel, effectiveTimezone, googleSearchEnabled, setStreaming, addMessage],
  );

  const createPlan = useCallback(
    async (text: string) => {
      if (!text.trim() || isStreaming) return;
      const plan = await createPlanRequest({
        session_id: sessionID,
        surface: "web",
        provider,
        model,
        prompt: text.trim(),
      });
      upsertPlan(plan);
    },
    [sessionID, provider, model, isStreaming, upsertPlan],
  );

  const approvePlan = useCallback(
    async (planID: string) => {
      if (isStreaming) return;
      const approved = await approvePlanRequest(planID);
      upsertPlan(approved);
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
      abortRef.current = planExecuteStream(planID, (chunk) => {
        handleChunk(chunk, planID);
      });
    },
    [isStreaming, upsertPlan, addMessage, setStreaming, setActiveToolName, setRetryStatus],
  );

  const rejectPlan = useCallback(
    async (planID: string) => {
      const rejected = await rejectPlanRequest(planID);
      upsertPlan(rejected);
    },
    [upsertPlan],
  );

  const loadPlans = useCallback(
    async (targetSessionID: string) => {
      const plans = await listPlans(targetSessionID);
      setPlans(plans);
    },
    [setPlans],
  );

  function handleChunk(chunk: StreamChunk, planID?: string) {
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
        const resp = chunk.response;
        setStreaming(false);
        setActiveToolName("");
        setRetryStatus("");

        if (resp) {
          const elapsed = resp.elapsed_ms ?? (Date.now() - startTimeRef.current);
          // Check for approval-required status
          if (
            resp.status === "approval_required" &&
            resp.pending_approvals &&
            resp.pending_approvals.length > 0
          ) {
            setPendingApprovals(resp.pending_approvals, resp.run_id ?? "", resp.provider, resp.model);
          }

          // Update the last assistant message with metadata
          updateLastAssistant((msg) => ({
            ...msg,
            role: "assistant",
            content:
              accumulatedTextRef.current ||
              resp.reply ||
              (resp.status === "approval_required" ? "等待工具核准..." : msg.content),
            usage: resp.usage,
            provider: resp.provider,
            model: resp.model,
            elapsed,
            toolEvents: resp.tool_events,
            reminders: resp.reminders,
            subagentArtifacts: resp.subagent_artifacts,
            grounding: resp.grounding,
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
        if (planID) {
          void getPlan(planID).then(upsertPlan).catch(() => {
            /* ignore */
          });
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

  return { sendMessage, sendApprovals, cancelStream, isStreaming, createPlan, approvePlan, rejectPlan, loadPlans };
}
