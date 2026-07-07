# PAIMOS — Threat Model and Security Invariants

**Owner:** the maintainer (single-person operation as of v2.0).
**Companion docs:** [`SECURITY.md`](../SECURITY.md), [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md), [`CONTINUITY.md`](CONTINUITY.md), [`CONFIGURATION.md`](CONFIGURATION.md), [`DEVELOPER_GUIDE.md`](DEVELOPER_GUIDE.md).
**Status:** v1 — covers the v2.0 surface. Reviewed every six months and after any material architecture change.

---

## 0 · Purpose & scope

This document is the **maintained mental model** of what must always remain true about PAIMOS, so future maintainers (and external reviewers) can evaluate the system against explicit design assumptions instead of reconstructing them from source.

It is **not**:

- A penetration-test report. PAIMOS hasn't had a formal external pen-test yet — that's tracked under [`PAI-139`](https://github.com/markus-barta/paimos/issues/139) and named explicitly in `paimos.com/trust.html` § limits.
- A compliance attestation. PAIMOS aims for NIS2 / GDPR alignment (per `claim-matrix.md`) but does not claim audited certification.
- An exhaustive enumeration of every conceivable attack. The threats below are the ones the project deliberately defends against; less-likely / out-of-scope threats are named in §5.

The bar to clear: a senior security engineer reading this document plus the linked code paths walks away with (a) a complete trust-boundary picture, (b) a checklist of invariants the project commits to, and (c) the verification path for each — testable, auditable, not aspirational.

---

## 1 · Architecture overview

PAIMOS is a single Go binary that serves both a JSON HTTP API and a built Vue 3 SPA from one port. SQLite is the only required data store; everything else is an optional integration that degrades gracefully when absent.

```
┌─────────────────────────────────────────────────────────────────────┐
│  Browser (Vue 3 SPA, served from /app/static)                        │
│   · session cookie  (HttpOnly, SameSite=Lax, Secure when configured) │
│   · CSRF token cookie  (non-HttpOnly, paired with X-CSRF-Token)      │
│   · API key clients use Bearer tokens; bypass CSRF                   │
└──────────────────────┬──────────────────────────────────────────────┘
                       │ HTTPS  (TLS terminated at reverse proxy / Caddy)
                       ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Go server  :8888  (chi router, single process)                      │
│   ├── auth  (sessions · API keys · TOTP · password reset · OIDC)     │
│   ├── handlers  (issues · projects · customers · attachments · …)    │
│   ├── middleware  (CSRF · per-project access · admin gates)          │
│   └── audit  (stdout · incident_log · ai_calls · mutation_log*)      │
└──┬───────────┬───────────┬───────────┬───────────┬──────────────────┘
   │           │           │           │           │
   ▼           ▼           ▼           ▼           ▼
┌─────┐  ┌─────────┐  ┌────────┐  ┌──────────┐  ┌────────────┐
│SQLite│  │ MinIO/  │  │ SMTP   │  │ OIDC IdP │  │ OpenRouter │
│ WAL  │  │  S3     │  │ (opt)  │  │ (opt)    │  │ (opt; AI)  │
│      │  │ (opt;   │  │        │  │          │  │            │
│      │  │ attach) │  │        │  │          │  │            │
└─────┘  └─────────┘  └────────┘  └──────────┘  └────────────┘
```

**Required:** Go binary + SQLite + filesystem under `$DATA_DIR`.
**Optional:** MinIO/S3 (`MINIO_ENDPOINT`), SMTP (`SMTP_HOST`), OIDC (`OIDC_ISSUER_URL`), OpenRouter (admin-config in `ai_settings`).

Each optional dependency adds an outbound trust assumption; §2.4 enumerates them.

---

## 2 · Trust boundaries

A trust boundary is any place where data crosses from a trusted context into a less-trusted one (or vice versa). Each boundary has a defence: a check, a transformation, an explicit refusal.

### 2.1 · Network boundary — browser ↔ server

- **Threat surface:** anyone with network access to the public TLS endpoint. Includes anonymous users, authenticated users via stolen cookies, tampering proxies.
- **Defences:**
  - HTTPS at the reverse proxy (operator-controlled; PAIMOS sets `Secure` cookie flag when `COOKIE_SECURE=true`).
  - Session cookies `HttpOnly`, `SameSite=Lax`. JavaScript cannot read them.
  - CSRF via per-session token (M72 `csrf_token` column) + `Origin`/`Referer` validation + `X-CSRF-Token` header on every cookie-authed mutation. API-key clients bypass CSRF (Bearer auth is not CSRF-vulnerable). The `csrf_token` cookie is non-HttpOnly (SPA reads it from JS) but `Secure`+`SameSite=Strict` and shares the session cookie's lifetime — including the 90-day absolute cap — so a browser restart cannot strand the user with a missing token (PAI-370). If the cookie is absent on an authenticated request, the middleware re-issues it from the existing DB token without rotating, so already-broken sessions heal without forcing a re-login. [`auth/csrf.go`](https://github.com/markus-barta/paimos/blob/main/backend/auth/csrf.go), middleware in `auth/middleware.go`.
  - Rate-limited login / forgot / reset / TOTP-verify (5 attempts / 10 min / IP+identity).
  - Global security headers: `nosniff`, `X-Frame-Options=SAMEORIGIN`, `Referrer-Policy`, `Permissions-Policy`, conditional HSTS, CSP-Report-Only with self-only (PAI-114).

### 2.2 · Process boundary — PAIMOS ↔ host OS

- **Threat surface:** adversary with shell access to the PAIMOS host (lateral movement, container escape, malicious operator).
- **Defences:**
  - PAIMOS does not require root; the Dockerfile drops to a non-root user.
  - Filesystem access is scoped to `$DATA_DIR`; the binary doesn't read or write outside it.
  - Operator secrets live in env vars or in field-level encrypted SQLite
    columns. User-entered provider secrets such as the OpenRouter API key go
    through `backend/secretvault`; operators who keep the master key on the
    data volume should still treat `$DATA_DIR` as sensitive.
  - No SUID, no capabilities, no host-namespace privileges.

### 2.3 · Data boundary — `$DATA_DIR` ↔ other tenants

- **Threat surface:** multi-tenant host where `$DATA_DIR` could be reachable from another container / process.
- **Defences:**
  - SQLite WAL + `busy_timeout` prevents concurrent-write corruption from another process.
  - Foreign-key constraints prevent dangling references.
  - PAIMOS does not assume process-level isolation; if the host is multi-tenant, the operator is responsible for filesystem isolation (per [`CONFIGURATION.md`](CONFIGURATION.md) operational guidance).

### 2.4 · Provider boundaries

Each optional integration adds an outbound trust assumption. PAIMOS does not — and cannot — verify the upstream provider's security posture; it commits to safe handling of credentials and clean degradation if the provider is unreachable or compromised.

| Provider | Credential | Failure mode |
|---|---|---|
| MinIO/S3 | `MINIO_ACCESS_KEY` + `MINIO_SECRET_KEY` (env) | Attachments unavailable; UI hides drop zones; download endpoints return 503 |
| SMTP | `SMTP_USER` + `SMTP_PASS` (env) | Password-reset endpoint refuses-with-warning; no link sent |
| OIDC | `OIDC_CLIENT_SECRET` (env) | SSO button hidden from login page when unconfigured |
| OpenRouter | `ai_settings.api_key_encrypted` (DB, secretvault) | AI feature surface disabled; UI falls back to "AI not configured" |

A compromised upstream provider can in theory exfiltrate data PAIMOS sent — see [`INCIDENT_RESPONSE.md` § 3.5](INCIDENT_RESPONSE.md) for the response runbook. PAIMOS-side defences:

- Audit lines record every AI call (action, user, model, outcome, tokens, latency) but **never the prompt or response body** (PAI-153 invariant — see §4.4).
- Attachment uploads are scoped per-issue; a compromised MinIO bucket exposes attachments but not core PAIMOS data.

### 2.5 · User boundaries

- **Roles** — `admin`, `member`, `external` — gate admin-only operations (project CRUD, user CRUD, retention sweeper config).
- **Per-project access levels** — `none`, `viewer`, `editor` — gate read/write per project. Stored in `project_members` (PAI-103 / `auth/access.go`).
- **Self vs. other** — uploader-ownership on pending attachments (PAI-112); a non-admin can only link their *own* pending attachments, not someone else's id-guessable upload.
- **External role** — restricted to portal endpoints; redirected away from internal routes; portal endpoints have their own access checks.

Diagrammatically:

```
                      ┌─────────────┐
                      │   admin     │  ← bypasses per-project; can do CRUD
                      └──────┬──────┘
                             │
            ┌────────────────┼─────────────────┐
            │                │                 │
       ┌────▼─────┐   ┌──────▼─────┐    ┌──────▼─────┐
       │ member   │   │ member     │    │ external   │
       │ (default │   │ (project   │    │ (portal-   │
       │  editor) │   │  viewer    │    │  only,     │
       │          │   │  override) │    │  per-proj  │
       └──────────┘   └────────────┘    │  granted)  │
                                        └────────────┘
```

### 2.6 · Local agent broker boundary — `paimos serve`

`paimos serve` is a CLI-side helper, not a public server feature. It
runs on a developer workstation or agent host and bridges three trust
zones: the local repository checkout, the local PAIMOS CLI config, and
the agent runtime consuming loopback HTTP or MCP stdio.

Defences:

- HTTP mode binds to loopback-only by default and rejects non-loopback
  clients in middleware. Non-loopback bind requires the explicit
  `--unsafe-allow-remote` operator flag.
- The broker exposes read-only tools only. It never writes files, runs
  package managers, executes arbitrary commands, or forwards mutation
  endpoints.
- Local file reads resolve symlinks and reject traversal or symlink
  escape outside the bound repo root.
- Generated and secret-prone paths (`.git`, `node_modules`, `dist`,
  `.env`, private keys, credential files) are blocked; returned text is
  passed through coarse secret redaction.
- Search and read operations use fixed command shapes (`git`, `rg`) with
  bounded output, timeouts, and no shell interpolation.
- Repo-derived payloads are labelled `untrusted_data` so clients can
  separate retrieved facts from instructions.
- Audit lines go to stderr and redact/truncate parameters before logging.

### 2.7 · Remote-triggered local execution boundary — `paimos run-agent`

The "Implement this" feature (PAI-605) crosses the sharpest boundary in the
system: a click in the *web UI* causes code to *execute on a developer's
workstation*. `paimos run-agent watch` subscribes to a project's event stream
and, on an `implement_requested` event, spawns a coding agent (Claude Code) in a
local repo checkout — and, when explicitly enabled, can run a deploy. This is the
inverse of the read-only `serve` broker (§2.6): it writes files, runs commands,
and may ship to production.

Defences:

- **Opt-in, per-workstation.** Nothing runs unless the developer started
  `paimos run-agent watch` themselves. The server never initiates a connection
  to a workstation; the workstation dials out and holds the SSE stream.
- **Capability is advertised, not assigned.** A runner sets `?implement=1` to
  mark itself implement-capable; the registry (`GET /projects/{id}/runners`) only
  lists runners that opted in AND are currently connected (live-presence
  intersection, not a stale timestamp).
- **Repo-scoped.** A runner only ever operates in its configured `--repo-root`.
  The triggering event names a run, never a path or command.
- **Consent-gated.** Each job prompts for confirmation before spawning unless the
  operator passed `--yes`. The spawned command is the operator's own `--exec`
  (default `claude`), never anything the server supplies.
- **One job at a time.** A busy runner refuses new jobs rather than spawning
  concurrent agents in the same checkout.
- **Report-back only by default; deploy is triple-gated.** The runner never
  deploys unless the operator passed BOTH `--allow-deploy` and a `--deploy-exec`
  command AND the run carries a `deploy_target`. Absent any of the three it can
  only move a run to `tests_passed` / `failed`.
- **Authorized + audited.** `POST /implement` is project-editor gated; run reads
  and updates are requester, project-editor, or admin only. Every request is
  recorded in the HTTP-level session audit (`session_activity`, §4.4). Note: the
  run lifecycle and the auto-posted report comment use direct writes, so — unlike
  ordinary issue/comment writes — they do **not** enter the field-level
  `mutation_log`/undo trail; they are reviewable via the `agent_runs` record and
  the comment, not undoable.

The trust assumption is explicit: a developer who runs `paimos run-agent watch`
delegates "run my coding agent (and optionally my deploy) when I click the
button" to their own PAIMOS account. A compromised account can therefore trigger
work on any workstation that account has a live runner on — which is precisely
why deploy stays off by default and behind two further flags.

Draft Implement-this providers (`openrouter_draft.implement` and
`local_model_draft.implement`) deliberately do **not** cross this local
execution boundary. They are server-side model calls that can post an internal
draft comment and store safe provenance on `agent_runs`, but they reject
`device_id` and `deploy_target`, do not claim a runner, do not mutate a repo,
and do not claim local test execution. Local-model endpoint labels strip
userinfo, query strings, and fragments before display.

---

## 3 · Threat actors

| Actor | Capability | Primary goal |
|---|---|---|
| **Anonymous external attacker** | Network access to TLS endpoint; no credentials | Probe for unauthenticated endpoints; brute-force login; CSRF against authenticated sessions; recon via error-shape differences |
| **Authenticated low-privilege user** (member/external) | Valid session or API key | Privilege escalation; access projects they shouldn't see; modify others' data; exfiltrate cross-project data |
| **Compromised authenticated user** | Stolen session cookie or API key | Whatever the compromised account could do; persistence (create new keys, modify TOTP) |
| **Compromised admin** | Stolen admin credentials | Project CRUD, user CRUD, secret rotation, audit-log tampering attempts; persistence at the org level |
| **Insider threat (legitimate admin)** | Authorised access; acting maliciously | Modify audit log to hide actions; extract sensitive customer data; create backdoor accounts |
| **Supply-chain attacker** | Compromised npm / Go module / Docker base image | Inject malicious code at build time; harvest credentials at runtime; backdoor releases |
| **Physical / host attacker** | Filesystem access to `$DATA_DIR` (lost laptop, compromised host) | Read SQLite directly, bypassing app-layer authz |

PAIMOS commits explicit defences against actors 1–4. Actor 5 (insider) is partially mitigated (audit log is append-only at the SQL layer; sessions table records who-did-what); a determined insider with DB write access can edit history. Actors 6–7 are partially out of scope — see §5.

---

## 4 · Security invariants

The numbering uses the convention `INV-<DOMAIN>-<NN>`. Each invariant has:

- **Statement** — what must be true
- **Code path** — where enforced
- **Verification** — how validated (test file, regression case, manual check)
- **Owner** — currently the maintainer for all (solo-maintained); the role rather than the person

A gap (no test, manual-only verification, etc.) is named explicitly. Gaps drive backlog tickets, not silent acceptance.

### 4.1 · Authentication

| ID | Statement | Code path | Verification |
|---|---|---|---|
| **INV-AUTH-01** | Passwords are stored as bcrypt hashes, never plaintext. | `auth/password.go:HashPassword` (bcrypt cost 12) | `auth/password_test.go` round-trips hash + verify; `quick_test.go` smoke |
| **INV-AUTH-02** | Sessions expire after `expires_at`; expired sessions do not authenticate. | `auth/middleware.go:CheckSession`; `sessions` table has `expires_at` | `session_audit_test.go` |
| **INV-AUTH-03** | API keys are stored as sha256 hashes; the plaintext key is shown once on create and never retrievable. | `auth/api_keys.go` | `quick_test.go`; documented in [`SECURITY.md`](../SECURITY.md) |
| **INV-AUTH-04** | Login / forgot / reset / TOTP-verify endpoints are rate-limited (5 attempts / 10 min / IP+identity). | `auth/ratelimit.go` | manual verification; **gap**: no automated rate-limit regression test |
| **INV-AUTH-05** | TOTP secrets are per-user; admin reset rotates the secret, does not expose it. | `auth/totp.go` | `quick_test.go`; manual smoke on admin-reset flow |
| **INV-AUTH-06** | Password-reset tokens are 32-byte random, sha256-stored, single-use, 60-minute TTL. | `auth/password_reset.go` | `password_reset_test.go` |
| **INV-AUTH-07** | Password reset invalidates all active sessions for that user (defence in depth). | `auth/password_reset.go:Reset` | `password_reset_test.go` |
| **INV-AUTH-08** | OIDC `email_verified` claim must be true for JIT provisioning; users with unverified email are refused. | `auth/oidc.go` | manual verification with mocked IdP; **gap**: no integration test |

### 4.2 · Authorization

| ID | Statement | Code path | Verification |
|---|---|---|---|
| **INV-AUTHZ-01** | Admin-only routes refuse non-admin callers (e.g., user CRUD, retention config, integration setup). | `auth/middleware.go:RequireAdmin` | `authz_fuzz_test.go` (PAI-127) covers role × endpoint matrix |
| **INV-AUTHZ-02** | Per-project view access is enforced at the route layer; 404 on no-view (no existence oracle). | `auth/middleware_project.go:RequireProjectView` | `authz_fuzz_test.go`; explicit cross-project test fixtures |
| **INV-AUTHZ-03** | Per-project edit access is enforced at the route layer; 403 when view-only. | `auth/middleware_project.go:RequireProjectEdit` | `authz_fuzz_test.go` |
| **INV-AUTHZ-04** | A non-admin user cannot link a pending attachment uploaded by a different user (PAI-112). | `handlers/attachments.go:LinkPending` | `security_regression_test.go` covers the hijack path |
| **INV-AUTHZ-05** | Admin role bypasses per-project checks (effectively editor everywhere) but does NOT bypass auth (admin still needs valid session/key). | `auth/access.go:CanView/CanEdit` | `authz_fuzz_test.go` |
| **INV-AUTHZ-06** | External-role users are redirected away from internal routes; portal endpoints enforce per-portal-project access. | `auth/middleware.go` route-meta `portal` flag | `portal_test.go` |
| **INV-AUTHZ-07** | Document download enforces scope-aware authorization: project-scoped requires project view; customer-scoped requires admin OR view of a project belonging to that customer (PAI-111). | `handlers/documents.go:Download` | `security_regression_test.go` |

### 4.3 · Files & uploads

| ID | Statement | Code path | Verification |
|---|---|---|---|
| **INV-FILES-01** | Attachment uploads are scoped to a single issue; cross-issue access requires explicit re-link. | `handlers/attachments.go` | `quick_test.go` |
| **INV-FILES-02** | Attachment downloads check authorization (scope-aware per INV-AUTHZ-07) before streaming bytes. | `handlers/attachments.go:Download`; `handlers/documents.go:Download` | `security_regression_test.go` |
| **INV-FILES-03** | File-serving sets `Content-Disposition: attachment` for non-image types so a user-uploaded `.html` does not render in the browser. | `handlers/attachment_safety.go`; `handlers/attachments.go:UploadAttachment/Download` | `attachment_safety_test.go`; PAI-110 shipped |
| **INV-FILES-04** | MIME type is validated server-side by magic bytes for images, not only by client-reported `Content-Type`. | `handlers/imageutil.go` | `quick_test.go` covers image upload happy path; **gap**: no negative-case test for spoofed MIME |
| **INV-FILES-05** | Uploaded images are re-encoded server-side (re-compression strips embedded scripts in SVG-as-PNG style attacks). | `handlers/imageutil.go:NormalizeImage` | manual verification; partial regression in `quick_test.go` |
| **INV-FILES-06** | Branding asset uploads (logo, favicon) check size + format; SVGs are served with restrictive CSP. | `handlers/branding.go` | `branding_test.go` |

PAI-110 shipped the **INV-FILES-03** application-layer fix end-to-end. Uploads now reject browser-executable content (HTML, SVG, JavaScript, executable types) using declared type, magic-byte sniffing, and payload-shape checks; the serve path re-sniffs stored bytes and forces anything outside the inline allowlist (PNG, JPEG, GIF, WebP, PDF) to download with a restrictive CSP.

### 4.4 · Audit

| ID | Statement | Code path | Verification |
|---|---|---|---|
| **INV-AUDIT-01** | AI action calls emit one stdout audit line per call (`audit: ai_action ...`) regardless of outcome — line count = attempt count. | `handlers/ai_action.go:auditAction` | `ai_optimize_audit_test.go` enforces |
| **INV-AUDIT-02** | AI audit lines never contain prompt or response body content — metadata only (action, user, model, tokens, latency, outcome). | `handlers/ai_action.go:auditAction` | `ai_optimize_audit_test.go` walks every code path that writes an audit line and asserts no body fields are interpolated |
| **INV-AUDIT-03** | Session-mutation audit (`X-PAIMOS-Session-Id`) is on by default; one row per mutation in `session_activity`. | `auth/session_audit.go` | `session_audit_test.go`; tunable via `PAIMOS_AUDIT_SESSIONS` |
| **INV-AUDIT-04** | Incident log (`incident_log`, M73) is admin-only CRUD; status transitions auto-stamp `resolved_at`. | `handlers/incidents.go` | manual verification; **gap**: dedicated regression test is a follow-on |
| **INV-AUDIT-05** | AI usage table (`ai_usage`, M77) records per-user per-day token totals; never logs prompt / response body. | `handlers/ai_action.go:RecordUsage` | `ai_optimize_audit_test.go` extension |
| **INV-AUDIT-06** | The retention sweeper (24h loop) prunes audit rows older than the configured window per class — sessions, reset tokens, access audit, session activity, closed incidents, pending TOTP. | `auth/retention.go` | manual verification; **gap**: time-warp regression test is a follow-on |

### 4.5 · Export & delete

| ID | Statement | Code path | Verification |
|---|---|---|---|
| **INV-EXPORT-01** | `GET /api/users/{id}/gdpr-export` is admin-only; returns full per-subject JSON dump of every row referencing the user. | `handlers/gdpr.go:Export` | manual verification; **gap**: regression test is a follow-on |
| **INV-EXPORT-02** | `POST /api/users/{id}/gdpr-erase` is admin-only; replaces PII with placeholders, drops sessions/keys, sets `status='deleted'`. Does NOT cascade-delete historical project data — preserves audit-log integrity. | `handlers/gdpr.go:Erase` | manual verification; **gap**: regression test |
| **INV-EXPORT-03** | Soft-deleted issues are accessible via key resolution but excluded from list/search results until restored. | `handlers/issues.go:ResolveIssueRef` | `quick_test.go` |
| **INV-EXPORT-04** | Hard-delete (purge) is final and irreversible — no undo path exists. The future `mutation_log` (PAI-211) records hard-deletes as audit-only entries with `undoable=false`. | `handlers/issues.go:Purge` (admin-only); referenced from PAI-209 design | manual verification + UI affordance gating |

### 4.6 · Provider integration

| ID | Statement | Code path | Verification |
|---|---|---|---|
| **INV-PROV-01** | OpenRouter API key is admin-set, encrypted at rest through `secretvault`, and never returned in API responses (the GET endpoint returns `has_api_key: bool` only). | `handlers/ai_settings.go` | `ai_test_connection_test.go` |
| **INV-PROV-02** | OIDC client secret is env-var only; never written to logs. | `auth/oidc.go` | manual verification |
| **INV-PROV-03** | SMTP password is env-var only; never written to logs. | `mail/smtp.go` | manual verification |
| **INV-PROV-04** | Provider-rejection responses (e.g., "model not found") are surfaced to the SPA but the underlying provider error class is captured in the audit line, not the body. | `handlers/ai_action.go` | `ai_optimize_audit_test.go` |

### 4.7 · Local broker

| ID | Statement | Code path | Verification |
|---|---|---|---|
| **INV-BROKER-01** | `paimos serve` HTTP mode is loopback-only by default and rejects non-loopback remote clients. | `cmd/paimos/cmd_serve.go:isLoopbackListenAddr`, `localOnly` | `cmd_serve_test.go` |
| **INV-BROKER-02** | Local broker file reads cannot traverse outside the repo root or follow symlinks outside it. | `cmd/paimos/cmd_serve.go:resolveRepoPath` | `cmd_serve_test.go` |
| **INV-BROKER-03** | Local broker reads block obvious secret files and redact common token/password shapes in returned content. | `cmd/paimos/cmd_serve.go:denyUnsafeRepoRel`, `redactSensitiveTextWithFlag` | `cmd_serve_test.go` |
| **INV-BROKER-04** | MCP stdio exposes the same read-only broker methods as HTTP mode; stdout carries JSON-RPC only. | `cmd/paimos/cmd_serve.go:serveMCP` | `cmd_serve_test.go` |

### 4.8 · Remote-triggered execution (PAI-605)

| ID | Statement | Code path | Verification |
|---|---|---|---|
| **INV-RUNNER-01** | A workstation runs implement jobs only when a developer started `paimos run-agent watch`; the server never dials a workstation. | `cmd/paimos/cmd_run_agent.go:runAgentWatch` | `cmd_run_agent_test.go` |
| **INV-RUNNER-02** | The runner is repo-scoped, runs one job at a time, and prompts before spawning unless `--yes`. | `cmd_run_agent.go:agentRunner.handle` + busy guard | `cmd_run_agent_test.go` |
| **INV-RUNNER-03** | Deploy is off by default — it requires `--allow-deploy` AND `--deploy-exec` AND a run-level `deploy_target`. | `cmd_run_agent.go:agentRunner.handle` | `cmd_run_agent_test.go` |
| **INV-RUNNER-04** | `POST /implement` is project-editor gated; run reads/updates are requester or admin only. | `handlers/agent_runs.go:canManageAgentRun` + `main.go` routes | `agent_runs_test.go` |
| **INV-RUNNER-05** | Draft Implement-this providers cannot use local runner, repo mutation, test, or deploy paths, and local endpoint labels do not display credentials. | `handlers/agent_runs.go:implementDraftIssue`, `handlers/ai_execution_options.go:safeEndpointLabel` | `agent_runs_test.go` |

---

## 5 · Out of scope

The following are deliberately **not** defended against by PAIMOS today:

- **Self-inflicted misconfiguration.** Running PAIMOS without `COOKIE_SECURE=true` over HTTPS, exposing the binary on a public IP without a reverse proxy, granting admin to anyone who asks. PAIMOS provides safe defaults; operators who choose otherwise own the consequence.
- **Volumetric DoS.** Rate limiting is best-effort; large-scale layer-4 / layer-7 floods are upstream-network territory.
- **Physical attacker with disk access.** A `$DATA_DIR` reader can read most SQLite columns directly — PAIMOS doesn't full-DB-encrypt; operators wanting that must layer on encrypted storage (LUKS, eCryptfs, etc.). Field-level exception (PAI-261): user-entered secrets (CRM provider tokens, `ai_settings.api_key`, future webhook secrets) ARE encrypted at rest under per-domain HKDF-derived AES-256-GCM keys via `backend/secretvault`. Under Tier 2 deployment ([`HARDENING.md` §3.6](HARDENING.md#36--secrets-management)) the master key lives in the operator's secret manager (env var, never on the data volume), so a stolen backup tarball or volume snapshot cannot decrypt those fields. Under Tier 1 (default for dev / single-node), the master key sits next to the ciphertext on the same volume — protects against application-layer leaks and casual peeks, but not against backup theft.
- **Compromised reverse proxy / TLS terminator.** PAIMOS trusts whatever forwards it via HTTP. Hardening the reverse proxy is operator scope.
- **Side-channel attacks on bcrypt / sha256.** Timing-attack-resistant comparison is used (`subtle.ConstantTimeCompare`), but attacker-with-cycle-counter scenarios are out of scope.
- **Supply-chain attacks on Go / npm dependencies.** PAIMOS publishes CycloneDX SBOMs (PAI-121) so operators can audit; PAIMOS itself does not run a vetting pipeline beyond `gosec` + `govulncheck` + `npm audit` in CI. PAI-128 tracks the secret-scanning + blocking-severity follow-up.
- **Insider threat at admin level.** A determined admin can edit audit logs in SQLite directly. The session-mutation audit (INV-AUDIT-03) makes this *visible* but not *prevented*. Append-only audit logs would require an external sink (SIEM); PAI-124 / PAI-131 frames this as future work.
- **Regulator notification flows.** PAIMOS doesn't hold GDPR-controller-class data in default deployments. If your deployment does, consult counsel — this is out of solo-maintainer scope.

These are tracked in [`claim-matrix.md`](claim-matrix.md) where they intersect a public claim.

---

## 6 · Maintenance

Review and update this document:

- Every six months on a fixed calendar reminder (next: **2026-10-26**).
- After any material architecture change: new entity boundary, new optional integration, new auth provider, new role, new endpoint family.
- After every Sev 0 / Sev 1 incident — the post-incident review (per [`INCIDENT_RESPONSE.md` §5](INCIDENT_RESPONSE.md#5--post-incident-review-template)) names runbook deltas; if it also names threat-model deltas, this document is updated in the same PR that ships the fix.

### Adding a new invariant

1. Decide which §4.x table the invariant belongs in.
2. Pick the next free `INV-<DOMAIN>-NN` id.
3. Write the statement (one sentence, present tense, declarative).
4. Identify the code path that enforces it. If no such path exists, the invariant is *aspirational* — name it as a gap and file a ticket to close the gap.
5. Identify the verification path (test file or manual procedure). Same: if none exists, name it as a gap.

### Retiring an invariant

Invariants are retired when the underlying capability is removed (e.g., if PAIMOS dropped TOTP, INV-AUTH-05 would retire). **Don't retire an invariant because it's hard to enforce** — that's a defect, not a model change. File a ticket; keep the invariant.

### Open gaps tracked

| Gap | Tracked in |
|---|---|
| INV-AUTH-04 — no automated rate-limit regression test | follow-on under PAI-126 |
| INV-AUTH-08 — no OIDC integration test with mocked IdP | follow-on |
| INV-FILES-03 — active-content upload hardening | closed by PAI-110; freshness guard tracked by PAI-551 |
| INV-FILES-04 — no spoofed-MIME negative-case test | follow-on under PAI-126 |
| INV-AUDIT-04 — no regression test for incident_log status transitions | follow-on |
| INV-AUDIT-06 — no time-warp test for retention sweeper | follow-on |
| INV-EXPORT-01 / 02 — no regression test for GDPR export / erase | follow-on |
| External pen-test programme | **PAI-139** (open) |
| Append-only audit log via external SIEM sink | future, framed by PAI-131 |

These are honest gaps in the regression layer, not unenforced invariants. The code paths exist; the regression-test layer is incomplete. Each "follow-on" item is a small ticket worth filing as the regression suite matures (PAI-126 is the umbrella).

---

## 7 · Cross-references

- **[`SECURITY.md`](../SECURITY.md)** — disclosure policy.
- **[`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md)** — incident severity, runbooks, post-incident review template; runbook deltas land here when they expose threat-model deltas.
- **[`CONTINUITY.md`](CONTINUITY.md)** — bus-factor / continuity plan; the threat model assumes one maintainer, the continuity plan covers the maintainer being out.
- **[`CONFIGURATION.md`](CONFIGURATION.md)** — every env var, including audit + retention controls (`PAIMOS_AUDIT_SESSIONS`, `PAIMOS_RETENTION_DAYS_*`).
- **[`DEVELOPER_GUIDE.md`](DEVELOPER_GUIDE.md)** — architecture overview, repo layout, contribution patterns. §4a (access model) is the developer-facing companion to §2.5.
- **[`claim-matrix.md`](claim-matrix.md)** — claim ↔ shipped-evidence registry; checked at release time.
- **[`2.0_AUDIT.md`](2.0_AUDIT.md)** — programme-scope audit + decisions log; D-001 through D-005 frame the architectural constraints this threat model is built on.
- **[`paimos.com/trust.html`](https://paimos.com/trust.html)** — public outward-facing trust posture; §05 limits aligns with this document's §5 out-of-scope.
- **[`HARDENING.md`](HARDENING.md)** — operator-facing companion to this document. Where this threat model says *what must be true*, the hardening guide says *how to make it true* in a deployment, with explicit verification commands per checklist item.
- **[`SECURITY_REVIEW.md`](SECURITY_REVIEW.md)** — agreed scanner posture (gitleaks, npm audit, gosec, govulncheck) + the security-sensitive code-review rules per invariant group. The review-rule §4 there mirrors the §4 invariant groups here 1:1.
- **[`REFERENCE_DEPLOYMENTS.md`](REFERENCE_DEPLOYMENTS.md)** — production-validation register; the §3 findings table is where this threat model's invariants get tested in earnest, against real workloads.
- **[`SECURITY_GOVERNANCE.md`](SECURITY_GOVERNANCE.md)** — the operating system for the trust-doc set: recurring controls, metrics, governance loop, unified calendar. Tells you *when* to revisit this doc; this doc tells you *what's in it*.
- **`backend/handlers/security_regression_test.go`** — the canonical regression suite for the security defects PAI-110-118 fixed; new invariants should add tests here.
- **`backend/handlers/authz_fuzz_test.go`** — authorization fuzzer (PAI-127); new role × endpoint pairs should land here.
- **`backend/handlers/ai_optimize_audit_test.go`** — audit-shape regression (PAI-153); the no-bodies invariant lives here.
