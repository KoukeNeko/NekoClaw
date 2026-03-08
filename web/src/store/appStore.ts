import { create } from "zustand";
import type {
  SessionInfo,
  UsageInfo,
  ToolEvent,
  PendingToolApproval,
} from "@/api/types";

// ---------------------------------------------------------------------------
// Chat message as displayed in the UI (superset of API Message)
// ---------------------------------------------------------------------------

export interface ChatMessage {
  id: string;
  role: "user" | "assistant" | "system" | "error" | "thinking";
  content: string;
  images?: { mime_type: string; data: string; file_name?: string }[];
  usage?: UsageInfo;
  provider?: string;
  model?: string;
  elapsed?: number; // ms
  toolEvents?: ToolEvent[];
  createdAt: string;
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export type Route =
  | "chat"
  | "settings/provider"
  | "settings/persona"
  | "settings/auth"
  | "settings/sessions"
  | "settings/memory"
  | "settings/usage"
  | "settings/mcp"
  | "settings/discord"
  | "settings/telegram"
  | "settings/tools";

// ---------------------------------------------------------------------------
// Store shape
// ---------------------------------------------------------------------------

interface AppState {
  // Routing
  route: Route;
  setRoute: (r: Route) => void;

  // Sidebar
  sidebarOpen: boolean;
  toggleSidebar: () => void;
  setSidebarOpen: (open: boolean) => void;

  // Provider / model
  provider: string;
  model: string;
  setProvider: (p: string) => void;
  setModel: (m: string) => void;

  // Session
  sessionID: string;
  sessions: SessionInfo[];
  setSessionID: (id: string) => void;
  setSessions: (s: SessionInfo[]) => void;

  // Chat messages for current session
  messages: ChatMessage[];
  setMessages: (msgs: ChatMessage[]) => void;
  addMessage: (msg: ChatMessage) => void;
  updateLastAssistant: (updater: (msg: ChatMessage) => ChatMessage) => void;
  clearMessages: () => void;

  // Streaming state
  isStreaming: boolean;
  setStreaming: (s: boolean) => void;

  // Active tool status (during streaming)
  activeToolName: string;
  retryStatus: string;
  setActiveToolName: (name: string) => void;
  setRetryStatus: (status: string) => void;

  // Tool approval
  pendingApprovals: PendingToolApproval[];
  currentRunID: string;
  setPendingApprovals: (approvals: PendingToolApproval[], runID: string) => void;
  clearApprovals: () => void;

  // Persona
  activePersona: string;
  setActivePersona: (name: string) => void;

  // Usage totals for current session
  totalUsage: UsageInfo;
  totalCost: number;
  addUsage: (usage: UsageInfo, cost: number) => void;
  resetUsage: () => void;

  // Context usage percentage (0-100)
  contextPercent: number;
  setContextPercent: (pct: number) => void;
}

let messageCounter = 0;
function nextMessageID(): string {
  return `msg-${Date.now()}-${++messageCounter}`;
}

export { nextMessageID };

export const useAppStore = create<AppState>((set) => ({
  // Routing
  route: "chat",
  setRoute: (r) => set({ route: r }),

  // Sidebar — expanded by default on desktop, hidden on mobile
  sidebarOpen: typeof window !== "undefined" && window.innerWidth >= 1024,
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),

  // Provider / model
  provider: "",
  model: "",
  setProvider: (p) => set({ provider: p }),
  setModel: (m) => set({ model: m }),

  // Session
  sessionID: "main",
  sessions: [],
  setSessionID: (id) => set({ sessionID: id }),
  setSessions: (s) => set({ sessions: s }),

  // Messages
  messages: [],
  setMessages: (msgs) => set({ messages: msgs }),
  addMessage: (msg) =>
    set((s) => ({ messages: [...s.messages, msg] })),
  updateLastAssistant: (updater) =>
    set((s) => {
      const msgs = [...s.messages];
      for (let i = msgs.length - 1; i >= 0; i--) {
        if (msgs[i].role === "assistant" || msgs[i].role === "thinking") {
          msgs[i] = updater(msgs[i]);
          break;
        }
      }
      return { messages: msgs };
    }),
  clearMessages: () => set({ messages: [] }),

  // Streaming
  isStreaming: false,
  setStreaming: (s) => set({ isStreaming: s }),

  // Tool status
  activeToolName: "",
  retryStatus: "",
  setActiveToolName: (name) => set({ activeToolName: name }),
  setRetryStatus: (status) => set({ retryStatus: status }),

  // Tool approval
  pendingApprovals: [],
  currentRunID: "",
  setPendingApprovals: (approvals, runID) =>
    set({ pendingApprovals: approvals, currentRunID: runID }),
  clearApprovals: () =>
    set({ pendingApprovals: [], currentRunID: "" }),

  // Persona
  activePersona: "",
  setActivePersona: (name) => set({ activePersona: name }),

  // Usage
  totalUsage: { input_tokens: 0, output_tokens: 0, total_tokens: 0 },
  totalCost: 0,
  addUsage: (usage, cost) =>
    set((s) => ({
      totalUsage: {
        input_tokens: s.totalUsage.input_tokens + usage.input_tokens,
        output_tokens: s.totalUsage.output_tokens + usage.output_tokens,
        total_tokens: s.totalUsage.total_tokens + usage.total_tokens,
      },
      totalCost: s.totalCost + cost,
    })),
  resetUsage: () =>
    set({
      totalUsage: { input_tokens: 0, output_tokens: 0, total_tokens: 0 },
      totalCost: 0,
    }),

  // Context
  contextPercent: 0,
  setContextPercent: (pct) => set({ contextPercent: pct }),
}));

// Convenience selector hook aliases
export const useRoute = () => useAppStore((s) => s.route);
export const useSetRoute = () => useAppStore((s) => s.setRoute);
