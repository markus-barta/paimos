#!/usr/bin/env bash
# Screenshot a route of the local PAIMOS dev UI via headless Chromium.
#
# Prereq: the dev stack is up in another terminal:  just dev-up
# Usage:  scripts/visual-shot.sh [route] [out.png]
#   scripts/visual-shot.sh                      # first seeded project's issues
#   scripts/visual-shot.sh /issues /tmp/x.png   # a specific route + output
#
# Playwright + Chromium are bootstrapped ONCE into scripts/.visual-tooling
# (gitignored). This deliberately does NOT add Playwright to the frontend's
# package.json — that would pull a ~150MB browser download into every CI
# `npm ci`. Chromium is shared via the global Playwright cache, so the
# bootstrap is near-instant once it's been installed once on the machine.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TOOL="$ROOT/scripts/.visual-tooling"

mkdir -p "$TOOL"
if [ ! -d "$TOOL/node_modules/playwright" ]; then
  echo "→ bootstrapping playwright into scripts/.visual-tooling (one-time)…" >&2
  # `npm init -y` derives the package name from the dir and rejects the leading
  # dot in ".visual-tooling", so write a minimal valid manifest ourselves.
  [ -f "$TOOL/package.json" ] || printf '{ "name": "paimos-visual-tooling", "private": true }\n' >"$TOOL/package.json"
  # Keep stderr visible so a failed install is diagnosable, not silent.
  ( cd "$TOOL" && npm i playwright@latest --no-audit --no-fund --loglevel=error )
  if [ ! -d "$TOOL/node_modules/playwright" ]; then
    echo "error: playwright failed to install into $TOOL" >&2
    echo "  retry manually: (cd '$TOOL' && npm i playwright)" >&2
    exit 1
  fi
fi
# Ensure the chromium binary is present (fast no-op once in the global cache).
( cd "$TOOL" && npx playwright install chromium >/dev/null 2>&1 ) || true

exec env NODE_PATH="$TOOL/node_modules" node "$ROOT/scripts/visual-shot.cjs" "$@"
