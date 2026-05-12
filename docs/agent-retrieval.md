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
derived symbols, blends in deterministic local vector matches via
reciprocal-rank fusion, then expands related graph neighbors.

Vector indexing is asynchronous. A retrieve call queues a background
index refresh for the project and uses whatever vectors are already
indexed; on a cold project the first response may be lexical-only, and
the next response will include vector hits once the worker has caught up.
The response `meta` includes `embedding_indexing: "async"`,
`embedding_model`, and `vector_index`. The current vector search path is
the built-in brute-force fallback; a SQLite-native ANN extension can
replace it later without changing the public response shape.

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
