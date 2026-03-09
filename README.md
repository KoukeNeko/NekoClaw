<div align="center">

<h1>NekoClaw</h1>

<p><strong>A self-hosted LLM agent runtime with Web UI, API, bots, MCP tools, memory, and provider failover.</strong></p>

<p>
  <img src="https://img.shields.io/github/actions/workflow/status/KoukeNeko/NekoClaw/docker-publish.yml?branch=main&style=for-the-badge&label=publish&labelColor=111827&color=22C55E" alt="Docker Publish">
  <img src="https://img.shields.io/badge/GO-1.24%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.24+">
  <img src="https://img.shields.io/badge/REACT-19-61DAFB?style=for-the-badge&logo=react&logoColor=0B1220" alt="React 19">
  <img src="https://img.shields.io/badge/SQLITE-FTS5-0F172A?style=for-the-badge&logo=sqlite&logoColor=white" alt="SQLite FTS5">
  <img src="https://img.shields.io/badge/DISCORD-BOT-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="Discord Bot">
  <img src="https://img.shields.io/badge/TELEGRAM-BOT-26A5E4?style=for-the-badge&logo=telegram&logoColor=white" alt="Telegram Bot">
  <img src="https://img.shields.io/badge/DOCKER-GHCR-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker GHCR">
</p>

<p>Browser chat UI · HTTP API · Multimodal input · Persistent memory · Provider failover · One deployable service</p>

<p>
  <a href="#quick-start">Quick Start</a> ·
  <a href="#web-ui--docker">Web UI / Docker</a> ·
  <a href="#api-endpoints">API</a> ·
  <a href="#discord-bot">Discord</a> ·
  <a href="#telegram-bot">Telegram</a> ·
  <a href="https://github.com/KoukeNeko/NekoClaw/actions/workflows/docker-publish.yml">Workflow</a>
</p>

</div>

---

NekoClaw is a Go-based LLM agent runtime that combines a browser chat UI, HTTP API, Discord bot, Telegram bot, MCP tools, multimodal inputs, persistent memory, and provider failover in one deployable service.

## Highlights

- Browser-based chat UI
- HTTP API and embedded Web UI
- Discord bot (emoji reactions, per-channel sessions, slash commands, image support)
- Telegram bot (per-chat sessions, slash commands, image support)
- Pluggable LLM provider architecture with automatic failover
- Account pool with health-based selection, cooldown escalation, and exponential backoff retry
- Per-model context window mapping (Gemini / Claude / GPT / O-series)
- Context compression (CJK-aware token estimation, LLM compaction, sliding window)
- Tool output head+tail truncation (preserves beginning and end of long outputs)
- Persistent channel-session bindings (survive bot restarts)
- Session lifecycle management (idle auto-expiry, retention cleanup, size rotation)
- Streaming response support across all frontends (Web UI, Discord, Telegram)
- Persona system with template rendering and few-shot anchors
- Memory system (long-term notes, daily logs, FTS5 search index)
- MCP (Model Context Protocol) tool integration
- Multimodal image support across all surfaces
- Real-time tool status display and usage stats

## Quick Start

For a zero-auth local smoke test, run the Web UI with the mock provider:

```bash
go run ./cmd/nekoclaw -mode web -provider mock
```

Run modes:

- `api` — API only
- `web` — API + embedded Web UI (default)

Defaults when flags are omitted:

- API: `127.0.0.1:8085`
- Mode: `web`
- Provider: `google-gemini-cli`
- Model: `default`
- Session: `main`

## Web UI / Docker

`-mode web` serves the embedded frontend from the same Go process. The Web UI includes chat, session history, live tool status, and settings panels for Provider, Persona, Auth, Sessions, Memory, Usage, MCP, Discord, Telegram, and Tools.

Local Web UI run:

```bash
cd web && npm ci && npm run build
go run ./cmd/nekoclaw -mode web -provider mock -addr 0.0.0.0:8085
```

Docker image:

```bash
docker build -t nekoclaw:local .
docker run --rm -p 8085:8085 -v nekoclaw-data:/data/.nekoclaw nekoclaw:local
```

The container defaults to `-mode web` and persists runtime state under `/data/.nekoclaw`.
The image also preinstalls Node.js, `@google/gemini-cli`, `@playwright/mcp`, and Chromium so Gemini OAuth plus the builtin Playwright MCP server can start without manual npm setup inside the container. Containerized Gemini OAuth now reads its OAuth client config from the bundled `gemini` CLI; no separate Gemini OAuth client env setup is required.

GitHub Actions can publish the image to GHCR as `ghcr.io/koukeneko/nekoclaw`:

- push to `main` -> `latest` and `sha-*`
- push `vX.Y.Z` -> `X.Y.Z`, `X.Y`, `X`, and `sha-*`

## Gemini Internal Provider

The project includes a `google-gemini-cli` provider that supports:

- endpoint fallback (`cloudcode-pa`, `daily-cloudcode-pa.sandbox`, `autopush-cloudcode-pa.sandbox`)
- quota query (`/v1internal:retrieveUserQuota`)
- project discovery/onboarding (`loadCodeAssist`, `onboardUser`, operation polling)

### OAuth Login (recommended)

Gemini OAuth flow supports:

- loopback callback (default `http://127.0.0.1:8085/oauth2callback`)
- browser-host aware startup (`localhost`/`127.0.0.1` keeps loopback flow; non-local origins still use manual completion against the loopback callback)
- manual fallback (paste callback URL or code)
- PKCE/state verification
- endpoint auto selection (`cloudcode-pa` -> `daily` -> `autopush`)
- project auto discovery via `loadCodeAssist/onboardUser` (if provisioning fails, re-run Gemini OAuth and confirm gemini CLI provisioning succeeded)
- token persistence: OS keychain + metadata JSON (no plaintext token in repo)

Web UI flow (Settings -> Auth):

- `開始 OAuth` starts the flow; Gemini CLI OAuth always uses the loopback callback, and public deployments finish via manual paste-back
- `重新整理` reloads profile and flow state after browser auth returns
- `Profiles` to inspect account availability and cooldown status
- `使用此 profile` to switch runtime profile
- `完成 Gemini OAuth` appears for manual fallback so you can paste the final `127.0.0.1` callback URL or auth code when loopback/popup flow is unavailable

API endpoints:

- `POST /v1/auth/gemini/start`
- `GET /oauth2callback`
- `POST /v1/auth/gemini/manual/complete`
- `GET /v1/auth/gemini/profiles`
- `POST /v1/auth/gemini/use`

`POST /v1/auth/gemini/start` request now also supports:

- `mode`: `auto` (default), `local`, `remote`
- `redirect_uri`: override the loopback callback URI when you need a custom local host/port

OAuth client source:

- Required: installed `gemini` CLI (NekoClaw extracts the OAuth client config from it)

Runtime state:

- OAuth state defaults to `~/.nekoclaw/auth`
- Web mode logs default to `~/.nekoclaw/logs/nekoclaw.log`
- Callback host/port default to `127.0.0.1:8085` unless overridden by CLI flags

## Google AI Studio Provider

The project includes a `google-ai-studio` provider that supports:

- API key profile management in the Web UI
- model listing via `GET /v1/ai-studio/models`
- default fallback target when no explicit fallback chain is configured

Web UI flow (Settings -> Auth):

- `a` add AI Studio API key
- `Enter` use selected profile
- `d` delete selected profile

API endpoints:

- `POST /v1/auth/ai-studio/add-key`
- `GET /v1/auth/ai-studio/profiles`
- `POST /v1/auth/ai-studio/use`
- `POST /v1/auth/ai-studio/delete`
- `GET /v1/ai-studio/models`

Credentials are managed through Web UI Settings > Auth and persisted in the auth store.

## Anthropic Provider (Claude setup-token / API key / browser login)

The project includes an `anthropic` provider that supports:

- Claude subscription setup-token (`sk-ant-oat01-...`)
- Anthropic API key
- browser login bridge with job polling and manual completion
- account pool rotation/cooldown/failover (`token` naturally preferred over `api_key`)
- default model runtime fallback (`claude-sonnet-4-6`)

Web UI flow (Settings -> Auth):

- `b` start Anthropic browser login
- `t` add Anthropic setup-token (masked input)
- `k` add Anthropic API key (masked input)
- `c` cancel active browser login job
- `Enter` use selected profile
- `d` delete selected profile

API endpoints:

- `POST /v1/auth/anthropic/add-token`
- `POST /v1/auth/anthropic/add-api-key`
- `GET /v1/auth/anthropic/profiles`
- `POST /v1/auth/anthropic/use`
- `POST /v1/auth/anthropic/delete`
- `POST /v1/auth/anthropic/browser/start`
- `POST /v1/auth/anthropic/browser/manual/complete`
- `POST /v1/auth/anthropic/browser/cancel`
- `GET /v1/auth/anthropic/browser/jobs/<job_id>`

Credentials are managed through Web UI Settings > Auth and persisted in the auth store.

## OpenAI / OpenAI Codex Providers

The project now includes:

- `openai` (API key path)
- `openai-codex` (OAuth token path + browser login bridge)

OpenClaw-aligned runtime behavior:

- separate providers and default models:
  - `openai` -> `gpt-5.1-codex`
  - `openai-codex` -> `gpt-5.3-codex`
- `openai` and `openai-codex` credentials are not mixed.
- if `provider=openai` has no API key but `openai-codex` OAuth exists, chat returns a clear guardrail error (use `openai-codex/...` or add an OpenAI API key in Web UI Settings > Auth).

Web UI flow (Settings -> Auth):

- `w` start OpenAI Codex browser login
- `p` add OpenAI API key
- `x` add OpenAI Codex OAuth token
- `c` cancel active browser login job
- `Enter` use selected profile
- `d` delete selected profile

API endpoints:

- `POST /v1/auth/openai/add-key`
- `POST /v1/auth/openai-codex/add-token`
- `POST /v1/auth/openai-codex/browser/start`
- `POST /v1/auth/openai-codex/browser/manual/complete`
- `POST /v1/auth/openai-codex/browser/cancel`
- `GET /v1/auth/openai-codex/browser/jobs/<job_id>`
- `GET /v1/auth/openai/profiles`
- `GET /v1/auth/openai-codex/profiles`
- `POST /v1/auth/openai/use`
- `POST /v1/auth/openai-codex/use`
- `POST /v1/auth/openai/delete`
- `POST /v1/auth/openai-codex/delete`

Credentials are managed through Web UI Settings > Auth and persisted in the auth store.

## Accounts File (optional)

Create `accounts.json` in repo root:

```json
{
  "accounts": [
    {
      "id": "gemini-1",
      "provider": "google-gemini-cli",
      "type": "oauth",
      "token": "<oauth-token>",
      "metadata": {
        "project_id": "my-project",
        "endpoint": "https://cloudcode-pa.googleapis.com"
      }
    },
    {
      "id": "openai-main",
      "provider": "openai",
      "type": "api_key",
      "token": "<openai-api-key>"
    },
    {
      "id": "openai-codex-main",
      "provider": "openai-codex",
      "type": "oauth",
      "token": "<openai-codex-oauth-token>"
    }
  ]
}
```

## Discord Bot

NekoClaw includes a built-in Discord bot that runs alongside all modes (`api` / `web`).

### Configuration

Set via Web UI Settings > Discord:

- **Bot Token** — persisted in `config.json`

Web UI settings also support:

- **Reply Mode** — Toggle whether the bot replies to the original message
- **Console Channel** — Channel ID for bot log output (startup, errors, session resets, persona changes)

### Bot Commands

| Command | Description |
|---------|-------------|
| `/reset` | Start a new conversation (old session preserved, accessible from Web UI Sessions) |
| `/persona` | Open the channel-visible persona selector dropdown |
| `/persona name:<name>` | Switch to a persona (case-insensitive, supports substring match) |
| `/persona name:off` | Deactivate current persona |

### Behavior

- Responds to: native slash commands, `@mention`, reply to bot, or DM
- Emoji lifecycle: 👀 (received) → 🔄 (processing) → ✅ (done)
- `/persona` renders a Discord select menu and updates the same message when the selection changes
- Placeholder message shows real-time MCP tool status; deleted on completion and replaced with a fresh reply
- Per-channel sequential message queue
- Each channel has its own independent session
- Channel-session bindings persist to `~/.nekoclaw/state/discord-bindings.json` (survive restarts)
- Idle sessions auto-rotate after 24 hours via background housekeeping
- Typing indicator every 8 seconds
- Image attachments are downloaded and sent as multimodal input
- Usage stats footer: elapsed time, token counts, throughput, provider/model
- Console channel logs detailed traffic (channel, user, message preview, model, tokens, tools, elapsed)

## Telegram Bot

NekoClaw includes a built-in Telegram bot using long polling and runs alongside all modes (`api` / `web`).

### Configuration

Set via Web UI Settings > Telegram:

- **Bot Token** — persisted in `config.json`

### Bot Commands

| Command | Description |
|---------|-------------|
| `/reset` | Start a new conversation |
| `/persona` | List available personas |
| `/persona <name>` | Switch to a persona |
| `/persona off` | Deactivate current persona |

### Behavior

- Responds to: private chat, `@mention`, reply to bot, or commands
- Placeholder message shows real-time MCP tool status; deleted on completion and replaced with a fresh reply
- Per-chat sequential message queue
- Each chat has its own independent session
- Chat-session bindings persist to `~/.nekoclaw/state/telegram-bindings.json` (survive restarts)
- Idle sessions auto-rotate after 24 hours via background housekeeping
- Typing indicator every 4 seconds
- Photo and image document attachments are downloaded and sent as multimodal input
- Usage stats footer: elapsed time, token counts, throughput, provider/model
- Message limit: 4096 characters (auto-split)

## Memory System

NekoClaw includes a persistent memory system that gives the LLM long-term context across sessions.

### Storage

```
~/.nekoclaw/memory/
├── MEMORY.md        # Long-term notes (manually or LLM-curated)
├── 2026-03-05.md    # Today's daily log
├── 2026-03-04.md    # Yesterday's daily log
└── search.db        # SQLite FTS5 full-text search index
```

Override path: `--memory-dir` flag.

### How It Works

1. **Read** — On every LLM request, `MEMORY.md` and the last 2 days of daily logs are loaded and injected as a system message (or embedded into the Persona template via `{{.Memory}}`)
2. **Write** — When context approaches the window limit, a silent LLM round extracts key information (decisions, preferences, code changes) and appends it to today's daily log
3. **Index** — After each chat turn, user and assistant messages are chunked (400 tokens, 80 overlap) and inserted into the FTS5 search index
4. **Search** — Web UI Settings > Memory tab, API `POST /v1/memory/search`, or the LLM `memory_search` tool

## API Endpoints

When `-mode web` is active, `/` serves the embedded SPA with client-side routing fallback. The API surface itself is grouped below.

Core:

- `GET /healthz`
- `GET /v1/providers`
- `GET /v1/accounts?provider=<id>`
- `GET /v1/models?provider=<id>&profile_id=<id>`
- `GET/PUT /v1/fallbacks`
- `GET/PUT /v1/default-provider`
- `POST /v1/chat`
- `POST /v1/chat/stream`
- `GET /v1/tool-status?session_id=<id>`

Provider auth:

- `GET /oauth2callback`
- `GET /v1/gemini/quota?provider=<id>&account_id=<id>`
- `POST /v1/gemini/discover-project`
- `POST /v1/auth/gemini/start`
- `POST /v1/auth/gemini/manual/complete`
- `GET /v1/auth/gemini/profiles`
- `POST /v1/auth/gemini/use`
- `POST /v1/auth/ai-studio/add-key`
- `GET /v1/auth/ai-studio/profiles`
- `POST /v1/auth/ai-studio/use`
- `POST /v1/auth/ai-studio/delete`
- `GET /v1/ai-studio/models?profile_id=<id>`
- `POST /v1/auth/anthropic/add-token`
- `POST /v1/auth/anthropic/add-api-key`
- `GET /v1/auth/anthropic/profiles`
- `POST /v1/auth/anthropic/use`
- `POST /v1/auth/anthropic/delete`
- `POST /v1/auth/anthropic/browser/start`
- `POST /v1/auth/anthropic/browser/manual/complete`
- `POST /v1/auth/anthropic/browser/cancel`
- `GET /v1/auth/anthropic/browser/jobs/<job_id>`
- `POST /v1/auth/openai/add-key`
- `POST /v1/auth/openai-codex/add-token`
- `POST /v1/auth/openai-codex/browser/start`
- `POST /v1/auth/openai-codex/browser/manual/complete`
- `POST /v1/auth/openai-codex/browser/cancel`
- `GET /v1/auth/openai-codex/browser/jobs/<job_id>`
- `GET /v1/auth/openai/profiles`
- `GET /v1/auth/openai-codex/profiles`
- `POST /v1/auth/openai/use`
- `POST /v1/auth/openai-codex/use`
- `POST /v1/auth/openai/delete`
- `POST /v1/auth/openai-codex/delete`

Sessions, usage, and memory:

- `GET /v1/sessions`
- `POST /v1/sessions/delete`
- `POST /v1/sessions/rename`
- `GET /v1/sessions/transcript?session_id=<id>`
- `GET /v1/usage/summary`
- `POST /v1/memory/search`

Config and tools:

- `GET/PUT /v1/general/config`
- `GET/PUT /v1/discord/config`
- `GET/PUT /v1/telegram/config`
- `GET /v1/tools/catalog`
- `GET/PUT /v1/tools/config`

MCP and personas:

- `GET /v1/mcp/servers`
- `GET /v1/mcp/tools`
- `GET /v1/mcp/builtin`
- `POST /v1/mcp/builtin/toggle`
- `GET /v1/mcp/configs`
- `POST /v1/mcp/configs/save`
- `POST /v1/mcp/configs/delete`
- `POST /v1/mcp/configs/apply`
- `GET /v1/personas`
- `GET /v1/personas/active`
- `POST /v1/personas/use`
- `POST /v1/personas/clear`
- `POST /v1/personas/reload`

Integrations:

- `POST /v1/integrations/discord/events`

## Context Window & Compression

NekoClaw uses a multi-layer strategy to keep conversations within model limits:

1. **Per-model context windows** — Each model (e.g. `gemini-2.5-pro` = 1M, `claude-sonnet-4` = 200K) has its own context window size via longest-prefix lookup, falling back to provider defaults
2. **CJK-aware token estimation** — Chinese/Japanese/Korean characters are weighted at ~1.5 tokens each instead of the naive 4-chars-per-token heuristic
3. **Sliding window compression** — When context approaches the limit, oldest messages are trimmed while preserving the system prompt
4. **LLM compaction** — Older messages are summarized by the LLM into a compact digest (skipped when fewer than 3 entries would be dropped)
5. **Post-injection guard** — After system prompt (persona + memory) is prepended, a final trim ensures total tokens stay within budget
6. **Tool output head+tail truncation** — Long tool results keep the first 40% and last 40% with a truncation marker, preserving both context and final output

## Account Management

The account pool supports:

- **Health-based selection** — Accounts are sorted by success rate (cumulative `SuccessCount / Total`), then error count, then type preference (OAuth > token > API key), then round-robin
- **Exponential backoff retry** — Rate-limited requests retry up to 3 times with `base * 2^retry` delay (500ms jitter), using a 5s default base when the server omits `Retry-After`
- **Cooldown escalation** — Billing/auth failures trigger escalating disable periods; rate limits use server-provided hints capped at 2 minutes
- **Circuit breaker** — 3+ consecutive model capacity failures trigger a 5-minute global cooldown across all accounts
- **Fallback chain** — When primary provider is exhausted, automatically tries configured fallback providers

## Architecture Notes

See:

- [`docs/openclaw-research.md`](./docs/openclaw-research.md) for extracted OpenClaw architecture mapping.
- [`docs/gemini-auth.md`](./docs/gemini-auth.md) for OAuth operation manual and risk notes.
