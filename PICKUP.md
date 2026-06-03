# Pickup — Time & Material reporting (PAI-579/580/581/582)

_Session handoff as of 2026-06-03. Branch `main`. Latest release **v3.10.0**,
live on ppm + PMO._

## TL;DR

A multi-part billing/reporting feature set was implemented, tested, and shipped
in two releases — **v3.9.5** (core) and **v3.10.0** (polish) — both deployed to
**ppm** (`pm.barta.cm`) and **PMO** (`pm.bytepoets.com`) and smoke-tested on
real data. Nothing is pending deploy. Remaining open items are the broader
**PAI-582** test-suite ticket and the minor caveats below.

## Shipped & live (v3.9.5)

Deployed to **ppm** (`pm.barta.cm`) and **PMO** (`pm.bytepoets.com`); both report
version 3.9.5, migration 116 applied, backups taken (rollback commands are in
each deploy log).

- **PAI-557** — Projektbericht PDF prints the customer's full postal address
  even when it lives only in the free-form `address` field (billing → visit →
  free-form fallback, country de-duped).
- **PAI-54** — AR EUR resolves the rate through issue → epic → project →
  customer. Verified live on PMO: ASC26 May-2026 = 127.80 h × €148.93 ≈
  **€19,033** (was blank before).
- **PAI-579** — `GET /api/projects/{id}/time-report?from=&to=&user=` — booked
  hours/material per user/day/issue over a window. Hours-only (no rate leak →
  project-view access). Work date = `date(started_at)` (settable via PAI-478);
  the ticket's "no work date field" premise was stale and is corrected in-ticket.
- **PAI-580** — Export dialog "By month" scope: `scope=time_booked` selects
  tickets with ≥1 booking in `[from,to]`, reports window-booked hours + material
  (T&M: `hours×rate_hourly + material×rate_lp`). Month quick-picker fills the
  editable From/To SSOT; dynamic state checkboxes (default = completed set);
  flat/epic grouping.
- **PAI-581** — `time_entries.material_lp` (migration 116): per-entry material
  (LP / token cost), independent of hours; wired through CRUD + undo snapshot +
  a minimal "Material (LP)" input on the time-entry form.
- **Go toolchain → 1.25.11** — patches GO-2026-5037/38/39 (crypto/x509, mime,
  net/textproto). Was blocking CI security-scan (and thus the docker image /
  deploy). Pinned in go.mod, Dockerfile, and CI + release workflows.

## Shipped & live (v3.10.0 — polish, all PAI-580)

Deployed to ppm + PMO; smoke-tested on real ASC26 data (Apr-2026 159.66 h /
€23,778.87 and May-2026 127.80 h / €19,033.01 as separate month groups;
booked-by rendered "bra, mba" etc.; customer PDF renders).

- Removed the `[keine Kundenfassung]` placeholder from the PDF (silent
  description fallback).
- German thousands separator: `19.033,01` for AR EUR / AR h / subtotals.
- Optional **"Booked by"** column (`cols=booked_by`) — short usernames per row.
- **Group by month** (flat | month | epic): splits each ticket per calendar
  month of its bookings (one row per ticket-month, grouped `YYYY-MM`).

## How to deploy a future cut

```
just release minor            # or patch / <x.y.z>; pre-write the CHANGELOG entry
# wait for CI green (test + security-scan + docker) on the main commit, then:
just deploy-ppm <ver>         # smoke: /api/health + time-report/time_booked endpoints
just deploy-pmo <ver>         # only after ppm smoke is clean (customer-facing)
```

Functional smoke (replace KEY/host):
```
curl -H "Authorization: Bearer $KEY" -H "User-Agent: x" \
  "https://<host>/api/projects/<id>/reports/lieferbericht?scope=time_booked&from=2026-05-01&to=2026-05-31&group=month&statuses=done,delivered,accepted,invoiced&cols=ar_h,ar_eur,booked_by"
```

## Open decisions / caveats

- **Semver**: 3.9.5 was cut as a *patch* (core features), 3.10.0 as a *minor*
  (polish). Both live; nothing pending.
- **Month-group title** is ISO `YYYY-MM` (not localized "Mai 2026") — simple and
  unambiguous; revisit if a localized header is wanted.
- **Booked-by scope**: populated only for `scope=time_booked` (within the
  window); empty for other scopes. Per-window LP/material requires PAI-581's
  `material_lp` source (now present).
- **TZ**: `date(started_at)` is UTC; a booking logged 00:00–02:00 local near a
  month boundary may attribute to the adjacent day. Accepted; documented in
  `reports.go`. Pin to Europe/Vienna if it ever bites.

## Tickets

- **PAI-579** booked-hours report — implemented, shipped.
- **PAI-580** by-month export scope (+ polish) — implemented; base shipped in
  3.9.5, polish in `45869ba` (unreleased).
- **PAI-581** per-entry material — implemented, shipped.
- **PAI-582** billing/AR money-path regression suite — partially realized via
  the tests below; broader suite still open as a tracking ticket.

## Where the code / tests live

- Report engine: `backend/handlers/reports.go` (scope/grouping/query),
  `reports_pdf.go` (PDF + `fmtDE` number format + `bodyTextForRow`),
  `reports_i18n.go` (`lbColSet`, column labels), `time_report.go` (PAI-579).
- Money-path tests: `backend/handlers/time_booked_report_test.go`,
  `reports_pdf_internal_test.go` (fmtDE, marker, address),
  `reports_pdf_test.go` (rate inheritance).
- Export dialog: `frontend/src/components/LieferberichtExportModal.vue`
  (+ `.test.ts`); time-entry form: `components/issue/IssueTimeEntries.vue`.

## Process notes (bit me this session)

- `go test ./...` exit code matters — don't read the exit of a trailing `grep`.
  CI caught a stale `latestSchemaVersion` guard (must bump with each migration:
  `backend/db/schema_regression_test.go`).
- Any `db.go` / SQL-string line shift re-drifts the **gosec baseline**.
  Regenerate `.gosec-baseline.txt` in the same commit:
  run gosec v2.27.1 → jq → `LC_ALL=C sort -u`; expect net-zero finding changes
  (only line numbers move).
- `docker` CI job `needs: [test, security-scan]` — a red security-scan blocks
  the image and therefore any deploy.
