# PAIMOS — Security Review Rules & Scanner Posture

**Owner:** the maintainer (single-person operation as of v2.0).
**Companion docs:** [`THREAT_MODEL.md`](THREAT_MODEL.md) (the invariants this guard-rails), [`HARDENING.md`](HARDENING.md) (the deployment-side checks), [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) (when defence fails).
**Audience:** the maintainer, future maintainers, code reviewers, external auditors.
**Status:** v1 — review every six months and after any material CI / scanner change. Next: **2026-10-26**.

---

## 0 · Purpose

This document is the **agreed posture** for PAIMOS's secure-SDLC pipeline:

- Which scanners run in CI, what each one defends against, and at what severity threshold each blocks the build.
- The honest current state — including which scanners have known finding piles in triage, why, and the ticket tracking the fix.
- The code-review rules a security-sensitive change goes through before merge.

It is **not**:

- A claim that PAIMOS is bug-free at the SAST or vulnerability layer. Both `gosec` and `govulncheck` currently produce findings (see §3) — those are tracked, not ignored.
- A pen-test report. External review is tracked under [`PAI-139`](https://github.com/markus-barta/paimos/issues/139).
- A SOC-2 control mapping. PAIMOS doesn't claim certification.

---

## 1 · The four scanners

PAIMOS's CI runs four security scanners against every push and PR. Each tool has a distinct role; together they cover the SAST / dependency-vulnerability / secret-scanning / supply-chain matrix the AC of [`PAI-128`](https://github.com/markus-barta/paimos/issues/128) named.

| Scanner | Role | Configured in | Status | Threshold |
|---|---|---|---|---|
| **gitleaks** | Secret scanning (history-aware) | `.gitleaks.toml` + `ci.yml` | **blocking** | any finding fails build; doc-example allowlist for `paimos_<≤16-hex>` in `*.md` |
| **npm audit** | Frontend dependency vulnerability | `ci.yml` (`frontend/` step) | **blocking** | `--audit-level=high`; production deps only (`--omit=dev`) |
| **gosec** | Go SAST (taint analysis, common patterns) | `ci.yml` + `.gosec-baseline.txt` | **blocking baseline gate** | severity=medium / confidence=medium; current findings are baselined, new findings fail CI |
| **govulncheck** | Go module + stdlib vulnerability | `ci.yml` (`backend/` step) | **blocking** | any reachable vulnerability reported by the CI Go toolchain fails CI |

All four scanners are release-blocking in CI. `gitleaks`, `npm audit`, and
`govulncheck` fail directly on findings at their configured threshold.
`gosec` fails through a committed baseline gate: the current residual set is
tracked in `.gosec-baseline.txt`, and any new medium+ severity / medium+
confidence finding is a CI failure. Shrinking that baseline remains PAI-223's
job; growing it requires explicit review.

---

## 2 · Per-scanner config and rationale

### 2.1 · gitleaks — secret scanning

**Why blocking from day one.** The `INCIDENT_RESPONSE.md § 3.1` tabletop named "leaked PAIMOS API key in a public PR" as the most-likely incident class. Catching that pattern at PR-time is the single highest-value secret-scan check the project has. The CI cost is minutes; the alternative (catching it via a third-party report after the leak hits a public mirror) is a Sev 1 incident.

**Config** lives in `.gitleaks.toml` at the repo root. Two key parts:

- **Custom rule** `paimos-api-key`: regex `paimos_[a-fA-F0-9]{32,}`. Matches real PAIMOS keys (64-char hex). Real keys are emitted by `POST /api/auth/api-keys` and are never expected in tracked files; if they appear, gitleaks blocks the build.
- **Documentation allowlist**: short `paimos_<≤16-hex>` strings used as documentation examples (e.g., `paimos_1a2b3c4` in `docs/AGENT_INTEGRATION.md`) are allowlisted because the bound is well below real-key length (32+) so the two ranges cannot collide.

CI step uses `gitleaks/gitleaks-action@v2` with `fetch-depth: 0` so the scanner sees full git history — without that, only the most recent commit is scanned and the point of secret scanning is defeated.

**Rotating the rules:** if a future PAIMOS feature introduces a new secret format (e.g., a `paimos_session_<…>` token), the rule is extended in `.gitleaks.toml` as a NEW custom rule, not by relaxing the existing one. The principle: rules tighten over time, never loosen.

### 2.2 · npm audit — frontend dependency vulnerabilities

**Why blocking, why `--audit-level=high`.** The frontend is a Vue 3 SPA bundled at deploy time; runtime exposure of a vulnerable npm package is direct (the bundled JS ships to every user's browser). High-severity vulnerabilities should fail the build; lower-severity ones bias toward false-positive noise and would teach reviewers to ignore the signal.

**Config**:

```yaml
- name: npm audit (frontend production deps)
  working-directory: frontend
  run: |
    npm ci
    npm audit --omit=dev --audit-level=high
```

`--omit=dev` excludes dev-only packages (Vitest, vue-tsc, etc.) which never ship to production browsers. `--audit-level=high` is the agreed threshold: medium-severity findings are advisory and surface in `npm audit` output without failing the build.

**When to bump to `--audit-level=moderate`:** if the project adds a security review programme (PAI-139) that closes the moderate-severity false-positive loop, the threshold tightens. Until then, `high` is the working trade-off.

### 2.3 · gosec — Go SAST

**Blocking shape.** A clean-room run on the v2.0 codebase produced **118 findings** at `severity=medium / confidence=medium`. Distribution:

- **38 high-severity findings** — mostly G115 (uint→int overflow in tree-sitter integration), G704 (SSRF taint in `jiraimport.go` and `auth/oidc.go`, where the *feature* is making outbound HTTP to operator-configured URLs — gosec's taint analysis flags the design intent), G701 (SQL injection taint in FTS5 query construction in `search.go`, where the queries are parameterised but gosec doesn't recognise SQLite FTS5's MATCH syntax).
- **80 medium-severity findings** — G306 (file-permissions 0o644 vs 0o600 expected), various G404 (insecure rand for non-crypto purposes), etc.

Most are false positives; a non-trivial subset are real edge cases that warrant either a `// #nosec G104` annotation with a justification comment, or a small refactor. **Triaging the residual set is multi-day work** that PAI-223 tracks. Until then, `scripts/check-gosec-baseline.sh` runs gosec and compares normalized findings against `.gosec-baseline.txt`; CI fails if the set grows.

**Config**:

```yaml
- name: gosec (Go SAST baseline gate)
  run: |
    go install github.com/securego/gosec/v2/cmd/gosec@latest
    ./scripts/check-gosec-baseline.sh
```

**The triage plan (PAI-223):**

1. Walk every finding; classify as false-positive (annotate), real-edge-case (fix or accept with comment), or out-of-scope (move out of `./...`).
2. Remove resolved findings from `.gosec-baseline.txt`.
3. Keep CI blocking on any new finding beyond the baseline.
4. Review the baseline every six months.

### 2.4 · govulncheck — Go module + stdlib vulnerabilities

**Blocking shape.** CI runs `govulncheck ./...` with the configured Go toolchain and fails on reachable vulnerabilities. If a local developer runs with an older or unpatched Go patch release, their local scan may fail even when CI is green; upgrade the local Go toolchain rather than weakening the gate.

**Config**:

```yaml
- name: govulncheck (Go vulnerability scan)
  working-directory: backend
  run: |
    go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...
```

Any newly-disclosed Go-runtime or module CVE that PAIMOS actually calls fails the build until the affected dependency is upgraded or the call path is removed.

---

## 3 · Agreed thresholds (the honest current state)

| Tool | Today | Goal | Tracked under |
|---|---|---|---|
| gitleaks | blocking · any finding | unchanged | — (this ticket) |
| npm audit | blocking · `--audit-level=high` | unchanged for now; tighten to `moderate` only after a moderate-finding triage cycle | future |
| gosec | blocking baseline gate · severity=medium / confidence=medium | shrink baseline as findings are fixed/annotated | **PAI-223** (filed) |
| govulncheck | blocking · reachable vulnerabilities | keep Go toolchain patched | **PAI-224** (historical upgrade) |

**This is the agreed threshold per the PAI-128 / PAI-288 AC.** "Blocking at the agreed severity threshold" = the table above. Any future advisory-only scanner must be named advisory in CI and in this document; otherwise scanner steps are assumed to be release-blocking.

---

## 4 · Security-sensitive code-review rules

When a PR touches code in any of the surfaces below, the reviewer (today: the maintainer) walks the matching review checklist before approving. The bar is *more thoughtful*, not *more bureaucratic* — the goal is to catch defects that the scanners' false-positive rates make them miss, not to add review-cycle ceremony.

The surfaces correspond to the [`THREAT_MODEL.md` § 4](THREAT_MODEL.md) invariant groups.

### 4.1 · Auth-touching changes

PRs touching `backend/auth/`, `backend/handlers/users.go`, `backend/handlers/api_keys.go`, or any session / TOTP / OIDC / password-reset path:

- [ ] Does this change preserve **INV-AUTH-01** through **INV-AUTH-08**? Walk the threat-model row by row.
- [ ] Are passwords still bcrypt-hashed (no plaintext storage anywhere new)?
- [ ] Are API keys still sha256-hashed at rest, returned plaintext only on create?
- [ ] If session lifetime changed: is `sessions.expires_at` consistently consulted?
- [ ] If rate-limits changed: do `auth/ratelimit.go` invariants still hold?
- [ ] Is timing-attack resistance (`subtle.ConstantTimeCompare`) preserved on every secret comparison?

### 4.2 · Authz-touching changes

PRs touching `backend/auth/middleware*.go`, `backend/auth/access.go`, or any `Require*` middleware consumer:

- [ ] Does the route either declare `auth.RequireAdmin` / `auth.RequireProjectView` / `auth.RequireProjectEdit` / `auth.RequireIssueAccess` / equivalent, OR have an explicit comment justifying public exposure?
- [ ] Does the response shape preserve the **404-on-no-view, 403-on-view-only-when-edit** convention? (No existence oracle.)
- [ ] Does `authz_fuzz_test.go` (PAI-127) cover the new role × endpoint pair? If not, add it.
- [ ] If the change widens what `admin` can do: is the broader scope intentional (admins SHOULD bypass per-project) and reviewed?

### 4.3 · File-handling changes

PRs touching `backend/handlers/attachments.go`, `backend/handlers/documents.go`, `backend/handlers/imageutil.go`, or `backend/handlers/branding.go`:

- [ ] Does the upload path validate MIME by magic bytes, not just by client-reported `Content-Type` (**INV-FILES-04**)?
- [ ] Does the download path enforce scope-aware authorization before streaming bytes (**INV-FILES-02**)?
- [ ] Are non-image types served with `Content-Disposition: attachment`? (**INV-FILES-03** — note: PAI-110 is the application-layer fix; reverse-proxy mitigation is interim per `HARDENING.md` § 3.4.)
- [ ] Does the change introduce any new file-output path that should match SQLite's 0o600 expectation rather than 0o644?

### 4.4 · Audit-touching changes

PRs touching `backend/handlers/ai_action.go`, `backend/handlers/ai_optimize.go`, `backend/auth/session_audit.go`, `backend/auth/retention.go`, or any `audit:` line emitter:

- [ ] Does the audit invariant (**INV-AUDIT-02**: no prompt or response body content in `audit:` lines) still hold? If a new field is added, is it metadata only?
- [ ] Does `ai_optimize_audit_test.go` (PAI-153) still pass after the change? If not, fix the regression — don't relax the test.
- [ ] If a new mutation is added: does it record exactly one audit row per attempt regardless of outcome (**INV-AUDIT-01**)?
- [ ] If retention behaviour changed: does the sweeper in `auth/retention.go` cover the new row class with a documented `PAIMOS_RETENTION_DAYS_*` knob?

### 4.5 · Export / delete changes

PRs touching `backend/handlers/gdpr.go` or any soft-delete / hard-delete / restore handler:

- [ ] Does GDPR export return JSON for every row class referencing the user (**INV-EXPORT-01**)?
- [ ] Does GDPR erase replace PII with placeholders rather than cascade-delete historical project data (**INV-EXPORT-02**)?
- [ ] Does the change preserve the **hard-delete is irreversible** posture (**INV-EXPORT-04**)? UI affordance gating must remain admin-only.
- [ ] Does soft-delete continue to allow key resolution (`ResolveIssueRef`) for restore/purge operations even though list/search exclude soft-deleted items (**INV-EXPORT-03**)?

### 4.6 · Provider-integration changes

PRs touching `backend/ai/`, `backend/handlers/ai_settings.go`, `backend/handlers/integrations.go`, `backend/auth/oidc.go`, or any external HTTP-out path:

- [ ] Does the credential remain admin-set / env-var-sourced, never client-supplied?
- [ ] Are credentials returned `has_*: bool` only in API responses (never the secret itself; **INV-PROV-01**)?
- [ ] Does the failure mode degrade gracefully (UI shows unconfigured-state; download/email returns 503 / refused; AI hidden) rather than 500-storming?
- [ ] If a new outbound HTTP target is introduced: is the URL admin-set, not user-set? (Mitigates SSRF beyond the gosec G704 false-positive set.)

### 4.7 · Migration changes

PRs touching `backend/db/db.go`:

- [ ] Is the migration **additive-only** (no destructive schema changes mid-version)?
- [ ] Is it idempotent (`CREATE TABLE IF NOT EXISTS`, `ALTER TABLE IF NOT EXISTS …`)?
- [ ] Does it bump the version counter (`schema_versions` row) atomically with the schema change?
- [ ] If the migration backfills data: does it run in batches that survive interruption?
- [ ] Does the schema regression test in `backend/db/schema_regression_test.go` cover the new table or column?

---

## 5 · When something gets through

The scanners and review checklist are defence in depth, not perfect. When something does get through:

1. The disclosure path is `security@paimos.com` per [`SECURITY.md`](../SECURITY.md).
2. The internal handling per-incident-class is in [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) § 3.
3. The post-incident review template (per [`INCIDENT_RESPONSE.md` § 5](INCIDENT_RESPONSE.md#5--post-incident-review-template)) names the scanner / review-rule delta the incident exposed. **The fix and the rule update ship in the same PR.**
4. Where the incident exposes a missing scanner or threshold, this document is updated in that same PR.

---

## 6 · Cross-references

- **[`THREAT_MODEL.md`](THREAT_MODEL.md)** — the invariants this guard-rails. § 4 invariant groups map 1:1 to § 4 review-rule groups here.
- **[`HARDENING.md`](HARDENING.md)** — the deployment-side checks (TLS / secrets / backups / audit egress).
- **[`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md)** — when defence fails. § 3.1 (compromised API key) is the tabletop that named the gitleaks `paimos_` rule as the targeted defence.
- **[`SECURITY.md`](../SECURITY.md)** — inbound disclosure policy.
- **`.gitleaks.toml`** — the gitleaks config (custom `paimos-api-key` rule + doc allowlist).
- **`.github/workflows/ci.yml`** — the four scanner steps.
- **`backend/handlers/security_regression_test.go`** + **`backend/handlers/authz_fuzz_test.go`** + **`backend/handlers/ai_optimize_audit_test.go`** — the regression suites that back the review rules.
- **PAI-223** — gosec triage follow-on (118 findings → annotate / fix / baseline).
- **PAI-224** — Go runtime upgrade follow-on (1.25.5 → 1.25.7 to clear the govulncheck stdlib pile).
- **[`SECURITY_GOVERNANCE.md`](SECURITY_GOVERNANCE.md)** — the operating system for this doc's review cadence; §1 names "gosec re-baseline" as a recurring control once PAI-223 lands.
