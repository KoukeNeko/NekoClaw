# OpenClaw 深入研究摘要（對齊 Go 實作）

本文整理 `openclaw/openclaw` 中與本專案最相關的四個主題：

1. LLM provider 架構
2. Gemini internal API (`cloudcode-pa` / `daily-cloudcode-pa.sandbox`)
3. 帳號池冷卻與分配機制
4. 上下文壓縮（compaction / sliding window / tool-result trimming）

## 1) Provider 架構

OpenClaw 將 provider 分成兩層：

- 模型與 auth 能力層（`src/agents/model-auth.ts`, `src/agents/models-config.providers.ts`）
- Plugin provider 註冊層（`src/plugins/providers.ts`, `src/plugins/loader.ts`）

主要特徵：

- Provider ID 統一規範化（normalization）
- auth 來源優先順序：profile -> env -> config
- provider plugin 可獨立註冊 auth flow（OAuth/token）
- run loop 內支援 provider/model fallback 與 profile failover

本專案對應：

- `internal/provider/provider.go`：Provider 介面
- `internal/app/service.go`：provider 路由 + failover
- `internal/api/server.go`：對外 API 供 TUI/Web/Discord 共用

## 2) Gemini internal API（重點）

OpenClaw 內建 Gemini CLI OAuth plugin（`extensions/google-gemini-cli-auth/`）的核心點：

- 端點常數：
  - `https://cloudcode-pa.googleapis.com`
  - `https://daily-cloudcode-pa.sandbox.googleapis.com`
  - (另有 `autopush`)
- 流程：
  - OAuth 取 token
  - `loadCodeAssist` 嘗試取得 tier/project
  - 需要時 `onboardUser` + poll operation
  - quota 讀取走 `v1internal:retrieveUserQuota`

參考檔：

- `extensions/google-gemini-cli-auth/oauth.ts`
- `src/infra/provider-usage.fetch.gemini.ts`
- `src/infra/provider-usage.auth.ts`

本專案對應：

- `internal/provider/gemini_internal.go`
  - `RetrieveQuota()`
  - `DiscoverProject()`
  - prod/daily endpoint fallback
  - provider 生成接口（`Generate`）

注意：OpenClaw 文件也明確標示此整合為 unofficial，存在帳號風險（plugin README）。

## 3) 帳號池冷卻分配機制

OpenClaw 主要邏輯分散在：

- `src/agents/auth-profiles/order.ts`
- `src/agents/auth-profiles/usage.ts`
- `src/agents/pi-embedded-runner/run.ts`
- `src/agents/model-fallback.ts`

關鍵機制：

- 分類失敗原因：`auth`, `auth_permanent`, `billing`, `rate_limit`, `timeout`, ...
- cooldown/backoff：
  - 一般錯誤：1m, 5m, 25m, 60m cap
  - billing/permanent auth：較長 disabled backoff（預設 5h 起，cap 24h）
- 清除過期 cooldown 時重置 error counters
- 選號策略：
  - 可用帳號優先
  - in-cooldown 帳號放後面（按最早恢復排序）
  - round-robin / lastUsed 與帳號型別偏好（oauth > token > api_key）

本專案對應：

- `internal/core/account_pool.go`
  - `Acquire`, `MarkUsed`, `MarkFailure`
  - failure window + cooldown/disabled
  - `ResolveUnavailableReason`

## 4) 語言模型壓縮與滑動視窗

OpenClaw 在 compaction/context pruning 的做法是混合策略：

- 長對話 compaction（摘要舊訊息）
- tool result trimming（soft trim）
- tool result hard clear
- cache-ttl context pruning
- 過大訊息時再配 sliding/chunked summarize

重點檔：

- `src/agents/compaction.ts`
- `src/agents/pi-extensions/compaction-safeguard.ts`
- `src/agents/pi-extensions/context-pruning/pruner.ts`
- `src/agents/pi-embedded-runner/tool-result-truncation.ts`

本專案對應：

- `internal/contextwindow/compressor.go`
  - soft trim（head/tail）
  - hard clear placeholder
  - sliding-window（保留新訊息 + 系統提示）

## 5) 目前 Go 版本的實作邊界

已完成：

- Provider 抽象
- Gemini internal endpoint fallback + quota/project API
- Account pool 冷卻與 failover
- TUI + HTTP API + Discord ingress endpoint

尚待你後續選擇是否擴充：

- 真正 Discord Bot gateway（目前是 HTTP ingress）
- 進階 compaction（LLM summary 串接）
- provider-specific tool schema normalization（類似 OpenClaw Gemini 修正）
- 持久化 session/account 狀態（目前在記憶體）
