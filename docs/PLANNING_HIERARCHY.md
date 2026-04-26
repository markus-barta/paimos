# PAIMOS Planning Hierarchy — Review & Recommendation

**Programme:** PAI-198 (under PAI-189)
**Date:** 2026-04-26
**Status:** recommendation final; no schema change recommended for 2.0; lightweight convention adopted instead.

---

## Question

Should PAIMOS extend its planning hierarchy beyond the current epic / ticket / task model — for example by introducing sub-epics, multi-level parents, or a programme entity above the epic — to better represent large multi-workstream efforts like PAI-189 itself?

---

## Current model

PAIMOS's data model uses a **single `issues` table** with a `type` discriminator, plus two relation patterns:

- **Strict 1:1 parent** via `parent_id` for ticket → task (and, by convention, for grouping ticket → epic when `parent_id` references the epic).
- **M:N relations** via `issue_relations` for `groups` (epic / cost_unit / release linking many tickets), `sprint` (sprint linking many tickets), `depends_on`, `impacts`, `follows_from`, `blocks`, `related`.

Issue types in the v2.0 enum: `epic`, `cost_unit`, `release`, `sprint`, `ticket`, `task`. All live in the same table; the type discriminates the rendering view, the allowed parent relations, and the grouping behaviour, not the storage path.

Two consequences relevant to the question:

- An epic *can* have any other issue type as its child via `parent_id` (the database doesn't refuse it), but the convention used across PAIMOS so far is `epic → ticket → task` exactly two hops deep.
- Epic-of-epics — i.e., a parent epic above other epics — is **not blocked at the schema level**, but no view, filter, or report assumes it exists.

---

## Evidence — where the awkwardness actually showed up

PAI-189 is the biggest test case so far. It is one umbrella epic with nine children, several of which are *themselves* multi-ticket workstreams (backend audit + refactor; frontend audit + refactor; tests; release process). The first instinct during planning was: "these workstream children should be sub-epics with their own tickets underneath." The instinct was **not acted on**, because the existing relation set already represents this:

- Each PAI-189 child is a `ticket` with prose-level acceptance criteria covering the full workstream.
- Where a workstream needs more granular tracking, that's done with **same-level sibling tickets** linked via `related` or `groups`, not a deeper hierarchy.
- The `groups` relation type already lets an epic "contain" arbitrary tickets without those tickets needing to be of type `epic` themselves.

The same shape recurred in PAI-200 (AI UX layer, 8 children) and PAI-209 (Undo, 9 children). Both fit the existing model without strain.

The remaining awkwardness is **cross-epic visibility** at programme scale:

- "Show me everything happening for v2.0" cuts across PAI-189, PAI-200, PAI-209, and several stand-alone tickets. There is no first-class entity that groups them.
- The CLI and the SPA both filter cleanly by `parent`, by `status`, by `priority`, and by `tag` — but not by an arbitrary set of issue keys representing a programme.

---

## Options considered

### A — Keep current model unchanged

Resist the urge to add a new entity type. Use the existing relations (`groups`, `parent_id`) plus the existing `tags` system to mark cross-cutting programmes. Add no migration, no schema change, no new view.

**Pros:** zero schema risk; respects the "thin substrate" stance; the awkwardness is real but small and maps cleanly to a tag convention.
**Cons:** programme-level views require remembering to apply the tag; no structural enforcement of "is this in the v2.0 programme."

### B — Add a "programme" entity above epic

Introduce a new issue type `programme` (or a separate `programmes` table). Programmes contain epics; epics contain tickets; tickets contain tasks.

**Pros:** structural representation of large efforts; enables programme-level dashboards; fits enterprise PM expectations.
**Cons:** non-trivial migration; views and filters across the SPA need updating; the vocabulary creep ("are we calling this a programme or an initiative?") inevitably starts; the CLI gains a new noun; schema migration cost without proportional product value at PAIMOS's current scale.

### C — Allow `parent_id` between epics

Don't add a new type. Allow an epic to have an epic parent. The existing rendering layer can be taught to traverse multi-level parent chains.

**Pros:** schema unchanged; minimal type-system churn; matches Linear's approach.
**Cons:** the rendering layer assumptions need updating across many views (issue tree, breadcrumbs, parent picker, complete-epic action, sprint membership); the conceptual model becomes "any depth," which is harder to reason about than "exactly two hops."

### D — Adopt a lightweight tag convention

Use the existing `tags` system to mark programme membership. A tag named `programme:2.0` (or similar) on every ticket, epic, or task that belongs to the programme makes the cross-cutting set queryable today. The view layer already supports filter-by-tag.

**Pros:** zero engineering cost; adopts an existing first-class entity (`tags`); composes cleanly with all existing filters; programme-end is just a stop-applying-the-tag operation; the convention can evolve without a migration if it doesn't pan out.
**Cons:** convention not enforcement — a ticket in the programme without the tag goes uncounted; relies on contributor discipline.

---

## Recommendation

**Adopt option D — the tag convention — and explicitly defer options B and C.**

Rationale:

1. **The awkwardness is real but small.** PAI-189 / PAI-200 / PAI-209 are the three largest umbrella efforts in PAIMOS history; all three fit the current model with only programme-level visibility loss. The cost of B is permanent; the gain is marginal at current scale.
2. **Tags already exist and are first-class.** The `tags` table, the per-issue M:N tag attachment, the SPA tag filter, the CLI `--tag` flag, the `system_tags` rule engine for auto-tagging — all of this is shipped and stable. Putting `programme:<name>` into the tag namespace requires no engineering, just convention.
3. **Convention failures are recoverable.** If the tag convention frays — for example, two contributors disagree on what counts as "in the v2.0 programme" — the cost is an audit-time `paimos issue list --tag programme:2.0` review, not a schema rollback.
4. **Reconsider when forced to.** The trigger to revisit this is unambiguous: when an actual customer or contributor asks for programme-level *reporting* or *cross-programme rollups* and the tag convention can't cleanly answer. At that point, B becomes justified by demand, not by speculation.
5. **C is the wrong shape.** Multi-level epic chains complicate the rendering layer for ambiguous benefit. If we're going to invest, B (a clean, explicit entity) is the right destination — not C.

---

## Adoption guidance

For programmes that want cross-cutting visibility today, use the `programme:<slug>` tag pattern:

```bash
# Apply the tag to every issue in a programme
paimos issue update PAI-189 --tag programme:2.0
paimos issue update PAI-200 --tag programme:2.0
paimos issue update PAI-209 --tag programme:2.0
# … and to each child ticket as it lands

# Query the programme set
paimos issue list --tag programme:2.0 --status backlog,in-progress
```

For programme-end:

```bash
# Remove the tag from every issue in the programme
paimos issue list --tag programme:2.0 --json | jq -r '.issues[].issue_key' | \
  xargs -I{} paimos issue update {} --tag-remove programme:2.0
```

The convention is opt-in per programme; small and short-lived efforts don't need it.

---

## Trigger for reconsideration

Move to option B if **two of the following three** hold simultaneously:

1. A customer or contributor explicitly requests programme-level dashboards or cross-programme rollups that the tag convention can't answer.
2. PAIMOS itself runs more than three concurrent programmes of PAI-189-scale.
3. A regulator, auditor, or downstream tool requires programme as a first-class entity for export.

Until then: tag convention is sufficient and the model stays simple.

---

## Companion documents

- [`2.0_AUDIT.md`](2.0_AUDIT.md) — the parent audit report; this document is its appendix on planning hierarchy.
- [`DATA_MODEL.md`](DATA_MODEL.md) — current schema reference.
- [`AGENT_INTERFACE.md`](AGENT_INTERFACE.md) — CLI / API patterns including tag operations.
