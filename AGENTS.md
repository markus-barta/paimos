# PAIMOS Agent Quickstart

Start here when opening this repo cold.

## Core commands

- `cd backend && go test ./...`
- `cd frontend && npm test -- --run`
- `cd frontend && npm run typecheck && npm run build`

## High-signal docs

- [`docs/DEVELOPER_GUIDE.md`](docs/DEVELOPER_GUIDE.md)
- [`docs/AGENT_INTERFACE.md`](docs/AGENT_INTERFACE.md)
- [`docs/AGENT_INTEGRATION.md`](docs/AGENT_INTEGRATION.md)
- [`docs/ANCHORS.md`](docs/ANCHORS.md)

## Project-context surface

- `GET /api/projects/{id}/repos`
- `GET /api/projects/{id}/manifest`
- `POST /api/projects/{id}/anchors`
- `GET /api/projects/{id}/graph`
- `GET /api/projects/{id}/graph/blast-radius`
- `POST /api/projects/{id}/retrieve`
- `GET /api/issues/{id}/anchors`

## Repo-side tooling

- `go run ./backend/cmd/paimos anchors scan --repo-root . --output .pmo/anchors.json`
- `go run ./backend/cmd/paimos anchors verify --repo-root . --index .pmo/anchors.json`
- `go run ./backend/cmd/paimos manifest pull --project PAI --repo-root .`

## Notes

- The committed `.pmo/anchors.json` is dogfood for the anchor tooling.
- Managed `AGENTS.md` blocks written by `paimos manifest pull` use
  `<!-- pmo-manifest: managed:start -->` / `<!-- pmo-manifest: managed:end -->`.
