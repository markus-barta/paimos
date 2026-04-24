#!/usr/bin/env bash
# Deploy a tag to one PAIMOS instance. Backup-first, never auto-rollback.
#
# Usage:
#   scripts/deploy.sh <instance> [tag]
#
# Instance = ppm | pmo. Tag defaults to the latest tag on origin.
# The flow (all remote via SSH):
#   1. verify tag exists on ghcr
#   2. stop the service
#   3. snapshot the data (tar archive)
#   4. validate the backup (gzip + entry count + db presence)
#   5. write a manifest with pre/post image digests
#   6. pin compose to the new tag, `docker compose pull && up -d`
#   7. tail logs briefly
#   8. external curl smoke test against /api/health
#
# On any failure, prints a manual rollback command. Never auto-rolls-back
# a live system.

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

INSTANCE="${1:-}"
TAG="${2:-}"

if [[ -z "$INSTANCE" ]]; then
  echo "usage: $0 <ppm|pmo> [tag]" >&2
  exit 1
fi

CONF="$ROOT/scripts/deploy.$INSTANCE.conf"
if [[ ! -f "$CONF" ]]; then
  echo "error: no config at $CONF" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$CONF"

if [[ -z "$TAG" ]]; then
  git fetch --tags --quiet origin
  TAG=$(git tag --sort=-creatordate | head -1 || true)
  if [[ -z "$TAG" ]]; then
    echo "error: no tags on origin — run \`just release …\` first" >&2
    exit 1
  fi
fi
TAG="${TAG#v}"
IMAGE="ghcr.io/markus-barta/paimos:$TAG"

# shellcheck disable=SC1091
source "$ROOT/scripts/_deploy-lib.sh"

if ! ghcr::image_exists "$IMAGE"; then
  echo "error: $IMAGE not found on ghcr — CI still running, or wrong tag" >&2
  exit 1
fi

echo "=== deploy $INSTANCE → $IMAGE ==="
deploy::run "$INSTANCE" "$TAG" "$IMAGE"
