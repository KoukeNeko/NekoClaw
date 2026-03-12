import { create } from "zustand";
import type {
  SessionInfo,
  ThinkingMode,
  UsageInfo,
  ToolEvent,
  PendingToolApproval,
  PlanRecord,
  ReminderEvent,
  SecurityStatus,
} from "@/api/types";
import {
  detectBrowserTimeZone,
  normalizeTimeZoneOverride,
  resolveEffectiveTimeZone,
} from "@/utils/timezone";

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
  reminders?: ReminderEvent[];
  createdAt: string;
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export type Route =
  | "login"
  | "setup"
  | "chat"
  | "settings/general"
  | "settings/compaction"
  | "settings/model-roles"
  | "settings/playbooks"
  | "settings/automations"
  | "settings/permissions"
  | "settings/security"
  | "settings/backup"
  | "settings/provider"
  | "settings/persona"
  | "settings/auth"
  | "settings/memory"
  | "settings/usage"
  | "settings/mcp"
  | "settings/discord"
  | "settings/telegram"
  | "settings/tools"
  | "settings/console";

// ---------------------------------------------------------------------------
// Store shape
// ---------------------------------------------------------------------------

interface AppState {
  // Routing
  route: Route;
  setRoute: (r: Route) => void;

  // Browser auth
  securityStatus: SecurityStatus | null;
  setSecurityStatus: (status: SecurityStatus | null) => void;
  resetForAuthBoundary: () => void;

  // Sidebar
  sidebarOpen: boolean;
  toggleSidebar: () => void;
  setSidebarOpen: (open: boolean) => void;

  // Provider / model
  provider: string;
  model: string;
  thinkingMode: ThinkingMode;
  setProvider: (p: string) => void;
  setModel: (m: string) => void;
  setThinkingMode: (mode: ThinkingMode) => void;

  // Session
  sessionID: string;
  sessions: SessionInfo[];
  setSessionID: (id: string) => void;
  setSessions: (s: SessionInfo[]) => void;

  // Chat messages for current session
  messages: ChatMessage[];
  plans: PlanRecord[];
  setMessages: (msgs: ChatMessage[]) => void;
  setPlans: (plans: PlanRecord[]) => void;
  upsertPlan: (plan: PlanRecord) => void;
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
  currentRunProvider: string;
  currentRunModel: string;
  setPendingApprovals: (approvals: PendingToolApproval[], runID: string, provider: string, model: string) => void;
  clearApprovals: () => void;

  // Persona
  activePersona: string;
  setActivePersona: (name: string) => void;

  // General settings
  browserTimezone: string;
  generalTimezoneOverride: string;
  effectiveTimezone: string;
  setBrowserTimezone: (timezone: string) => void;
  setGeneralTimezoneOverride: (timezone: string) => void;

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
  securityStatus: null,
  setSecurityStatus: (status) => set({ securityStatus: status }),
  resetForAuthBoundary: () =>
    set({
      sessionID: "main",
      sessions: [],
      messages: [],
      plans: [],
      isStreaming: false,
      activeToolName: "",
      retryStatus: "",
      pendingApprovals: [],
      currentRunID: "",
      currentRunProvider: "",
      currentRunModel: "",
      activePersona: "",
      thinkingMode: "auto",
      totalUsage: { input_tokens: 0, output_tokens: 0, total_tokens: 0 },
      totalCost: 0,
      contextPercent: 0,
    }),

  // Sidebar — expanded by default on desktop, hidden on mobile
  sidebarOpen: typeof window !== "undefined" && window.innerWidth >= 1024,
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),

  // Provider / model
  provider: "",
  model: "",
  thinkingMode: "auto",
  setProvider: (p) => set({ provider: p }),
  setModel: (m) => set({ model: m }),
  setThinkingMode: (thinkingMode) => set({ thinkingMode }),

  // Session
  sessionID: "main",
  sessions: [],
  setSessionID: (id) => set({ sessionID: id }),
  setSessions: (s) => set({ sessions: s }),

  // Messages
  messages: [],
  plans: [],
  setMessages: (msgs) => set({ messages: msgs }),
  setPlans: (plans) => set({ plans }),
  upsertPlan: (plan) =>
    set((s) => {
      const existing = s.plans.findIndex((item) => item.id === plan.id);
      if (existing === -1) {
        return { plans: [plan, ...s.plans] };
      }
      const next = [...s.plans];
      next[existing] = plan;
      return { plans: next };
    }),
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
  currentRunProvider: "",
  currentRunModel: "",
  setPendingApprovals: (approvals, runID, provider, model) =>
    set({
      pendingApprovals: approvals,
      currentRunID: runID,
      currentRunProvider: provider,
      currentRunModel: model,
    }),
  clearApprovals: () =>
    set({
      pendingApprovals: [],
      currentRunID: "",
      currentRunProvider: "",
      currentRunModel: "",
    }),

  browserTimezone: detectBrowserTimeZone(),
  generalTimezoneOverride: "",
  effectiveTimezone: detectBrowserTimeZone(),
  setBrowserTimezone: (timezone) =>
    set((state) => {
      const browserTimezone = resolveEffectiveTimeZone(timezone, "");
      return {
        browserTimezone,
        effectiveTimezone: resolveEffectiveTimeZone(
          browserTimezone,
          state.generalTimezoneOverride,
        ),
      };
    }),
  setGeneralTimezoneOverride: (timezone) =>
    set((state) => {
      const generalTimezoneOverride = normalizeTimeZoneOverride(timezone);
      return {
        generalTimezoneOverride,
        effectiveTimezone: resolveEffectiveTimeZone(
          state.browserTimezone,
          generalTimezoneOverride,
        ),
      };
    }),

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
