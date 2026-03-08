/**
 * Typed fetch wrapper — mirrors internal/client/api_client.go.
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
  PersonaInfo,
  MemorySearchRequest,
  MemorySearchResponse,
  DiscordConfig,
  TelegramConfig,
  ToolsConfig,
  ToolStatusResult,
  TranscriptEntry,
} from "./types";

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

async function fetchJSON<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const resp = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  if (!resp.ok) throw await parseErrorBody(resp);
  // Some endpoints return 204 No Content
  if (resp.status === 204) return undefined as unknown as T;
  return resp.json();
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

export function toggleMCPBuiltin(
  name: string,
  enabled: boolean,
): Promise<void> {
  return post("/v1/mcp/builtin/toggle", { name, enabled });
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
// Discord / Telegram / Tools config
// ---------------------------------------------------------------------------

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

export function getToolsConfig(): Promise<ToolsConfig> {
  return get("/v1/tools/config");
}

export function setToolsConfig(config: ToolsConfig): Promise<void> {
  return put("/v1/tools/config", config);
}

// re-export del for external use when needed
export { del as deleteRequest };
