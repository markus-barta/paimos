#!/usr/bin/env bash
set -euo pipefail

if [ $# -lt 2 ]; then
  echo "usage: scripts/upload-anchors.sh <project-key-or-id> <repo-id>" >&2
  exit 2
fi

project_ref="$1"
repo_id="$2"

repo_root="$(git rev-parse --show-toplevel)"

cd "$repo_root/backend"
go run ./cmd/paimos anchors scan --repo-root .. --output ../.pmo/anchors.json
go run ./cmd/paimos anchors upload --repo-root .. --index ../.pmo/anchors.json --project "$project_ref" --repo-id "$repo_id"
