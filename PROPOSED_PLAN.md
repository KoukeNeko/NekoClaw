# Proposed Plan — Claude Code CLI Provider

## 1. Context

**Problem.** NekoClaw 目前的 provider 都是直接呼叫遠端 HTTP API（Anthropic、Gemini、OpenAI、OpenCode、GitHub Models）。
個人使用者想用本地已登入的 **Claude Code CLI**（已透過個人 Pro/Max 訂閱認證）作為對話 backend，
不需要再保管 API key、不另計 API token 費用，純粹自用。

**Scope.** 只支援「對話聊天」，**不**支援 tool calling、不暴露給其他人使用、不嘗試規避 Anthropic ToS（CLI 訂閱僅限本人使用）。

**Non-goals.**
- ❌ Tool calling / function calling（CLI 雖能跑工具，但混入聊天語意複雜，且風險高）
- ❌ Streaming（v1 先不做；CLI 串流有 NDJSON 解析成本，blocking JSON 已足夠用）
- ❌ Image input（CLI 在 `-p` 模式下不接受多模態輸入）
- ❌ OAuth flow（CLI 自己管理登入態）
- ❌ Account 多帳號 pool（CLI 一次只認一個登入身份）

## 2. High-Level Design

新增 `internal/provider/claude_cli.go`，實作 `provider.Provider` interface（3 方法）。
**不**實作 `ToolCallingProvider`、`StreamingProvider`、`AuthProvider`、`ModelDiscoveryProvider`。
Service 層既有的「provider 不支援 tools 就降級」邏輯會自然處理。

### 呼叫策略

```
claude -p "<serialized_messages>" \
  --bare \
  --model <sonnet|opus|haiku|claude-xxx> \
  --output-format json \
  --no-session-persistence \
  --tools "" \
  --permission-mode bypassPermissions \
  --append-system-prompt "<system_prompt_if_any>"
```

關鍵 flag 選擇：
- `--bare` — 跳過 hooks / LSP / plugins / auto-memory / CLAUDE.md 探勘。聊天場景不需要這些。
- `--tools ""` — 完全禁用工具，避免模型在純聊天時意外觸發 Bash/Edit。
- `--no-session-persistence` — 不在 `~/.claude/projects/` 寫 session 檔，每次都 stateless。
- `--permission-mode bypassPermissions` — 因為已經 `--tools ""`，不會跑任何工具，bypass 只是避免任何 confirmation 卡住。
- `--output-format json` — 一次回傳 `{result, session_id, total_cost_usd, usage{...}}`，好解析。

### Multi-turn 對話策略

採 **Stateless：每次重送完整歷史**。把 `req.Messages` 序列化成單一 prompt 字串：

```
[system messages → 合併進 --append-system-prompt]
[user/assistant 交錯 → 序列化成帶角色標記的純文字 prompt]
```

理由：
- 對齊 NekoClaw 既有 `core.Message` 模型（service 層已管好歷史）
- 不依賴 CLI session 檔（`--no-session-persistence` 配合）
- Anthropic API 端的 prompt cache 仍會自然命中相同前綴

### 專案結構契合度

| 既有契約 | 我的對應 |
|---------|---------|
| `Provider.ID()` | 回 `"claude-cli"` |
| `Provider.ContextWindow(model)` | 200_000（Claude 4.x 全系列；用 model id 細分可選） |
| `Provider.Generate(ctx, req)` | spawn `claude -p ...`、解析 JSON、填 `GenerateResponse` |
| `core.UsageInfo` | 從 `usage.input_tokens` / `output_tokens` 抓出 |
| `provider.FailureError` | CLI exit code ≠ 0 / JSON 缺欄位 → 包成對應 `core.FailureReason` |
| `cmd/nekoclaw/main.go` 註冊 | 跟 `mockProvider` 一樣加一行 `svc.RegisterProvider(...)` |
| `accounts.json` | 不需要真實 token，但 service 層需要至少一個 account 才能進 pool；用一個 placeholder account（`Type: AccountAPIKey, Token: "cli"`）走完流程 |

### 設定項

`ClaudeCLIOptions`：
- `BinPath` — 預設 `"claude"`，可被 `NEKOCLAW_CLAUDE_CLI_BIN` 覆蓋
- `ContextWindow` — 預設 `200_000`
- `DefaultModel` — 預設 `"sonnet"`
- `Timeout` — 預設 `120 * time.Second`
- `WorkDir` — 預設 OS temp dir 下的 sandbox 子目錄；避免 CLI 汙染 cwd

## 3. Step-by-Step Implementation

依 incremental-implementation：每步可獨立編譯 + 測試。

| Step | 內容 | 驗收 |
|------|------|------|
| **S1** | 寫 harness：`scripts/verify_claude_cli.sh` — 直接呼叫 `claude -p` 驗證環境 | bash 跑出 exit 0 + 印出 result |
| **S2** | 新增 `internal/provider/claude_cli.go` 骨架 + `ID()` / `ContextWindow()` / `NewClaudeCLIProvider()` | `go build ./...` 通過 |
| **S3** | 實作 `serializeMessages()`、`mapModel()` 兩個純函式 + 表格驅動單元測試 | `go test ./internal/provider -run ClaudeCLI` 綠 |
| **S4** | 實作 `Generate()`：spawn process、解析 JSON、錯誤分類 | 單元測試以 fake binary（shell script）模擬 CLI |
| **S5** | 在 `cmd/nekoclaw/main.go` 註冊 provider + placeholder pool | `go build` 通過、啟動不 panic |
| **S6** | 端對端 harness：`scripts/verify_claude_cli_e2e.sh` — 啟動 server，打一個 chat request | 看到合法 chat response |
| **S7** | 文件：在 README 或 `docs/` 加一段「How to enable Claude CLI provider」 | 使用者照著走能跑起來 |

## 4. Verification Strategy

### 自動化 harness（不依賴人工點按）

**S1 / Standalone harness** — `scripts/verify_claude_cli.sh`：
```bash
#!/usr/bin/env bash
set -euo pipefail
out=$(claude -p "Reply with exactly the word OK" \
  --bare --model sonnet --output-format json \
  --no-session-persistence --tools "" \
  --permission-mode bypassPermissions)
echo "$out" | jq -e '.result | test("OK")' >/dev/null
echo "✅ claude CLI provider harness passed"
```

**S3-S4 / Go unit tests** — `internal/provider/claude_cli_test.go`：
- `TestSerializeMessages_*` — 表格驅動：純 user / system+user / multi-turn / 含特殊字元
- `TestMapModel_*` — `"sonnet"` → `"sonnet"`, `"claude-opus-4-7"` → `"claude-opus-4-7"`, `""` → default
- `TestGenerate_FakeBinary_*` — 用 `t.TempDir()` 寫一個 shell script 當作假 `claude`，驗證：
  - 成功回應（fake 印出 valid JSON）
  - exit 非 0 → `FailureError{Reason: FailureUnknown}`
  - 壞 JSON → `FailureError{Reason: FailureFormat}`
  - context cancel → 進程被 kill

**S6 / E2E harness** — `scripts/verify_claude_cli_e2e.sh`：
啟動 nekoclaw server（用 `--accounts` 指向 placeholder accounts.json），用 `curl` 打 chat endpoint，
驗證 HTTP 200 + 回傳 text 非空。

### Plan→Harness→Execute 守則

依 `~/.claude/CLAUDE.md`：harness 必須能自動跑出明確 success/failure，**不**請使用者手動測。

## 5. 潛在風險與緩解

| 風險 | 緩解 |
|------|------|
| CLI 冷啟動 1-3s 拖慢對話 | 文件中說明；v2 可考慮常駐 process pool |
| CLI 版本升級改變輸出格式 | 解析時對缺欄位寬容、有明確錯誤訊息；測試以 fake binary 鎖住合約 |
| 長 prompt 撞 ARG_MAX | 走 stdin（`--input-format text` + cmd.Stdin）而非 argv |
| 使用者沒登入 / 訂閱過期 | CLI 會回明確錯誤訊息 → 包成 `FailureAuthPermanent` |
| 工作目錄汙染 / 寫入意外檔案 | `cmd.Dir` 指 sandbox + `--no-session-persistence` + `--tools ""` |
| 不小心讓他人透過 NekoClaw 用到我的訂閱 | 文件中明確標示「個人本機自用」；不在 README 推薦 |

## 6. Open Questions（請使用者確認）

1. **Model 對應**：`req.Model` 進來可能長什麼樣？是「`sonnet` / `opus` / `haiku` 別名」還是「`claude-opus-4-7` 完整 id」？兩者都接，但要確認預設往哪邊靠。
   → **預設方案**：兩者都直接傳給 CLI（CLI 自己接受 alias 和完整 id）。空字串才 fallback 到 `DefaultModel`。

2. **System prompt 處理**：你的 `core.Message` 有 `RoleSystem`，多個 system message 要合併還是只取第一個？
   → **預設方案**：全部合併、用 `\n\n` 串接，整段傳給 `--append-system-prompt`。

3. **Account 設計**：CLI 不需要 token，但 service 層的 pool 機制需要至少一個 account。要不要在 `cmd/nekoclaw/main.go` 自動註冊一個 placeholder pool（不用 user 在 `accounts.json` 寫東西）？
   → **預設方案**：是。跟現有 `mockProvider` 一樣自動註冊一個 `claude-cli-default` account。

4. **是否要 streaming？** v1 我打算跳過。
   → **預設方案**：v1 跳過。如果後續想要再加 `StreamingProvider` 實作。

如果以上預設方案都 OK，我就直接進 Phase 2（寫 harness）+ Phase 3（實作）。
