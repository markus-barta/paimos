#!/usr/bin/env bash
# Pure deploy-target resolution helpers. Sourced by scripts/deploy.sh and
# tested by scripts/test-deploy-target.sh.

deploy_target::latest_release_tag() {
  git fetch --tags --quiet origin
  git tag --sort=-creatordate | grep -E '^v?[0-9]+\.[0-9]+\.[0-9]+$' | head -1 || true
}

deploy_target::head_sha_tag() {
  printf 'sha-%s' "$(git rev-parse --short HEAD)"
}

# Resolve a user-provided deploy target into a ghcr tag.
#
# Globals set:
#   DEPLOY_TARGET_TAG
#   DEPLOY_TARGET_REASON
#   DEPLOY_TARGET_REQUESTED
#   DEPLOY_TARGET_LATEST_TAG
#   DEPLOY_TARGET_HEAD_SHA
#   DEPLOY_TARGET_AHEAD_COUNT
deploy_target::resolve() {
  local requested="${1:-}"

  DEPLOY_TARGET_TAG=""
  DEPLOY_TARGET_REASON=""
  DEPLOY_TARGET_REQUESTED="$requested"
  DEPLOY_TARGET_LATEST_TAG=""
  DEPLOY_TARGET_HEAD_SHA="$(git rev-parse --short HEAD)"
  DEPLOY_TARGET_AHEAD_COUNT=0

  if [[ -n "$requested" ]]; then
    case "$requested" in
      current|head|HEAD|@)
        DEPLOY_TARGET_TAG="$(deploy_target::head_sha_tag)"
        DEPLOY_TARGET_REASON="current-head"
        ;;
      *)
        DEPLOY_TARGET_TAG="${requested#v}"
        DEPLOY_TARGET_REASON="explicit"
        ;;
    esac
    return 0
  fi

  DEPLOY_TARGET_LATEST_TAG="$(deploy_target::latest_release_tag)"
  if [[ -z "$DEPLOY_TARGET_LATEST_TAG" ]]; then
    echo "error: no semver release tags on origin — run \`just release …\` first or pass an explicit image tag" >&2
    return 1
  fi

  DEPLOY_TARGET_AHEAD_COUNT="$(git rev-list --count "$DEPLOY_TARGET_LATEST_TAG..HEAD" 2>/dev/null || echo 0)"
  if [[ "$DEPLOY_TARGET_AHEAD_COUNT" != "0" ]]; then
    local head_tag
    head_tag="$(deploy_target::head_sha_tag)"
    {
      echo "error: deploy target omitted, but HEAD ($DEPLOY_TARGET_HEAD_SHA) is $DEPLOY_TARGET_AHEAD_COUNT commit(s) ahead of latest release tag $DEPLOY_TARGET_LATEST_TAG."
      echo "       Refusing to guess between the release image and the untagged main image."
      echo "       Release deploy: scripts/deploy.sh <instance> $DEPLOY_TARGET_LATEST_TAG"
      echo "       Green main deploy: scripts/deploy.sh <instance> $head_tag"
      echo "       Shortcut for current HEAD: scripts/deploy.sh <instance> current"
    } >&2
    return 2
  fi

  DEPLOY_TARGET_TAG="${DEPLOY_TARGET_LATEST_TAG#v}"
  DEPLOY_TARGET_REASON="default-release"
}

deploy_target::print_summary() {
  local instance="$1" image="$2" preflight_only="${3:-0}"
  local requested="$DEPLOY_TARGET_REQUESTED"
  if [[ -z "$requested" ]]; then
    requested="(omitted; latest release tag)"
  fi

  echo "--- resolved deploy target"
  echo "    instance:           $instance"
  echo "    requested:          $requested"
  echo "    mode:               $DEPLOY_TARGET_REASON"
  echo "    local HEAD:         $DEPLOY_TARGET_HEAD_SHA"
  if [[ -n "$DEPLOY_TARGET_LATEST_TAG" ]]; then
    echo "    latest release tag: $DEPLOY_TARGET_LATEST_TAG"
  fi
  echo "    image:              $image"
  if [[ "$preflight_only" == "1" ]]; then
    echo "    action:             preflight only (no stop, backup, or restart)"
  fi
}
