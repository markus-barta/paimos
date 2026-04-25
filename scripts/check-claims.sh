#!/usr/bin/env bash
# PAI-123: lightweight release gate.
#
# Reads docs/claim-matrix.md and refuses to exit 0 if any row is
# stuck on `aspirational` without an open ticket reference. Called
# from scripts/release.sh before the version bump so a careless
# release cannot ship while the public claim matrix is out of sync
# with what the code does.
#
# Intentionally NOT exhaustive — this script is one careful pair of
# eyes, not an automated truth oracle. Bypass with --yolo if a
# release truly must go out before the matrix is updated; the bypass
# is logged.

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
MATRIX="$ROOT/docs/claim-matrix.md"

if [[ ! -f "$MATRIX" ]]; then
  echo "[claim-gate] FAIL: docs/claim-matrix.md not found" >&2
  exit 1
fi

if [[ "${1:-}" == "--yolo" ]]; then
  echo "[claim-gate] WARN: --yolo bypass requested. Reason should be in the release commit message." >&2
  exit 0
fi

# Pull the table body — every line that starts with "| " and is not the
# header or separator. Awk because the file is small and we want zero
# external deps.
fail=0
while IFS= read -r line; do
  # Skip header (contains "Claim (paimos.com") and separator (--- only).
  case "$line" in
    *"Claim (paimos.com"*) continue ;;
    *---*) continue ;;
  esac
  status=$(echo "$line" | awk -F'|' '{ gsub(/^ +| +$/, "", $3); print $3 }')
  tickets=$(echo "$line" | awk -F'|' '{ gsub(/^ +| +$/, "", $5); print $5 }')

  case "$status" in
    *aspirational*)
      # Allow the row only if the ticket column references a PAI-### or
      # explicitly says "wording rolled back" / "→".
      if [[ "$tickets" == "—" || -z "$tickets" ]]; then
        if [[ "$status" != *"→"* ]]; then
          echo "[claim-gate] FAIL: aspirational row with no follow-on ticket:"
          echo "    $line" >&2
          fail=1
        fi
      fi
      ;;
  esac
done < <(grep -E '^\| ' "$MATRIX")

if [[ $fail -ne 0 ]]; then
  echo
  echo "[claim-gate] One or more website claims are unbacked." >&2
  echo "Either land an implementation, narrow the claim, or open a ticket and reference it in docs/claim-matrix.md." >&2
  echo "Bypass (record reason in commit message): $0 --yolo" >&2
  exit 1
fi

echo "[claim-gate] OK"
