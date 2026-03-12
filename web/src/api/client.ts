/**
 * Typed fetch wrapper for the browser UI.
 *
 * All methods throw ApiError on non-2xx responses. Callers should
 * handle errors via try/catch and inspect code/message.
 */
import type {
  ChatRequest,
  ChatResponse,
  SessionInfo,
  ModelsResponse,
  DefaultProviderResponse,
  SetDefaultProviderRequest,
  FallbacksResponse,
  SetFallbacksRequest,
  GeminiAuthStartRequest,
  GeminiAuthStartResponse,
  GeminiAuthManualCompleteRequest,
  GeminiAuthCompleteResponse,
  GeminiAuthProfile,
  AIStudioAddKeyRequest,
  AIStudioAddKeyResponse,
  AIStudioProfile,
  AIStudioModelsResponse,
  AnthropicAddTokenRequest,
  AnthropicAddAPIKeyRequest,
  AnthropicAddCredentialResponse,
  AnthropicProfile,
  AnthropicBrowserStartRequest,
  AnthropicBrowserStartResponse,
  AnthropicBrowserJobResponse,
  AnthropicBrowserManualCompleteRequest,
  OpenAIAddKeyRequest,
  OpenAICodexAddTokenRequest,
  OpenAIAddCredentialResponse,
  OpenAIProfile,
  OpenAICodexBrowserStartRequest,
  OpenAICodexBrowserStartResponse,
  OpenAICodexBrowserJobResponse,
  OpenAICodexBrowserManualCompleteRequest,
  MCPServer,
  MCPTool,
  MCPBuiltinServer,
  MCPConfigsResponse,
  SaveMCPConfigRequest,
  PersonaInfo,
  BackupEntry,
  BackupImportJob,
  BackupRestoreResponse,
  UsageSummary,
  MemorySearchRequest,
  MemorySearchResponse,
  GeneralConfig,
  CompactionConfig,
  ModelRolesConfig,
  DiscordConfig,
  TelegramConfig,
  SecurityConfig,
  SecurityLoginRequest,
  SecurityPasswordRequest,
  SecuritySetupRequest,
  SecurityStatus,
  ToolCatalogEntry,
  ToolsConfig,
  ToolStatusResult,
  TranscriptEntry,
  CreatePlanRequest,
  PlanRecord,
  PlaybookEntry,
  TaskList,
  PermissionGrant,
  WorkflowRecord,
  WorkflowRun,
  SnapshotRecord,
  TraceRecord,
} from "./types";
import { dispatchBrowserAuthEvent } from "./authEvents";

// ---------------------------------------------------------------------------
// Error type
// ---------------------------------------------------------------------------

export class ApiError extends Error {
  constructor(
    public statusCode: number,
    public code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

const BASE = ""; // same-origin; Vite proxy handles /v1 in dev
const TRUSTED_UI_HEADER = "X-NekoClaw-Request";
const TRUSTED_UI_HEADER_VALUE = "browser-ui";

export type UploadProgress = {
  loaded: number;
  total: number;
  percent: number;
};

async function parseErrorBody(resp: Response): Promise<ApiError> {
  let code = "";
  let message = `HTTP ${resp.status}`;
  try {
    const body = await resp.json();
    if (typeof body.error === "string") {
      message = body.error;
    } else if (body.error?.message) {
      code = body.error.code ?? "";
      message = body.error.message;
    }
  } catch {
    /* non-JSON body */
  }
  return new ApiError(resp.status, code, message);
}

function parseErrorJSON(statusCode: number, raw: string): ApiError {
  let code = "";
  let message = `HTTP ${statusCode}`;
  try {
    const body = JSON.parse(raw);
    if (typeof body.error === "string") {
      message = body.error;
    } else if (body.error?.message) {
      code = body.error.code ?? "";
      message = body.error.message;
    }
  } catch {
    if (raw.trim()) {
      message = raw.trim();
    }
  }
  return new ApiError(statusCode, code, message);
}

async function fetchJSON<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const headers = new Headers(init?.headers);
  const method = (init?.method ?? "GET").toUpperCase();
  const isFormData =
    typeof FormData !== "undefined" && init?.body instanceof FormData;
  if (!headers.has("Content-Type") && init?.body != null && !isFormData) {
    headers.set("Content-Type", "application/json");
  }
  if (
    (method === "POST" || method === "PUT" || method === "DELETE") &&
    !headers.has(TRUSTED_UI_HEADER)
  ) {
    headers.set(TRUSTED_UI_HEADER, TRUSTED_UI_HEADER_VALUE);
  }
  const resp = await fetch(`${BASE}${path}`, {
    ...init,
    credentials: "same-origin",
    headers,
  });
  if (!resp.ok) {
    const error = await parseErrorBody(resp);
    if (
      error.statusCode === 401 &&
      (error.code === "setup_required" ||
        error.code === "auth_required" ||
        error.code === "session_expired")
    ) {
      dispatchBrowserAuthEvent(error.code);
    }
    throw error;
  }
  // Some endpoints return 204 No Content
  if (resp.status === 204) return undefined as unknown as T;
  return resp.json();
}

async function fetchBinary(
  path: string,
  init?: RequestInit,
): Promise<Response> {
  const resp = await fetch(`${BASE}${path}`, {
    ...init,
    credentials: "same-origin",
    headers: init?.headers,
  });
  if (!resp.ok) {
    const error = await parseErrorBody(resp);
    if (
      error.statusCode === 401 &&
      (error.code === "setup_required" ||
        error.code === "auth_required" ||
        error.code === "session_expired")
    ) {
      dispatchBrowserAuthEvent(error.code);
    }
    throw error;
  }
  return resp;
}

function post<T>(path: string, body?: unknown): Promise<T> {
  return fetchJSON<T>(path, {
    method: "POST",
    body: body != null ? JSON.stringify(body) : undefined,
  });
}

function get<T>(path: string): Promise<T> {
  return fetchJSON<T>(path, { method: "GET" });
}

function put<T>(path: string, body?: unknown): Promise<T> {
  return fetchJSON<T>(path, {
    method: "PUT",
    body: body != null ? JSON.stringify(body) : undefined,
  });
}

function del<T>(path: string, body?: unknown): Promise<T> {
  return fetchJSON<T>(path, {
    method: "DELETE",
    body: body != null ? JSON.stringify(body) : undefined,
  });
}

// ---------------------------------------------------------------------------
// Chat
// ---------------------------------------------------------------------------

export function chat(req: ChatRequest): Promise<ChatResponse> {
  return post("/v1/chat", req);
}

export function createPlan(req: CreatePlanRequest): Promise<PlanRecord> {
  return post("/v1/plans", req);
}

export async function listPlans(sessionID: string): Promise<PlanRecord[]> {
  const resp = await get<{ plans: PlanRecord[] }>(
    `/v1/plans?session_id=${encodeURIComponent(sessionID)}`,
  );
  return resp.plans ?? [];
}

export function getPlan(planID: string): Promise<PlanRecord> {
  return get(`/v1/plans/${encodeURIComponent(planID)}`);
}

export function approvePlan(planID: string): Promise<PlanRecord> {
  return post(`/v1/plans/${encodeURIComponent(planID)}/approve`);
}

export function rejectPlan(planID: string): Promise<PlanRecord> {
  return post(`/v1/plans/${encodeURIComponent(planID)}/reject`);
}

// Streaming is handled separately in sse.ts

// ---------------------------------------------------------------------------
// Providers & Models
// ---------------------------------------------------------------------------

export async function listProviders(): Promise<string[]> {
  const resp = await get<{ providers: string[] }>("/v1/providers");
  return resp.providers ?? [];
}

export function listModels(provider: string): Promise<ModelsResponse> {
  return get(`/v1/models?provider=${encodeURIComponent(provider)}`);
}

export function getDefaultProvider(): Promise<DefaultProviderResponse> {
  return get("/v1/default-provider");
}

export function setDefaultProvider(
  req: SetDefaultProviderRequest,
): Promise<void> {
  return put("/v1/default-provider", req);
}

export async function getFallbacks(): Promise<FallbacksResponse> {
  return get("/v1/fallbacks");
}

export function setFallbacks(req: SetFallbacksRequest): Promise<void> {
  return put("/v1/fallbacks", req);
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

export async function listSessions(): Promise<SessionInfo[]> {
  const resp = await get<{ sessions: SessionInfo[] }>("/v1/sessions");
  return resp.sessions ?? [];
}

export function deleteSession(sessionID: string): Promise<void> {
  return post("/v1/sessions/delete", { session_id: sessionID });
}

export function renameSession(
  sessionID: string,
  title: string,
): Promise<void> {
  return post("/v1/sessions/rename", { session_id: sessionID, title });
}

export async function getTranscript(
  sessionID: string,
): Promise<TranscriptEntry[]> {
  const resp = await get<{ messages: TranscriptEntry[] }>(
    `/v1/sessions/transcript?session_id=${encodeURIComponent(sessionID)}`,
  );
  return resp.messages ?? [];
}

export function getUsageSummary(): Promise<UsageSummary> {
  return get("/v1/usage/summary");
}

// ---------------------------------------------------------------------------
// Auth — Gemini
// ---------------------------------------------------------------------------

export function startGeminiOAuth(
  req: GeminiAuthStartRequest,
): Promise<GeminiAuthStartResponse> {
  return post("/v1/auth/gemini/start", req);
}

export function completeGeminiOAuthManual(
  req: GeminiAuthManualCompleteRequest,
): Promise<GeminiAuthCompleteResponse> {
  return post("/v1/auth/gemini/manual/complete", req);
}

export async function listGeminiProfiles(): Promise<GeminiAuthProfile[]> {
  const resp = await get<{ profiles: GeminiAuthProfile[] }>("/v1/auth/gemini/profiles");
  return resp.profiles ?? [];
}

export function useGeminiProfile(profileID: string): Promise<void> {
  return post("/v1/auth/gemini/use", { profile_id: profileID });
}

// ---------------------------------------------------------------------------
// Auth — AI Studio
// ---------------------------------------------------------------------------

export function addAIStudioKey(
  req: AIStudioAddKeyRequest,
): Promise<AIStudioAddKeyResponse> {
  return post("/v1/auth/ai-studio/add-key", req);
}

export async function listAIStudioProfiles(): Promise<AIStudioProfile[]> {
  const resp = await get<{ profiles: AIStudioProfile[] }>("/v1/auth/ai-studio/profiles");
  return resp.profiles ?? [];
}

export function useAIStudioProfile(profileID: string): Promise<void> {
  return post("/v1/auth/ai-studio/use", { profile_id: profileID });
}

export function deleteAIStudioProfile(profileID: string): Promise<void> {
  return post("/v1/auth/ai-studio/delete", { profile_id: profileID });
}

export function listAIStudioModels(): Promise<AIStudioModelsResponse> {
  return get("/v1/ai-studio/models");
}

// ---------------------------------------------------------------------------
// Auth — Anthropic
// ---------------------------------------------------------------------------

export function addAnthropicToken(
  req: AnthropicAddTokenRequest,
): Promise<AnthropicAddCredentialResponse> {
  return post("/v1/auth/anthropic/add-token", req);
}

export function addAnthropicAPIKey(
  req: AnthropicAddAPIKeyRequest,
): Promise<AnthropicAddCredentialResponse> {
  return post("/v1/auth/anthropic/add-api-key", req);
}

export async function listAnthropicProfiles(): Promise<AnthropicProfile[]> {
  const resp = await get<{ profiles: AnthropicProfile[] }>("/v1/auth/anthropic/profiles");
  return resp.profiles ?? [];
}

export function useAnthropicProfile(profileID: string): Promise<void> {
  return post("/v1/auth/anthropic/use", { profile_id: profileID });
}

export function deleteAnthropicProfile(profileID: string): Promise<void> {
  return post("/v1/auth/anthropic/delete", { profile_id: profileID });
}

export function startAnthropicBrowserLogin(
  req: AnthropicBrowserStartRequest,
): Promise<AnthropicBrowserStartResponse> {
  return post("/v1/auth/anthropic/browser/start", req);
}

export function getAnthropicBrowserJob(
  jobID: string,
): Promise<AnthropicBrowserJobResponse> {
  return get(`/v1/auth/anthropic/browser/jobs/${encodeURIComponent(jobID)}`);
}

export function completeAnthropicBrowserManual(
  req: AnthropicBrowserManualCompleteRequest,
): Promise<void> {
  return post("/v1/auth/anthropic/browser/manual/complete", req);
}

export function cancelAnthropicBrowserLogin(jobID: string): Promise<void> {
  return post("/v1/auth/anthropic/browser/cancel", { job_id: jobID });
}

// ---------------------------------------------------------------------------
// Auth — OpenAI
// ---------------------------------------------------------------------------

export function addOpenAIKey(
  req: OpenAIAddKeyRequest,
): Promise<OpenAIAddCredentialResponse> {
  return post("/v1/auth/openai/add-key", req);
}

export async function listOpenAIProfiles(): Promise<OpenAIProfile[]> {
  const resp = await get<{ profiles: OpenAIProfile[] }>("/v1/auth/openai/profiles");
  return resp.profiles ?? [];
}

export function useOpenAIProfile(profileID: string): Promise<void> {
  return post("/v1/auth/openai/use", { profile_id: profileID });
}

export function deleteOpenAIProfile(profileID: string): Promise<void> {
  return post("/v1/auth/openai/delete", { profile_id: profileID });
}

// ---------------------------------------------------------------------------
// Auth — OpenAI Codex
// ---------------------------------------------------------------------------

export function addOpenAICodexToken(
  req: OpenAICodexAddTokenRequest,
): Promise<OpenAIAddCredentialResponse> {
  return post("/v1/auth/openai-codex/add-token", req);
}

export async function listOpenAICodexProfiles(): Promise<OpenAIProfile[]> {
  const resp = await get<{ profiles: OpenAIProfile[] }>("/v1/auth/openai-codex/profiles");
  return resp.profiles ?? [];
}

export function useOpenAICodexProfile(profileID: string): Promise<void> {
  return post("/v1/auth/openai-codex/use", { profile_id: profileID });
}

export function deleteOpenAICodexProfile(profileID: string): Promise<void> {
  return post("/v1/auth/openai-codex/delete", { profile_id: profileID });
}

export function startOpenAICodexBrowserLogin(
  req: OpenAICodexBrowserStartRequest,
): Promise<OpenAICodexBrowserStartResponse> {
  return post("/v1/auth/openai-codex/browser/start", req);
}

export function getOpenAICodexBrowserJob(
  jobID: string,
): Promise<OpenAICodexBrowserJobResponse> {
  return get(
    `/v1/auth/openai-codex/browser/jobs/${encodeURIComponent(jobID)}`,
  );
}

export function completeOpenAICodexBrowserManual(
  req: OpenAICodexBrowserManualCompleteRequest,
): Promise<void> {
  return post("/v1/auth/openai-codex/browser/manual/complete", req);
}

export function cancelOpenAICodexBrowserLogin(
  jobID: string,
): Promise<void> {
  return post("/v1/auth/openai-codex/browser/cancel", { job_id: jobID });
}

// ---------------------------------------------------------------------------
// Memory
// ---------------------------------------------------------------------------

export async function searchMemory(
  req: MemorySearchRequest,
): Promise<MemorySearchResponse> {
  return post("/v1/memory/search", req);
}

// ---------------------------------------------------------------------------
// MCP
// ---------------------------------------------------------------------------

export async function listMCPServers(): Promise<MCPServer[]> {
  const resp = await get<{ servers: MCPServer[] }>("/v1/mcp/servers");
  return resp.servers ?? [];
}

export async function listMCPTools(): Promise<MCPTool[]> {
  const resp = await get<{ tools: MCPTool[] }>("/v1/mcp/tools");
  return resp.tools ?? [];
}

export async function listMCPBuiltin(): Promise<MCPBuiltinServer[]> {
  const resp = await get<{ servers: MCPBuiltinServer[] }>("/v1/mcp/builtin");
  return resp.servers ?? [];
}

export function getMCPConfigs(): Promise<MCPConfigsResponse> {
  return get("/v1/mcp/configs");
}

export function toggleMCPBuiltin(
  name: string,
  enabled: boolean,
): Promise<void> {
  return post("/v1/mcp/builtin/toggle", { name, enabled });
}

export async function saveMCPConfig(
  req: SaveMCPConfigRequest,
): Promise<{ file_name: string }> {
  return post("/v1/mcp/configs/save", req);
}

export function deleteMCPConfig(fileName: string): Promise<void> {
  return post("/v1/mcp/configs/delete", { file_name: fileName });
}

export function applyMCPConfigs(): Promise<void> {
  return post("/v1/mcp/configs/apply", {});
}

// ---------------------------------------------------------------------------
// Personas
// ---------------------------------------------------------------------------

export async function listPersonas(): Promise<PersonaInfo[]> {
  const resp = await get<{ personas: PersonaInfo[] }>("/v1/personas");
  return resp.personas ?? [];
}

export async function getActivePersona(): Promise<PersonaInfo | null> {
  const resp = await get<{ persona: PersonaInfo | null }>("/v1/personas/active");
  return resp.persona ?? null;
}

export function usePersona(dirName: string): Promise<void> {
  return post("/v1/personas/use", { dir_name: dirName });
}

export function clearPersona(): Promise<void> {
  return post("/v1/personas/clear", {});
}

export function reloadPersonas(): Promise<void> {
  return post("/v1/personas/reload", {});
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

export async function listBackups(): Promise<BackupEntry[]> {
  const resp = await get<{ backups: BackupEntry[] }>("/v1/backups");
  return resp.backups ?? [];
}

export async function createBackup(password: string): Promise<BackupEntry> {
  const resp = await post<{ backup: BackupEntry }>("/v1/backups/create", {
    password,
  });
  return resp.backup;
}

export function startBackupImport(
  file: File,
  password: string,
  onUploadProgress?: (progress: UploadProgress) => void,
): Promise<BackupImportJob> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${BASE}/v1/backups/import`);
    xhr.withCredentials = true;
    xhr.setRequestHeader(TRUSTED_UI_HEADER, TRUSTED_UI_HEADER_VALUE);

    xhr.upload.onprogress = (event) => {
      if (!onUploadProgress) return;
      const total = event.lengthComputable && event.total > 0 ? event.total : file.size;
      const percent = total > 0 ? Math.min(100, Math.round((event.loaded / total) * 100)) : 0;
      onUploadProgress({
        loaded: event.loaded,
        total,
        percent,
      });
    };

    xhr.onload = () => {
      const raw = xhr.responseText ?? "";
      if (xhr.status < 200 || xhr.status >= 300) {
        const error = parseErrorJSON(xhr.status, raw);
        if (
          error.statusCode === 401 &&
          (error.code === "setup_required" ||
            error.code === "auth_required" ||
            error.code === "session_expired")
        ) {
          dispatchBrowserAuthEvent(error.code);
        }
        reject(error);
        return;
      }
      try {
        const payload = JSON.parse(raw) as { job?: BackupImportJob };
        if (!payload.job) {
          reject(new ApiError(xhr.status, "", "Missing backup import job"));
          return;
        }
        onUploadProgress?.({
          loaded: file.size,
          total: file.size,
          percent: 100,
        });
        resolve(payload.job);
      } catch {
        reject(new ApiError(xhr.status, "", "Invalid backup import response"));
      }
    };

    xhr.onerror = () => {
      reject(new ApiError(0, "", "Network error while uploading backup archive"));
    };

    xhr.onabort = () => {
      reject(new ApiError(0, "", "Backup archive upload aborted"));
    };

    const form = new FormData();
    form.append("file", file);
    form.append("password", password);
    xhr.send(form);
  });
}

export async function getBackupImportJob(jobID: string): Promise<BackupImportJob> {
  const resp = await get<{ job: BackupImportJob }>(
    `/v1/backups/import/jobs/${encodeURIComponent(jobID)}`,
  );
  return resp.job;
}

export function deleteBackup(id: string): Promise<void> {
  return post("/v1/backups/delete", { id });
}

export async function restoreBackup(
  id: string,
  password: string,
): Promise<BackupRestoreResponse> {
  return post("/v1/backups/restore", { id, password });
}

export async function downloadBackupArchive(id: string): Promise<void> {
  const resp = await fetchBinary(
    `/v1/backups/download?id=${encodeURIComponent(id)}`,
    { method: "GET" },
  );
  const blob = await resp.blob();
  const disposition = resp.headers.get("Content-Disposition") ?? "";
  const match = disposition.match(/filename="?([^"]+)"?/i);
  const fileName = match?.[1] ?? `backup-${id}.zip`;
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = fileName;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => window.URL.revokeObjectURL(url), 1000);
}

// ---------------------------------------------------------------------------
// Tool status (polling)
// ---------------------------------------------------------------------------

export function getToolStatus(
  sessionID: string,
): Promise<ToolStatusResult> {
  return get(
    `/v1/tool-status?session_id=${encodeURIComponent(sessionID)}`,
  );
}

// ---------------------------------------------------------------------------
// Browser security
// ---------------------------------------------------------------------------

export function getSecurityStatus(): Promise<SecurityStatus> {
  return get("/v1/security/status");
}

export function setupSecurity(
  req: SecuritySetupRequest,
): Promise<SecurityStatus> {
  return post("/v1/security/setup", req);
}

export function loginSecurity(
  req: SecurityLoginRequest,
): Promise<SecurityStatus> {
  return post("/v1/security/login", req);
}

export function logoutSecurity(): Promise<SecurityStatus> {
  return post("/v1/security/logout", {});
}

export function logoutAllSecurity(): Promise<SecurityStatus> {
  return post("/v1/security/logout-all", {});
}

export function getSecurityConfig(): Promise<SecurityConfig> {
  return get("/v1/security/config");
}

export function setSecurityConfig(config: SecurityConfig): Promise<SecurityConfig> {
  return put("/v1/security/config", config);
}

export function changeSecurityPassword(
  req: SecurityPasswordRequest,
): Promise<SecurityStatus> {
  return post("/v1/security/password", req);
}

// ---------------------------------------------------------------------------
// General / Compaction / Discord / Telegram / Tools config
// ---------------------------------------------------------------------------

export function getGeneralConfig(): Promise<GeneralConfig> {
  return get("/v1/general/config");
}

export function setGeneralConfig(config: GeneralConfig): Promise<void> {
  return put("/v1/general/config", config);
}

export function getCompactionConfig(): Promise<CompactionConfig> {
  return get("/v1/compaction/config");
}

export function setCompactionConfig(config: CompactionConfig): Promise<void> {
  return put("/v1/compaction/config", config);
}

export function getModelRolesConfig(): Promise<ModelRolesConfig> {
  return get("/v1/model-roles/config");
}

export function setModelRolesConfig(config: ModelRolesConfig): Promise<void> {
  return put("/v1/model-roles/config", config);
}

export async function listPlaybooks(status?: string): Promise<PlaybookEntry[]> {
  const suffix = status ? `?status=${encodeURIComponent(status)}` : "";
  const resp = await get<{ entries: PlaybookEntry[] }>(`/v1/playbooks${suffix}`);
  return resp.entries ?? [];
}

export function savePlaybook(entry: Partial<PlaybookEntry>): Promise<PlaybookEntry> {
  return post("/v1/playbooks", entry);
}

export function approvePlaybook(id: string): Promise<PlaybookEntry> {
  return post(`/v1/playbooks/${encodeURIComponent(id)}/approve`);
}

export function archivePlaybook(id: string): Promise<PlaybookEntry> {
  return post(`/v1/playbooks/${encodeURIComponent(id)}/archive`);
}

export function getTasks(sessionID: string): Promise<TaskList> {
  return get(`/v1/tasks?session_id=${encodeURIComponent(sessionID)}`);
}

export function saveTasks(list: TaskList): Promise<TaskList> {
  return put("/v1/tasks", list);
}

export async function getPermissions(): Promise<PermissionGrant[]> {
  const resp = await get<{ grants: PermissionGrant[] }>("/v1/permissions");
  return resp.grants ?? [];
}

export async function savePermissions(grants: PermissionGrant[]): Promise<PermissionGrant[]> {
  const resp = await put<{ grants: PermissionGrant[] }>("/v1/permissions", { grants });
  return resp.grants ?? [];
}

export async function listAutomations(): Promise<WorkflowRecord[]> {
  const resp = await get<{ automations: WorkflowRecord[] }>("/v1/automations");
  return resp.automations ?? [];
}

export function saveAutomation(record: Partial<WorkflowRecord>): Promise<WorkflowRecord> {
  if (record.id) {
    return put(`/v1/automations/${encodeURIComponent(record.id)}`, record);
  }
  return post("/v1/automations", record);
}

export function deleteAutomation(id: string): Promise<void> {
  return del(`/v1/automations/${encodeURIComponent(id)}`);
}

export function runAutomation(id: string, payload?: unknown): Promise<WorkflowRun> {
  return post(`/v1/automations/${encodeURIComponent(id)}/runs`, { payload });
}

export async function listSnapshots(sessionID: string): Promise<SnapshotRecord[]> {
  const resp = await get<{ snapshots: SnapshotRecord[] }>(
    `/v1/snapshots?session_id=${encodeURIComponent(sessionID)}`,
  );
  return resp.snapshots ?? [];
}

export function undoSnapshot(id: string): Promise<{ snapshot: SnapshotRecord; restored: boolean; message: string }> {
  return post(`/v1/snapshots/${encodeURIComponent(id)}/undo`);
}

export async function listTraces(sessionID: string): Promise<TraceRecord[]> {
  const resp = await get<{ traces: TraceRecord[] }>(
    `/v1/traces?session_id=${encodeURIComponent(sessionID)}`,
  );
  return resp.traces ?? [];
}

export function getDiscordConfig(): Promise<DiscordConfig> {
  return get("/v1/discord/config");
}

export function setDiscordConfig(config: DiscordConfig): Promise<void> {
  return put("/v1/discord/config", config);
}

export function getTelegramConfig(): Promise<TelegramConfig> {
  return get("/v1/telegram/config");
}

export function setTelegramConfig(config: TelegramConfig): Promise<void> {
  return put("/v1/telegram/config", config);
}

export async function getToolsCatalog(): Promise<ToolCatalogEntry[]> {
  const resp = await get<{ tools: ToolCatalogEntry[] }>("/v1/tools/catalog");
  return resp.tools ?? [];
}

export function getToolsConfig(): Promise<ToolsConfig> {
  return get("/v1/tools/config");
}

export function setToolsConfig(config: ToolsConfig): Promise<void> {
  return put("/v1/tools/config", config);
}

// re-export del for external use when needed
export { del as deleteRequest };
