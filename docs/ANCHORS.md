# Anchor Conventions

PAIMOS anchors are lightweight source comments that tie a code location
to an issue key. They exist so repo-side tooling can generate a stable
`.pmo/anchors.json` index and PMO can render issue-to-file deep links.

## Canonical syntax

Preferred v1 form:

```go
// @paimos PAI-N "anchor ingest endpoint"
```

Supported alias:

```go
// @pmo PAI-N "anchor ingest endpoint"
```

The issue reference is an ordinary PAIMOS issue key like `PAI-68` or
`PMO26-631`. The optional quoted label should name the canonical entry
point, not restate the whole issue title.

## Language-specific comment forms

Go / TypeScript / JavaScript / Vue script:

```ts
// @paimos PAI-N "anchor scanner command"
```

Python / shell / YAML:

```yaml
# @paimos PAI-N "manifest mirror job"
```

SQL:

```sql
-- @paimos PAI-N "blast radius fixture"
```

Markdown / HTML / Vue template:

```html
<!-- @paimos PAI-N "agent onboarding note" -->
```

## Placement rules

- Anchor the canonical entry point of a feature or decision.
- Prefer one to a few anchors per issue, not every touched line.
- Update the anchor when the canonical entry point moves.
- Remove the anchor when the issue no longer maps to a live code path.
- Labels should be short and stable, for example `"anchor ingest endpoint"` or `"manifest mirror command"`.

## Multi-repo and monorepo guidance

- Keep the source comment syntax minimal. Repo qualifiers belong in the
  generated `.pmo/anchors.json`, not in the comment text.
- Root-level `AGENTS.md` is the default for a repo checkout.
- Nested `AGENTS.md` files are reserved for future subproject-specific
  guidance in monorepos; until that support is added, do not assume
  nested files are read automatically.

## Stale anchors

An anchor is stale when:

- the file no longer exists
- the anchor comment is gone
- the surrounding entry point was deleted without updating the index

Line drift inside the same file is tolerated as long as the anchor
comment still exists. Regenerate `.pmo/anchors.json` after moving an
anchor so the committed index stays clean.

## Confidence tiers

The generated index and server-side graph share three confidence tiers:

- `declared` — explicit source comment or deterministic CI-authored row
- `derived` — deterministically inferred from other structure
- `suggested` — heuristic or agent-proposed, requires review

All v1 repo-authored anchors are `declared`.

## Forward compatibility

The v1 syntax stays unchanged even when richer symbol metadata lands.
Future scanners may enrich the generated JSON with:

- `symbol` — containing symbol metadata
- `confidence` — promoted to `derived` for structural enrichments

Those fields belong in the generated index, not the source comment.
