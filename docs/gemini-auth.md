# Gemini OAuth 操作手冊

## 1) 前置條件

- API 服務可由本機存取（預設 `127.0.0.1:8085`）
- callback 預設 `http://127.0.0.1:8085/oauth2callback`
- OAuth client 預設從已安裝的 `gemini` CLI 讀取
- 容器或主機必須可執行 `gemini`

## 2) Loopback 流程（預設）

1. `POST /v1/auth/gemini/start`
2. 取得 `auth_url` 後，瀏覽器完成授權
3. OAuth callback 進入 `GET /oauth2callback`
4. 完成後以 `GET /v1/auth/gemini/profiles` 確認 profile 狀態

`start` 可指定 `mode`：

- `auto`（預設）：優先 loopback；Web UI 在 `localhost`/loopback host 會用這個模式
- `local`：強制 `127.0.0.1` loopback callback
- `remote`：使用指定的 `redirect_uri`，或走 manual/paste fallback

## 3) Manual 流程（fallback / remote）

當 callback 無法使用（例如遠端環境、popup 被阻擋，或你明確走 `remote` 模式），`start` 會回傳 `mode=manual`：

1. 開啟 `auth_url`
2. 取得 redirect URL（或僅 code）
3. 呼叫 `POST /v1/auth/gemini/manual/complete`：
   - `state`
   - `callback_url_or_code`

`remote` 模式仍然是為了 manual/paste fallback。若指定 `redirect_uri`，請維持在本機 loopback host/port；Gemini CLI OAuth 不會使用公網 callback。

## 4) Web UI 操作

Web UI（Settings -> Auth）：

- `Provider` -> `google-gemini-cli`
- `開始 OAuth`
- `重新整理`
- `使用此 profile`
- `完成 Gemini OAuth`（manual fallback 時貼 `127.0.0.1` callback URL 或 code）

> 不再手動選 endpoint / project。  
> endpoint 由系統自動 fallback；project 由 `loadCodeAssist/onboardUser` 自動決策。

目前 Web UI 會依瀏覽器 host 自動選擇模式：

- `localhost` / `127.0.0.1` / `::1`：優先 loopback callback
- 公網或反向代理 host：送 `remote` 模式，但仍使用 `127.0.0.1` loopback callback，完成後需手動貼回 callback URL 或 code

若 profile 仍然缺少 project_id，請重新執行 Gemini OAuth，並確認 `gemini` CLI 端的 provisioning 已完成。

## 5) 儲存模型

- 敏感 token：OS keychain（fallback 為本機加密檔）
- metadata：`profiles.json`（不含 access/refresh token）
- 預設資料夾：`~/.nekoclaw/auth`（可用 `NEKOCLAW_AUTH_DIR` 改寫）

## 6) 風險與注意事項

- Gemini internal API 屬非官方穩定公開介面，可能隨時變更。
- 若所有 profile 進入 cooldown/disabled，聊天會回傳不可用原因與可重試時間。
- Discord ingress 不直接執行 OAuth；需先在 API 或 Web UI 完成登入。
