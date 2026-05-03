# PAIMOS — External Technical Review Programme

**Owner:** the maintainer (single-person operation as of v2.0).
**Companion docs:** [`SECURITY_GOVERNANCE.md`](SECURITY_GOVERNANCE.md) (the operating loop this fits into), [`THREAT_MODEL.md`](THREAT_MODEL.md), [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md), [`paimos.com/trust.html`](https://paimos.com/trust.html) (§05 limits aligns with §1 here).
**Status:** v1 — review every six months. Next: **2026-10-26**.

---

## 0 · Purpose & scope

This document is the **framework** for engaging external technical review of PAIMOS. It says:

- What "external review" means for a project at this scale.
- The triggers that would make engaging a reviewer worth the cost.
- What the project would prepare *before* engaging one (so the reviewer's time is well spent).
- How findings from such a review would flow back into doc and code updates.

It is **not**:

- A claim that PAIMOS has *had* an external review. It hasn't. As of 2026-04-26, every audit, drill, and tabletop in the trust-doc set has been internal — the maintainer reviewing the maintainer's work, with all the limits that implies.
- A commitment to engage one by a specific date. Funding, scale, and timing aren't there. The honest position is "framework documented; execution awaits the right window."
- A substitute for community review. Open-source FOSS gets continuous informal review every time someone reads the source — that has real value, but it's structurally different from a paid engagement.

**The bar to clear:** when an external review becomes feasible (a sponsor, a research collaborator, a security-aware adopter willing to fund), the project doesn't waste their time figuring out scope and prep. This doc is what they'd read first.

---

## 1 · The honest current state

PAIMOS has **never had a paid external technical review** as of v2.0 (2026-04-26). Every defect found and fixed in this project's history (PAI-110 through PAI-118 in the v1.7.0 enterprise-security cycle; PAI-189 in the v2.0 audit; subsequent governance docs) was found by the maintainer reviewing the maintainer's work, plus a small set of contributors who reviewed PRs at the GitHub level.

Two things follow from that honestly:

1. **The trust posture has limits.** Internal review catches what the maintainer is competent to look for; external review catches what they're not. [`paimos.com/trust.html` § 05 limits](https://paimos.com/trust.html) names this explicitly: *"No third-party security review yet."*
2. **The trust posture is not nothing.** The 32 invariants in [`THREAT_MODEL.md`](THREAT_MODEL.md), the 45 hardening checks in [`HARDENING.md`](HARDENING.md), the four-scanner pipeline in [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md), the captured drills in [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) and [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md), the production validation in [`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md) — these are real, documented, evidence-grounded artefacts. They're what an external reviewer would *use as input*, not what they'd replace.

The framework below is what we'd hand a reviewer when one becomes feasible.

---

## 2 · What "external review" means for PAIMOS

Three classes of external engagement, in order of cost and depth:

### 2.1 · Targeted review (focused scope, single domain)

A reviewer with subject-matter depth in one specific area looks at one specific thing. Examples:

- **Auth-flow review** — a reviewer experienced with session / OIDC / TOTP / password-reset patterns walks `backend/auth/` against [`THREAT_MODEL.md`](THREAT_MODEL.md) §4.1 invariants and the corresponding §4.1 review rules in [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md).
- **CSP / browser-side review** — a reviewer with web-security depth looks at the SPA delivery + frontend bundle + reverse-proxy config recommended in [`HARDENING.md`](HARDENING.md) §3.1.
- **SQLite / data-integrity review** — a reviewer experienced with WAL semantics + integrity_check + restore semantics validates [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) against real production-scale data.

**Cost:** typically days, not weeks. The narrow scope is the cost-control mechanism.
**Output:** a written findings document with severity-tagged items; PAIMOS-side ingestion is the same flow as [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §5 post-incident review.

### 2.2 · Programme review (broad scope, multi-domain)

A reviewer (or small team) walks the trust-doc set end to end and probes the substrate against it. Examples:

- **Pre-1.0 → 1.0 readiness review** — when PAIMOS reaches the threshold for a 1.0 declaration with stronger backwards-compatibility commitments, a programme review validates that the documented invariants actually hold.
- **Phase 2 → Phase 3 transition review** — per [`brand/BRAND.md`](brand/BRAND.md), Phase 3 is the commercial phase. A programme review would precede that transition since commercial customers expect verifiable claims.
- **Major-version review** — every major version (v3.0, v4.0, …) is a natural cut-line for a fresh programme review.

**Cost:** typically weeks, often a small team. The breadth is the cost.
**Output:** structured findings register + recommendations + any executive summary needed for the engaging party (sponsor, customer, regulator).

### 2.3 · Pen-test / red-team engagement (active probing)

A reviewer (or team) attacks a deployed PAIMOS instance — black-box, grey-box, or white-box — and reports what they got through. Examples:

- **Pre-deployment pen-test** for a serious adopter who needs a third-party report before going live with PAIMOS at scale.
- **Bug bounty** as a continuous-but-async version of the same loop. Not a programme that happens at points in time; a steady-state inbound channel for adversarial review.

**Cost:** highly variable. Pen-test engagements are typically week-scale; bug bounty is ongoing operational cost.
**Output:** vulnerability disclosures via [`SECURITY.md`](../SECURITY.md), handled per [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §3.4 (inbound vulnerability disclosure runbook).

---

## 3 · Triggers — when external review becomes worth pursuing

A solo-FOSS project doesn't engage external review continuously; the cost dominates the value at small scale. The triggers below are the points at which the value catches up.

| Trigger | Why it matters | Relevant review class |
|---|---|---|
| **A sponsor funds it.** A grant, a research collaboration, an adopter who pays for a third-party report. | Cost is the binding constraint today; funding lifts it. | targeted or programme |
| **PAIMOS reaches Phase 3 (commercial).** Per [`brand/BRAND.md`](brand/BRAND.md), Phase 3 is when commercial offerings come online. Commercial customers reasonably expect verifiable third-party validation. | external review becomes a *prerequisite* for the phase, not a nice-to-have | programme + pen-test |
| **A reference-deployment operator at scale demands it.** If a serious adopter (e.g., an enterprise running PAIMOS for hundreds of users) requires a third-party report, that's a market signal worth responding to. | adoption-blocker becomes adoption-driver after the review lands | targeted (operator pays) or pen-test |
| **A regulator demands it.** PAIMOS doesn't currently process GDPR-controller-class data in default deployments, so this isn't on the immediate horizon. If a future use-case puts a deployment under regulatory scope, the regulator is the trigger. | compliance becomes a hard requirement | programme + pen-test |
| **A material architectural change.** A substantial refactor (e.g., switching from SQLite to a different store; introducing a multi-tenant deployment mode) substantially changes the threat model — external review of the new posture is worth the cost. | the threat model isn't validated by the previous review anymore | targeted (scoped to the change) |

**What does NOT trigger external review:**

- Reaching a star count, fork count, or other vanity-metric threshold.
- Calendar-driven "we should probably do one" without a specific reason.
- Maintainer's discomfort with their own code (that's fixed by code review with peers, not by paying for a stranger's read).

The principle: the trigger is a real driver, not a self-soothing exercise.

---

## 4 · Lightweight alternatives in use today

The trust-doc set was assembled without external review. The substitutes that get us most of the way:

| Substitute | What it gives us | Limit |
|---|---|---|
| **Maintainer self-review against the THREAT_MODEL invariants** | every PR touching `backend/auth/`, `backend/handlers/`, `backend/db/`, `backend/handlers/ai_*` walks the [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md) §4 review checklist before merge | catches what the maintainer is competent to look for; doesn't catch unknown-unknowns |
| **Captured tabletops** | the three captured tabletops in this trust-doc set (incident response, continuity, DR) force walking specific scenarios end-to-end | tabletops surface gaps in *runbooks*, not gaps in *threat-model coverage* |
| **CI scanner pipeline** ([`SECURITY_REVIEW.md`](SECURITY_REVIEW.md)) | gitleaks (blocking) + npm audit (blocking) + govulncheck (blocking) + gosec baseline gate (blocking on new findings; burn-down tracked by PAI-223) cover the SAST / dep / secret matrix | scanners catch known-pattern defects, not novel ones |
| **Reference-deployment operator (pmo)** | bytepoets running pmo independently is the closest thing to external review the project has — every deploy that succeeds without intervention validates the runbook for someone who didn't write it | not adversarial; doesn't probe for vulnerabilities |
| **Community review via GitHub** | every PR merged is visible; every commit is greppable; every release is signed; the project's bug-tracking is public on PAIMOS itself | informal; depends on people choosing to look |
| **Public CycloneDX SBOMs + cosign signatures** | every release is independently verifiable to the source; supply-chain provenance is a real artefact | provides verifiability, doesn't itself find defects |

These substitutes are real and named in [`paimos.com/trust.html`](https://paimos.com/trust.html). They're not a replacement for external review; they're the trust-floor while external review is out of reach.

---

## 5 · How a future external review would flow

When the first external review eventually happens, the integration-into-the-trust-doc-set is the same flow as any other findings source:

```
external review engagement
   ↓
review delivered as written findings document
   ↓
each finding triaged into one of:
   · [confirmed defect] → fix + ticket per INCIDENT_RESPONSE §3.4
                          (or a new ticket if pre-disclosure)
   · [accepted risk] → annotated in THREAT_MODEL.md §5 out-of-scope
                       with the explicit reasoning
   · [recommendation] → tracked as a backlog ticket; review at
                        next 6-month governance review
   · [false positive] → noted in this doc's §6 maintenance log
                        (with reasoning)
   ↓
findings + remediation summary appended to:
   · this document's §6 maintenance log
   · SECURITY_GOVERNANCE.md §6 maintenance log (next 6-month entry)
   · paimos.com/trust.html §05 limits (update from "No third-party
     security review yet" to "First third-party review on <date>;
     findings summary at …" and link)
   ↓
high-severity findings ship per SECURITY.md disclosure timeline
   (≥ 7 days after patched release for operators to update; then
   public advisory)
```

The discipline: **the runbook delta and the code fix ship together** (per [`SECURITY_GOVERNANCE.md`](SECURITY_GOVERNANCE.md) §3.1). External review findings are no different — the threat-model delta, the runbook delta, and the code fix land in the same PR.

### Pre-engagement preparation

Before engaging a reviewer, the project provides:

- **Read-first artefact:** this document + [`THREAT_MODEL.md`](THREAT_MODEL.md). The reviewer's first hour should be on the threat model so they understand what we *think* must be true.
- **Architecture orientation:** [`docs/DEVELOPER_GUIDE.md`](DEVELOPER_GUIDE.md) (repo layout + conventions) + [`docs/DATA_MODEL.md`](DATA_MODEL.md) (schema reference) + [`backend/main.go`](../backend/main.go) (route table).
- **Validation evidence:** [`HARDENING.md`](HARDENING.md), [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md), [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md), [`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md). The reviewer probes the substrate against the documented checks.
- **Open gaps register:** [`SECURITY_GOVERNANCE.md`](SECURITY_GOVERNANCE.md) §6 maintenance log + the open-gaps register in [`THREAT_MODEL.md`](THREAT_MODEL.md) §6. The reviewer doesn't need to discover what we already know is open.
- **Test deployment access:** depending on engagement scope, either ppm-clone (forensic copy of production data) or a fresh dev environment with synthetic data.
- **A concrete scope statement** — e.g., "review the auth flow per THREAT_MODEL §4.1 against backend/auth/" rather than "look at PAIMOS for security issues."

The pre-engagement checklist is one PR-sized piece of work the project can do at any time before a reviewer is engaged, then update at engagement time.

---

## 6 · Maintenance log

Append-only. Each entry is either an external review event (started / completed) or the maintainer's six-monthly note that no external review has occurred and the alternatives in §4 are still the substrate.

### 2026-04-26 — initial entry, no external review yet

The trust-doc set was assembled internally. No external review has occurred. The §4 lightweight alternatives are the substrate. **The honest framing on [`paimos.com/trust.html`](https://paimos.com/trust.html) §05 limits** ("No third-party security review yet") is current and correct.

Next review: **2026-10-26** (per [`SECURITY_GOVERNANCE.md`](SECURITY_GOVERNANCE.md) unified calendar).

---

## 7 · Cross-references

- **[`SECURITY_GOVERNANCE.md`](SECURITY_GOVERNANCE.md)** — the operating loop this document fits into; §1 names this document's review as a recurring control.
- **[`THREAT_MODEL.md`](THREAT_MODEL.md)** — the model an external review would validate against. §5 out-of-scope is where accepted-risk findings land.
- **[`SECURITY_REVIEW.md`](SECURITY_REVIEW.md)** — the build-side scanner pipeline that's an alternative to external review for a subset of defect classes.
- **[`HARDENING.md`](HARDENING.md)** — operator-side hardening checklist; one of the artefacts a reviewer would use as input.
- **[`BACKUP_RESTORE.md`](BACKUP_RESTORE.md)** + **[`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md)** + **[`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md)** — captured drills + tabletops + production findings the reviewer would consume.
- **[`SECURITY.md`](../SECURITY.md)** — disclosure policy; the inbound path for findings even when no formal review is in flight.
- **[`paimos.com/trust.html`](https://paimos.com/trust.html)** §05 limits — the public statement aligned with §1 here.
- **[`brand/BRAND.md`](brand/BRAND.md)** — phasing plan; Phase 3 transition is one of the §3 triggers.
- **[`PAI-139`](https://github.com/markus-barta/paimos/issues/139)** — this ticket; the framework lands; the engagement awaits the right window.
