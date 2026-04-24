# PAIMOS — Release & Trust Evidence

This document describes what every PAIMOS tag publishes, where the
artefacts live, and how an operator can verify them before deploying.

## What a tag publishes

When CI runs against a `v*` tag, the following artefacts are produced:

1. **Container image** — `ghcr.io/markus-barta/paimos:<x.y.z>` (immutable
   per tag) plus `:<x>.<y>` and `:<x>` moving aliases. The same digest
   is also tagged `sha-<short>` for SHA-pinned deploys.
2. **CycloneDX SBOMs** (PAI-121) — uploaded as a release artifact named
   `sbom-v<x.y.z>` containing `backend.sbom.json` and
   `frontend.sbom.json`. These describe every Go module and every npm
   package that ended up in the image, including transitive
   dependencies and resolved licenses.
3. **Sigstore signatures + SBOM attestations** (PAI-121) — `cosign sign`
   binds the image manifest digest to a keyless signature backed by
   GitHub's OIDC token; `cosign attest` attaches each SBOM as a
   verifiable attestation against the same digest. No long-lived
   signing key is stored anywhere — the workflow's OIDC token is the
   only thing that can produce a signature for that digest.

`main` builds keep the previous behaviour: image + `latest` tag, no
SBOM, no signature.

## How to verify a release

Verify the signature (replace `<x.y.z>` with the tag you're pulling):

    cosign verify ghcr.io/markus-barta/paimos:<x.y.z> \
      --certificate-identity-regexp '^https://github.com/markus-barta/paimos/.+' \
      --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'

Pull the SBOM attestation:

    cosign download attestation \
      --predicate-type 'https://cyclonedx.org/bom' \
      ghcr.io/markus-barta/paimos:<x.y.z> | \
      jq -r '.payload | @base64d | fromjson | .predicate'

The decoded predicate is the same CycloneDX JSON that lives next to
the GitHub release artifact, so an operator who pulls only by digest
gets the bill of materials directly off the registry.

## Generating SBOMs locally

`just sbom` (or `scripts/sbom.sh`) regenerates both SBOMs into
`dist/sbom/`. Useful when reviewing dependency exposure before cutting,
or when a downstream auditor asks for a snapshot.

## Background

PAI-121 closed the audit's call for "SBOM · CycloneDX manifest of every
dependency, published with each release", and the trust posture for the
"Self-hostable" / "Open API" claims. PAI-124 follows on with the rest of
the evidence-and-repeatability layer (provenance, regression gates,
incident-response drills).
