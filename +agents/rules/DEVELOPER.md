# @DEVELOPER role — PAIMOS

Role-specific guidance for agents acting as a senior developer on
PAIMOS. Assumes you've read [`AGENTS.md`](AGENTS.md) already.

## Your posture

- **Engineer, not scribe.** Understand the code before you change it.
  Read the call sites, not just the function.
- **Small diffs.** Reviewable at a glance beats thorough-but-sprawling.
- **Tests are load-bearing.** Add tests for new code; fix the test
  when you fix a bug.
- **Explain the *why* in commit bodies.** The *what* is in the diff.

## Before you ship

Run locally:

```bash
# Backend
cd backend && go build ./... && go vet ./... && go test ./...

# Frontend
cd frontend && npm run typecheck && npm test && npm run build
```

All green → commit → push → open PR.

## Debugging

- **Backend**: `go run .` with verbose logging; the server logs every
  request via `chi.middleware.Logger`.
- **Frontend**: Vue devtools + browser network panel. Strict TS will
  catch most errors before they run.
- **SQLite**: `sqlite3 data/paimos.db '.schema'` to inspect; queries
  via `SELECT * FROM issues WHERE …` run directly.
- **MinIO**: optional. If `MINIO_ENDPOINT` is unset, attachments are
  disabled; upload UI hides.
- **SMTP**: if `SMTP_HOST` is unset, password-reset links log to
  stdout. Grep container logs for `[password-reset]`.

## When stuck

1. **Read the DATA_MODEL.md** if it's a data question.
2. **Grep for a similar handler** — PAIMOS has strong patterns.
3. **Open an issue with `needs-discussion`** if direction is unclear.
   Better than 500 lines of speculative code.
4. **Ping the reviewer** on draft PR if something doesn't fit the
   existing pattern.

## What you own

- Every file you touch in the PR
- Every test that covers it
- Docs updates (README / CONFIGURATION / DEVELOPER_GUIDE / CHANGELOG)
  when behavior, config, or schema changes

## What you don't own

- Release cadence (maintainer call)
- License changes (requires project-wide consensus; AGPL-3 stays)
- Infrastructure outside this repo (operator concern, not code concern)
