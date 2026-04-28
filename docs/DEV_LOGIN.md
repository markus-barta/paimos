# Dev Login (PAI-267)

Agent-driveable local authentication for PAIMOS. Lets a human or an
agent (Claude Code, playwright, anything that drives `curl`) drop into
a logged-in PAIMOS UI without ever touching production credentials —
so layout / RBAC / responsive bugs become DOM-inspectable instead of
description-only.

> **This route does not exist in production binaries.** The handler
> code is gated behind a Go build tag (`dev_login`) and never compiled
> into release images. Setting `PAIMOS_DEV_LOGIN_TOKEN` on a deployed
> instance is a no-op — the route returns 404 because the function
> body isn't there. CI's `PAI-270` job re-asserts the canary strings
> are absent on every release.

---

## 0 · TL;DR

```bash
# one-time per machine
mkdir -p ~/Secrets/dev
printf 'PAIMOS_DEV_LOGIN_TOKEN=%s\n' "$(openssl rand -hex 32)" \
  > ~/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env
chmod 600 ~/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env

# every session
just dev-up
```

`just dev-up` builds the backend with `-tags dev_login`, idempotently
seeds the dev fixtures (4 users, 4 projects, memberships matrix),
starts the backend on `:8888`, hands off to vite on `:5173`, and
prints the agent recipe for grabbing a session cookie.

---

## 1 · Token generation

The token is a 32-byte (64 hex char) random string. Anything shorter
is rejected at backend boot. Generate it once, store it under
`~/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env`, and never commit it.

```bash
mkdir -p ~/Secrets/dev
printf 'PAIMOS_DEV_LOGIN_TOKEN=%s\n' "$(openssl rand -hex 32)" \
  > ~/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env
chmod 600 ~/Secrets/dev/PAIMOS_DEV_LOGIN_TOKEN.env
```

The path is configurable via `PAIMOS_DEV_LOGIN_TOKEN_FILE` if you
prefer a different location, but the default lives outside the repo
deliberately so a `git add -A` cannot pull it in by mistake.

`scripts/dev-up.sh` sources this file at the top of the script. If
the file is missing or empty, the script bails before doing any
build work and prints the recipe above.

---

## 2 · Security model — the build tag is load-bearing

The defence does not rest on the token alone. It rests on **the
token never being checked in production**, because the route that
would check it isn't compiled into shipping binaries:

```
backend/auth/
├── dev_login_dev.go    //go:build dev_login   ← real handler
└── dev_login_prod.go   //go:build !dev_login  ← no-op stub
```

The production `Dockerfile` runs `go build` without `-tags
dev_login`, so only `dev_login_prod.go` compiles. Its `DevLoginEnabled`
returns `false`; `main.go` skips mounting the route entirely; the
handler symbol does not exist in the binary.

You can see this for yourself:

```bash
# build the production binary
( cd backend && go build -o /tmp/paimos-prod . )

# canary strings are absent
strings /tmp/paimos-prod | grep -F 'DEV-LOGIN ROUTE ENABLED'
# (no output)

# rebuild with the dev tag
( cd backend && go build -tags dev_login -o /tmp/paimos-dev . )
strings /tmp/paimos-dev | grep -F 'DEV-LOGIN ROUTE ENABLED'
# ⚠️  DEV-LOGIN ROUTE ENABLED — token sha256 prefix: %s — DO NOT USE IN PRODUCTION
```

CI runs the same canary check on every push (see `.github/workflows/ci.yml`
step `Backend — dev-login symbol absent in production binary`). If a
future refactor ever moves dev-login code into a non-tagged file, the
build fails before any image is pushed.

---

## 3 · The four defence layers

| Layer | What it does | Where it lives |
|---|---|---|
| 1 — Build tag | Handler code only compiles with `-tags dev_login`. Production binary cannot route to it. | `dev_login_dev.go` / `dev_login_prod.go` |
| 2 — Token check | Per-call `{username, token}` body; constant-time compare against `PAIMOS_DEV_LOGIN_TOKEN`. | `DevLoginHandler` |
| 3 — Boot panic | Backend refuses to start if `dev_login` tag is active AND `PAIMOS_ENV=production`. Belt-and-suspenders against an operator who builds dev images for prod. | `ValidateDevLoginConfig` |
| 4 — Audit + banner + cap | Every dev-login attempt → `audit:` log line; sessions carry `via_dev_login=1`; SPA renders a non-dismissable red banner; sessions hard-cap at 24h regardless of the global session-duration config. | DB column `sessions.via_dev_login` (M85), `AppDevLoginBanner.vue` |

You should be able to defeat layers 2–4 mentally as a thought
experiment and end up with a binary that **still doesn't expose the
route**, because layer 1 is independent of all the others.

Boot-time misconfig refusal table:

| Build with `-tags dev_login`? | `PAIMOS_ENV` | `PAIMOS_DEV_LOGIN_TOKEN` | Behaviour |
|---|---|---|---|
| no | any | any | Route returns 404. No checks run. |
| yes | `production` / `prod` | any | **Backend panics on startup.** Refuse to run. |
| yes | other / unset | unset / empty | Route returns 503. Boot warning logged. |
| yes | other / unset | < 32 chars | **Backend panics on startup.** |
| yes | other / unset | placeholder (`1` / `true` / `dev` / `password` / `secret` / `token`, case-insensitive) | **Backend panics on startup.** |
| yes | other / unset | valid | Route active. Boot prints `⚠️  DEV-LOGIN ROUTE ENABLED — token sha256 prefix: <8-char>` (sha256 prefix only — never the full token). |

---

## 4 · Switching users mid-session

There's no in-app user switcher (intentional — a switcher would invite
mistakes against real instances). The recipe is logout + dev-login as
someone else.

```bash
# 1. log out (via the SPA's avatar menu, OR via curl)
curl -sS -b /tmp/paimos-dev-cookies.txt -X POST \
     http://localhost:8888/api/auth/logout

# 2. dev-login as the next user
curl -sS -c /tmp/paimos-dev-cookies.txt \
     -H 'Content-Type: application/json' \
     -d '{"username":"dev_viewer","token":"'"$PAIMOS_DEV_LOGIN_TOKEN"'"}' \
     http://localhost:8888/api/auth/dev-login

# 3. reload the SPA — the banner reflects the new user
```

The non-dismissable banner shows the active username + global role +
project-membership summary, so a glance at the top of the page tells
you who you are without inspecting cookies.

---

## 5 · What NOT to use it for

- ❌ **Production-deployed instances.** Won't work — the handler
  symbol isn't in the binary — but worth saying anyway. Setting
  `PAIMOS_DEV_LOGIN_TOKEN` on a `pm.barta.cm` or `pm.bytepoets.com`
  shell is a no-op.
- ❌ **Pre-prod / staging environments where real users exist.** The
  fixture users have empty password columns; if dev-login is ever
  accidentally enabled on a server with real customer data, an
  attacker who exfiltrates the dev token can log in as any fixture
  user — and from there potentially access the same admin surface
  real admins use.
- ❌ **Sharing tokens.** Every operator generates their own. Don't
  paste tokens in Slack or PRs. Don't put them in CI secret stores
  (CI doesn't need to dev-login — it has the API-key path).
- ❌ **Authenticating real users via the dev-login route.** Real
  users belong on the normal `/auth/login` flow with real passwords
  + TOTP. Dev-login bypasses TOTP and the 5-attempts/10-min rate
  limit; it has no business near real identities.
- ❌ **Long-running sessions.** The 24h hard cap exists for a reason
  — a forgotten browser tab cannot grant indefinite access. If you
  need longer than 24h, you're trying to use this for production
  work; see point 1.

---

## 6 · The user × project matrix

`paimos dev-seed` creates four users (pinned ids `9001–9004` so
playwright selectors are stable) and four fixture projects. The
matrix below is what each user gets when they log in.

| User           | Global role | PAIT     | ACME     | BUGZ     | LOGS     |
|----------------|-------------|----------|----------|----------|----------|
| `dev_admin`    | `admin`     | (all)    | (all)    | (all)    | (all)    |
| `dev_editor`   | `member`    | editor   | editor   | viewer   | —        |
| `dev_viewer`   | `member`    | viewer   | —        | —        | viewer   |
| `dev_outsider` | `external`  | —        | —        | —        | —        |

- `dev_admin` has global role `admin`, which inherits all-access
  without explicit `project_members` rows. It's the agent's default
  (set username to `dev_admin` if you don't have a reason to pick
  someone else).
- `dev_editor` has uneven memberships **deliberately** — it's the
  agent's tool for testing the project picker, sidebar list, and
  search rendering "mixed-access reality" (some projects visible,
  some not, one with view-only). LOGS-absent forces "you have no
  access to this project" UI to render.
- `dev_viewer` is the read-only flow tester. It can `GET` a couple
  of projects but every write surface should refuse it (403). Use
  it to prove no edit-button leaks past `auth.canEdit()` gates.
- `dev_outsider` has global role `external` and zero project rows
  — i.e. the user that should hit the "no projects visible to you"
  empty state and be redirected away from internal routes. Use it
  to verify the portal-only experience for external users.

| Project key | What it is | Phase-1 surface |
|-------------|------------|-----------------|
| `PAIT`      | Paimos Testing — RBAC sandbox | 5 issues spanning the status enum |
| `ACME`      | Acme GmbH — commercial customer engagement | 5 issues; rich-fixture variety pending in PAI-269 |
| `BUGZ`      | Open-source bug tracker | 5 issues; 100+ issues + relations + soft-deletes pending in PAI-269 |
| `LOGS`      | Personal-OS captain's log | 5 issues; comment threads + attachments pending in PAI-269 |

Phase-1 ships ~5 issues per project. The richer surface area
(ACME's sprints + time entries, BUGZ's relation graph, LOGS's
comment threads + attachments) is tracked in [PAI-269](https://pm.barta.cm/issues/PAI-269).

---

## 7 · Agent recipe

The minimum to get a session cookie and use it on subsequent calls:

```bash
# log in as dev_admin and persist the cookie jar
curl -sS -c /tmp/paimos-dev-cookies.txt \
     -H 'Content-Type: application/json' \
     -d '{"username":"dev_admin","token":"'"$PAIMOS_DEV_LOGIN_TOKEN"'"}' \
     http://localhost:8888/api/auth/dev-login

# subsequent requests reuse the cookie
curl -sS -b /tmp/paimos-dev-cookies.txt \
     http://localhost:8888/api/auth/me
# => {"user":{...},"access":{...},"via_dev_login":true}
```

For playwright / browser automation, navigate to
`http://localhost:5173` first, then `POST /api/auth/dev-login` from
the same browser context — the response sets the `session` cookie
on the same origin and the SPA picks it up on the next route.

The `via_dev_login: true` field on `/api/auth/me` is the canonical
source of truth that the SPA reads to decide whether to render
`AppDevLoginBanner`. If you're driving the UI and don't see the
banner, that's the field to inspect first.

---

## 8 · Cross-references

- [PAI-267](https://pm.barta.cm/issues/PAI-267) — the originating ticket
- [PAI-269](https://pm.barta.cm/issues/PAI-269) — phase-2 follow-up: rich fixture data
- [PAI-270](https://pm.barta.cm/issues/PAI-270) — phase-2 follow-up: CI symbol-absent assertion (shipped, lives in `.github/workflows/ci.yml`)
- [PAI-271](https://pm.barta.cm/issues/PAI-271) — this document
- [`HARDENING.md` § 3.2](HARDENING.md#32--authentication) — production auth invariants (the rules dev-login deliberately operates outside of)
- [`THREAT_MODEL.md`](THREAT_MODEL.md) — the threat model dev-login is **not** a defence in (it's a development convenience that ships only in dev binaries)
