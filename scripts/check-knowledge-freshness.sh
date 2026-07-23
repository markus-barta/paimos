#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

version=$(tr -d '[:space:]' < VERSION)
fail=0

check_no_matches() {
  local label=$1
  local pattern=$2
  shift 2
  local out
  out=$(rg -n "$pattern" "$@" 2>/dev/null || true)
  if [[ -n "$out" ]]; then
    echo "knowledge freshness: $label" >&2
    echo "$out" >&2
    fail=1
  fi
}

docs_scope=(README.md docs)
cache_scope=()
if [[ -d .paimos/cache/PAI ]]; then
  cache_scope=(.paimos/cache/PAI)
fi

# Project manifests were retired in PAI-358. Adapter manifests,
# backup manifest.yaml files, and historical CHANGELOG entries are
# legitimate and intentionally excluded.
manifest_hits=$(rg -n \
  '(/api)?/projects[/:{][^`[:space:]]*manifest|paimos manifest pull|structured manifest|Recommended manifest (fields|keys)' \
  "${docs_scope[@]}" "${cache_scope[@]}" 2>/dev/null \
  | rg -v 'docs/CHANGELOG.md|adapter-protocol|BACKUP_RESTORE|DEPLOY|manifest\.yaml|removed|retired|legacy|PAI-358|There is no project manifest|manifest endpoint was removed|replaces the removed' || true)
if [[ -n "$manifest_hits" ]]; then
  echo "knowledge freshness: stale project-manifest references" >&2
  echo "$manifest_hits" >&2
  fail=1
fi

if ! rg -q "VER=$version" docs/INSTALL.md; then
  echo "knowledge freshness: docs/INSTALL.md pinned VER does not match VERSION ($version)" >&2
  fail=1
fi
if ! rg -q "paimos --version[[:space:]]+# $version" docs/INSTALL.md; then
  echo "knowledge freshness: docs/INSTALL.md paimos --version example does not match VERSION ($version)" >&2
  fail=1
fi

check_no_matches "stale schema examples" 'version[=:][[:space:]]+1\.[12]\.0' docs/AGENT_INTERFACE.md "${cache_scope[@]}"
check_no_matches "PAI-110 still described as open" 'PAI-110.*(open|still open)|still open.*PAI-110' docs "${cache_scope[@]}"

if [[ "${PAIMOS_CHECK_LIVE:-0}" == "1" ]]; then
  for url in https://pm.barta.cm; do
    live=$(curl -fsS "$url/api/health" | jq -r '.version')
    if [[ "$live" != "$version" ]]; then
      echo "knowledge freshness: $url health version $live != VERSION $version" >&2
      fail=1
    fi
    schema=$(curl -fsS "$url/api/schema" | jq -r '.version')
    if [[ "$schema" != "1.5.0" ]]; then
      echo "knowledge freshness: $url schema version $schema != 1.5.0" >&2
      fail=1
    fi
  done
fi

if [[ $fail -ne 0 ]]; then
  exit 1
fi

echo "knowledge freshness: ok"
