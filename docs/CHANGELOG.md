# Changelog

All notable changes to PAIMOS are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and PAIMOS adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] — 2026-04-19

### Initial release

PAIMOS v1.0.0 is the first public release. It provides a self-hosted
project management system with first-class support for tracking both
humans and AI agents as participants.

**Core features**

- Hierarchical issues (epic → ticket → task) with parent-child relations
- Rich issue metadata: status, priority, assignee, cost unit, release,
  dependencies, impacts, tags, attachments, comments, history
- Sprints, accruals, cost units, releases
- Per-user time tracking with inline timers and billing lifecycle
- Full-text search with partial issue-key matching
- Custom views (saved filter + column sets), per-user view ordering
- Admin panel: users, projects, tags, integrations
- External-user portal with read-only projects + acceptance workflow
- PDF delivery reports (Lieferbericht) with configurable column layout

**Security**

- Session cookies (HttpOnly, SameSite=Lax, optional Secure)
- TOTP 2FA with QR setup, reset, disable
- API keys (`paimos_` prefix, sha256-hashed storage)
- Magic-link password reset with 60-minute token TTL
- Rate-limited auth endpoints (login / forgot / reset / totp-verify)

**Integrations**

- Jira import (project list, field mapping, issue relations)
- Mite time-tracking import (DE/AT; user mapping via note field)
- CSV import/export (per-project and cross-project)

**Branding & configuration**

- **Live-editable branding** via admin-only **Settings → Branding** tab:
  edit product name, tagline, website, page title, full colour palette,
  upload custom logo + favicon — all applied live with no restart.
- Admin API: `PUT /api/branding`, `POST /api/branding/logo`,
  `POST /api/branding/favicon`. Public `GET /brand/<filename>` for
  pre-auth login-page assets (SVG served with restrictive CSP).
- Runtime branding config at `$DATA_DIR/branding.json`, plus
  `branding-<slug>.json` for multi-brand installs.
- Operator-configurable identity via `BRAND_*` env vars (product name,
  company, website, TOTP issuer, email from, API key prefix, DB filename,
  MinIO bucket — see `docs/CONFIGURATION.md`).
- Optional MinIO-backed attachments (graceful-disable when unset).
- Optional SMTP for password-reset emails (dev mode logs links to stdout).

### Provenance

PAIMOS is forked from an internal prototype and published under AGPL-3.0.
All upstream branding, CI, and deployment infrastructure have been removed.
Git history starts fresh with this release.
