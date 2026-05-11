#!/usr/bin/env bash
# End-to-end harness: builds the nekoclaw API server, starts it on a random
# port with an empty accounts file, fires a chat request at the claude-cli
# provider, and asserts a 200 with non-empty text.
#
# Run: ./scripts/verify_claude_cli_e2e.sh
# Exit 0 = pass; non-zero = fail.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

CLAUDE_BIN="${NEKOCLAW_CLAUDE_CLI_BIN:-claude}"
if ! command -v "$CLAUDE_BIN" >/dev/null 2>&1; then
    echo "❌ '$CLAUDE_BIN' not found in PATH. Install Claude Code or set NEKOCLAW_CLAUDE_CLI_BIN." >&2
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "❌ 'jq' not found in PATH." >&2
    exit 1
fi

# web/dist must exist for the embed.FS directive even if the UI is unused.
if [ ! -d web/dist ]; then
    mkdir -p web/dist
    : > web/dist/.placeholder
fi

WORK="$(mktemp -d)"
trap 'cleanup' EXIT
cleanup() {
    if [ -n "${SERVER_PID:-}" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$WORK"
}

BINARY="$WORK/nekoclaw"
echo "▶ building nekoclaw…"
go build -o "$BINARY" ./cmd/nekoclaw >/dev/null

# Pick a free port by binding-then-releasing via Python.
PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
ADDR="127.0.0.1:$PORT"

# Empty accounts file — claude-cli registers its own placeholder pool.
echo '{"accounts":[]}' > "$WORK/accounts.json"

LOG="$WORK/server.log"
echo "▶ starting server on $ADDR (logs: $LOG)…"
"$BINARY" \
    --mode api \
    --addr "$ADDR" \
    --provider claude-cli \
    --model sonnet \
    --accounts "$WORK/accounts.json" \
    --auth-dir "$WORK/auth" \
    --sessions-dir "$WORK/sessions" \
    --memory-dir "$WORK/memory" \
    >"$LOG" 2>&1 &
SERVER_PID=$!

# Wait for the server to start listening (max 10s).
for i in $(seq 1 50); do
    if curl -sf "http://$ADDR/healthz" >/dev/null 2>&1 \
        || curl -sf "http://$ADDR/" >/dev/null 2>&1 \
        || nc -z 127.0.0.1 "$PORT" 2>/dev/null; then
        break
    fi
    sleep 0.2
done

if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "❌ server died before responding. Tail of log:" >&2
    tail -40 "$LOG" >&2
    exit 1
fi

REQ='{"session_id":"e2e-claude-cli","surface":"web","provider":"claude-cli","model":"sonnet","message":"Reply with exactly the single word OK and nothing else."}'

echo "▶ POST /v1/chat …"
HTTP_CODE="$(curl -s -o "$WORK/resp.json" -w '%{http_code}' \
    -X POST "http://$ADDR/v1/chat" \
    -H 'Content-Type: application/json' \
    --max-time 60 \
    -d "$REQ")"

if [ "$HTTP_CODE" != "200" ]; then
    echo "❌ chat returned HTTP $HTTP_CODE. Body:" >&2
    cat "$WORK/resp.json" >&2
    echo "--- server log tail ---" >&2
    tail -40 "$LOG" >&2
    exit 1
fi

REPLY="$(jq -r '.reply // empty' "$WORK/resp.json")"
if [ -z "$REPLY" ]; then
    echo "❌ chat response missing .reply. Body:" >&2
    cat "$WORK/resp.json" >&2
    echo "--- server log tail ---" >&2
    tail -40 "$LOG" >&2
    exit 1
fi

if ! echo "$REPLY" | grep -qi 'OK'; then
    echo "❌ reply did not contain 'OK': $REPLY" >&2
    exit 1
fi

PROVIDER="$(jq -r '.provider // empty' "$WORK/resp.json")"
if [ "$PROVIDER" != "claude-cli" ]; then
    echo "❌ provider field = '$PROVIDER', want 'claude-cli'" >&2
    exit 1
fi

INPUT_TOKENS="$(jq -r '.usage.input_tokens // empty' "$WORK/resp.json")"
OUTPUT_TOKENS="$(jq -r '.usage.output_tokens // empty' "$WORK/resp.json")"

echo "✅ claude-cli E2E harness passed"
echo "   provider:     $PROVIDER"
echo "   reply:        $REPLY"
echo "   input_tokens: ${INPUT_TOKENS:-?}"
echo "   output_tokens:${OUTPUT_TOKENS:-?}"
