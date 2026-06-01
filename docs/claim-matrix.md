# PAIMOS — Claim / Evidence Matrix

This file is the source of truth that ties every claim made on
`paimos.com` (especially under `/03 / specs`) back to the shipped
implementation in this repo. PAI-123 introduced it as a release gate:
the file is checked at release time (`scripts/check-claims.sh`) and a
release will refuse to cut if any claim is stuck on `aspirational`
without an open follow-on ticket.

## Status legend

- **shipped** — the claim is supportable from the current `main` build.
- **partial** — most of the claim is supportable; specific gaps are
  listed and have an open ticket.
- **aspirational** — not yet supportable. Either the website wording
  must be narrowed (PAI-122 style) or an implementation ticket must be
  open and referenced here.

## Matrix

| Claim (paimos.com / 03-specs) | Status | Evidence | Open tickets |
|---|---|---|---|
| NIS2 ready audit trails, access control, incident logging | partial → shipped | session audit default-on (PAI-116), access control via `project_members`, incident_log table + admin CRUD + JSON/CSV export | PAI-124 (full SIEM-export pipeline) |
| GDPR-compliant data minimization, export + deletion primitives, EU-hostable | shipped | retention sweeper + `/api/users/{id}/gdpr-export` + `/api/users/{id}/gdpr-erase` (PAI-117); EU hosting is operator-controlled | — |
| Self-hostable single docker compose; run on your own tin; no SaaS dependency | shipped | `docker-compose.yml`, `Dockerfile`, no SaaS calls in app code; runtime fonts removed (PAI-118) | — |
| Enterprise grade SSO, RBAC, audit logs, air-gap deployment | partial | OIDC SSO end-to-end (PAI-120), RBAC via project_members + roles, audit (PAI-116), runtime third-party requests removed (PAI-118). SAML is not shipped. | PAI-124 (SAML, optional) |
| Zero tracking: no analytics, no 3rd-party JS, no telemetry | shipped | no analytics/telemetry libs; fonts bundled via `@fontsource` (PAI-118); CSP-Report-Only is fully self-only (PAI-114) | — |
| Open API: REST + OpenAPI spec; scriptable from day one | shipped | `/api/openapi.json` (PAI-119), `/api/schema` (PAI-87), `paimos` CLI | — |
| SBOM · CycloneDX manifest of every dependency, published with each release | shipped | CI tag-push generates `backend.sbom.json` + `frontend.sbom.json`, signs the image keylessly via cosign + GitHub OIDC, attaches each SBOM as a cosign attestation (PAI-121); `just sbom` for local generation | — |
| Code-aware agents · structured project facts agents can read (linked repos, knowledge, canonical agent artifacts, issue-to-file anchors, mixed-context retrieval) | shipped | `/api/projects/{id}/{repos,knowledge,anchors,graph,retrieve}` + `/api/projects/{id}/agents/{name}.json` + `/api/issues/{id}/anchors` documented in `docs/AGENT_INTEGRATION.md` §1a and `docs/api-minimal.md` § Agent Context (PAI-29 / PAI-30, contract-promoted in v2.0.0; manifest retired in PAI-358) | — |
| Built-in AI assist · in-app prose optimize, translate, spec-out, suggest-enhancement, sub-task generation, customer-facing report summaries; admin-tunable, audit-clean | shipped | 13 actions registered via `POST /api/ai/action` dispatcher (adds `customer_rewrite` + `exec_summary` in v3.5.0 / PAI-418); admin-tunable prompts via `/api/ai/prompts` CRUD; daily token cap (`PAIMOS_AI_DAILY_CAP_TOKENS`); audit invariant — bodies never logged (PAI-146 / PAI-159 → PAI-183 / PAI-418, see `docs/CONFIGURATION.md` § AI assist) | — |
| Local AI roadmap; designed to run alongside Ollama / LM Studio / vLLM / llama.cpp, not in today's build | shipped (rolled back) | paimos.com `/03 / specs` badge re-worded per `docs/website-claims-rollback.md` (PAI-122) — public copy now matches what the audited build ships | — |

## Where the security defects from the 2026-04-24 audit landed

Cross-reference for the audit's findings. None of these are website
claims; they are the shipped-defect side of the same epic and need to
stay closed for the matrix above to remain credible.

- PAI-110 (Critical, postponed) — active-content upload hardening.
  Carry into the next phase; postponed for compatibility.
- PAI-111 — scope-aware authz on `/api/documents/{id}/download`.
- PAI-112 — uploader ownership on pending attachment link.
- PAI-113 — per-session CSRF token + middleware + frontend wire-up.
- PAI-114 — global security headers (nosniff, X-Frame-Options=SAMEORIGIN,
  Referrer-Policy, Permissions-Policy, conditional HSTS, CSP-Report-Only).
- PAI-115 — password-reset link logging gated on `PAIMOS_DEV_MODE=true`.
- PAI-116 — session audit default-on + incident_log table.
- PAI-117 — retention sweeper + per-subject export/erase.

## Updating this file

The release gate refuses to ship if a row is `aspirational` without an
open ticket reference, OR if the audit notes a finding that has no row
above. When closing a follow-on ticket, move the row's status forward
and trim its "open tickets" column.
