#!/usr/bin/env bash
# Deploy an image target to one PAIMOS instance. Backup-first, never auto-rollback.
#
# Usage:
#   scripts/deploy.sh <instance> [tag|sha-abcdef0|current] [--preflight]
#
# Instance = ppm | pmo. An omitted target defaults to the latest release
# tag only when HEAD is not ahead of that tag. If HEAD contains untagged
# commits, the script refuses to guess; pass a release tag, sha-* image tag,
# or "current" explicitly.
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

PREFLIGHT_ONLY=0

usage() {
  cat >&2 <<'USAGE'
usage: scripts/deploy.sh <ppm|pmo> [tag|sha-abcdef0|current] [--preflight]

Targets:
  v2.4.8 / 2.4.8    deploy a release image
  sha-abcdef0        deploy a CI-published commit image
  current            deploy sha-$(git rev-parse --short HEAD)
  omitted            deploy latest release tag only when HEAD is not ahead of it

Flags:
  --preflight, --dry-run
      Resolve target, verify the image exists, and inspect the remote
      current image without stopping or restarting the service.
USAGE
}

ARGS=()
for arg in "$@"; do
  case "$arg" in
    --preflight|--dry-run)
      PREFLIGHT_ONLY=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      ARGS+=("$arg")
      ;;
  esac
done

INSTANCE="${ARGS[0]:-}"
TAG="${ARGS[1]:-}"
if [[ ${#ARGS[@]} -gt 2 ]]; then
  usage
  exit 1
fi

if [[ -z "$INSTANCE" ]]; then
  usage
  exit 1
fi

CONF="$ROOT/scripts/deploy.$INSTANCE.conf"
if [[ ! -f "$CONF" ]]; then
  echo "error: no config at $CONF" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$CONF"

# shellcheck disable=SC1091
source "$ROOT/scripts/deploy-target.sh"
deploy_target::resolve "$TAG"
TAG="$DEPLOY_TARGET_TAG"
IMAGE="ghcr.io/markus-barta/paimos:$TAG"

# shellcheck disable=SC1091
source "$ROOT/scripts/_deploy-lib.sh"

deploy_target::print_summary "$INSTANCE" "$IMAGE" "$PREFLIGHT_ONLY"

if ! ghcr::image_exists "$IMAGE"; then
  echo "error: $IMAGE not found on ghcr — CI still running, or wrong tag" >&2
  exit 1
fi

if [[ $PREFLIGHT_ONLY -eq 1 ]]; then
  echo "=== preflight $INSTANCE → $IMAGE ==="
else
  echo "=== deploy $INSTANCE → $IMAGE ==="
fi
deploy::run "$INSTANCE" "$TAG" "$IMAGE" "$PREFLIGHT_ONLY"
