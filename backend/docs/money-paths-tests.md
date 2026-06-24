# Money-paths test map (PAI-582)

The billing/AR money paths are customer-facing and financial, so the **core
paths are regression-tested rather than left to manual checks** — see _Known
gaps_ below for what is deliberately not yet covered. Before changing anything
near rates, hours, material, or the report/PDF money output, run this suite first
and keep it green.

```bash
# the whole money-path suite (fast):
cd backend && go test ./handlers/ -run 'MoneyPaths|TimeBookedReport|Lieferbericht|Projektbericht|TodaySummary'
# frontend money/report views:
cd frontend && npx vitest run src/**/*export* src/**/*time*report*
```

## The paths and what locks them

| # | Money path | Source of truth | Tests |
|---|---|---|---|
| 1 | **Effective rate hierarchy** (PAI-54): issue → cost_unit → project → customer | `system_tags.go` `ResolveRateCascade`, `projects.go` `applyEffectiveRates`, `reports.go` SQL COALESCE | `TestResolveRateCascade_MoneyPaths`, `TestApplyEffectiveRates_MoneyPaths`, `TestLieferberichtJSON_InheritsCustomerRate` (the 2026-06 blank-AR-EUR regression) |
| 2 | **Budget hours** (LP→hours): `estLp × rateLp/rateHourly` | `system_tags.go` `computeBudgetHours` | `TestComputeBudgetHours_MoneyPaths` (direct hours, LP conversion, null/zero/negative LP, zero-rate division guard) |
| 3 | **Booked hours** (PAI-579): `override / (stopped−started)×24 / running=0` | `time_report.go`, `system_tags.go`, `issues.go` | `TestTimeBookedReport_MoneyPaths`, `TestTodaySummary_StoppedInWindow_Sums`, `TestTodaySummary_EmptyDay_ReturnsZero` |
| 4 | **Export scope** (PAI-580): window selection, state filter, flat/epic/month grouping, AR h / AR EUR totals + grand total | `reports.go`, `project_reports.go` | `TestTimeBookedReport_MoneyPaths`, `TestLieferbericht_UngroupedUsesProjectKey`, `TestLieferberichtJSON_ColsParam` |
| 5 | **Material** (spin-off): per-window `material_lp` sum + AR SP | `time_report.go`, `reports.go`, `time_entries.go` | `TestTimeBookedReport_MoneyPaths` (material_lp aggregation + window exclusion) |
| 6 | **PDF party block** (PAI-557): postal-address fallback billing→visit→free-form, country de-dup, UID/FN omit-when-empty | `reports_pdf.go` `projectReportCustomer*`, `hasPostalDetail`, `compactPostalAddressLines` | `TestProjectReportCustomerAddressLines_MoneyPaths`, `TestHasPostalDetail_MoneyPaths`, `TestProjectReportCustomerContact_MoneyPaths`, `TestCompactPostalAddressLines_MoneyPaths`, `TestProjektberichtCustomerPartyHelpersIncludePostalAndLegalDetails`, `TestProjektberichtCustomerAddressLines_FreeFormFallback` |

## Suite layout

- `handlers/money_paths_internal_test.go` (`package handlers`) — unexported,
  DB-free units: `applyEffectiveRates`, `computeBudgetHours` (paths that don't
  hit the DB), and the PDF party-block helpers.
- `handlers/money_paths_test.go` (`package handlers_test`) — the DB-backed
  `ResolveRateCascade` cascade (incl. the PAI-599 cost_unit-edge resolution and
  a dangling-edge fall-through).
- `handlers/time_booked_report_test.go` — the end-to-end booked-hours / export /
  material money path (pre-existing; the integration anchor).
- `handlers/reports_pdf_test.go` + `reports_pdf_internal_test.go` — Lieferbericht /
  Projektbericht JSON + PDF, incl. the customer-rate inheritance regression.

## Key invariants (the things that must never silently drift)

1. **Customer-only rates must reach AR EUR.** A customer with rates and a
   project/issue with none must still produce a non-blank AR EUR (the 2026-06
   bug). Locked by `TestLieferberichtJSON_InheritsCustomerRate`.
2. **The cascade is per-rate-kind independent.** hourly can inherit while lp
   overrides, and vice versa.
3. **cost_unit is resolved by edge id, not title** (PAI-599) — robust to renames;
   a dangling edge falls through, never errors.
4. **Running timers contribute 0 hours**; `override` always wins over derived.
5. **The window is inclusive of both ends**; an out-of-window booking is excluded
   (e.g. a June entry must not appear in a May report).
6. **Zero is a real rate override, not an inherit trigger.**

## Known gaps (future hardening — not yet covered)

- Booked-hours: explicit month-boundary-spanning-timezone case, multi-day span,
  sub-millisecond rounding (current coverage uses clean values).
- Export scope: cost_unit/release **filter** (vs grouping) via `issue_relations`
  edges, and `grand_total == Σ subtotals` after independent rounding.
- Frontend: export-dialog param construction + booked-hours/time-report view
  rendering (PAI-582 acceptance item 3).
