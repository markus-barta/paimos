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
- `GET /api/projects/{id}/knowledge` — unified knowledge plane (PAI-338); replaces the removed `/manifest` endpoint
- `POST /api/projects/{id}/anchors`
- `GET /api/projects/{id}/graph`
- `GET /api/projects/{id}/graph/blast-radius`
- `POST /api/projects/{id}/retrieve`
- `GET /api/issues/{id}/anchors`
- `GET /api/projects/{id}/agents/{name}.json` — canonical agent artifact (PAI-329)

## Repo-side tooling

- `paimos anchors scan --output .pmo/anchors.json`
- `paimos anchors verify --index .pmo/anchors.json`
- `paimos onboard --project PAI [--agent <name>]` — single-shot project briefing (PAI-340)
- `paimos skill render <agent>` — render an agent artifact through a harness adapter (PAI-329 / PAI-332)

## Notes

- The committed `.pmo/anchors.json` is dogfood for the anchor tooling.
- Skill files rendered by `paimos skill render` carry a paimos-managed header so `paimos sync check` can detect drift; the legacy `paimos manifest pull` flow was removed in PAI-358 (replaced by the knowledge plane).
