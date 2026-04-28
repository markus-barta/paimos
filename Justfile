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

# Deploy a tag to ppm (pm.barta.cm). Default tag = latest on origin.
deploy-ppm tag="":
    @./scripts/deploy.sh ppm {{tag}}

# Deploy a tag to pmo (pm.bytepoets.com). Default tag = latest on origin.
deploy-pmo tag="":
    @./scripts/deploy.sh pmo {{tag}}

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
