import { getTranscript, listPlans } from "@/api/client";
import type { TranscriptEntry } from "@/api/types";
import type { ChatMessage } from "@/store/appStore";
import { useAppStore } from "@/store/appStore";
import { calculateCost } from "@/utils/pricing";

const MOBILE_BREAKPOINT = 1024;

function transcriptToMessages(entries: TranscriptEntry[]): ChatMessage[] {
  return entries.map((entry, index) => ({
    id: `tx-${index + 1}`,
    role: entry.role as ChatMessage["role"],
    content: entry.content ?? "",
    createdAt: entry.created_at,
    provider: entry.provider,
    model: entry.model,
    usage: entry.usage,
    toolEvents: entry.tool_events,
    reminders: entry.reminders,
    elapsed: entry.elapsed_ms,
  }));
}

function recomputeUsageFromTranscript(entries: TranscriptEntry[]) {
  const { addUsage } = useAppStore.getState();
  for (const entry of entries) {
    if (entry.role !== "assistant" || !entry.usage) continue;
    const cost = calculateCost(
      entry.usage.input_tokens,
      entry.usage.output_tokens,
      entry.model ?? "",
    );
    addUsage(entry.usage, cost);
  }
}

export async function openSessionConversation(sessionID: string): Promise<void> {
  const entries = await getTranscript(sessionID);
  const messages = transcriptToMessages(entries);
  const state = useAppStore.getState();

  state.setSessionID(sessionID);
  state.resetUsage();
  state.setMessages(messages);
  state.setPlans(await listPlans(sessionID));
  recomputeUsageFromTranscript(entries);
  state.setRoute("chat");

  if (typeof window !== "undefined" && window.innerWidth < MOBILE_BREAKPOINT) {
    state.setSidebarOpen(false);
  }
}
