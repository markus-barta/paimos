#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BACKEND="$ROOT/backend"
BASELINE="$ROOT/.gosec-baseline.txt"

if ! command -v jq >/dev/null 2>&1; then
  echo "gosec baseline: missing jq" >&2
  exit 2
fi
GOSEC_BIN=$(command -v gosec || true)
if [[ -z "$GOSEC_BIN" && -x "$(go env GOPATH)/bin/gosec" ]]; then
  GOSEC_BIN="$(go env GOPATH)/bin/gosec"
fi
if [[ -z "$GOSEC_BIN" ]]; then
  echo "gosec baseline: missing gosec" >&2
  exit 2
fi
if [[ ! -f "$BASELINE" ]]; then
  echo "gosec baseline: missing $BASELINE" >&2
  exit 2
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

raw="$tmp_dir/gosec.json"
current="$tmp_dir/current.txt"
unexpected="$tmp_dir/unexpected.txt"

cd "$BACKEND"
"$GOSEC_BIN" -fmt=json -out "$raw" \
  -severity=medium \
  -confidence=medium \
  -exclude=G104 \
  -exclude-dir=cmd/genreport \
  ./... >/dev/null 2>&1 || true

jq -r --arg prefix "$BACKEND/" '
  .Issues[]
  | .file = (if (.file | startswith($prefix)) then (.file[($prefix | length):]) else .file end)
  | [.rule_id, .severity, .confidence, .file, (.line | tostring), .details]
  | @tsv
' "$raw" | LC_ALL=C sort -u > "$current"

comm -13 "$BASELINE" "$current" > "$unexpected"

if [[ -s "$unexpected" ]]; then
  echo "gosec baseline: new unbaselined findings" >&2
  cat "$unexpected" >&2
  exit 1
fi

echo "gosec baseline: ok ($(wc -l < "$current" | tr -d ' ') finding(s), all baselined)"
