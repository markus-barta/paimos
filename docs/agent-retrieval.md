# Agent Retrieval Examples

Canonical read-only queries agents can run against the PMO context
layer. These examples are also mirrored into generated `AGENTS.md`
files.

## Retrieve mixed context

```bash
curl -s -H "Authorization: Bearer $PAIMOS_API_KEY" \
  -H "Content-Type: application/json" \
  "$PAIMOS_URL/api/projects/$PROJECT_ID/retrieve" \
  -d '{"q":"password reset flow","k":10}'
```

This currently fuses lexical hits across issue text, anchors, and
derived symbols, blends in local semantic vector matches via
reciprocal-rank fusion, then expands related graph neighbors.

Vector indexing is asynchronous. A retrieve call queues a background
index refresh for the project and uses whatever vectors are already
indexed; on a cold project the first response may be lexical-only, and
the next response will include vector hits once the worker has caught up.
The response `meta` includes `embedding_indexing: "async"`,
`embedding_model`, `embedding_provider`, `vector_index`, and `freshness`.
As of v3.10.3, `embedding_model` is `local-semantic-v2` and
`vector_index` is `sqlite-scalar-cosine`: vectors are stored in SQLite
and ranked inside SQL via the deterministic `paimos_cosine()` scalar
function. This keeps the Docker/no-CGO build path stable; a future ANN
extension such as sqlite-vec can replace the ranking implementation
without changing the public response shape.

## Local broker for coding agents

For repository-aware agents, prefer the local broker when the agent needs
both PMO context and local files:

```bash
paimos serve --project "$PROJECT_KEY" --repo-root . --addr 127.0.0.1:8765
```

HTTP endpoints:

- `GET /health`
- `GET /context/repo`
- `POST /context/search` with `{ "q": "...", "k": 12 }`
- `POST /context/read` with `{ "path": "backend/main.go", "start_line": 1, "end_line": 80 }`
- `POST /context/symbols`
- `POST /context/retrieve`
- `POST /context/pack`

For MCP clients that launch stdio servers:

```bash
paimos serve --project "$PROJECT_KEY" --repo-root . --mcp-stdio
```

The broker is read-only, loopback-only by default, blocks path traversal
and symlink escape, skips generated/secret-prone paths, caps reads and
searches, and labels repo-derived content as `untrusted_data`.

## Traverse the project graph

```bash
curl -s -H "Authorization: Bearer $PAIMOS_API_KEY" \
  "$PAIMOS_URL/api/projects/$PROJECT_ID/graph?root=project:$PROJECT_ID&depth=2"
```

## Blast radius of an issue

```bash
curl -s -H "Authorization: Bearer $PAIMOS_API_KEY" \
  "$PAIMOS_URL/api/projects/$PROJECT_ID/graph/blast-radius?issue=PAI-79&depth=3"
```

## Inspect issue anchors

```bash
curl -s -H "Authorization: Bearer $PAIMOS_API_KEY" \
  "$PAIMOS_URL/api/issues/PAI-29/anchors"
```
