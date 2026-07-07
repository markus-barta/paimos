# PAIMOS release + deploy commands.
# Run `just` with no argument to list recipes.

default:
    @just --list

# Show the current release state (last tag + commits since).
status:
    @git fetch --tags --quiet origin
    @echo "--- last 5 release tags"
    @git tag --sort=-creatordate | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -5
    @last=$(git tag --sort=-creatordate | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1); \
      echo "--- commits on origin/main since $last"; \
      git log "$last..origin/main" --oneline; \
      echo "--- runtime-relevant only (backend/ frontend/src/)"; \
      git log "$last..origin/main" --oneline -- backend/ frontend/src/

# Cut a release: bump VERSION + CHANGELOG, tag, push, wait for CI.
# Mode: patch | minor | major | <x.y.z>. Omit for AI-assist (commit log dump).
release mode="":
    @./scripts/release.sh {{mode}}

# Generate CycloneDX SBOMs locally (PAI-121). Mirrors the CI tag-push
# step; useful for reviewing dependency exposure before cutting.
sbom:
    @./scripts/sbom.sh

# Check source docs and pulled ppm knowledge cache for stale high-risk
# project facts (removed manifest API, version examples, shipped security
# gaps). Set PAIMOS_CHECK_LIVE=1 to compare ppm/pmo health endpoints too.
knowledge-freshness:
    @./scripts/check-knowledge-freshness.sh

# Prune accumulated GHCR manifest versions (untagged + old sha-* tags).
# Dry-run by default. Run periodically to keep the package well below
# the threshold that triggered the 2026-05-26 push-denied incident
# (see memory/paimos_ghcr_ghost_lock.md). Forward extra args to the
# script, e.g.: `just ghcr-prune --execute --keep-recent-sha=10`.
ghcr-prune *FLAGS:
    @./scripts/ghcr-prune.sh {{FLAGS}}

# Verify a published release image's signature, SBOM attestations, provenance,
# and claim matrix before deploying it.
verify-release tag:
    @./scripts/verify-release.sh {{tag}}

# Wait for tag-push workflows that publish release evidence.
wait-release-ci tag:
    @./scripts/wait-release-ci.sh {{tag}}

# Deploy to ppm: release tag, sha-* image tag, or current HEAD.
deploy-ppm target="":
    @./scripts/deploy.sh ppm {{target}}

# Preflight ppm deploy target without stopping/restarting the service.
deploy-ppm-preflight target="current":
    @./scripts/deploy.sh ppm {{target}} --preflight

# Deploy local HEAD's CI image to ppm.
deploy-ppm-current:
    @./scripts/deploy.sh ppm current

# Deploy to pmo: release tag, sha-* image tag, or current HEAD.
deploy-pmo target="":
    @./scripts/deploy.sh pmo {{target}}

# Preflight pmo deploy target without stopping/restarting the service.
deploy-pmo-preflight target="current":
    @./scripts/deploy.sh pmo {{target}} --preflight

# Deploy local HEAD's CI image to pmo.
deploy-pmo-current:
    @./scripts/deploy.sh pmo current

# File a "doc/site sync follow-up" ticket in PAIMOS for a tag (default
# = latest). Run after `just release` so README, docs/, paimos-site,
# and screenshots don't drift out of sync with the new code.
doc-sync tag="":
    @./scripts/release-doc-sync.sh {{tag}}

# PAI-267 — bring up the local dev stack with the dev-login build
# tag enabled, fixtures seeded, backend on :8888, vite on :5173.
# Drops the operator (or an agent) into a logged-in UI in one
# command. See scripts/dev-up.sh for what it actually does.
dev-up:
    @./scripts/dev-up.sh

# Screenshot a route of the local dev UI to a PNG (needs `just dev-up` running).
# Bootstraps headless Chromium on first run. See docs/VISUAL_VERIFY.md.
shot route="" out="/tmp/paimos-shot.png":
    @./scripts/visual-shot.sh "{{route}}" "{{out}}"
