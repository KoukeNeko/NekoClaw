# NekoClaw

Go-based agent runtime with:

- TUI chat client
- HTTP API for future Web UI
- Discord bot (emoji reactions, per-channel sessions, slash commands, image support)
- Telegram bot (per-chat sessions, slash commands, image support)
- Pluggable LLM provider architecture with automatic failover
- Account pool with health-based selection, cooldown escalation, and exponential backoff retry
- Per-model context window mapping (Gemini / Claude / GPT / O-series)
- Context compression (CJK-aware token estimation, LLM compaction, sliding window)
- Tool output head+tail truncation (preserves beginning and end of long outputs)
- Persistent channel-session bindings (survive bot restarts)
- Session lifecycle management (idle auto-expiry, retention cleanup, size rotation)
- Streaming response support across all frontends (TUI, Discord, Telegram)
- Persona system with template rendering and few-shot anchors
- Memory system (long-term notes, daily logs, FTS5 search index)
- MCP (Model Context Protocol) tool integration
- Multimodal image support across all surfaces
- Real-time tool status display and usage stats

## Quick Start

```bash
go run ./cmd/nekoclaw -mode both
```

Defaults:

- API: `127.0.0.1:8085`
- TUI -> API: `http://127.0.0.1:8085`
- Provider: `mock`

## Gemini Internal Provider

The project includes a `google-gemini-cli` provider that supports:

- endpoint fallback (`cloudcode-pa`, `daily-cloudcode-pa.sandbox`, `autopush-cloudcode-pa.sandbox`)
- quota query (`/v1internal:retrieveUserQuota`)
- project discovery/onboarding (`loadCodeAssist`, `onboardUser`, operation polling)

### OAuth Login (recommended)

Gemini OAuth flow supports:

- localhost callback (default `http://localhost:8085/oauth2callback`)
- manual fallback (paste callback URL or code)
- PKCE/state verification
- endpoint auto selection (`cloudcode-pa` -> `daily` -> `autopush`)
- project auto discovery via `loadCodeAssist/onboardUser` (tier needs may require `GOOGLE_CLOUD_PROJECT` or `GOOGLE_CLOUD_PROJECT_ID`)
- token persistence: OS keychain + metadata JSON (no plaintext token in repo)

TUI menu flow (arrow keys + Enter):

- `Provider` -> select `google-gemini-cli`
- `OAuth Auto` (auto detect local/remote), or:
  - `OAuth Local` force localhost callback mode
  - `OAuth Remote` force manual mode
- `Profiles` to inspect account availability and cooldown status
- `Use Profile` to switch runtime profile
- `Manual Complete` to paste callback URL or code when running in remote mode

API endpoints:

- `POST /v1/auth/gemini/start`
- `GET /oauth2callback`
- `POST /v1/auth/gemini/manual/complete`
- `GET /v1/auth/gemini/profiles`
- `POST /v1/auth/gemini/use`

`POST /v1/auth/gemini/start` request now also supports:

- `mode`: `auto` (default), `local`, `remote`
- `redirect_uri`: override callback URI (useful in `remote` mode)

OAuth client env:

- `OPENCLAW_GEMINI_OAUTH_CLIENT_ID`
- `OPENCLAW_GEMINI_OAUTH_CLIENT_SECRET`
- `GEMINI_CLI_OAUTH_CLIENT_ID`
- `GEMINI_CLI_OAUTH_CLIENT_SECRET`

Runtime OAuth env:

- `OPENCLAW_GEMINI_OAUTH_CALLBACK_HOST` (default: `localhost`)
- `OPENCLAW_GEMINI_OAUTH_CALLBACK_PORT` (default: `8085`)
- `NEKOCLAW_AUTH_DIR` (default: `~/.nekoclaw/auth`)
- `NEKOCLAW_LOG_FILE` (optional; in `tui/both` mode defaults to `~/.nekoclaw/logs/nekoclaw.log`)
- `NEKOCLAW_LOG_STDERR=1` (optional; keep logs on terminal; may break TUI rendering)

### Token from env (optional)

```bash
export GEMINI_INTERNAL_TOKEN="<oauth-access-token>"
# or multiple:
export GEMINI_INTERNAL_TOKENS="token1,token2"
export GEMINI_INTERNAL_PROJECT_ID="my-gcp-project"
go run ./cmd/nekoclaw -mode both -provider google-gemini-cli -model gemini-3-pro-preview
```

## Anthropic Provider (Claude setup-token / API key)

The project includes an `anthropic` provider that supports:

- Claude subscription setup-token (`sk-ant-oat01-...`)
- Anthropic API key
- account pool rotation/cooldown/failover (`token` naturally preferred over `api_key`)
- default model runtime fallback (`claude-sonnet-4-6`)

TUI auth flow (Auth section):

- `t` add Anthropic setup-token (masked input)
- `k` add Anthropic API key (masked input)
- `Enter` use selected profile
- `d` delete selected profile

API endpoints:

- `POST /v1/auth/anthropic/add-token`
- `POST /v1/auth/anthropic/add-api-key`
- `GET /v1/auth/anthropic/profiles`
- `POST /v1/auth/anthropic/use`
- `POST /v1/auth/anthropic/delete`

Env credential loading (optional):

- `ANTHROPIC_OAUTH_TOKEN` / `ANTHROPIC_SETUP_TOKEN`
- `ANTHROPIC_OAUTH_TOKENS` (CSV)
- `ANTHROPIC_OAUTH_TOKEN_*`
- `ANTHROPIC_API_KEY`
- `ANTHROPIC_API_KEYS` (CSV)
- `ANTHROPIC_API_KEY_*`
- `ANTHROPIC_BASE_URL` (default `https://api.anthropic.com`)

## OpenAI / OpenAI Codex Providers

The project now includes:

- `openai` (API key path)
- `openai-codex` (OAuth token path)

OpenClaw-aligned runtime behavior:

- separate providers and default models:
  - `openai` -> `gpt-5.1-codex`
  - `openai-codex` -> `gpt-5.3-codex`
- `openai` and `openai-codex` credentials are not mixed.
- if `provider=openai` has no API key but `openai-codex` OAuth exists, chat returns a clear guardrail error (use `openai-codex/...` or set `OPENAI_API_KEY`).

Env credential loading (optional):

- OpenAI API key:
  - `OPENAI_API_KEY`
  - `OPENAI_API_KEYS` (CSV)
  - `OPENAI_API_KEY_*`
- OpenAI Codex OAuth token:
  - `OPENAI_OAUTH_TOKEN`
  - `OPENAI_OAUTH_TOKENS` (CSV)
  - `OPENAI_OAUTH_TOKEN_*`
  - `OPENAI_CODEX_OAUTH_TOKEN`
  - `OPENAI_CODEX_OAUTH_TOKENS` (CSV)
  - `OPENAI_CODEX_OAUTH_TOKEN_*`
- Base URL:
  - `OPENAI_BASE_URL` (default `https://api.openai.com/v1`)
  - `OPENAI_CODEX_BASE_URL` (optional override for `openai-codex`)

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

NekoClaw includes a built-in Discord bot that runs alongside all modes (api/tui/both).

### Configuration

Set via environment variable or TUI Settings > Discord:

- `DISCORD_BOT_TOKEN` — Bot token (env takes precedence over config.json)

TUI settings also support:

- **Reply Mode** — Toggle whether the bot replies to the original message
- **Console Channel** — Channel ID for bot log output (startup, errors, session resets, persona changes)

### Bot Commands

| Command | Description |
|---------|-------------|
| `/reset` | Start a new conversation (old session preserved, accessible from TUI Sessions) |
| `/persona` | List available personas |
| `/persona <name>` | Switch to a persona (case-insensitive, supports substring match) |
| `/persona off` | Deactivate current persona |

### Behavior

- Responds to: `@mention`, reply to bot, or DM
- Emoji lifecycle: 👀 (received) → 🔄 (processing) → ✅ (done)
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

NekoClaw includes a built-in Telegram bot using long polling.

### Configuration

Set via environment variable or TUI Settings > Telegram:

- `TELEGRAM_BOT_TOKEN` — Bot token (env takes precedence over config.json)

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

Override path: `--memory-dir` flag or `NEKOCLAW_MEMORY_DIR` env.

### How It Works

1. **Read** — On every LLM request, `MEMORY.md` and the last 2 days of daily logs are loaded and injected as a system message (or embedded into the Persona template via `{{.Memory}}`)
2. **Write** — When context approaches the window limit, a silent LLM round extracts key information (decisions, preferences, code changes) and appends it to today's daily log
3. **Index** — After each chat turn, user and assistant messages are chunked (400 tokens, 80 overlap) and inserted into the FTS5 search index
4. **Search** — TUI `/memory <query>`, Settings > Memory tab, API `POST /v1/memory/search`, or the LLM `memory_search` tool

## API Endpoints

- `GET /healthz`
- `GET /v1/providers`
- `GET /v1/accounts?provider=<id>`
- `POST /v1/chat`
- `POST /v1/integrations/discord/events`
- `GET /v1/gemini/quota?provider=google-gemini-cli&account_id=<id>&profile_id=<id>`
- `POST /v1/gemini/discover-project`
- `POST /v1/auth/gemini/start`
- `GET /oauth2callback`
- `POST /v1/auth/gemini/manual/complete`
- `GET /v1/auth/gemini/profiles`
- `POST /v1/auth/gemini/use`
- `POST /v1/auth/ai-studio/add-key`
- `GET /v1/auth/ai-studio/profiles`
- `POST /v1/auth/ai-studio/use`
- `POST /v1/auth/ai-studio/delete`
- `GET /v1/ai-studio/models`
- `POST /v1/auth/anthropic/add-token`
- `POST /v1/auth/anthropic/add-api-key`
- `GET /v1/auth/anthropic/profiles`
- `POST /v1/auth/anthropic/use`
- `POST /v1/auth/anthropic/delete`

- `POST /v1/memory/search`

- `GET/PUT /v1/discord/config`
- `GET/PUT /v1/telegram/config`

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
