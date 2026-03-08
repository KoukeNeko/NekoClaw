// ---------------------------------------------------------------------------
// Core types — mirrors internal/core/types.go
// ---------------------------------------------------------------------------

export type MessageRole = "system" | "user" | "assistant" | "tool";
export type Surface = "tui" | "discord" | "telegram" | "web";
export type AccountType = "oauth" | "token" | "api_key";
export type ChatStatus = "completed" | "approval_required";

export interface ImageData {
  mime_type: string;
  data: string; // base64
  file_name?: string;
}

export interface Message {
  role: MessageRole;
  content: string;
  images?: ImageData[];
  tool_name?: string;
  tool_call_id?: string;
  created_at: string; // ISO 8601
}

export interface ToolApprovalDecision {
  approval_id: string;
  decision: "allow" | "deny";
}

export interface PendingToolApproval {
  approval_id: string;
  tool_call_id?: string;
  tool_name: string;
  arguments_preview?: string;
  risk_level?: string;
  reason?: string;
}

export interface ToolEvent {
  at: string;
  tool_call_id?: string;
  tool_name?: string;
  phase?: string; // requested | approved | denied | executed | failed
  mutating?: boolean;
  decision?: string;
  output_preview?: string;
  error?: string;
}

export interface FallbackEntry {
  provider: string;
  model: string;
}

export interface UsageInfo {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
}

export interface CompressionMeta {
  original_tokens: number;
  compressed_tokens: number;
  dropped_messages: number;
  soft_trimmed: number;
  hard_cleared: number;
}

// ---------------------------------------------------------------------------
// Chat request / response
// ---------------------------------------------------------------------------

export interface ChatRequest {
  session_id: string;
  disable_session?: boolean;
  ephemeral_messages?: Message[];
  surface: Surface;
  provider: string;
  model: string;
  message: string;
  images?: ImageData[];
  enable_tools?: boolean;
  run_id?: string;
  tool_approvals?: ToolApprovalDecision[];
}

export interface ChatResponse {
  session_id: string;
  provider: string;
  model: string;
  reply: string;
  compressed: boolean;
  compression: CompressionMeta;
  account_id?: string;
  usage: UsageInfo;
  elapsed_ms?: number;
  status?: ChatStatus;
  run_id?: string;
  pending_approvals?: PendingToolApproval[];
  tool_events?: ToolEvent[];
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

export type StreamChunkType =
  | "text"
  | "tool_status"
  | "retry_status"
  | "error"
  | "done";

export interface StreamChunk {
  type: StreamChunkType;
  content?: string;
  tool_name?: string;
  tool_phase?: string;
  retry_status?: string;
  response?: ChatResponse;
  error?: string;
}

// ---------------------------------------------------------------------------
// Session
// ---------------------------------------------------------------------------

export interface SessionInfo {
  session_id: string;
  title?: string;
  message_count: number;
  created_at: string;
  updated_at: string;
}

export interface SessionMetadata {
  session_id: string;
  title?: string;
  message_count: number;
  input_tokens: number;
  output_tokens: number;
  context_tokens: number;
  compaction_count: number;
  created_at: string;
  updated_at: string;
}

// ---------------------------------------------------------------------------
// Auth — Gemini
// ---------------------------------------------------------------------------

export interface GeminiAuthStartRequest {
  profile_id?: string;
  mode?: "auto" | "local" | "remote";
  redirect_uri?: string;
}

export interface GeminiAuthStartResponse {
  auth_url: string;
  state: string;
  redirect_uri: string;
  expires_at: string;
  mode: string;
  oauth_mode?: string;
}

export interface GeminiAuthManualCompleteRequest {
  state: string;
  callback_url_or_code: string;
}

export interface GeminiAuthCompleteResponse {
  profile_id: string;
  provider: string;
  email?: string;
  project_id: string;
  active_endpoint?: string;
}

export interface GeminiAuthProfile {
  profile_id: string;
  provider: string;
  type: string;
  email?: string;
  project_id?: string;
  project_ready: boolean;
  unavailable_reason?: string;
  endpoint?: string;
  created_at: string;
  updated_at: string;
  available: boolean;
  cooldown_until?: string;
  disabled_until?: string;
  disabled_reason?: string;
  preferred: boolean;
}

// ---------------------------------------------------------------------------
// Auth — AI Studio
// ---------------------------------------------------------------------------

export interface AIStudioAddKeyRequest {
  api_key: string;
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
}

export interface AIStudioAddKeyResponse {
  profile_id: string;
  provider: string;
  display_name: string;
  key_hint: string;
  preferred: boolean;
  available: boolean;
}

export interface AIStudioProfile {
  profile_id: string;
  provider: string;
  type: string;
  display_name?: string;
  key_hint?: string;
  created_at: string;
  updated_at: string;
  available: boolean;
  cooldown_until?: string;
  disabled_until?: string;
  disabled_reason?: string;
  preferred: boolean;
}

export interface AIStudioModelsResponse {
  provider: string;
  profile_id: string;
  models: string[];
  source: string;
  cached_until?: string;
}

// ---------------------------------------------------------------------------
// Auth — Anthropic
// ---------------------------------------------------------------------------

export interface AnthropicAddTokenRequest {
  setup_token: string;
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
}

export interface AnthropicAddAPIKeyRequest {
  api_key: string;
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
}

export interface AnthropicAddCredentialResponse {
  profile_id: string;
  provider: string;
  type: string;
  display_name: string;
  key_hint: string;
  preferred: boolean;
  available: boolean;
}

export interface AnthropicProfile {
  profile_id: string;
  provider: string;
  type: string;
  display_name?: string;
  key_hint?: string;
  created_at: string;
  updated_at: string;
  available: boolean;
  cooldown_until?: string;
  disabled_until?: string;
  disabled_reason?: string;
  preferred: boolean;
}

export interface AnthropicBrowserStartRequest {
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
  mode?: string;
}

export interface AnthropicBrowserStartResponse {
  job_id: string;
  provider: string;
  mode: string;
  status: string;
  expires_at: string;
  message?: string;
  manual_hint?: string;
}

export interface AnthropicBrowserJobEvent {
  at: string;
  message: string;
}

export interface AnthropicBrowserJobResponse {
  job_id: string;
  provider: string;
  mode: string;
  status: string;
  events?: AnthropicBrowserJobEvent[];
  profile_id?: string;
  key_hint?: string;
  expires_at: string;
  message?: string;
  manual_hint?: string;
  error_code?: string;
  error_message?: string;
}

export interface AnthropicBrowserManualCompleteRequest {
  job_id: string;
  setup_token: string;
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
}

// ---------------------------------------------------------------------------
// Auth — OpenAI / OpenAI Codex
// ---------------------------------------------------------------------------

export interface OpenAIAddKeyRequest {
  api_key: string;
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
}

export interface OpenAICodexAddTokenRequest {
  token: string;
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
}

export interface OpenAICodexBrowserStartRequest {
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
  mode?: string;
}

export interface OpenAICodexBrowserStartResponse {
  job_id: string;
  provider: string;
  mode: string;
  status: string;
  expires_at: string;
  message?: string;
  manual_hint?: string;
}

export interface OpenAICodexBrowserJobEvent {
  at: string;
  message: string;
}

export interface OpenAICodexBrowserJobResponse {
  job_id: string;
  provider: string;
  mode: string;
  status: string;
  events?: OpenAICodexBrowserJobEvent[];
  profile_id?: string;
  key_hint?: string;
  expires_at: string;
  message?: string;
  manual_hint?: string;
  error_code?: string;
  error_message?: string;
}

export interface OpenAICodexBrowserManualCompleteRequest {
  job_id: string;
  token: string;
  display_name?: string;
  profile_id?: string;
  set_preferred?: boolean;
}

export interface OpenAIAddCredentialResponse {
  profile_id: string;
  provider: string;
  type: string;
  display_name: string;
  key_hint: string;
  preferred: boolean;
  available: boolean;
}

export interface OpenAIProfile {
  profile_id: string;
  provider: string;
  type: string;
  display_name?: string;
  key_hint?: string;
  created_at: string;
  updated_at: string;
  available: boolean;
  cooldown_until?: string;
  disabled_until?: string;
  disabled_reason?: string;
  preferred: boolean;
}

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

export interface ModelsResponse {
  provider: string;
  profile_id: string;
  models: string[];
  source: string;
}

// ---------------------------------------------------------------------------
// Default provider
// ---------------------------------------------------------------------------

export interface DefaultProviderResponse {
  provider: string;
  model: string;
}

export interface SetDefaultProviderRequest {
  provider: string;
  model: string;
}

// ---------------------------------------------------------------------------
// Fallbacks
// ---------------------------------------------------------------------------

export interface FallbacksResponse {
  fallbacks: FallbackEntry[];
}

export interface SetFallbacksRequest {
  fallbacks: FallbackEntry[];
}

// ---------------------------------------------------------------------------
// MCP
// ---------------------------------------------------------------------------

export interface MCPServer {
  name: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
  enabled: boolean;
  tools?: MCPTool[];
}

export interface MCPTool {
  server: string;
  name: string;
  description: string;
}

export interface MCPBuiltinServer {
  name: string;
  enabled: boolean;
  tool_count: number;
}

// ---------------------------------------------------------------------------
// Personas
// ---------------------------------------------------------------------------

export interface PersonaInfo {
  id: string;
  dir_name: string;
  name: string;
  description: string;
}

// ---------------------------------------------------------------------------
// Memory
// ---------------------------------------------------------------------------

export interface MemorySearchRequest {
  query: string;
  limit?: number;
}

export interface MemorySearchResult {
  content: string;
  score: number;
  source: string;
}

export interface MemorySearchResponse {
  results: MemorySearchResult[];
}

// ---------------------------------------------------------------------------
// Tool status
// ---------------------------------------------------------------------------

export interface ToolStatusResult {
  tool_name: string;
  retry_status: string;
}

// ---------------------------------------------------------------------------
// Discord / Telegram config
// ---------------------------------------------------------------------------

export interface DiscordConfig {
  bot_token: string;
  reply_mode: boolean;
  console_channel: string;
}

export interface TelegramConfig {
  bot_token: string;
}

// ---------------------------------------------------------------------------
// Tools config
// ---------------------------------------------------------------------------

export interface ToolsConfig {
  brave_api_key: string;
}

// ---------------------------------------------------------------------------
// Session transcript — matches Go TranscriptMessage struct
// (API already filters to user/assistant only, strips image base64)
// ---------------------------------------------------------------------------

export interface TranscriptEntry {
  role: MessageRole;
  content: string;
  image_names?: string[];
  created_at: string;
  // Assistant response metadata (populated for role=assistant only)
  provider?: string;
  model?: string;
  usage?: UsageInfo;
  tool_events?: ToolEvent[];
  elapsed_ms?: number;
}
