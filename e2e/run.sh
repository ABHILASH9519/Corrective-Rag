#!/usr/bin/env bash
# Runs the browser tests against the real crag binary, with a fake model server so no API key or network is needed.
# Needs: Go, Python 3, and `pip install playwright` plus a browser (`playwright install chromium`, or set CHROME).
set -euo pipefail
cd "$(dirname "$0")/.."

LLM_PORT=${LLM_PORT:-9101}
APP_PORT=${APP_PORT:-8100}
LIMITED_PORT=${LIMITED_PORT:-8101}
PY=${PYTHON:-python3}
LOG_DIR=$(mktemp -d)
pids=()
cleanup() { for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

go build -o bin/crag ./cmd/crag

"$PY" e2e/fake_llm.py "$LLM_PORT" 0.45 >"$LOG_DIR/llm.log" 2>&1 & pids+=($!)
export GROQ_API_KEY=test GROQ_BASE_URL="http://127.0.0.1:$LLM_PORT" CRAG_LOG_LEVEL=warn
CRAG_ADDR=":$APP_PORT" CRAG_RATE_PER_MINUTE=100 ./bin/crag serve >"$LOG_DIR/app.log" 2>&1 & pids+=($!)
CRAG_ADDR=":$LIMITED_PORT" CRAG_RATE_PER_MINUTE=1 ./bin/crag serve >"$LOG_DIR/limited.log" 2>&1 & pids+=($!)

for port in "$APP_PORT" "$LIMITED_PORT"; do
  for _ in $(seq 1 50); do
    curl -fs --noproxy '*' "http://127.0.0.1:$port/health" >/dev/null 2>&1 && break
    sleep 0.2
  done
done

export E2E_BASE="http://localhost:$APP_PORT" E2E_BASE_LIMITED="http://localhost:$LIMITED_PORT"
status=0
"$PY" e2e/ui_test.py || status=1
"$PY" e2e/ui_test_limits.py || status=1
[ "$status" -eq 0 ] && echo "e2e: all passed" || { echo "e2e: failures (server logs in $LOG_DIR)"; trap - EXIT; cleanup; }
exit "$status"
