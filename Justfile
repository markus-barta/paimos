# PAIMOS release + deploy commands.
# Run `just` with no argument to list recipes.

default:
    @just --list

# Show the current release state (last tag + commits since).
status:
    @git fetch --tags --quiet origin
    @echo "--- last 5 tags"
    @git tag --sort=-creatordate | head -5
    @last=$(git tag --sort=-creatordate | head -1); \
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
