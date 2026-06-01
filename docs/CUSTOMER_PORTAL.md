# Customer Portal — Operator Guide & Rollout Runbook

This document covers how the PAI-458 customer-portal-v2 visibility
model works in production, how operators flip an issue's visibility
state, how the one-time backfill seeds the new tag, and the exact
checkpoints to walk through before turning enforcement on.

If you're a developer who just needs to know which tag to attach,
skip to [How an issue becomes customer-visible](#how-an-issue-becomes-customer-visible).
If you're rolling out the v2 model on a fresh instance, jump to
[Rollout runbook](#rollout-runbook).

---

## Why this exists

Before PAI-458, the customer portal returned every non-deleted issue in
projects the customer had access to. That surfaced internal-only types
(`Memory`, `Guideline`, `Runbook`, `External_system`, `Related_project`)
along with cross-project notes ("BON26 is the pattern source for
ASC26…") and operational warnings ("ASC26-Zenta repo — HANDS OFF").
Visibility was implicit and inverted by default — opt-out via type
filtering. We needed the opposite: opt-in by an explicit signal.

The signal is the `CUSTOMERPORTAL` system tag.

## How an issue becomes customer-visible

Three paths attach the tag:

1. **Customer submits a request via the portal.** `PortalSubmitRequest`
   creates the issue and attaches `CUSTOMERPORTAL` in the same
   transaction. The audit row uses mutation type
   `portal.submit.auto_tag` so it reads distinctly from interactive
   toggles in the admin visibility report.

2. **Internal user clicks the toggle in `IssueDetailView`.** Lives near
   the status/priority subheader. Calls the standard issue-tag API —
   nothing special on the wire. Requires editor access on the project.

3. **Internal user runs the bulk action in IssueList.** Multi-select +
   "Make visible in portal" / "Hide from portal". Confirmation modal at
   ≥10 selected. Backs onto `POST /api/issues/batch/tags` with a single
   shared batch_id in `mutation_log`. Mixed-permission selections fail
   the whole batch with 403; UI prevents this via the disabled state.

There is **no auto-rule** — `system_tag_rules` is untouched. The tag
is pure manual signal except for the portal-submission auto-attach.

Detaching uses the same three paths in reverse. The tag itself cannot
be renamed or deleted (`isSystemTag` is enforced in `DeleteTag`).

## The status-transition nudge

When an issue moves to `delivered` or `done` and the tag is missing, a
soft amber banner appears on `IssueDetailView` above the description:

> "This change isn't visible to the customer portal yet. Make visible →"

Clicking the link attaches the tag (same call as the toggle). The
banner has no close-without-action button by design — dismissing
without attaching would defeat the discipline. The banner disappears
automatically when the tag attaches, when status leaves the
delivered/done set, or when the issue is cancelled.

## The visibility marker on IssueList

Two affordances on every row:

- **Distinct chip** in the tags column: the `CUSTOMERPORTAL` tag renders
  as a compact eye + `CP` marker with a solid border and a tooltip
  ("issue is shown in customer portal").
- **Always-visible eye glyph** in the type cell: even when the user
  has hidden the tags column entirely, the glyph survives. The
  CUSTOMERPORTAL marker is load-bearing — it never disappears.

A three-state filter chip cycles `Any → Visible → Hidden → Any` so
operators can scan "what is the customer seeing right now" with one
click. The filter composes on top of the existing status/type/tag
chips; everything narrows together.

## The admin visibility report

`/admin/projects/:id/portal-visibility` (admin-only) shows:

- **Header metric** — "Visible to customer right now: N issues"
- **Issue table** — every CUSTOMERPORTAL-tagged issue with its
  last-toggled actor + event type
- **Audit feed** — chronological paginated stream of every attach /
  detach / migration event from `mutation_log` (50 per page, oldest at
  the bottom)
- **CSV exports** — two endpoints:
  - `?section=current` — current visibility state
  - `?section=audit` — full audit feed
  Both return `Content-Disposition: attachment` so the browser saves
  the file directly. Useful for compliance pulls and "what was the
  customer seeing at moment T" reconstructions.

The view bounces non-admins back to `/` on mount; backend gates with
`auth.RequireAdmin + auth.RequireProjectView`.

## How the API filter works

Every portal endpoint that returns issues or per-status counters
applies an `EXISTS (SELECT 1 FROM issue_tags …)` subquery against the
issues row. `EXISTS` keeps the rest of each query's plan untouched and
composes cleanly with the existing access gate. The filter is gated
behind `portalVisibilityEnforced()` so the dry-run env var (below) is
a one-line on/off.

Endpoints affected:

- `GET /api/portal/overview` — KPIs + projects + awaiting list +
  recent Projektberichte
- `GET /api/portal/projects` — list with `issue_count` / `done_count`
- `GET /api/portal/projects/:id` — same shape per-project
- `GET /api/portal/projects/:id/issues` — list (plus the new
  multi-select filter params and safe-allowlisted sort/order, PAI-461)
- `GET /api/portal/projects/:id/issues/:issueId` — returns **404**
  (not 403) when the tag is absent so the endpoint never discloses an
  internal issue's existence at a given id
- `GET /api/portal/projects/:id/summary` — by-status rollup
- `GET /api/portal/projects/:id/projektberichte` — left unfiltered.
  A Projektbericht snapshot is an explicit override: an accepted
  snapshot remains readable through the snapshot path even if some
  embedded issues lack the tag.

Tag id is process-cached via an `atomic.Value` — one lookup at first
use, then pure reads forever.

## The dry-run env var

```
PAIMOS_PORTAL_VISIBILITY_DRY_RUN=true
```

Default: unset / false → enforcement is **on**. Setting it to `true`:

- Leaves every portal endpoint **unfiltered** — the system behaves as
  if the filter doesn't exist.
- Adds a per-project `would_hide_count` field to `/api/portal/overview`
  showing exactly how many non-deleted issues lack the tag. That's
  what the live filter would hide. Operators read it, decide whether
  they're ready to flip, and unset the env var.

The env var is read on every request (no daemon restart needed beyond
re-reading the env at deploy time).

## Rollout runbook

This is the exact sequence to flip a fresh instance.

### Step 0 — ship the release with dry-run on

Deploy the release containing PAI-458. Set
`PAIMOS_PORTAL_VISIBILITY_DRY_RUN=true` in the runtime env (it's
already wired via `~/Secrets/ppm/.env` on `pm.barta.cm` and
`~/Secrets/PMO/.env` on `pm.bytepoets.com`; just append the line if
not already set).

Migration M109 creates the tag, M110 backfills every terminal-status
issue (delivered / done / accepted / invoiced) idempotently. Both
migrations run automatically on startup. Re-running M110 is a no-op:
the NOT EXISTS gate skips already-tagged issues, the temp table is
empty on the second pass, no duplicate audit rows.

**Checkpoint:** confirm M110 ran by querying:

```sh
paimos --instance ppm doctor
```

…and by spot-checking the admin visibility report on a project you
know had terminal-status issues — the audit feed should list
`migration_backfill` rows under the `m110-customerportal-backfill`
batch.

### Step 1 — observe for one week with dry-run on

Watch the per-project `would_hide_count` on `/api/portal/overview`.
Pull it from the admin user's portal landing page or via:

```sh
paimos --instance ppm curl /portal/overview | jq '.projects[] | {key, name, would_hide_count}'
```

(Adjust to your actual access pattern — the field appears on every
project in `projects[]` when the env var is set.)

What you're looking for:

- Are there projects with surprisingly high `would_hide_count`?
  Those need a per-project review — the backfill only caught terminal-
  status issues, not active work that customers also see today. Use
  the IssueList bulk action to tag the rest.
- Are there any project where `would_hide_count` is `> 0` but you
  know the customer expects to keep seeing the items? Same fix.

### Step 2 — per-project review (or whole-tenant confirm)

Walk through each customer project and decide. The fastest path is the
IssueList bulk action: filter to "all issues currently customer-
visible-but-untagged" via the three-state chip (Hidden), select-all-
matching, and `Make visible in portal`. The confirmation modal at ≥10
selected gives a final pause before the bulk apply lands.

Or — for a fresh instance with no real customer usage yet — confirm
the whole tenant and skip the per-project review.

### Step 3 — unset the env var → enforcement live

Remove the `PAIMOS_PORTAL_VISIBILITY_DRY_RUN=true` line from the env
file and re-deploy (or `docker compose restart paimos` for csb1). The
filter switches on; portal users now see only tagged issues.

Sanity-check from the admin visibility report — visible_count should
match what the customer sees in their portal.

### Step 4 — customer comms

Per the 2026-05-20 CEO call (Markus, bytepoets): **silent rollout** on
the bytepoets-side instance. No real customers are in production on
`pm.bytepoets.com` yet; the cutover happens before anyone notices a
diff. The ppm/personal instance is internal-only and needs no comms.

If real customers land later, the comms script is:

> "We narrowed customer-portal visibility to an explicit opt-in tag.
> Items you've been seeing aren't going anywhere; items you weren't
> meant to see won't slip through. If anything that used to be
> visible looks missing, ping us."

---

## Internationalisation

Every new string surfaces both DE and EN under the `visibility.*`
catalog group in `frontend/src/i18n/{de,en}.ts`. Key entries:

| key | EN | DE |
|---|---|---|
| `visibility.label` | Visible in Customer Portal | Im Kundenportal sichtbar |
| `visibility.hint` | Customers with portal access see this issue. | Kunden mit Portalzugang sehen dieses Issue. |
| `visibility.hintOff` | This issue is internal-only. | Dieses Issue ist nur intern sichtbar. |
| `visibility.disabledTooltip` | Editor access required | Editor-Berechtigung erforderlich |
| `visibility.nudge` | This change isn't visible to the customer portal yet. | Diese Änderung ist im Kundenportal noch nicht sichtbar. |
| `visibility.nudgeAction` | Make visible → | Sichtbar machen → |
| `visibility.bulkMakeVisible` | Make visible in portal | Im Portal sichtbar machen |
| `visibility.bulkHide` | Hide from portal | Aus dem Portal ausblenden |
| `visibility.filterTitle` | Customer Portal | Kundenportal |
| `visibility.filterVisible` | Visible | Sichtbar |
| `visibility.filterHidden` | Hidden | Ausgeblendet |
| `visibility.filterAny` | Any | Alle |
| `visibility.auditLine` | Last toggled by {actor} · {when} | Zuletzt geändert von {actor} · {when} |
| `visibility.auditAuto` | Auto-tagged on portal submission · {when} | Automatisch vergeben bei Portal-Einreichung · {when} |
| `visibility.auditMigration` | Auto-tagged by rollout migration · {when} | Automatisch vergeben durch Rollout-Migration · {when} |

If you add new visibility-adjacent copy, prefer extending this group
over scattering it across other catalogs.

## Are-we-ready-to-flip checkpoints

Before unsetting the dry-run env var:

- [ ] Migration M109 + M110 ran successfully (admin report shows the
      `migration_backfill` batch).
- [ ] Per-project `would_hide_count` reviewed for surprises. Anything
      that should stay visible is tagged.
- [ ] Spot-check `/api/portal/projects/:id/issues/:issueId` returns
      404 (not 403) for untagged issues. (Use any internal `Memory`
      row's id under a customer-accessible project to verify.)
- [ ] Spot-check projektbericht acceptance still works for issues
      embedded in a snapshot — the snapshot path is an explicit
      override; verify with at least one in-progress acceptance flow.
- [ ] Customer-comms decision documented (silent vs notify).

Once all five are green, unset `PAIMOS_PORTAL_VISIBILITY_DRY_RUN`,
re-deploy, and the v2 visibility model is live.

## Sharing a deep-link to one issue (PAI-479)

The portal project view (and the internal IssueList views it mirrors)
sync the open side-panel selection to `?selected=<ISSUE_KEY>` in the
URL. To share a specific issue in context: open the project, click the
row so the side panel appears, copy the URL bar, send. The recipient
lands on the same project with the same issue auto-opened in the side
panel — regardless of which tab or filters they had stored locally.

The URL update uses `replaceState`, so scanning a long list does not
pollute browser history. Keys that aren't accessible to the recipient
(not in the CUSTOMERPORTAL set, or in a project the recipient can't
view) fail gracefully — the panel stays closed and the URL is left
untouched.
