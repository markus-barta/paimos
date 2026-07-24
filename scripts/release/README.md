# scripts/release/

Helpers used by the [release-v2.yml](../../.github/workflows/release-v2.yml) workflow
(PAI-99 — signed + notarized macOS CLI release).

## Apple Developer secrets

`release-v2.yml` expects these five repo secrets in `inspr-at/paimos`
(they live only in the encrypted GitHub secret store — never in the tree):

- `APPLE_CERTIFICATE`            — base64 of the .p12
- `APPLE_CERTIFICATE_PASSWORD`   — password for the .p12
- `APPLE_ID`                     — Apple Developer account email
- `APPLE_PASSWORD`               — app-specific password (notarytool auth)
- `APPLE_TEAM_ID`                — 10-char Apple team id

They were provisioned via a one-shot mirror workflow (removed after use, per
its own instructions). If they ever need re-provisioning or rotation, set them
directly from the personal Apple Developer credential source:

```bash
gh secret set APPLE_CERTIFICATE -R inspr-at/paimos < cert.p12.b64
# … repeat for the other four
gh secret list -R inspr-at/paimos   # confirm the 5 names
```

PAI-688 tracks documenting the canonical personal source of truth for these
credentials.
