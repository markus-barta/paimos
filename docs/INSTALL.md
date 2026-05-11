# Installing the PAIMOS CLI

The `paimos` CLI (and its sibling `paimos-mcp`) ships as a signed,
notarized universal binary for macOS and unsigned tarballs for Linux.
A new release lands on every `v*` git tag (PAI-99); the
`releases/latest/` URL always points at the most recent one.

---

## macOS — one-liner (recommended)

```bash
curl -fL https://github.com/markus-barta/paimos/releases/latest/download/paimos_darwin_universal.tar.gz \
  | tar xz -C /usr/local/bin paimos
```

Or, pinned to a version:

```bash
VER=3.2.4
curl -fL https://github.com/markus-barta/paimos/releases/download/v$VER/paimos_${VER}_darwin_universal.tar.gz \
  | tar xz -C /usr/local/bin paimos
```

The binary is **codesigned + notarized** under "Developer ID
Application: BYTEPOETS GmbH". Gatekeeper accepts it on first run; no
`xattr` dance, no System Settings approval. First run requires an
internet connection so macOS can fetch the notarization ticket from
Apple — after that it works offline.

Verify the signature manually if you want:

```bash
spctl --assess -vv /usr/local/bin/paimos
# → /usr/local/bin/paimos: accepted
# → source=Notarized Developer ID
# → origin=Developer ID Application: BYTEPOETS GmbH (TEAM_ID)
```

For `paimos-mcp` (the MCP server), substitute `paimos-mcp` everywhere:

```bash
curl -fL https://github.com/markus-barta/paimos/releases/latest/download/paimos-mcp_darwin_universal.tar.gz \
  | tar xz -C /usr/local/bin paimos-mcp
```

---

## Linux

Same shape, no signing (Linux has no Gatekeeper-equivalent):

```bash
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
curl -fL https://github.com/markus-barta/paimos/releases/latest/download/paimos_linux_${ARCH}.tar.gz \
  | tar xz -C /usr/local/bin paimos
```

---

## Verify checksums

Every release ships a `sha256sums.txt` next to the tarballs:

```bash
VER=3.2.4
curl -fLO https://github.com/markus-barta/paimos/releases/download/v$VER/sha256sums.txt
curl -fLO https://github.com/markus-barta/paimos/releases/download/v$VER/paimos_${VER}_darwin_universal.tar.gz
shasum -a 256 -c sha256sums.txt --ignore-missing
```

---

## Build from source

If you have Go 1.25+ and don't need the signed binary (e.g., on a
Linux server, in CI, or for a contribution):

```bash
go install github.com/markus-barta/paimos/backend/cmd/paimos@latest
go install github.com/markus-barta/paimos/backend/cmd/paimos-mcp@latest
```

The Nix flake at [`pkgs/paimos-cli`](https://github.com/markus-barta/nixcfg)
also builds from source declaratively.

---

## After install

```bash
paimos --version    # 3.2.4
paimos auth login   # interactive — writes ~/.paimos/config.yaml
paimos issue list --help
```

For the full agent-driving guide see [Agent Interface Guide](AGENT_INTERFACE.md).
