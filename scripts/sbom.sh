#!/usr/bin/env bash
# PAI-121: generate CycloneDX SBOMs for the backend (Go module) and the
# frontend (npm tree). Mirrors what CI does on tag push, so an operator
# can produce the same artefacts locally before a release.
#
# Output: dist/sbom/backend.sbom.json, dist/sbom/frontend.sbom.json
#
# Usage:
#   scripts/sbom.sh
#
# Requirements:
#   - go (cyclonedx-gomod is fetched via `go run` so no global install
#     is needed)
#   - npm (uses npx @cyclonedx/cyclonedx-npm; cached after first run)

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

OUT="$ROOT/dist/sbom"
mkdir -p "$OUT"

echo "[sbom] backend (Go) → $OUT/backend.sbom.json"
( cd backend && \
  go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest \
    app -licenses -json -output "$OUT/backend.sbom.json" -main . )

echo "[sbom] frontend (npm) → $OUT/frontend.sbom.json"
( cd frontend && \
  npx --yes @cyclonedx/cyclonedx-npm \
    --output-format JSON \
    --output-file "$OUT/frontend.sbom.json" )

echo "[sbom] done"
ls -la "$OUT"
