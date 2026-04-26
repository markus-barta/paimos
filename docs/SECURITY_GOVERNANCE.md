# PAIMOS — Security Governance

**Owner:** the maintainer (single-person operation as of v2.0).
**Companion docs:** [`THREAT_MODEL.md`](THREAT_MODEL.md), [`HARDENING.md`](HARDENING.md), [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md), [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md), [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md), [`CONTINUITY.md`](CONTINUITY.md), [`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md).
**Status:** v1 — review every six months. Next: **2026-10-26**.

---

## 0 · Purpose & scope

This document is the **operating system for the trust-doc set**. The other six trust documents in this directory each describe a single domain — what must be true, how to make it true, how to recover when it isn't. This one describes **how those documents stay true over time**:

- The metrics that matter.
- The recurring controls (what gets reviewed, by whom, on what cadence).
- The unified calendar so the cadences don't drift apart.
- The governance loop — how findings (from incidents, drills, releases, reviews) propagate into doc and code updates.

This is **not**:

- A SOC-2 / ISO-27001 control framework. PAIMOS doesn't claim certified governance.
- A maturity model. The bar isn't "level 4 of 5"; the bar is "concrete enough that a successor can pick it up without reconstructing intent."
- A replacement for the runbooks themselves. This document tells you *when* to look at INCIDENT_RESPONSE.md or BACKUP_RESTORE.md; the runbooks themselves tell you *what to do*.

The bar to clear: a solo-maintainer FOSS project, run honestly. **Calendar-driven review of trust artefacts; trigger-driven runbook execution; both feeding back into doc updates.** That's the loop.

---

## 1 · Recurring controls

Each row is a thing the project commits to do on a cadence. The owner column names the **role**, not the person — a successor inherits the role.

| Control | Cadence | Owner | Source of truth |
|---|---|---|---|
| Trust-doc review (THREAT_MODEL, HARDENING, SECURITY_REVIEW, INCIDENT_RESPONSE, CONTINUITY, BACKUP_RESTORE, REFERENCE_DEPLOYMENTS, this doc) | every 6 months | maintainer | the docs themselves; each names its next review date |
| **DR drill** — execute the §3.2 / §3.3 restore scenario in [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) against a real or near-real target; record timeline and gaps | every 6 months, alternating scenarios | maintainer | [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) §5 (captured drill) |
| **Incident-response tabletop** — walk one [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §3 runbook end-to-end; record gaps | every 6 months, alternating scenarios | maintainer | [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §4 |
| **Continuity tabletop** — walk one [`CONTINUITY.md`](CONTINUITY.md) §3 scenario; record gaps | annually | maintainer + (when present) recovery contacts | [`CONTINUITY.md`](CONTINUITY.md) §7 |
| **API key audit** — review every PAIMOS-issued API key (`paimos_…`); revoke unused | every 6 months | maintainer (or admin per deployment) | per-instance via UI: `Settings → Users → <user> → API keys` (`last_used_at` is the signal) |
| **Provider credential rotation** — rotate `OIDC_CLIENT_SECRET`, `MINIO_SECRET_KEY`, `SMTP_PASS`, OpenRouter API key | every 3 months | operator (per-deployment) | [`HARDENING.md`](HARDENING.md) §3.6 |
| **Claim-matrix audit** — re-walk every public claim and verify shipped evidence still holds | at every release | release script ([`scripts/check-claims.sh`](../scripts/check-claims.sh) — automated) | [`docs/claim-matrix.md`](claim-matrix.md) |
| **Doc-sync after release** — README / docs/ / paimos-site / brand assets reviewed for drift | at every release | maintainer | [`scripts/release-doc-sync.sh`](../scripts/release-doc-sync.sh) auto-files the follow-up ticket |
| **gosec re-baseline** — once flipped to blocking (PAI-223), re-baseline to absorb intentional new findings | every 6 months after PAI-223 lands | maintainer | [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md) §2.3 |
| **Reference-deployment register update** — append new findings; status of each reference deployment validated | every 6 months + per finding | maintainer + (per deployment) operator | [`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md) §3 |
| **Brand framework review** — phase posture, claim matrix, public copy alignment | annually + per phase transition | maintainer | [`docs/brand/BRAND.md`](brand/BRAND.md) phasing plan |
| **Trademark check** — TMview / DPMA / EUIPO search for PAIMOS or near-names | every 6 months | maintainer | [`docs/brand/BRAND.md`](brand/BRAND.md) §Re-run trademark checks |
| **External technical review** — framework documented; engagement awaits the right trigger (sponsor, Phase 3, scale, regulator) | not yet committed; framework documented | maintainer (engagement) + reviewer (delivery) | [`EXTERNAL_REVIEW.md`](EXTERNAL_REVIEW.md) |

The pattern: **most controls are 6-monthly**, a few are release-triggered (claim matrix, doc-sync), and a couple are quarterly (provider credential rotation per deployment). Annual cadence is reserved for things where 6 months is overkill (trademark, brand framework as a whole). External review is honestly named as "not yet committed" — see [`PAI-139`](https://github.com/markus-barta/paimos/issues/139).

---

## 2 · Metrics

Solo-FOSS metrics are not enterprise dashboards. The bar is **signals worth tracking over time**, not a comprehensive observability stack.

### 2.1 · Tracked today

| Metric | Source | Target | What it signals |
|---|---|---|---|
| **Days since last release** | `git tag --sort=-creatordate \| head -1` + date | < 60 d (no commitment) | a project that hasn't released in a while is either stable, on hiatus, or abandoned — readers can read the signal honestly |
| **Days since last DR drill** | [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) §5 last-run date | ≤ 180 d | drill staleness; if > 180 d, runbooks haven't been verified recently |
| **Days since last incident-response tabletop** | [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §4 last-run date | ≤ 180 d | same |
| **Open Sev 0/1 disclosures** | `incident_log` table + `security@paimos.com` inbox | 0 (handle as they arrive per [`SECURITY.md`](../SECURITY.md)) | open high-severity incidents are the strongest negative signal a project can produce |
| **gosec finding count** | clean-run output | trending down (currently 118; PAI-223 triages) | SAST regression direction |
| **govulncheck finding count** | clean-run output | trending down (currently 8 stdlib reachable; PAI-224 clears) | dependency-vulnerability direction |
| **CI pass rate on `main`** | GitHub Actions UI / API | ≥ 95% | a noisy `main` masks real defects in the noise |
| **Active reference deployments** | [`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md) §2 count | growing (currently 2) | adoption signal; structurally validates the runbook readability |
| **Time-to-acknowledge on security disclosures** | `security@paimos.com` inbox audit | ≤ 72h per [`SECURITY.md`](../SECURITY.md) | the most important external-facing commitment |
| **Time-to-fix on Sev 0** | `incident_log` `created_at` → `resolved_at` | < 24h per [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §1 | severity-ladder discipline |
| **CHANGELOG entry quality** | manual spot-check at review time | every entry has prose, not the auto-generated TODO stub | release-process discipline (see `2.0_AUDIT.md` D-004 for the failure mode this guards against) |

### 2.2 · Not tracked, deliberately

These are signals other projects track but PAIMOS doesn't, with reasoning:

- **GitHub stars / forks.** Vanity metrics; weak correlation with adoption quality. Reference-deployment count (§2.1) is the better adoption signal.
- **MTTR / MTBF.** Frequency of incidents at solo-FOSS scale is too low for these to mean anything statistically. Time-to-fix on Sev 0 (§2.1) is the operational signal.
- **Code-coverage percentage.** A specific number (e.g., "85% coverage") creates the wrong incentive — adding low-value tests to chase a target. The regression-suite invariants in `THREAT_MODEL.md` §6 (each invariant has a verification path) is the right shape.
- **Developer-velocity / PR-throughput.** PAIMOS is not in a market where PR velocity is a moat; release cadence + claim-matrix gate are the discipline that matter.

The metrics that aren't tracked aren't deliberately opaque — anyone can run the numbers from public artefacts. They're just not what governance reports against.

### 2.3 · Reporting

Today's reporting is **manual at review time**. Every six months the maintainer rolls the metrics into a short prose update appended to this document's §6 Maintenance log. No dashboard; no on-call rotation; no incident-volume KPI. The bar is "the trend is visible to a future maintainer reading the prose."

A future iteration could automate the metric snapshot (a `scripts/governance-metrics.sh` that prints the table). That's a follow-on if/when it pays for itself in time saved; not today.

---

## 3 · The governance loop

How feedback flows from real-world events into doc and code updates. Five trigger types; same outcome for all five — runbook delta + this-doc delta land in the same PR that ships the fix.

### 3.1 · Trigger: a Sev 0 / Sev 1 incident

```
incident → INCIDENT_RESPONSE.md §3 runbook executed
        → §5 post-incident review template filled in (docs/incidents/<UTC-date>-<slug>.md)
        → review names the runbook delta + threat-model delta + governance delta (if any)
        → all deltas + the code fix ship in ONE PR
        → CHANGELOG entry tags it `SEC-YYYY-NN` per SECURITY.md
        → public advisory ≥ 7 days after the patched release
```

**The discipline:** the runbook delta and the code fix ship together. A project that ships fixes without runbook updates accumulates code that's correct and runbooks that aren't.

### 3.2 · Trigger: a captured drill (DR or incident-response tabletop)

```
drill executed → timeline + gaps captured in BACKUP_RESTORE.md §5 or INCIDENT_RESPONSE.md §4
              → each gap drives EITHER a code/doc PR OR a tracked ticket (no silent gaps)
              → next drill (6 months later) targets a DIFFERENT scenario so the rotation covers all runbooks
              → governance log (§6 here) names the drill outcome at the next 6-month review
```

The two-scenario rotation is documented in `BACKUP_RESTORE.md` §5 ("re-run on a different scenario in 6 months") and `INCIDENT_RESPONSE.md` §4 (same).

### 3.3 · Trigger: a release

```
release cut → claim-matrix gate (scripts/check-claims.sh) refuses release if any
              `aspirational` row lacks a tracked ticket
            → image published with cosign signature + SBOM attestation
            → ppm deployed first (canary); pmo deployed second (independence test)
            → just doc-sync files the README/docs/site/brand sync follow-up ticket
            → CHANGELOG entry written manually (no auto-stub leak per 2.0_AUDIT D-004)
```

The release flow IS a governance loop in itself — every release re-validates the public claim surface against the shipped code.

### 3.4 · Trigger: a 6-month review

```
calendar event → review every trust doc:
                 · is its cadence still right?
                 · are its findings logs current?
                 · do its open gaps map to live tickets?
              → roll metrics §2.1 forward into the §6 maintenance log
              → walk the recurring-controls table §1; flag any control that's
                been skipped past its cadence (red flag, not a quiet drift)
              → schedule the next 6-month review on the calendar
```

The next review is on **2026-10-26**. Adding it to a real calendar (not just this doc) is part of what makes the loop concrete.

### 3.5 · Trigger: a structural change (architecture, scope, brand phase)

```
material change → THREAT_MODEL.md updated (new invariants OR retirements)
               → HARDENING.md updated (new operator-side checks)
               → SECURITY_REVIEW.md updated (new review-rule rows)
               → REFERENCE_DEPLOYMENTS.md may gain a new finding row
               → brand/BRAND.md updated if the change is phase-relevant
               → 2.0_AUDIT.md (or its successor for the next major) gains a decisions-log entry
```

Examples already in the project's history: PAI-29/30 contract promotion (drove THREAT_MODEL.md §1 architecture diagram); brand Phase 2 transition (drove BRAND.md phasing plan + paimos.com banner + about.html readings); v2.0 audit close-out (drove 2.0_AUDIT.md D-001 → D-005).

---

## 4 · Unified calendar

The next 18 months. Cadences from §1 collapsed into a single ordered timeline.

| Date | Event | Source |
|---|---|---|
| **continuous** | claim-matrix gate at every release | [`scripts/check-claims.sh`](../scripts/check-claims.sh) |
| **continuous** | doc-sync ticket at every release | [`scripts/release-doc-sync.sh`](../scripts/release-doc-sync.sh) |
| **2026-10-26** | trust-doc 6-month review (this doc + 7 companions) | every trust doc's §"next review" |
| **2026-10-26** | DR drill — alternate scenario from `BACKUP_RESTORE.md` §3.2; first run targeted §3.3 forensic restore against a synthetic DB at production-realistic scale | [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) §5 |
| **2026-10-26** | incident-response tabletop — alternate scenario from `INCIDENT_RESPONSE.md` §3.1 (compromised API key, run 2026-04-26); next runs §3.3 (DB corruption with deliberately-corrupted dev DB) | [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §4 |
| **2026-10-26** | API key audit (admin UI walk; revoke unused) | [`HARDENING.md`](HARDENING.md) §3.6 |
| **2026-10-26** | reference-deployment register findings update; per-deployment status validated | [`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md) §6 |
| **2026-10-26** | trademark check (TMview / DPMA / EUIPO) | [`brand/BRAND.md`](brand/BRAND.md) |
| **2026-10-26** | metrics roll-up into this document's §6 maintenance log | this doc |
| **2026-07-26** | provider credential rotation (every 3 months) | [`HARDENING.md`](HARDENING.md) §3.6 |
| **2026-10-26** | provider credential rotation | same |
| **2027-01-26** | provider credential rotation | same |
| **2027-04-26** | brand framework annual review | [`brand/BRAND.md`](brand/BRAND.md) |
| **2027-04-26** | continuity tabletop — alternate scenario from `CONTINUITY.md` §3.2 (long-term unavailability, run 2026-04-26); next runs §3.4 (GitHub org compromise with deliberately-revoked test token) | [`CONTINUITY.md`](CONTINUITY.md) §7 |
| **2027-04-26** | provider credential rotation | [`HARDENING.md`](HARDENING.md) §3.6 |
| **2027-04-26** | trust-doc 6-month review (second cycle of this calendar) | every trust doc's §"next review" |

**Reading the calendar:** 2026-10-26 is the dense day — most 6-month items converge there because that's six months after this trust-doc set was assembled (April 2026). Future iterations may want to stagger items so a single missed day doesn't skip multiple controls; for v1, alignment is fine because the maintainer can do a single dedicated review-day every six months.

The provider-credential-rotation rows belong to **per-deployment operators**, not the maintainer — they appear here because the maintainer's operations log includes ppm's rotation cadence, but a different operator's pmo cadence is owned by them.

---

## 5 · Ownership

Every recurring control in §1 has an owner column. Today, almost all of them resolve to the same person — **the maintainer**. That's the solo-FOSS reality.

What this document commits to:

- **The role is documented, not the name.** "The maintainer" is a role; future maintainers inherit it. A successor reading this document doesn't need to know who the previous maintainer was.
- **Per-deployment controls are explicitly the operator's**, not the maintainer's. Provider credential rotation for ppm is the maintainer's operations work because the maintainer runs ppm; provider credential rotation for pmo is bytepoets's. The principle: **whoever runs the deployment owns its operational controls.**
- **Privately-named recovery contacts are out of scope here.** [`CONTINUITY.md`](CONTINUITY.md) §2.3 documents that recovery contacts exist (in the maintainer's password-manager vault metadata) without naming them in public; the same applies for governance — successor names live there, not here.

If/when the project grows past a single maintainer, this document's ownership column gains rows like "release manager" / "security lead" / "release-doc-sync owner". For v1: one role, multiple hats.

---

## 6 · Maintenance log

Append-only. Each entry is the maintainer's six-monthly review note: which controls held, which slipped, which gaps were found, what changed.

### 2026-04-26 — initial entry

The trust-doc set was assembled in this six-month window (PAI-125 / 132 / 133 / 138 / 144 / 131 / 141 / 122 / this doc). Initial state baseline:

- **Trust-doc set complete:** 7 docs (THREAT_MODEL / HARDENING / SECURITY_REVIEW / BACKUP_RESTORE / INCIDENT_RESPONSE / CONTINUITY / REFERENCE_DEPLOYMENTS) + this governance doc.
- **Drills captured:** DR drill (synthetic 500-issue SQLite, 0.432 s wall-time, integrity_check ok, 5 gaps identified for next iteration) — see [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) §5; incident-response tabletop on §3.1 (compromised API key) — see [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §4; continuity tabletop on §3.2 (long-term unavailability) — see [`CONTINUITY.md`](CONTINUITY.md) §7.
- **Reference deployments:** 2 (ppm + pmo) — see [`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md) §2.
- **Open Sev 0/1:** 0.
- **gosec findings:** 118 (38 high + 80 medium); triage tracked in [`PAI-223`](https://github.com/markus-barta/paimos/issues/223).
- **govulncheck findings:** 8 reachable stdlib; clears with go1.25.7 upgrade tracked in [`PAI-224`](https://github.com/markus-barta/paimos/issues/224).
- **Active reference deployments:** 2 (ppm + pmo).
- **CHANGELOG quality:** v2.0.2 + v2.0.3 entries had to be filled in retroactively (see [`2.0_AUDIT.md`](2.0_AUDIT.md) D-004); subsequent entries (v2.0.0, v2.0.1, v2.0.4) were hand-written cleanly.

**Open governance gaps named at initial entry:**

- gosec + govulncheck non-blocking pending PAI-223 + PAI-224
- External technical review programme not yet established (PAI-139)
- Off-host backup destination for ppm not yet wired (REFERENCE_DEPLOYMENTS.md F-08)
- Project Context bottom-docked workbench (PAI-184) — UX spec exists but not built
- Per-issue activity panel + general undo system (PAI-200 / PAI-209 epics) — designed, not implemented

These gaps are tracked, not silent. Each has a ticket; each is named in either §1 (this doc) or one of the trust docs it's anchored in.

### 2026-10-26 — pending

(To be filled in at the next review day.)

---

## 7 · Cross-references

- **[`THREAT_MODEL.md`](THREAT_MODEL.md)** — what must be true (32 invariants).
- **[`HARDENING.md`](HARDENING.md)** — how to make it true in deployment (45 ops checks).
- **[`SECURITY_REVIEW.md`](SECURITY_REVIEW.md)** — how to keep it true in builds (4 scanners + 7 review domains).
- **[`BACKUP_RESTORE.md`](BACKUP_RESTORE.md)** — what happens when something fails (drill captured).
- **[`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md)** — when something gets through (5 runbooks + tabletop).
- **[`CONTINUITY.md`](CONTINUITY.md)** — when the maintainer is out (6 scenarios + tabletop).
- **[`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md)** — what we've learned running it (12 findings).
- **[`EXTERNAL_REVIEW.md`](EXTERNAL_REVIEW.md)** — framework for engaging external technical review when feasible. The §1 "external technical review" recurring control above lives there in detail.
- **[`SECURITY.md`](../SECURITY.md)** — inbound disclosure policy.
- **[`2.0_AUDIT.md`](2.0_AUDIT.md)** — programme-scope decisions log.
- **[`claim-matrix.md`](claim-matrix.md)** — claim ↔ shipped-evidence registry.
- **[`brand/BRAND.md`](brand/BRAND.md)** — brand framework + phasing plan.
- **[`paimos.com/trust.html`](https://paimos.com/trust.html)** — public outward trust posture.
- **[`scripts/check-claims.sh`](../scripts/check-claims.sh)** — release-time gate.
- **[`scripts/release-doc-sync.sh`](../scripts/release-doc-sync.sh)** — release-time doc-sync.
