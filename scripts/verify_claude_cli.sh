#!/usr/bin/env bash
# Verifies that the local `claude` CLI is reachable and returns a sane JSON
# response under the exact flag set the Claude CLI provider uses in production.
#
# Run: ./scripts/verify_claude_cli.sh
# Exit code 0 = harness passed; non-zero = harness failed.

set -euo pipefail

CLAUDE_BIN="${NEKOCLAW_CLAUDE_CLI_BIN:-claude}"

if ! command -v "$CLAUDE_BIN" >/dev/null 2>&1; then
    echo "❌ '$CLAUDE_BIN' not found in PATH. Install Claude Code or set NEKOCLAW_CLAUDE_CLI_BIN." >&2
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "❌ 'jq' not found in PATH. Install jq to run this harness." >&2
    exit 1
fi

PROMPT='Reply with exactly the single word OK and nothing else.'

# Run from a sandbox dir so the CLI does not pollute the repo with session
# state, even though --no-session-persistence should already prevent that.
SANDBOX="$(mktemp -d)"
trap 'rm -rf "$SANDBOX"' EXIT

OUTPUT="$(cd "$SANDBOX" && "$CLAUDE_BIN" \
    --print \
    --model sonnet \
    --output-format json \
    --no-session-persistence \
    --tools "" \
    --permission-mode bypassPermissions \
    --setting-sources "" \
    --strict-mcp-config \
    --disable-slash-commands \
    "$PROMPT")"

if echo "$OUTPUT" | jq -e '.is_error == true' >/dev/null 2>&1; then
    echo "❌ Claude CLI returned is_error=true. Raw output:" >&2
    echo "$OUTPUT" >&2
    exit 1
fi

if ! echo "$OUTPUT" | jq -e '.result' >/dev/null 2>&1; then
    echo "❌ Response missing .result field. Raw output:" >&2
    echo "$OUTPUT" >&2
    exit 1
fi

RESULT="$(echo "$OUTPUT" | jq -r '.result')"
if ! echo "$RESULT" | grep -qi 'OK'; then
    echo "❌ Result did not contain 'OK': $RESULT" >&2
    exit 1
fi

INPUT_TOKENS="$(echo "$OUTPUT" | jq -r '.usage.input_tokens // empty')"
OUTPUT_TOKENS="$(echo "$OUTPUT" | jq -r '.usage.output_tokens // empty')"

echo "✅ claude CLI provider harness passed"
echo "   model:        sonnet"
echo "   result:       $RESULT"
echo "   input_tokens: ${INPUT_TOKENS:-?}"
echo "   output_tokens:${OUTPUT_TOKENS:-?}"
