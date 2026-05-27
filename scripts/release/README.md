# scripts/release/

Helpers used by the [release-v2.yml](../../.github/workflows/release-v2.yml) workflow
(PAI-99 — signed + notarized macOS CLI release).

## mirror-apple-secrets.yml

One-shot copier that mirrors the 5 Apple Developer secrets from `bytepoets-mba/bp-esc`
into this repo without ever decrypting them on a developer's laptop. Drop into
`bytepoets-mba/bp-esc/.github/workflows/`, add a `MIRROR_PAT` secret (a fine-scoped PAT
with `secrets:write` on `markus-barta/paimos`), then:

```bash
gh workflow run mirror-apple-secrets -R bytepoets-mba/bp-esc
gh run watch -R bytepoets-mba/bp-esc           # ~5 sec
gh secret list -R markus-barta/paimos          # confirm the 5 names appeared
```

After it succeeds, **delete `MIRROR_PAT` from bp-esc and remove the workflow file** —
neither is needed again unless secrets rotate.

The secrets land with the names `release-v2.yml` expects:

- `APPLE_CERTIFICATE`            — base64 of the .p12
- `APPLE_CERTIFICATE_PASSWORD`   — password for the .p12
- `APPLE_ID`                     — Apple Developer account email
- `APPLE_PASSWORD`               — app-specific password (notarytool auth)
- `APPLE_TEAM_ID`                — 10-char Apple team id
