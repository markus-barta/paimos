# Contributing to PAIMOS

Thanks for considering a contribution! PAIMOS is AGPL-3.0, developed
openly, and happy to take pull requests from anyone.

## Before you start

- **Bugs**: check existing issues, then open a new one with repro
  steps. A failing test case in your PR is 10× more valuable than a
  description.
- **Features**: open an issue or discussion first so we can agree on
  direction before you invest time. The bar for new surface area is
  higher than for bug fixes and internal-quality improvements.
- **Security**: do **not** open public issues. See
  [`SECURITY.md`](SECURITY.md) for the reporting process.

## Development setup

### Requirements

- Go 1.23+
- Node.js 22+
- Docker (for local smoke tests)
- `devenv` (recommended; provides a pinned Go + Node toolchain)

### First run

```bash
git clone https://github.com/markus-barta/paimos.git
cd paimos

# with devenv
devenv shell -- bash -c "cd backend && DATA_DIR=../data STATIC_DIR=../frontend/dist go run ."

# separate terminal
devenv shell -- bash -c "cd frontend && npm install && npm run dev"
```

Frontend dev server: <http://localhost:5173>; API: <http://localhost:8888>.
Vite proxies `/api/*` to the Go backend.

First run: set `ADMIN_PASSWORD` before starting the backend to seed the
initial admin user.

## Running tests

```bash
# backend
cd backend && go test ./...

# frontend
cd frontend && npm test
```

## Code style

- **Go**: `gofmt`, `goimports`. CI will block unformatted diffs.
- **TypeScript / Vue**: strict TS. Run `npm run typecheck` before
  submitting. Vue SFCs use `<script setup lang="ts">`.
- **Commit messages**: conventional-ish prefixes
  (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`). Subject line
  ≤72 chars. Body paragraphs explain *why*, not *what*.
- **Comments**: only when the *why* is non-obvious. Don't restate what
  well-named code already says.

## Developer Certificate of Origin

PAIMOS uses the [DCO](DCO.md) in place of a CLA. Every commit must
include a sign-off line:

```
Signed-off-by: Your Name <you@example.com>
```

Add it automatically with `git commit -s` (or `-sm "message"`).

The sign-off certifies that you wrote the change or otherwise have the
right to submit it under AGPL-3.0. By signing off, you acknowledge the
DCO terms in `DCO.md`.

Enable the repo's pre-commit hook once:

```bash
git config core.hooksPath .githooks
```

## Pull request flow

1. Fork the repo and branch from `main` (`feat/…`, `fix/…`, `docs/…`).
2. Write the change + tests. Keep the diff focused — one concern per
   PR.
3. Ensure `go test ./...`, `npm run typecheck`, `npm test`, and
   `npm run build` all pass locally.
4. Commit with DCO sign-off (`git commit -s`).
5. Push and open a PR. Describe **what changed and why** in the body;
   link the issue.

## What makes a PR easier to review

- Small and focused. A 50-line PR gets reviewed same-day; a 5,000-line
  PR waits.
- Self-contained. No drive-by renames of unrelated files.
- Justified. If it's a trade-off, name the trade-off.
- Tested. New code path → new test. Bug fix → test that would have
  failed pre-fix.

## What we're likely to push back on

- Features that add surface area without clear user demand
- Dependencies on services beyond SQLite + optional MinIO + optional
  SMTP (PAIMOS's "zero-dep by default" stance is load-bearing)
- UI changes that break keyboard-first flows
- Backwards-incompatible API or DB-schema changes without a migration
  path
- Contributions that require giving up the AGPL-3.0 license (no
  re-licensing without a real CLA, which we don't have)

## Issue labels

- `good-first-issue` — small, well-scoped, good to cut your teeth on
- `help-wanted` — bigger than trivial, would welcome outside help
- `bug` / `feature` / `docs` / `security`
- `needs-discussion` — design direction unclear; comment before coding

## Getting help

Open a discussion on GitHub or file an issue tagged `question`. Keep it
here in the open — DMs don't scale.
