#!/usr/bin/env bash
# ghcr-prune.sh — periodic hygiene for ghcr.io/inspr-at/paimos.
#
# Built after the v3.7.7 incident (2026-05-26) where ~2,000 accumulated
# manifest versions (multi-arch sub-manifests + cosign .sig/.att/bare
# attestations across many tags) ended up tripping a `denied: denied`
# response on every new docker push and cascaded into a stuck workflow
# queue. See memory/paimos_ghcr_ghost_lock.md.
#
# Usage:
#   scripts/ghcr-prune.sh              # interactive, --dry-run by default
#   scripts/ghcr-prune.sh --execute    # actually delete
#   scripts/ghcr-prune.sh --execute --keep-recent-sha=20
#
# Flags:
#   --execute            Actually call DELETE. Without this, prints the plan only.
#   --keep-recent-sha=N  Keep the N most recent `sha-XXXXXXX`-only versions
#                        for rollback safety. Default: 20.
#   --owner=NAME         GHCR package owner. Default: inspr-at.
#   --package=NAME       Package name. Default: paimos.
#
# What it preserves:
#   - Every semver-tagged version (`1.0.0`, `latest`, `3.7`, etc.).
#   - The N most recent versions whose ONLY tags are `sha-XXXXXXX`.
#   - Cosign signature/attestation versions (`sha256-*.sig`, `.att`, bare)
#     — these are small and cross-ref to specific main images; leaving
#     them avoids accidental verify-release breakage.
#
# What it deletes:
#   - Versions with NO tags (orphan blobs, mostly multi-arch sub-manifests).
#   - `sha-XXXXXXX`-only versions older than the keep-N cutoff.
#
# Requires: gh, jq. The gh account must hold `delete:packages` scope on
# the package owner.

set -euo pipefail

DRY_RUN=1
KEEP_RECENT_SHA=20
OWNER="inspr-at"
PACKAGE="paimos"
SLEEP_BETWEEN=0.3

for arg in "$@"; do
  case "$arg" in
    --execute) DRY_RUN=0 ;;
    --keep-recent-sha=*) KEEP_RECENT_SHA="${arg#*=}" ;;
    --owner=*) OWNER="${arg#*=}" ;;
    --package=*) PACKAGE="${arg#*=}" ;;
    -h|--help)
      sed -n '1,/^set -euo pipefail/p' "$0" | sed -n '2,/^set -euo pipefail/p' | sed 's/^# \{0,1\}//' | head -40
      exit 0 ;;
    *) echo "ghcr-prune: unknown flag: $arg" >&2; exit 2 ;;
  esac
done

if ! command -v gh >/dev/null 2>&1; then
  echo "ghcr-prune: missing gh" >&2; exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "ghcr-prune: missing jq" >&2; exit 2
fi

API_BASE="/users/${OWNER}/packages/container/${PACKAGE}"

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

echo "ghcr-prune: fetching versions for ${OWNER}/${PACKAGE} (this paginates)…" >&2
gh api --paginate "${API_BASE}/versions?per_page=100" > "$tmp_dir/versions.json"
total=$(jq -s 'add | length' "$tmp_dir/versions.json")
echo "ghcr-prune: ${total} versions on package" >&2

jq -s '
  add
  | (map(select(
      (.metadata.container.tags | length) > 0 and
      (.metadata.container.tags | all(test("^sha-[0-9a-f]+$")))
    )) | sort_by(.created_at) | reverse) as $sha_sorted
  | (
      (map(select((.metadata.container.tags | length) == 0))) +
      ($sha_sorted | .['"$KEEP_RECENT_SHA"':])
    )
  | map({id, created_at, tags: .metadata.container.tags})
' "$tmp_dir/versions.json" > "$tmp_dir/candidates.json"

count=$(jq 'length' "$tmp_dir/candidates.json")
untagged=$(jq '[.[] | select(.tags | length == 0)] | length' "$tmp_dir/candidates.json")
sha_only=$(jq '[.[] | select(.tags | length > 0)] | length' "$tmp_dir/candidates.json")

echo "ghcr-prune: plan — delete ${count} versions (${untagged} untagged + ${sha_only} sha-only past keep-${KEEP_RECENT_SHA})" >&2

if [[ "$DRY_RUN" == "1" ]]; then
  echo "ghcr-prune: DRY RUN — re-run with --execute to actually delete." >&2
  echo "ghcr-prune: first 5 candidates:" >&2
  jq -r '.[:5][] | "  \(.created_at)  id=\(.id)  tags=\(.tags | join(","))"' "$tmp_dir/candidates.json" >&2
  exit 0
fi

log="$tmp_dir/prune.log"
echo "[start] $(date -u '+%FT%TZ') — deleting ${count} versions" > "$log"
ok=0; fail=0; i=0
while read -r id; do
  i=$((i+1))
  if gh api -X DELETE "${API_BASE}/versions/${id}" >/dev/null 2>&1; then
    ok=$((ok+1))
  else
    fail=$((fail+1))
    echo "[fail] $id" >> "$log"
  fi
  if (( i % 100 == 0 )); then
    echo "[progress] $i/$count  ok=$ok fail=$fail  ts=$(date -u '+%FT%TZ')" | tee -a "$log" >&2
  fi
  sleep "$SLEEP_BETWEEN"
done < <(jq -r '.[].id' "$tmp_dir/candidates.json")
echo "[done] $(date -u '+%FT%TZ') — ok=$ok fail=$fail total=$count" | tee -a "$log" >&2

# Surface the log so a CI cron run can capture it.
cat "$log"
