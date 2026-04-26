#!/usr/bin/env bash
# Atomic release: bump VERSION + CHANGELOG + git tag in one commit, push,
# and wait for CI to publish the Docker image.
#
# Usage:
#   scripts/release.sh patch|minor|major|<x.y.z>   # cut a release
#   scripts/release.sh                             # dump commits since last tag, exit
#
# After this succeeds, CI publishes (see .github/workflows/ci.yml):
#   ghcr.io/markus-barta/paimos:<x.y.z>       (immutable, use for deploys)
#   ghcr.io/markus-barta/paimos:<x>.<y>       (moving)
#   ghcr.io/markus-barta/paimos:<x>           (moving)
#   ghcr.io/markus-barta/paimos:sha-<short>   (immutable, per-commit)

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

MODE="${1:-}"

git fetch --tags --quiet origin

LAST_TAG=$(git tag --sort=-creatordate | head -1 || true)
if [[ -z "$LAST_TAG" ]]; then
  echo "error: no tags yet — create v0.1.0 manually first" >&2
  exit 1
fi
LAST_VERSION="${LAST_TAG#v}"
IFS=. read -r LAST_MAJOR LAST_MINOR LAST_PATCH <<<"$LAST_VERSION"

# No-arg: report, exit. (The "AI-assist" step happens in chat against this output.)
if [[ -z "$MODE" ]]; then
  echo "Last release: $LAST_TAG"
  echo
  echo "All commits since $LAST_TAG:"
  git log "$LAST_TAG..origin/main" --oneline
  echo
  echo "Runtime-relevant (backend/ frontend/src/):"
  git log "$LAST_TAG..origin/main" --oneline -- backend/ frontend/src/ || echo "  (none)"
  echo
  echo "Re-run with: patch | minor | major | <x.y.z>"
  exit 0
fi

case "$MODE" in
  patch) NEW="$LAST_MAJOR.$LAST_MINOR.$((LAST_PATCH + 1))" ;;
  minor) NEW="$LAST_MAJOR.$((LAST_MINOR + 1)).0" ;;
  major) NEW="$((LAST_MAJOR + 1)).0.0" ;;
  [0-9]*.[0-9]*.[0-9]*) NEW="$MODE" ;;
  *)
    echo "error: mode must be patch|minor|major|<x.y.z> (got: $MODE)" >&2
    exit 1
    ;;
esac
NEW_TAG="v$NEW"

if git rev-parse "$NEW_TAG" >/dev/null 2>&1; then
  echo "error: tag $NEW_TAG already exists" >&2
  exit 1
fi

# Preconditions
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "error: working tree not clean — commit or stash first" >&2
  exit 1
fi
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$BRANCH" != "main" ]]; then
  echo "error: not on main (currently $BRANCH)" >&2
  exit 1
fi
if [[ -n "$(git log origin/main..HEAD 2>/dev/null || true)" ]]; then
  echo "error: local main has unpushed commits — push first" >&2
  exit 1
fi
if [[ -n "$(git log HEAD..origin/main 2>/dev/null || true)" ]]; then
  echo "error: origin/main is ahead of local — pull first" >&2
  exit 1
fi

# PAI-123: claim/evidence gate. Refuses to cut a release if any
# `aspirational` row in docs/claim-matrix.md lacks a follow-on ticket.
# Bypass (with reason in the commit message): scripts/check-claims.sh --yolo
"$ROOT/scripts/check-claims.sh"

echo "Bumping $LAST_TAG → $NEW_TAG"
TODAY=$(date -u +%Y-%m-%d)

# 1. VERSION file — single source of truth for SPA-embedded version at `go run`.
echo "$NEW" > VERSION

# 2. CHANGELOG entry. If an entry for this version already exists (drift case),
#    just correct its date. Otherwise prepend a draft and open $EDITOR.
if grep -qE "^## \[$NEW\] " docs/CHANGELOG.md; then
  echo "CHANGELOG: [$NEW] entry exists — updating date to $TODAY"
  # BSD/macOS + GNU both accept `sed -i '' ...` vs `sed -i ...`; portable form:
  tmp=$(mktemp)
  sed -E "s|^## \[$NEW\] .*|## [$NEW] — $TODAY|" docs/CHANGELOG.md > "$tmp"
  mv "$tmp" docs/CHANGELOG.md
else
  echo "CHANGELOG: prepending draft entry for $NEW"
  tmp=$(mktemp)
  {
    awk 'BEGIN{p=1} /^## \[/{p=0} p' docs/CHANGELOG.md
    printf '## [%s] — %s\n\n' "$NEW" "$TODAY"
    printf '### Added — TODO fill in before committing\n\n'
    git log "$LAST_TAG..HEAD" --format='- %s' -- backend/ frontend/src/ docs/ scripts/ || true
    printf '\n'
    awk 'f{print} /^## \[/{if(!f){f=1; print}}' docs/CHANGELOG.md
  } > "$tmp"
  mv "$tmp" docs/CHANGELOG.md

  echo
  echo "Opening CHANGELOG in \$EDITOR (${EDITOR:-vi}) for review…"
  "${EDITOR:-vi}" docs/CHANGELOG.md
fi

# 3. Commit + tag + push
git add VERSION docs/CHANGELOG.md
git commit -m "release: $NEW_TAG"
git tag -a "$NEW_TAG" -m "release $NEW"
git push origin main
git push origin "$NEW_TAG"

echo
echo "Pushed $NEW_TAG. Waiting for CI to publish ghcr.io/markus-barta/paimos:$NEW"
# Public registry — works without local docker (curl-fallback in
# ghcr::image_exists from _deploy-lib.sh).
# shellcheck disable=SC1091
source "$(dirname "$0")/_deploy-lib.sh"
for i in $(seq 1 60); do
  if ghcr::image_exists "ghcr.io/markus-barta/paimos:$NEW"; then
    echo "✔ ghcr.io/markus-barta/paimos:$NEW is live."
    echo
    echo "Next:"
    echo "  just deploy-ppm $NEW_TAG"
    echo "  just deploy-pmo $NEW_TAG"
    echo "  just doc-sync       # file the README / docs / paimos-site sync ticket"
    exit 0
  fi
  sleep 10
done

echo "warning: still not visible after 10m. Check GitHub Actions:" >&2
echo "  gh run list --repo markus-barta/paimos --event push --branch $NEW_TAG" >&2
exit 2
