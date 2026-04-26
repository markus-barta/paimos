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

This currently fuses lexical hits across issue text, anchors, manifest
content, ADRs, and NFRs, then expands related graph neighbors.

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
