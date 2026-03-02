# Gemini OAuth 操作手冊

## 1) 前置條件

- API 服務可由本機存取（預設 `127.0.0.1:8085`）
- callback 預設 `http://localhost:8085/oauth2callback`
- 如需覆蓋 OAuth client：
  - `OPENCLAW_GEMINI_OAUTH_CLIENT_ID`
  - `OPENCLAW_GEMINI_OAUTH_CLIENT_SECRET`
  - 或相容別名 `GEMINI_CLI_OAUTH_CLIENT_ID` / `GEMINI_CLI_OAUTH_CLIENT_SECRET`

## 2) Loopback 流程（預設）

1. `POST /v1/auth/gemini/start`
2. 取得 `auth_url` 後，瀏覽器完成授權
3. OAuth callback 進入 `GET /oauth2callback`
4. 完成後以 `GET /v1/auth/gemini/profiles` 確認 profile 狀態

`start` 可指定 `mode`：

- `auto`（預設）：本機可用時走 loopback，否則 manual
- `local`：優先 loopback（localhost callback）
- `remote`：強制 manual/paste callback

## 3) Manual 流程（fallback / remote）

當 callback 無法使用（例如遠端環境），`start` 會回傳 `mode=manual`：

1. 開啟 `auth_url`
2. 取得 redirect URL（或僅 code）
3. 呼叫 `POST /v1/auth/gemini/manual/complete`：
   - `state`
   - `callback_url_or_code`

`remote` 模式可搭配 `redirect_uri` 指到公網 callback（例如反向代理後的 `/oauth2callback`）。

## 4) TUI 指令

改為選單操作（方向鍵 + Enter）：

- `Provider` -> `google-gemini-cli`
- `OAuth Auto` / `OAuth Local` / `OAuth Remote`
- `Manual Complete`（remote/manual 時貼 callback URL 或 code）
- `Profiles` / `Use Profile`

> 不再手動選 endpoint / project。  
> endpoint 由系統自動 fallback；project 由 `loadCodeAssist/onboardUser` 自動決策。

若帳號 tier 需要明確 project，請設定：

- `GOOGLE_CLOUD_PROJECT`
- 或 `GOOGLE_CLOUD_PROJECT_ID`

## 5) 儲存模型

- 敏感 token：OS keychain（fallback 為本機加密檔）
- metadata：`profiles.json`（不含 access/refresh token）
- 預設資料夾：`~/.nekoclaw/auth`（可用 `NEKOCLAW_AUTH_DIR` 改寫）

## 6) 風險與注意事項

- Gemini internal API 屬非官方穩定公開介面，可能隨時變更。
- 若所有 profile 進入 cooldown/disabled，聊天會回傳不可用原因與可重試時間。
- Discord ingress 不直接執行 OAuth；需先在 API 或 TUI 完成登入。
