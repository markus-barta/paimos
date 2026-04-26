# Security policy

## Reporting a vulnerability

**Do not open a public GitHub issue for security problems.**

Send a private report to:

- **Email**: `security@paimos.com`
- **Subject**: `[PAIMOS security] <brief description>`

Include:

- Affected version (commit hash or release tag)
- Impact summary and, if known, the attack scenario
- Reproduction steps (minimal repro preferred; PoC script welcome)
- Your preferred attribution (name, handle, or anonymous)

You should get an acknowledgement within 72 hours. If you haven't after
a week, it's safe to assume the message didn't land — please re-send
via a different channel.

## Disclosure timeline

PAIMOS follows a coordinated-disclosure model:

1. **Triage** (within 7 days): I confirm the issue, assess impact,
   assign a severity.
2. **Fix** (aim: within 30 days for high-severity; longer for low):
   developed privately, with the reporter kept in the loop.
3. **Release**: the fix ships in a new patch release tagged with a
   `SEC-YYYY-NN` identifier.
4. **Disclosure**: 7 days after the release (so operators can update),
   a public advisory goes up with the reporter's preferred
   attribution.

If I can't meet these timelines I'll tell you why and we'll adjust.

The internal handling that follows from a confirmed report (severity
ladder, runbooks per incident class, post-incident review template) is
documented in [`docs/INCIDENT_RESPONSE.md`](docs/INCIDENT_RESPONSE.md).
It's there for transparency and for future maintainers; reporters
shouldn't need to read it.

## Supported versions

Only the most recent release is supported with security fixes. PAIMOS
is pre-1.0 and I can't maintain multiple release branches yet.

## Scope

In scope:

- The PAIMOS codebase (`backend/`, `frontend/`)
- Default Docker image built from `Dockerfile`
- Docs claiming security guarantees (rate limits, session behavior,
  auth flows)

Out of scope:

- Self-inflicted misconfiguration (e.g., running without
  `COOKIE_SECURE=true` over HTTPS)
- DoS via excessive legitimate requests (rate-limiting is a
  best-effort defense, not a DoS shield)
- Issues in upstream dependencies — report those upstream; I'll update
  once patches land

## Thanks

Security reports are one of the most valuable contributions to FOSS.
Thank you for taking the time.
