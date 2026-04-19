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

When `SMTP_HOST` is unset, password-reset emails are logged to stdout
instead of sent. This is the default dev-mode behavior and is safe for
running PAIMOS without any email infrastructure.

| Var | Default | Notes |
|---|---|---|
| `SMTP_HOST` | *(unset)* | Unset = dev mode. Set to enable real sending. |
| `SMTP_PORT` | `587` | STARTTLS submission port |
| `SMTP_USER` | *(empty)* | Leave blank for unauthenticated relay |
| `SMTP_PASS` | *(empty)* | Pair with `SMTP_USER` |

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
**Settings → Branding** — edit product name, tagline, URLs, page title,
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
