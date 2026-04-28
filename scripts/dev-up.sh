#!/usr/bin/env bash
# PAI-267 — bring up the local PAIMOS dev stack with the dev-login
# build tag enabled and fixture data seeded. Designed for an agent
# (Claude Code) or human to drop into a logged-in UI in one command.
#
# Flow:
#   1. Source ~/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env (gitignored)
#      so PAIMOS_DEV_LOGIN_TOKEN lands in this script's env.
#   2. Build the backend with `-tags dev_login` so the dev-login
#      handler symbol is in the binary; production builds omit it.
#   3. Run `paimos dev-seed` if the dev fixture rows are missing,
#      so a fresh DB gets the 4 users + 4 projects without the user
#      having to remember a separate command.
#   4. Start the backend (port 8888) in the background and log to
#      ./.dev-up-backend.log.
#   5. Start vite (port 5173) in the foreground; Ctrl-C tears the
#      whole stack down via the trap below.
#   6. Print an example curl command so the agent can grab a
#      session cookie in one round-trip.

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

TOKEN_FILE="${PAIMOS_DEV_LOGIN_TOKEN_FILE:-$HOME/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env}"
if [[ ! -f "$TOKEN_FILE" ]]; then
  echo "error: $TOKEN_FILE not found" >&2
  echo "  Generate one via:" >&2
  echo "    mkdir -p \$(dirname \"$TOKEN_FILE\")" >&2
  echo "    printf 'PAIMOS_DEV_LOGIN_TOKEN=%s\\n' \"\$(openssl rand -hex 32)\" > \"$TOKEN_FILE\"" >&2
  echo "    chmod 600 \"$TOKEN_FILE\"" >&2
  exit 1
fi
# shellcheck disable=SC1090
source "$TOKEN_FILE"
if [[ -z "${PAIMOS_DEV_LOGIN_TOKEN:-}" ]]; then
  echo "error: PAIMOS_DEV_LOGIN_TOKEN was not set after sourcing $TOKEN_FILE" >&2
  exit 1
fi
export PAIMOS_DEV_LOGIN_TOKEN
export PAIMOS_ENV="${PAIMOS_ENV:-development}"

# Build the backend with the dev_login tag. Cached when sources haven't
# changed, so the round-trip is fast on incremental runs.
echo "→ building backend with -tags dev_login"
( cd backend && go build -tags dev_login -o "$ROOT/.dev-up-backend" . )

# Seed the dev fixtures. Idempotent — re-running is a no-op once the
# rows exist (see backend/devseed/devseed_dev.go).
echo "→ seeding dev fixtures (idempotent)"
"$ROOT/.dev-up-backend" dev-seed

# Start the backend in the background.
mkdir -p "$ROOT/.dev-up-logs"
LOG="$ROOT/.dev-up-logs/backend.log"
echo "→ starting backend on :8888 (log: $LOG)"
"$ROOT/.dev-up-backend" >"$LOG" 2>&1 &
BACKEND_PID=$!

# Tear-down trap covers both clean Ctrl-C and abnormal exits. Wait
# briefly for the backend to either bind or crash so we never leave
# vite running against a dead upstream.
cleanup() {
  echo
  echo "→ stopping backend (pid $BACKEND_PID)"
  kill "$BACKEND_PID" 2>/dev/null || true
  wait "$BACKEND_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Wait up to 5s for the backend health endpoint to come up.
for i in 1 2 3 4 5; do
  if curl -sS -m 1 http://localhost:8888/api/health >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
    echo "error: backend exited early — see $LOG" >&2
    exit 1
  fi
  sleep 1
done

# Print the agent recipe for grabbing a session cookie.
cat <<EOF

dev stack up:
  backend  http://localhost:8888  (PID $BACKEND_PID, log: $LOG)
  vite     http://localhost:5173  (foreground — Ctrl-C tears down)

agent recipe — log in as dev_admin and persist the cookie:

  curl -sS -c /tmp/paimos-dev-cookies.txt \\
       -H 'Content-Type: application/json' \\
       -d '{"username":"dev_admin","token":"'"\$PAIMOS_DEV_LOGIN_TOKEN"'"}' \\
       http://localhost:8888/api/auth/dev-login

  # subsequent requests reuse the cookie:
  curl -sS -b /tmp/paimos-dev-cookies.txt http://localhost:8888/api/auth/me

EOF

# Hand off to vite in the foreground.
( cd frontend && npm run dev )
