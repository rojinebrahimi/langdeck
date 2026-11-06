#!/bin/sh
# Starts LangDeck, then the demo agent pointed at it. Render injects PORT and RENDER_EXTERNAL_URL.
set -eu

PORT="${PORT:-10000}"
export LANGDECK_ADDR=":${PORT}"
export LANGDECK_PUBLIC_URL="${RENDER_EXTERNAL_URL:-http://localhost:${PORT}}"
export LANGDECK_BOOTSTRAP_AGENT_KEY="ldk_${DEMO_AGENT_SECRET:?DEMO_AGENT_SECRET must be set}"

langdeck serve &
server=$!

export LANGDECK_URL="http://127.0.0.1:${PORT}"
export LANGDECK_API_KEY="${LANGDECK_BOOTSTRAP_AGENT_KEY}"
python /app/agent.py &
agent=$!

trap 'kill -TERM "$agent" "$server" 2>/dev/null' TERM INT
wait "$server"
