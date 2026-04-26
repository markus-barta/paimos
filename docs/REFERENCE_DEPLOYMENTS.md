# PAIMOS — Reference Deployments & Production Validation

**Owner:** the maintainer (single-person operation as of v2.0).
**Companion docs:** [`HARDENING.md`](HARDENING.md) (deployment hardening checklist), [`DEPLOY.md`](DEPLOY.md) (release + rollback runbook), [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md) (DR proof pack), [`THREAT_MODEL.md`](THREAT_MODEL.md), [`2.0_AUDIT.md`](2.0_AUDIT.md).
**Audience:** prospective adopters, external auditors, future maintainers.
**Status:** v1 — review every six months. Next: **2026-10-26**.

---

## 0 · Purpose & scope

This document is the **production-validation register** for PAIMOS. It says:

- What counts as a reference deployment (criteria, not vibes).
- Which real deployments meet those criteria today.
- The structured findings that real operation has surfaced.
- Which findings drove which product or documentation update.

The bar matters because solo-maintainer FOSS projects are everywhere; **production-validated** solo-maintainer FOSS projects are rare. This register is the difference between "I built a thing" and "I built a thing and have run it in earnest for long enough to learn from it."

It is **not**:

- A claim that PAIMOS is enterprise-validated. The reference set is two deployments (one is the maintainer's, one is at a small consultancy). That's a real but small base.
- A list of every install. Self-hosters elsewhere are presumably running PAIMOS, but unless the operator submits structured findings the install isn't a *reference* — it's an unaudited install.
- A promise that adoption beyond the reference set is supported the same way. See [`SECURITY.md` § Supported versions](../SECURITY.md#supported-versions) for the actual support boundary.

---

## 1 · What counts as a reference deployment

Five criteria. A deployment becomes a reference when **all five** hold.

| # | Criterion | Why it matters |
|---|---|---|
| 1 | **Active operator** — someone is responsible for the deployment, monitors it, and would notice if it broke | Without an active operator, the install isn't being validated; it's just running. |
| 2 | **Multi-month uptime in production-like usage** — at least 90 days of real user activity, not test data | A 90-day window catches configuration drift, certificate renewals, log rotation, retention sweeps, and seasonal patterns that a one-week pilot misses. |
| 3 | **Real users with real data** — not synthetic; the operator would care if the data were lost | Synthetic data hides bugs that only appear with realistic content (long descriptions, attached files, complex relations, etc.). |
| 4 | **Survived at least one upgrade cycle** — operator has run `just deploy-{ppm,pmo} v<X.Y.Z>` from one minor version to the next | Upgrade is the highest-risk operation; surviving one is the strongest single signal that the runbook works. |
| 5 | **At least one backup / restore exercise** — real or drilled, captured in writing | Backups that have never been restored aren't backups; they're tarballs with hopeful filenames. |

A deployment that meets 4-of-5 (e.g., never upgraded yet) is a *candidate* reference, not a reference. The matrix in §3 below tracks each criterion per deployment.

---

## 2 · Active reference deployments

Two as of 2026-04-26. Both real, both production-like, both used to validate v2.0.

### 2.1 · ppm · `pm.barta.cm`

| Property | Value |
|---|---|
| Operator | the PAIMOS maintainer |
| Host | `csb1.barta.cm` (Hetzner-class, EU-region) |
| Storage | Docker named volume on encrypted FS |
| Reverse proxy | Caddy (TLS via Let's Encrypt) |
| Attachments | MinIO (separate bucket) |
| AI assist | OpenRouter (configured), `anthropic/claude-sonnet-4.5` model |
| OIDC | not configured (local password + TOTP) |
| Active since | v1.x (continuously upgraded; no fresh-install in current era) |
| Last upgrade | v2.0.4 (2026-04-26; earlier in this session) |
| Backup pattern | per-deploy via `scripts/deploy.sh`; `$BACKUP_ROOT` on the same host (acknowledged limitation tracked under [§3 Findings](#3--structured-findings) F-08) |
| Audience | the maintainer + a small group; canary for every release before pmo |

**Role in the project:** ppm is the **first production deployment for every release**. The release flow is `just release` → `just deploy-ppm` → wait → `just deploy-pmo`. ppm is where bugs in a release first show up if they do; pmo is the second deployment that validates it.

**This is the maintainer's own instance**, which is both a strength (real engagement, every defect is felt) and a weakness (operator and maintainer are the same person, so the validation isn't independent).

### 2.2 · pmo · `pm.bytepoets.com`

| Property | Value |
|---|---|
| Operator | bytepoets (Austrian software consultancy) |
| Host | bytepoets-operated |
| Storage | host bind-mount on encrypted FS |
| Reverse proxy | Caddy (TLS via Let's Encrypt) |
| Attachments | MinIO (separate bucket) |
| AI assist | OpenRouter (separately-configured key, separate billing) |
| OIDC | not configured |
| Active since | v1.x |
| Last upgrade | v2.0.4 (deployed alongside ppm) |
| Backup pattern | per-deploy + scheduled (operator-controlled cadence) |
| Audience | bytepoets internal; second canary; **operator is independent of maintainer** |

**Role in the project:** pmo is the **independence test**. Operator and maintainer are different people; the deploy / restore / upgrade runbooks have to be readable by someone who didn't write them. Configuration is documented in `scripts/deploy.pmo.conf` and the deployment flow is identical to ppm's. **If pmo upgrades cleanly without intervention, the runbook works.**

This is the deployment that converted PAIMOS from "the maintainer's project" to "a project that can be run by a second party" — and which therefore validates the bus-factor framing in [`CONTINUITY.md`](CONTINUITY.md).

---

## 3 · Structured findings

Findings logged in chronological order. Each finding has the **observation** (what was noticed in production), the **diagnosis** (what the root cause was), and the **action** (what changed in code or docs as a result). The action column is the one that turns "operating PAIMOS" into "improving PAIMOS".

| # | Date | Deployment | Observation | Diagnosis | Action | Tracked |
|---|---|---|---|---|---|---|
| **F-01** | 2026-04-25 | ppm | Empty "AI actions" menu after login on every text field | First catalogue load fired before login → 401 → cache permanently `actionsLoaded=true` with empty array | Failed loads no longer flip `actionsLoaded`; AiActionMenu retries on mount when catalogue empty | v1.10.2 / PAI-181 |
| **F-02** | 2026-04-25 | ppm | "AI optimization failed:" red banner sticky with empty message | `v-if="aiOptimize.lastError"` checked the Vue Ref object (always truthy), not its value; nested ref access skipped auto-unwrap | Destructured `lastError`/`clearError` to top-level setup bindings | v1.9.1 |
| **F-03** | 2026-04-26 | ppm | CHANGELOG entries for v2.0.2 and v2.0.3 shipped with the auto-generated `### Added — TODO fill in before committing` placeholder | `release.sh` opens `$EDITOR` for changelog cleanup; the editor step was bypassed without filling in | Filled in retroactively as part of the 2.0_AUDIT close-out; documented in 2.0_AUDIT.md decision D-004 (CHANGELOG ownership stays manual; release flow does not auto-generate descriptive entries from commit messages) | 2.0_AUDIT.md D-004 |
| **F-04** | 2026-04-26 | both | Brand asset divergence — `frontend/public/logo.svg` md5 `4f8491…` vs canonical `c26829…` from paimos-site | Asset frozen at v0.1.0 cutover while canonical evolved | Resync to canonical; `docs/brand/` is the in-repo source of truth | PAI-98 |
| **F-05** | 2026-04-26 | both | `docs/AGENT_INTEGRATION.md` line 54 contains `paimos_1a2b3c4` (a documentation example showing API-key prefix shape) — would trigger generic-api-key gitleaks rule | Doc example uses a realistic-looking prefix but isn't a real key | Custom `paimos-api-key` rule in `.gitleaks.toml` matches real keys (≥32 hex); doc-example allowlist matches `paimos_<≤16-hex>` in `*.md` (the two ranges cannot collide) | PAI-128 / `.gitleaks.toml` |
| **F-06** | 2026-04-26 | both | App header is partially covered by pinned sidebar; header doesn't reflow on narrow viewports | Header layout doesn't consume the sidebar's pinned-state flag; no responsive breakpoint for narrow widths | Frontend layout fix; cosmetic; tracked for a future polish window | PAI-225 |
| **F-07** | continuous | both | The deploy script's rollback one-liner is the only artefact a stressed maintainer should reach for; reconstructing it from memory mid-incident is error-prone | Runbook readability matters more than runbook completeness | `scripts/deploy.sh` ends every successful deploy with the *exact* rollback command for the host; `DEPLOY.md` § Rollback documents the same | DEPLOY.md (continuous) |
| **F-08** | continuous | ppm | `$BACKUP_ROOT` is on the same host as `$DATA_DIR` — a single host loss takes both | Off-host backup destination not configured for ppm | Documented as the operator-responsibility step in `HARDENING.md` § 3.7 (off-host minimum, off-site preferred); not yet remediated for ppm itself (acknowledged residual risk; tracked) | HARDENING.md § 3.7; future ppm config update |
| **F-09** | 2026-04-26 | both | Schema audit utility was missing — operators couldn't tell if a deployed image's expected schema actually matched the live DB | "I think it migrated correctly" is not enough at audit time | `backend/db/schema_audit.go` (small introspection helpers) + `schema_regression_test.go` (asserts expected table set, key columns, indexes after migration) | v2.0.1 / PAI-189 wave-1 |
| **F-10** | 2026-04-26 | both | The release → deploy → doc-sync gap surfaced repeatedly: README / docs / paimos-site / brand assets drift between code releases | Without an explicit reminder, the doc pass after release was easy to skip | `scripts/release-doc-sync.sh` + `just doc-sync` recipe + the four-command flow documented in DEPLOY.md | PAI-187 |
| **F-11** | 2026-04-26 | both | Test report visibility in production was implicit — a deploy could ship a green-CI version with no actual test reports surfaced in the admin UI | The product confused "no reports" with "ready" | Test-report runtime visibility hardening: explicit `ready` / `partial` / `missing_reports` states surfaced; admin-side bundle ingest path | v2.0.1 / PAI-188 |
| **F-12** | 2026-04-26 | both | The empty-AI-action-catalog F-01 fix was tested by hand but had no regression coverage — a future refactor could regress it silently | Bug-fix-without-test is debt | `ai_action_catalog_test.go` covers the catalogue assembly across registry / placement / admin override; CI regression layer covers it | v2.0.1 / PAI-189 wave-1 |

### What this list demonstrates

The findings above are not a catalogue of failures — they're the **shape of a real production-validation cycle**. Each row is "we ran the thing, we noticed something, we changed code or docs as a result." That cycle is what makes the reference deployments a reference, rather than just two installs.

The F-01 → F-02 → F-12 chain is particularly illustrative: an in-production defect (F-01: empty AI menu after login) prompted an immediate fix (v1.10.2), which exposed a related framework defect (F-02: the v-if-on-ref bug), which then drove the regression layer (F-12: catalog test) so the underlying class of defect can't recur silently. That's evidence-grounded production engineering, not aspirational claims.

---

## 4 · Validation matrix — per deployment × per workflow

Per workflow, has each reference deployment exercised it in a way that produced a finding worth keeping?

| Workflow | ppm | pmo |
|---|---|---|
| Fresh install from scratch | ❌ (continuously upgraded) | ✓ (initial install verified) |
| Image upgrade `just deploy-* <tag>` | ✓ (every release; ~30+ cycles) | ✓ (every release) |
| Rollback (image-pin only, per `DEPLOY.md` § Rollback) | ✓ (≥1 real rollback) | ✓ (≥1 real rollback) |
| Full DB restore from tarball | drilled (per `BACKUP_RESTORE.md` § 5) | drilled |
| Schema migration through a minor version | ✓ (M77, M78, M79 in v1.10.x; M80+ in v2.0.x) | ✓ (same) |
| GDPR export / erase | ❌ (not exercised against real users; capability shipped) | ❌ (same) |
| Incident response (Sev 1 or higher) | ❌ (none experienced; tabletop only — see `INCIDENT_RESPONSE.md` § 4) | ❌ (none experienced) |
| AI assist usage at scale | ✓ (active; visible in usage panel) | ✓ (active) |
| Branding customisation | ✓ (per-instance branding via Settings → Visual) | ✓ (per-instance branding) |
| Multi-user roles + permissions | ✓ (admin + member combinations) | ✓ |
| External-role / portal | ❌ (not exercised in production yet) | partial |
| OIDC SSO | ❌ (not configured) | ❌ (not configured) |
| Attachments via MinIO | ✓ | ✓ |
| Email password-reset (SMTP) | partial | ✓ |
| Cosign signature verification on a release pull | ✓ (verified during release process) | ✓ |
| SBOM attestation pull | ✓ (verified) | ✓ |

**Reading the matrix:** the green rows are workflows we have observational evidence for. The red rows are workflows whose code paths are tested in CI but haven't been exercised against real production data; that's an honest gap, not a defect. **External-role / portal**, **OIDC SSO**, and **incident response above tabletop** are the most prominent.

---

## 5 · What's not a reference deployment yet

Honest framing: the reference set is **two**. That's small but real. Expansion is a goal.

### What would make a third reference deployment valuable

Per §1 criteria, a candidate must:

- Have a non-maintainer, non-bytepoets operator (independence)
- Run for ≥ 90 days with real users
- Survive at least one upgrade
- Drill or experience at least one backup / restore
- Be willing to log structured findings into a successor of this register

The most useful third deployment would be one in **a different environment class** than the current two:

- **Air-gapped / on-prem** (validates the no-egress posture in earnest)
- **Larger team** (validates multi-user authz scaling; the current ppm + pmo are small-team)
- **Different OS / arch** (e.g., arm64 host) — validates cross-arch image builds
- **OIDC-configured** (validates the SSO path against a real IdP)
- **Heavy-attachment workload** (validates MinIO at scale)

Operators considering becoming a reference deployment should reach out via the disclosure / support channels in [`SECURITY.md`](../SECURITY.md). The bar is high but the value to the project is correspondingly high — this register is the difference between a maintainer's-personal-tool and a production-validated FOSS project.

### What's deliberately NOT a reference

- The maintainer's local-dev environment (`localhost:8888`). Not production-like, no real users, intentionally fragile.
- CI-spun ephemeral instances (Vitest test fixtures, integration test docker-compose).
- Forks of PAIMOS run by other parties without a feedback loop into this register.

---

## 6 · Maintenance

This register is reviewed every six months (next: **2026-10-26**) and after any of:

- A new deployment qualifying as a reference (criteria § 1, all five met)
- A major-version release (currently rare; v2.0 was the most recent)
- A structured finding that warrants logging (§ 3 row addition)
- An incident that produces a runbook delta (per [`INCIDENT_RESPONSE.md` § 5](INCIDENT_RESPONSE.md#5--post-incident-review-template))

The findings table in §3 is **append-only**. Resolved findings stay in the table with their action documented. Removing a row would be lossy — future-maintainer reading "was this ever an issue?" should see the answer in the table, not in commit history alone.

---

## 7 · Cross-references

- **[`HARDENING.md`](HARDENING.md)** — operator-side hardening checklist; the §3.7 backup posture rationale comes from F-08 in this document.
- **[`DEPLOY.md`](DEPLOY.md)** — release / deploy / rollback runbook; the F-07 finding and the runbook's "rollback one-liner is printed at deploy end" pattern are the same observation.
- **[`BACKUP_RESTORE.md`](BACKUP_RESTORE.md)** — the captured drill in § 5 is what backs the "drilled" entries in § 4 above.
- **[`THREAT_MODEL.md`](THREAT_MODEL.md)** — the security invariants this register's findings are tested against.
- **[`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md)** — the runbook tabletops (§ 4 there, § 7 in CONTINUITY) are what cover the red "incident response" rows in § 4 above pending real incidents.
- **[`CONTINUITY.md`](CONTINUITY.md)** — pmo's independence (§ 2.2 above) is what validates CONTINUITY's bus-factor framing.
- **[`2.0_AUDIT.md`](2.0_AUDIT.md)** — programme-scope decisions log; D-004 (CHANGELOG manual ownership) is F-03 in this register.
- **[`paimos.com/trust.html`](https://paimos.com/trust.html)** — the public trust posture; § 02 references this register's reference deployments.
