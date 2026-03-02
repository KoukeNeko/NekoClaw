# NekoClaw

Go-based agent runtime with:

- TUI chat client
- HTTP API for future Web UI
- Discord event ingress API
- Pluggable LLM provider architecture
- Account pool cooldown/failover scheduler
- Context compression (soft trim, hard clear, sliding window)

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
    }
  ]
}
```

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

## Architecture Notes

See:

- [`docs/openclaw-research.md`](./docs/openclaw-research.md) for extracted OpenClaw architecture mapping.
- [`docs/gemini-auth.md`](./docs/gemini-auth.md) for OAuth operation manual and risk notes.
