#!/usr/bin/env bash
# PAI-297 — boot the dev stack, run the Playwright E2E smoke suite, tear down.
# Works on a dev box and in CI: the dev-login token is a throwaway value gated
# behind the `dev_login` build tag, never a prod credential. Pass extra args
# straight through to `playwright test` (e.g. `scripts/e2e.sh --headed`).
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

# Must be >= 32 chars (backend rejects shorter dev-login tokens at startup).
E2E_TOKEN="${PAIMOS_DEV_LOGIN_TOKEN:-e2e-smoke-token-not-for-prod-do-not-reuse}"
TOKDIR=$(mktemp -d)
printf 'PAIMOS_DEV_LOGIN_TOKEN=%s\n' "$E2E_TOKEN" >"$TOKDIR/token.env"
export PAIMOS_DEV_LOGIN_TOKEN_FILE="$TOKDIR/token.env"

# Boot the stack (backend :8888 + vite :5173) in the background.
echo "→ booting dev stack (log: $ROOT/.e2e-devup.log)"
bash "$ROOT/scripts/dev-up.sh" >"$ROOT/.e2e-devup.log" 2>&1 &
DEVUP_PID=$!

cleanup() {
  echo "→ tearing down dev stack"
  pkill -P "$DEVUP_PID" 2>/dev/null || true
  kill "$DEVUP_PID" 2>/dev/null || true
  pkill -f 'dev-up-backend' 2>/dev/null || true
  pkill -f 'vite' 2>/dev/null || true
  rm -rf "$TOKDIR"
}
trap cleanup EXIT

wait_for() {
  local url=$1 name=$2 i
  for i in $(seq 1 60); do
    curl -sS -m 1 "$url" >/dev/null 2>&1 && {
      echo "  ✓ $name up"
      return 0
    }
    if ! kill -0 "$DEVUP_PID" 2>/dev/null; then
      echo "error: dev stack exited early — see $ROOT/.e2e-devup.log" >&2
      tail -20 "$ROOT/.e2e-devup.log" >&2 || true
      return 1
    fi
    sleep 2
  done
  echo "error: $name did not come up at $url" >&2
  return 1
}

echo "→ waiting for the stack"
wait_for http://localhost:8888/api/health backend
wait_for http://localhost:5173 vite

echo "→ running Playwright smoke"
cd "$ROOT/frontend"
PAIMOS_DEV_LOGIN_TOKEN="$E2E_TOKEN" E2E_API_URL="http://localhost:8888" \
  npx playwright test "$@"
