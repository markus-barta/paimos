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
VER=3.10.7
curl -fL https://github.com/markus-barta/paimos/releases/download/v$VER/paimos_${VER}_darwin_universal.tar.gz \
  | tar xz -C /usr/local/bin paimos
```

The binary is **codesigned + notarized** under "Developer ID
Application: Markus Barta (P66J39QV6V)". Gatekeeper accepts it on
first run; no `xattr` dance, no System Settings approval. First run
requires an internet connection so macOS can fetch the notarization
ticket from Apple — after that it works offline.

Verify the signature manually if you want:

```bash
spctl --assess -vv /usr/local/bin/paimos
# → /usr/local/bin/paimos: accepted
# → source=Notarized Developer ID
# → origin=Developer ID Application: Markus Barta (P66J39QV6V)
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
VER=3.10.7
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

## After install — first-use checklist

```bash
paimos --version    # 3.10.7
```

### 1. Log in

```bash
paimos auth login
# Instance URL [https://pm.barta.cm]: <your PAIMOS host>
# API key (input hidden): <paste a key you generated in Settings → API Keys>
# ✓ logged in as <you> at <url>
#   saved to ~/.paimos/config.yaml as instance "default"
#   api key stored in OS keyring (service "paimos-cli", account "default")
```

The instance URL goes to `~/.paimos/config.yaml` (mode `0600`). The
API key goes to your **OS keyring** — Keychain on macOS, Secret
Service / KWallet on Linux, Credential Manager on Windows — under
service `paimos-cli`, account `<instance-name>`. It is never written
to disk in plaintext.

### 2. Try a read-only command

```bash
paimos issue list --project PAI --limit 5
paimos issue get PAI-1
```

If those work, the CLI is wired up correctly.

### 3. Common patterns

```bash
# Multi-line markdown — no shell-quoted-JSON foot-gun
paimos issue create --project PAI --type ticket \
  --title "Refactor auth middleware" \
  --description-file /tmp/desc.md \
  --ac-file /tmp/ac.md

# Idempotent status transitions — safe to re-run
paimos issue ensure-status PAI-83 done

# Update + close-note in one atomic-ish step
paimos issue update PAI-83 --status done \
  --close-note-file /tmp/closing-note.md

# Search, then tag the issue you found
paimos search "flaky session" --project PAI --limit 5
paimos tag list
paimos issue tag add PAI-83 --tag backend

# Track work without leaving the terminal
paimos time start PAI-83 --note "Investigating session expiry"
paimos time stop
paimos time list --issue PAI-83

# Attach a screenshot or generated artefact to the ticket
paimos attach PAI-83 /tmp/screenshot.png
paimos attach list --issue PAI-83

# Machine-readable output for shell pipelines
paimos --json issue list --project PAI --status backlog
```

### Multi-instance / headless

```bash
# Multiple PAIMOS instances
paimos auth login --name ppm       --url https://pm.barta.cm
paimos auth login --name bytepoets --url https://pm.bytepoets.com
paimos --instance ppm issue list   # switch per-command

# CI / containers (no OS keyring available)
export PAIMOS_API_KEY="paimos_<your-key>"
paimos issue list --project PAI

# Fully bypass config + keyring (temporary agent credentials)
PAIMOS_URL=https://pm.barta.cm \
PAIMOS_API_KEY=paimos_<your-key> \
  paimos issue list --project PAI
```

---

## Deeper guides

The above covers install + auth + the everyday verbs. For the full
agent-driving surface (bulk operations, declarative `apply` YAML, MCP
integration with Claude Desktop, REST fall-back patterns), see:

- [docs/AGENT_INTERFACE.md](AGENT_INTERFACE.md) — the comprehensive CLI guide
- [docs/AGENT_INTEGRATION.md](AGENT_INTEGRATION.md) — REST integration patterns
- [docs/api-minimal.md](api-minimal.md) — REST reference
