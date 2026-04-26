# PAIMOS — Configuration Reference

Every environment variable PAIMOS reads, grouped by concern. Defaults
shown in parentheses. Unless noted, all vars are optional.

## Core server

| Var | Default | Notes |
|---|---|---|
| `PORT` | `8888` | Listen port |
| `STATIC_DIR` | `/app/static` | Path to the built Vue SPA |
| `DATA_DIR` | `/app/data` | Path for SQLite DB, branding JSON, logos, avatars |
| `ADMIN_PASSWORD` | *(empty)* | **First-run only.** Seeds the `admin` user on a fresh DB. No effect once `admin` exists. |
| `COOKIE_SECURE` | *(unset)* | Set to `true` on HTTPS deployments to add `Secure` to session cookies |
| `INSTANCE_LABEL` | *(empty)* | Shows a banner in the sidebar (e.g. `STAGING`) — useful on non-prod instances |

## Branding

All are optional; defaults produce "PAIMOS" out of the box.

| Var | Default | Used for |
|---|---|---|
| `BRAND_PRODUCT_NAME` | `PAIMOS` | Startup log, email subject/body, default page title, TOTP issuer (unless overridden) |
| `BRAND_COMPANY_NAME` | *(empty)* | Appended to page title (`PAIMOS — ACME Corp`) and email footer when set |
| `BRAND_WEBSITE_URL` | `https://paimos.com` | Default `branding.json` website, email footer, password-reset URL fallback |
| `BRAND_PUBLIC_URL` | *(empty)* | Required for password-reset magic links. Falls back to `BRAND_WEBSITE_URL` if unset. No trailing slash. |
| `BRAND_EMAIL_FROM` | *(empty)* | `From:` header on outgoing emails. Falls back to `noreply@<host-of-BRAND_WEBSITE_URL>` when SMTP is configured but this is unset. |
| `BRAND_TOTP_ISSUER` | `BRAND_PRODUCT_NAME` | Shown by authenticator apps on TOTP enrollment |
| `BRAND_HEALTH_SERVICE_NAME` | lowercase `BRAND_PRODUCT_NAME` | `GET /api/health` → `{"service": "…"}` |
| `BRAND_PAGE_TITLE` | `BRAND_PRODUCT_NAME` [+ ` — ` + `BRAND_COMPANY_NAME`] | Shipped as the `pageTitle` in default branding |

### Set-once (changing after data exists has consequences)

| Var | Default | Caveat |
|---|---|---|
| `BRAND_API_KEY_PREFIX` | `paimos_` | Changing after keys are issued orphans the old keys — the prefix is stored verbatim and matched on auth |
| `BRAND_DB_FILENAME` | `paimos.db` | Change only on an empty `DATA_DIR`. No auto-migration. |
| `BRAND_MINIO_BUCKET` | `paimos-attachments` | Change before uploads begin. Existing objects won't follow. |

## Email (SMTP — optional)

PAIMOS only sends password-reset emails when `SMTP_HOST` is set. With
SMTP unconfigured the reset endpoint refuses to send and logs a
misconfiguration warning — your users will see "If an account with
that email exists, a reset link has been sent" but no link will reach
them. To run a true local-dev flow without an SMTP server, also set
`PAIMOS_DEV_MODE=true`; this prints the reset link to stdout so a
developer can paste it into the browser. Never set `PAIMOS_DEV_MODE`
in shared staging or production — the link is a one-shot password
reset and anyone with log access can use it (PAI-115).

| Var | Default | Notes |
|---|---|---|
| `SMTP_HOST` | *(unset)* | Unset = no email sent. Set to enable real sending. |
| `SMTP_PORT` | `587` | STARTTLS submission port |
| `SMTP_USER` | *(empty)* | Leave blank for unauthenticated relay |
| `SMTP_PASS` | *(empty)* | Pair with `SMTP_USER` |
| `PAIMOS_DEV_MODE` | *(unset)* | When `true` AND `SMTP_HOST` unset, log reset links to stdout. Local dev only. |

## Single Sign-On (OpenID Connect — PAI-120)

PAIMOS supports a single OIDC provider end-to-end with PKCE and JIT
user provisioning. The flow is hidden from the login page until all
three required vars are set; once configured, the SPA renders an
"SSO" button alongside the password form.

| Var | Default | Notes |
|---|---|---|
| `OIDC_ISSUER_URL` | *(unset)* | Required. e.g. `https://login.example.com` (no trailing slash). The discovery doc must be reachable at `${OIDC_ISSUER_URL}/.well-known/openid-configuration`. |
| `OIDC_CLIENT_ID` | *(unset)* | Required. |
| `OIDC_CLIENT_SECRET` | *(unset)* | Optional for public clients (PKCE-only); required for confidential clients. |
| `OIDC_REDIRECT_URL` | *(unset)* | Required. Must exactly match the IdP-registered redirect (e.g. `https://paimos.example.com/api/auth/oidc/callback`). |
| `OIDC_SCOPES` | `openid email profile` | Space-separated. |
| `OIDC_BUTTON_LABEL` | `Sign in with SSO` | Shown on the login page. |
| `OIDC_POST_LOGIN_REDIRECT` | `/` | SPA path to land on after a successful SSO login. |

JIT provisioning rules:
- A returning user is matched by case-insensitive email.
- A new user is created with role `member`, status `active`, no password,
  username derived from `preferred_username` (or the email local-part),
  with a random suffix on collision.
- An OIDC user with no verified email is refused — operators who run
  IdPs that omit `email_verified` should set the claim to `true` on the
  IdP side or the redirect lands on `/login?sso_error=email_required`.

The id_token signature is not verified locally; trust comes from the
TLS-protected userinfo round trip back to the issuer. This trade-off
keeps the dependency surface small. JWKS-based id_token verification
is a follow-on if a future deployment requires it.

## Audit & retention (PAI-116 / PAI-117)

The session-mutation audit is on by default for NIS2 readiness. Set
`PAIMOS_AUDIT_SESSIONS=false` (or `0`) to opt out — primarily useful in
sandbox or local-dev runs where the noise is unwanted. The retention
sweeper runs every 24 hours and trims rows older than the configured
window for each class. Tune any variable below; defaults are the
"careful operator" baseline, not regulator maxima.

| Var | Default | Notes |
|---|---|---|
| `PAIMOS_AUDIT_SESSIONS` | `true` | Set `false`/`0` to disable the session-mutation audit middleware. |
| `PAIMOS_RETENTION_DAYS_SESSIONS` | `30` | Sessions are also auto-expired by their own `expires_at`; this is the cleanup floor. |
| `PAIMOS_RETENTION_DAYS_RESET_TOKENS` | `7` | Password-reset tokens are single-use; this caps the audit trail. |
| `PAIMOS_RETENTION_DAYS_ACCESS_AUDIT` | `365` | Project membership-change audit log. |
| `PAIMOS_RETENTION_DAYS_SESSION_ACTIVITY` | `90` | Per-mutation session activity rows. |
| `PAIMOS_RETENTION_DAYS_INCIDENT_CLOSED` | `730` | Closed incidents only — open/investigating/resolved are kept until closed. |
| `PAIMOS_RETENTION_DAYS_TOTP_PENDING_MIN` | `60` | Pending TOTP tokens; minutes, not days. |

Per-subject GDPR endpoints (admin only):

- `GET  /api/users/{id}/gdpr-export` — JSON dump of every row referencing the user.
- `POST /api/users/{id}/gdpr-erase`  — replaces PII with placeholders, drops sessions/keys, sets `status='deleted'`.
- `GET  /api/gdpr/retention`         — current retention policy (introspection).

## AI assist (PAI-146 / PAI-159 → PAI-183)

The AI assist feature exposes a multi-action menu next to multiline
text fields and on issue-level surfaces (issue header, side panel).
**Off by default.** Configuration is in the database — admins set it
under **Settings → AI** and **Settings → AI prompts**, not via env
vars — so this section is reference, not tuning.

### Provider + model

`ai_settings` (M74, singleton row) holds:

- `enabled`, `provider` (only `openrouter` ships today; PAI-122
  reserves the field for local-model backends), `model` (the
  OpenRouter model slug — e.g. `anthropic/claude-sonnet-4.5`),
  `api_key`, `optimize_instruction` (admin-editable preface to the
  Optimize action's wrapper).

Set them from **Settings → AI**:
- **Test connection** runs a fixed-prompt smoke test against the
  unsaved form values, falling back to the saved key when the field
  is blank — admins don't have to re-paste the key just to verify.
  Audited under a separate `audit: ai_test ...` line.
- The **model picker** is fed live by `GET /api/ai/models`
  (server-cached 1h) showing top 4 models in six categories: Frontier,
  Value, Fastest, Cheapest, Open-weights, Free. Frontier picks are
  vendor-diverse (one model from each of Anthropic / OpenAI / xAI /
  Google). Manual model-id input stays always-visible.

### Actions

Each action is registered in code and surfaced via the
`POST /api/ai/action` dispatcher.

Built-in actions (11):
- `optimize`, `optimize_customer` — rewrite the field
- `suggest_enhancement` (sub-actions: security, performance, ux, dx,
  flow, risks)
- `spec_out` — description → AC checklist
- `find_parent` — top-3 plausible parents from the project tree
- `translate` (sub-actions: de_en, en_de)
- `generate_subtasks` — propose 3–7 child issues
- `estimate_effort` — hours + LP + reasoning
- `detect_duplicates` — top-5 similar issues in the project
- `ui_generation` — markdown UI spec
- `tone_check` — de-sales rewrite (customer surface)

### Placement (PAI-181)

Each action carries a `placement` field — `text`, `issue`, or
`both`:
- **text** — inline next to text fields (textareas)
- **issue** — in issue-level menus only (issue header, side-panel
  header, edit-mode toolbar)
- **both** — everywhere

Defaults: `optimize`, `suggest_enhancement`, `spec_out`, `translate`,
`ui_generation`, `tone_check`, `optimize_customer` → text.
`find_parent`, `generate_subtasks`, `estimate_effort`,
`detect_duplicates` → issue.

Admins override per-row in **Settings → AI prompts → Edit**.

### Prompt CRUD (PAI-175 → PAI-177)

`ai_prompts` (M78, with `placement` added in M79) is the admin-edited
prompt store. Built-in rows are seeded lazily from the action
registry on first list call, so admins see the actual default in the
editor (not an empty textarea). Action handlers read the live row at
request time via `resolveActionPrompt(key)` with constant-default
fallback — admin edits actually take effect.

Endpoints (admin-only, CSRF-protected):
- `GET /api/ai/prompts` — list
- `POST /api/ai/prompts` — create custom
- `PUT /api/ai/prompts/{id}` — update (built-in: prompt + enabled +
  placement; custom: all editable fields)
- `DELETE /api/ai/prompts/{id}` — delete (custom only)
- `POST /api/ai/prompts/{id}/reset` — reset built-in to current
  code default
- `POST /api/ai/prompts/{id}/dry-run` — render the template against
  a real issue and call the LLM; returns rendered prompt + response
  side-by-side. NO state changes.

Templates use Go `text/template` syntax. Surface-specific variables:
- Issue: `Title`, `Description`, `AcceptanceCriteria`, `Notes`,
  `Type`, `Status`, `IssueKey`, `ProjectName`, `ParentEpic`
- Customer: `CustomerName`, `Industry`, `Notes`, `CooperationType`,
  `SLADetails`, `CooperationNotes`

### Usage cap (PAI-161)

`ai_usage` (M77) tracks per-user per-day token spend. Default cap is
**100 000 tokens / user / day**, configurable via the env var
`PAIMOS_AI_DAILY_CAP_TOKENS`. Per-user override goes to
`users.ai_cap_override_tokens` (nullable INT; null = use default,
0 = disabled, positive = raised cap). Admins are exempt from the
soft block but get an `X-AI-Over-Cap: true` response header for UI
warning. Settings → AI surfaces the org-wide totals + per-user
table.

### Audit shape

One stdout audit line per call:

```
audit: ai_action action=optimize sub_action= user_id=42
       field=description issue_id=123
       model="anthropic/claude-sonnet-4.5" outcome=ok
       latency_ms=850 prompt_tokens=100 completion_tokens=50
```

Test-connection pings emit a separate `audit: ai_test ...` line
(fewer fields). The audit prefix moved from `ai_optimize` to
`ai_action action=<key>` in v1.10.0 — operators with grep patterns
on `ai_optimize` need a one-line update.

Outcome is a closed enum (one bucket per exit path):

- `ok` — provider returned a result (token counts populated)
- `fail_timeout` — handler-imposed deadline fired before the provider
  responded (raise the cap or pick a faster model)
- `fail_upstream` — provider replied with 4xx / 5xx or a structurally
  invalid body (transient: retry, or check provider status)
- `denied` — caller cannot view the target issue
- `unconfigured` — feature toggle off or settings incomplete
- `bad_request` — body decode failed, action not registered, field
  not in the allow-list, text empty / too large, or daily cap hit
- `provider_missing` — configured provider name not registered
- `cfg_load_fail` — settings row failed to load (DB error)
- `ctx_fail` — issue-context lookup failed (DB error, not access)
- `unauth` — unauthenticated (defensive; the route is auth-gated so
  this is unreachable in practice)

Every exit path emits exactly one line, so the line count equals
the attempt count regardless of outcome. Test-connection has its
own outcomes (`test_ok`, `test_fail`).

### What is NOT logged

**Prompt and response bodies are NEVER logged.** The audit line
carries metadata only. PAI-146 / PAI-153 explicitly forbid body
logging; a regression test in
`backend/handlers/ai_optimize_audit_test.go` (renamed to cover
auditAction) enforces this and will fail CI if a future refactor
reintroduces body text into the line.

Provider-rejection responses (e.g. "model not found", "rate
limited") are logged separately at the call site, also without
bodies. Admins see the upstream message in the SPA banner;
operators see the full chain in `docker compose logs paimos`.

### Operational guidance

- The `api_key` is stored unencrypted in the SQLite database. Keep
  the data volume on encrypted storage if your threat model requires
  that — the rest of the secrets surface (session cookies, OIDC
  client secret) has the same property.
- Token cost is on the operator's OpenRouter account. The optimize
  endpoint caps input at 32 KiB and output at ~3000 tokens per call;
  per-user spend is bounded by `PAIMOS_AI_DAILY_CAP_TOKENS`.
- "Test connection" calls don't go through the per-user cap (admin
  smoke tests should always work) but they do count against
  OpenRouter billing.
- Frontier picks pull from an undocumented OpenRouter frontend
  endpoint (`/api/frontend/models/find?order=top-weekly`); it can
  break. The picker has a static-fallback list for cold-start
  resilience and serves the last-known-good snapshot when the
  upstream call fails (with a `stale: true` flag in the response).

## Attachments (MinIO / S3 — optional)

When `MINIO_ENDPOINT` is unset, the attachments feature is disabled:
upload UI is hidden, and download endpoints return 503. This is safe
for installations that don't need file uploads.

| Var | Default | Notes |
|---|---|---|
| `MINIO_ENDPOINT` | *(unset)* | Hostname:port, e.g. `minio.internal:9000` |
| `MINIO_ACCESS_KEY` | *(empty)* | Required when endpoint is set |
| `MINIO_SECRET_KEY` | *(empty)* | Required when endpoint is set |
| `MINIO_USE_SSL` | `false` | Set `true` for HTTPS endpoints |
| `MINIO_BUCKET` | `paimos-attachments` (from `BRAND_MINIO_BUCKET`) | Bucket name; created on first boot if missing |

## Example minimal `.env` (prod)

```env
# Core
PORT=8888
DATA_DIR=/app/data
COOKIE_SECURE=true

# Branding
BRAND_PRODUCT_NAME=ACME PM
BRAND_COMPANY_NAME=ACME Corp
BRAND_WEBSITE_URL=https://pm.acme.example
BRAND_PUBLIC_URL=https://pm.acme.example
BRAND_EMAIL_FROM=noreply@acme.example

# Email
SMTP_HOST=smtp.postmarkapp.com
SMTP_PORT=587
SMTP_USER=<postmark-token>
SMTP_PASS=<postmark-token>

# Attachments
MINIO_ENDPOINT=minio.internal:9000
MINIO_ACCESS_KEY=<key>
MINIO_SECRET_KEY=<secret>
MINIO_USE_SSL=false
```

Bootstrap on first run:

```bash
ADMIN_PASSWORD='<temp-password>' docker compose up -d
```

Rotate that temp password via the UI, remove `ADMIN_PASSWORD` from the
env, and restart.

## Runtime branding

The **preferred** way to brand a PAIMOS install is the admin UI at
**Settings → Visual → Workspace Branding** — edit product name, tagline, URLs, page title,
the full colour palette, and upload custom logo + favicon without a
restart or redeploy. Changes apply live the moment you hit Save.

Behind the UI, branding lives in `$DATA_DIR/branding.json`; uploaded
assets live in `$DATA_DIR/branding-assets/` and are served from
`/brand/<filename>` (public — the login page needs the logo pre-auth).
The JSON file is human-readable; ops who prefer git-versioned branding
can edit it directly and it will be picked up on next request.

The `BRAND_*` env vars (`BRAND_PRODUCT_NAME`, `BRAND_WEBSITE_URL`, …)
remain as the **pre-UI fallback**: they generate a default
`branding.json` on first boot and still drive server-side identity
that the UI can't edit (email `From:` header, API-key prefix, TOTP
issuer, health-check service name).

Additional `branding-<name>.json` files in `$DATA_DIR/` can be selected
at runtime via `?file=branding-<name>.json` on `GET /api/branding`
— useful for multi-tenant white-labeling. The admin UI writes to
whichever file is currently selected in the viewer's localStorage, so
edit under the brand you want to change.

### `branding.json` shape

```json
{
  "name": "PAIMOS",
  "company": "",
  "product": "PAIMOS",
  "tagline": "Your Professional & Personal AI Project OS",
  "website": "https://paimos.com",
  "logo": "/logo.svg",
  "favicon": "/favicon.svg",
  "colors": {
    "primary": "#2e6da4",
    "primaryDark": "#1f4d75",
    "primaryLight": "#4a8fc2",
    "primaryPale": "#dce9f4",
    "accent": "#16a34a",
    "sidebarBg": "#1a2d42",
    "sidebarText": "#c8d5e2",
    "loginBg": "#1a2d42",
    "loginPattern": "#243650"
  },
  "pageTitle": "PAIMOS"
}
```

### Asset endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /api/branding` | public | Current branding JSON (login page reads this pre-auth) |
| `PUT /api/branding` | admin | Write `branding.json` (accepts `?file=branding-<slug>.json`) |
| `POST /api/branding/logo` | admin | Multipart `file` field, SVG/PNG/JPEG, ≤ 1 MB |
| `POST /api/branding/favicon` | admin | Multipart `file` field, SVG/PNG/ICO, ≤ 256 KB |
| `GET /brand/<filename>` | public | Serves uploaded assets with SVG-safe CSP |

Default `/logo.svg` and `/favicon.svg` resolve against the bundled
static assets — they're only used when no uploaded branding asset has
taken over the `logo` / `favicon` JSON fields.
