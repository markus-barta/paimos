#!/usr/bin/env bash
set -euo pipefail

TAG="${1:-}"
if [[ -z "$TAG" ]]; then
  echo "usage: $0 <tag>" >&2
  echo "       e.g. $0 v2.0.0" >&2
  exit 1
fi

TAG="${TAG#v}"
IMAGE="ghcr.io/markus-barta/paimos:$TAG"
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

for cmd in cosign gh jq; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "[verify] missing tool: $cmd" >&2
    exit 2
  fi
done

ID_RE='^https://github.com/markus-barta/paimos/.+'
OIDC='https://token.actions.githubusercontent.com'

echo "[verify] $IMAGE"
echo
echo "[1/4] cosign signature"
if cosign verify "$IMAGE" \
  --certificate-identity-regexp "$ID_RE" \
  --certificate-oidc-issuer "$OIDC" >/dev/null 2>&1; then
  echo "  ok signed by GitHub Actions OIDC identity"
else
  echo "  failed image signature verification" >&2
  exit 3
fi

echo
echo "[2/4] CycloneDX SBOM attestations"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
if cosign verify-attestation "$IMAGE" \
  --type cyclonedx \
  --certificate-identity-regexp "$ID_RE" \
  --certificate-oidc-issuer "$OIDC" 2>/dev/null \
  | jq -s '.[].payload | @base64d | fromjson | .predicate' > "$tmp_dir/sboms.json"; then
  count=$(jq -s 'length' "$tmp_dir/sboms.json")
  echo "  ok verified $count SBOM attestation(s)"
else
  echo "  failed SBOM attestation verification" >&2
  exit 4
fi

echo
echo "[3/4] SLSA build provenance"
if gh attestation verify "oci://$IMAGE" --owner markus-barta >/dev/null 2>&1; then
  echo "  ok verified build provenance attestation"
else
  echo "  failed provenance verification" >&2
  exit 5
fi

echo
echo "[4/4] claim gate"
if "$ROOT/scripts/check-claims.sh" >/dev/null; then
  echo "  ok claim matrix is current"
else
  echo "  failed claim matrix gate" >&2
  exit 6
fi

echo
echo "All release verification checks passed for $IMAGE."
