#!/usr/bin/env bash
# Wait for the GitHub Actions runs that make a semver tag deployable.
#
# Usage:
#   scripts/wait-release-ci.sh v3.7.3
#   scripts/wait-release-ci.sh v3.7.3 --workflows ci,release --timeout 1800

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

TAG=""
WORKFLOWS="${WAIT_RELEASE_WORKFLOWS:-ci,release}"
TIMEOUT="${WAIT_RELEASE_TIMEOUT:-1800}"
POLL="${WAIT_RELEASE_POLL:-10}"
REPO="${GITHUB_REPOSITORY:-}"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/wait-release-ci.sh <tag> [--workflows ci,release] [--timeout seconds] [--poll seconds] [--repo owner/name]

Waits for the tag-push GitHub Actions workflows that publish release evidence.
Default workflows: ci,release.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --workflows)
      WORKFLOWS="${2:-}"
      shift 2
      ;;
    --timeout)
      TIMEOUT="${2:-}"
      shift 2
      ;;
    --poll)
      POLL="${2:-}"
      shift 2
      ;;
    --repo)
      REPO="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      echo "error: unknown flag: $1" >&2
      usage
      exit 2
      ;;
    *)
      if [[ -n "$TAG" ]]; then
        echo "error: multiple tags provided: $TAG and $1" >&2
        usage
        exit 2
      fi
      TAG="$1"
      shift
      ;;
  esac
done

if [[ -z "$TAG" || -z "$WORKFLOWS" ]]; then
  usage
  exit 2
fi
if ! [[ "$TIMEOUT" =~ ^[0-9]+$ && "$POLL" =~ ^[0-9]+$ && "$POLL" -gt 0 ]]; then
  echo "error: --timeout and --poll must be positive integers" >&2
  exit 2
fi
if ! command -v gh >/dev/null 2>&1; then
  echo "error: gh CLI is required to wait for release workflows" >&2
  exit 2
fi
if [[ -z "$REPO" ]]; then
  REPO=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
fi

IFS=, read -r -a WORKFLOW_LIST <<<"$WORKFLOWS"
start=$(date +%s)

echo "[wait-release-ci] repo=$REPO tag=$TAG workflows=$WORKFLOWS timeout=${TIMEOUT}s"

while true; do
  now=$(date +%s)
  if (( now - start > TIMEOUT )); then
    echo "error: timed out waiting for release workflows for $TAG" >&2
    echo "       gh run list --repo $REPO --event push --branch $TAG" >&2
    exit 3
  fi

  all_done=1
  missing=0

  for workflow in "${WORKFLOW_LIST[@]}"; do
    workflow=$(echo "$workflow" | xargs)
    if [[ -z "$workflow" ]]; then
      continue
    fi

    filter=".[] | select(.workflowName == \"$workflow\") | [.databaseId, .status, (.conclusion // \"\"), .url] | @tsv"
    line=$(gh run list \
      --repo "$REPO" \
      --event push \
      --branch "$TAG" \
      --limit 50 \
      --json databaseId,workflowName,status,conclusion,url \
      --jq "$filter" | head -n1 || true)

    if [[ -z "$line" ]]; then
      echo "  $workflow: waiting for run to appear"
      all_done=0
      missing=1
      continue
    fi

    IFS=$'\t' read -r run_id status conclusion url <<<"$line"
    if [[ "$status" != "completed" ]]; then
      echo "  $workflow: $status ($url)"
      all_done=0
      continue
    fi

    if [[ "$conclusion" != "success" ]]; then
      echo "error: $workflow completed with conclusion=$conclusion" >&2
      echo "       $url" >&2
      echo "       gh run view $run_id --repo $REPO --log-failed" >&2
      exit 4
    fi

    echo "  $workflow: success ($url)"
  done

  if [[ $all_done -eq 1 ]]; then
    echo "[wait-release-ci] all requested workflows passed for $TAG"
    exit 0
  fi

  if [[ $missing -eq 1 ]]; then
    sleep 3
  else
    sleep "$POLL"
  fi
done
