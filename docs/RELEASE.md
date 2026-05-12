# PAIMOS — Release & Trust Evidence

This document describes what every PAIMOS tag publishes, where the
artefacts live, and how an operator can verify them before deploying.

## What a tag publishes

When CI runs against a `v*` tag, **two workflows** fire in parallel:
[`ci.yml`](https://github.com/markus-barta/paimos/blob/main/.github/workflows/ci.yml)
produces the container image and supply-chain evidence,
[`release.yml`](https://github.com/markus-barta/paimos/blob/main/.github/workflows/release.yml)
(PAI-99) produces the signed CLI binaries. Either can fail without
blocking the other; both must succeed for a release to be considered
fully published.

### Container image (ci.yml)

1. **Image** — `ghcr.io/markus-barta/paimos:<x.y.z>` (immutable per
   tag) plus `:<x>.<y>` and `:<x>` moving aliases. The same digest is
   also tagged `sha-<short>` for SHA-pinned deploys.
2. **CycloneDX SBOMs** (PAI-121) — uploaded as a release artifact
   named `sbom-v<x.y.z>` containing `backend.sbom.json` and
   `frontend.sbom.json`. These describe every Go module and every npm
   package that ended up in the image, including transitive
   dependencies and resolved licenses.
3. **Sigstore signatures + SBOM attestations** (PAI-121) — `cosign
   sign` binds the image manifest digest to a keyless signature backed
   by GitHub's OIDC token; `cosign attest` attaches each SBOM as a
   verifiable attestation against the same digest. No long-lived
   signing key is stored anywhere — the workflow's OIDC token is the
   only thing that can produce a signature for that digest.

### CLI binaries (release.yml — PAI-99)

The `paimos` CLI and the `paimos-mcp` MCP server are built for three
platforms and attached to the GitHub Release as tarballs:

| Artifact (versioned) | Alias (unversioned) | Signed? |
|---|---|---|
| `paimos_<x.y.z>_darwin_universal.tar.gz` | `paimos_darwin_universal.tar.gz` | ✅ Developer ID + notarized |
| `paimos_<x.y.z>_linux_amd64.tar.gz` | `paimos_linux_amd64.tar.gz` | — |
| `paimos_<x.y.z>_linux_arm64.tar.gz` | `paimos_linux_arm64.tar.gz` | — |
| `paimos-mcp_<x.y.z>_darwin_universal.tar.gz` | `paimos-mcp_darwin_universal.tar.gz` | ✅ |
| `paimos-mcp_<x.y.z>_linux_amd64.tar.gz` | `paimos-mcp_linux_amd64.tar.gz` | — |
| `paimos-mcp_<x.y.z>_linux_arm64.tar.gz` | `paimos-mcp_linux_arm64.tar.gz` | — |
| `sha256sums.txt` — versioned filenames only | — | — |

The unversioned aliases let `releases/latest/download/<name>` work in
the install one-liner without a "look up the latest tag first"
round-trip. Bytes are identical to the versioned form, so the sums
file lists only the versioned names.

**macOS signing** uses a Developer ID Application certificate held in
a personal Apple Developer account ("Developer ID Application: Markus
Barta (P66J39QV6V)"). Codesign sets the hardened runtime + a secure
timestamp; `xcrun notarytool submit --wait` ships each binary to Apple
for notarization. The ticket lives on Apple's servers (stapler can't
bind to bare Mach-O executables) — Gatekeeper fetches it on first run.

**Pre-release tags** (anything containing a hyphen, e.g. `v3.2.4-rc1`)
are auto-marked `prerelease: true` and don't take over
`/releases/latest/`.

`main` builds keep the previous behaviour: container image + `latest`
tag, no SBOM, no signature, no CLI binaries.

## How to verify a release

The short path is:

    just verify-release v<x.y.z>

That wraps [`scripts/verify-release.sh`](../scripts/verify-release.sh) and
checks the image signature, SBOM attestations, GitHub provenance
attestation, and claim matrix. It requires `cosign`, `gh`, and `jq`
locally. The manual commands below are the same evidence surface broken out
for inspection.

### Container image

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

### CLI binary (macOS)

After downloading the darwin universal tarball, confirm the signature
chain and the notarization ticket:

    codesign --display --verbose=2 paimos        # shows the cert chain
    codesign --test-requirement="=notarized" \
             --verify --verbose=2 paimos         # explicit requirement satisfied → notarized

The expected `Authority` line is `Developer ID Application: Markus
Barta (P66J39QV6V)` followed by Apple's intermediate and root CAs.

Verify the SHA-256 against the published sums file:

    curl -fLO https://github.com/markus-barta/paimos/releases/download/v<x.y.z>/sha256sums.txt
    shasum -a 256 -c sha256sums.txt --ignore-missing

## Generating SBOMs locally

`just sbom` (or `scripts/sbom.sh`) regenerates both SBOMs into
`dist/sbom/`. Useful when reviewing dependency exposure before cutting,
or when a downstream auditor asks for a snapshot.

## Cutting a release

Pick patch / minor / major; the script handles VERSION bump, README
badge, CHANGELOG date, commit, tag, and the wait for `ghcr.io/.../<ver>`
to appear:

    just release patch
    just release minor
    just release <x.y.z>      # explicit override (e.g., for post-rc cuts)

For agent / non-TTY runs, the CHANGELOG entry for the new version must
already exist (the script refuses to auto-generate the stub when
`$EDITOR` is missing). Add the `## [<x.y.z>]` section to
[`docs/CHANGELOG.md`](CHANGELOG.md) first, then:

    ./scripts/release.sh patch --no-edit
    # or the explicit form, e.g. when the latest tag is an -rc pre-release:
    ./scripts/release.sh <x.y.z> --no-edit

After the tag is pushed, both workflows run in parallel — total
wall-clock is typically 8–15 minutes (Apple's notarytool dominates the
darwin job).

## Background

PAI-121 closed the audit's call for "SBOM · CycloneDX manifest of every
dependency, published with each release", and the trust posture for the
"Self-hostable" / "Open API" claims. PAI-124 follows on with the rest of
the evidence-and-repeatability layer (provenance, regression gates,
incident-response drills). PAI-99 (v3.2.4) added the signed CLI release
pipeline so external users have a one-liner install path on macOS
without the Gatekeeper-quarantine dance.
