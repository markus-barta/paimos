# Agent Rules — PAIMOS

Guidance for AI agents (Claude Code, Cursor, etc.) editing the PAIMOS
codebase. Complements `CONTRIBUTING.md` — that's for humans, this
surfaces the bits an agent needs extra reminders for.

## Working in this repo

- **Pre-commit hook**: users enable it with `git config core.hooksPath
  .githooks`. Don't bypass with `--no-verify`.
- **DCO sign-off**: every commit must end with `Signed-off-by: Name
  <email>` (use `git commit -s`). No DCO = no merge.
- **Small, focused diffs**: one concern per PR. If you notice an
  adjacent issue, open a separate PR or file a tracking issue.
- **Tests first on bug fixes**: write a failing test that reproduces
  the bug, then fix. Commit both together.

## What lives where

| What | Where |
|---|---|
| Env-var → brand-identity mapping | `backend/brand/brand.go` |
| All routes | `backend/main.go` |
| DB migrations | `backend/db/db.go` (`migrate()` function) |
| Operator env-var reference | `docs/CONFIGURATION.md` |
| Contribution mechanics | `CONTRIBUTING.md` |
| Security reporting | `SECURITY.md` |
| Brand guide | `docs/brand/BRAND.md` |
| Dev deep-dive | `docs/DEVELOPER_GUIDE.md` |

## Release workflow

1. Merge PR to `main`.
2. Update `docs/CHANGELOG.md`: new entry at the top, format
   `## [X.Y.Z] — YYYY-MM-DD`, one-line headline, bullet list of
   notable changes. Short and readable.
3. Bump `VERSION` and `frontend/package.json` "version" together.
4. Tag the commit: `git tag -s vX.Y.Z -m "vX.Y.Z"`.
5. Publish (CI / manual).

## Code conventions

- **Go**: `gofmt`, `goimports`. No new deps without discussion. Handlers
  are thin — push logic into sibling files, not handler bodies.
- **Vue / TS**: `<script setup lang="ts">`, strict TS, Pinia for
  cross-view state. All localStorage keys prefixed `paimos:`.
- **Comments explain *why*, not *what*.** Well-named identifiers
  already cover the *what*.
- **Error handling**: validate at boundaries (HTTP input, external
  APIs). Trust internal code paths. No defensive try/catch for
  scenarios that can't happen.

## Pitfalls to avoid

- Hardcoding identity strings — use `brand.Default.<Field>` instead.
- Writing non-idempotent migrations — `IF NOT EXISTS` everywhere.
- Breaking graceful-disable for optional services (MinIO, SMTP). They
  should degrade, not panic.
- Forgetting the frontend after a schema change — TS types in
  `frontend/src/api/` need updating too.
- Rewriting history on `main`. Never force-push to `main`.
- Bundling unrelated refactors into a bug-fix PR.

## Questions → GitHub issues

Open a discussion or issue before touching anything controversial
(new surface area, dep changes, API-breaking changes). "Ask first"
saves everyone time.
