#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

hits="$(
  rg -n \
    "(new Intl\\.(NumberFormat|DateTimeFormat|RelativeTimeFormat)|\\.toLocaleString\\s*\\(|\\.toLocaleDateString\\s*\\(|\\.toLocaleTimeString\\s*\\(|\\.toFixed\\s*\\()" \
    frontend/src/views frontend/src/components frontend/src/composables frontend/src/utils frontend/src/config \
    -g '!*.test.ts' -g '!*.test.tsx' \
  | rg -v \
    "frontend/src/composables/use(Number|Date)Format\\.ts|frontend/src/components/issue/AttachmentLightbox\\.vue" \
  || true
)"

if [[ -n "$hits" ]]; then
  echo "[frontend-formatting] raw locale formatter calls found:" >&2
  echo "$hits" >&2
  echo "[frontend-formatting] route display through useNumberFormat/useDateFormat or add a documented exemption." >&2
  exit 1
fi

echo "[frontend-formatting] OK"
