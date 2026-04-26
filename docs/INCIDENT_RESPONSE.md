# PAIMOS — Incident Response Runbook

**Owner:** the maintainer (single-person operation as of v2.0).
**Companion docs:** [`SECURITY.md`](../SECURITY.md) (disclosure policy), [`DEPLOY.md`](DEPLOY.md) (release + rollback runbook).
**Status:** v1 — covers the five most likely incident classes for a self-hosted PAIMOS instance. Revised as new incident shapes are encountered (see *Section 5: Post-incident review template*).

---

## 1 · Severity model

PAIMOS uses a four-level severity ladder. The level determines time-to-acknowledge, time-to-fix targets, and how loudly the incident is communicated.

| Sev | Trigger | TTA target | Fix target | Public note |
|---|---|---|---|---|
| **Sev 0** | External user-data exposure · remote code execution · authentication bypass · production outage with no immediate workaround | < 4 h | < 24 h | Yes, immediate |
| **Sev 1** | Privilege escalation across roles · data integrity / corruption · CSRF/IDOR/auth-flow defect · prolonged service degradation | < 24 h | < 7 d | Yes, after fix |
| **Sev 2** | Limited-scope vulnerability · partial outage · degraded UX with workaround · supply-chain warning that hasn't materialised | < 7 d | < 30 d | Optional |
| **Sev 3** | Documentation / process gap · low-risk finding · hardening opportunity · upstream advisory not yet exploited | < 30 d | next minor release | No |

**Time-to-acknowledge (TTA)** is the wall-clock time from incident-aware to "I have read this and understood the scope". **Fix target** is the calendar window for shipping a remediation, measured from TTA. Both are *targets*, not contractual SLAs (PAIMOS is FOSS; see [`SECURITY.md` § Supported versions](../SECURITY.md#supported-versions)).

A scenario can shift severity as evidence accumulates: a Sev 2 vulnerability disclosure becomes Sev 0 on confirmed in-the-wild exploitation. Re-classify *up* immediately when new evidence warrants; never rely on the original triage if facts have changed.

---

## 2 · Roles and ownership (solo-maintainer reality)

PAIMOS is maintained by one person. The classic incident-response role split (incident commander / liaison / scribe / fixer) collapses to a single seat — but the *function* of each role still has to be performed. Document each step as you take it; you are also the scribe, and a stressed maintainer six months later will not remember.

The minimum viable execution loop:

1. **Acknowledge** in the channel the incident arrived through (email reply for vuln disclosures; status ping for outages). Buys time.
2. **Triage** in private — read the report, reproduce locally if possible, set the severity. Do not rush this; an incorrect severity costs more than a 30-minute delay.
3. **Open an incident_log row** via the admin UI or `POST /api/incidents` (PAI-116 schema). The row is the single artefact every subsequent step references.
4. **Contain** before you fix — stop the bleeding (revoke keys, stop the affected service, rotate secrets). Containment is reversible; eradication is not always.
5. **Fix** — branch, patch, test, release per [`DEPLOY.md`](DEPLOY.md). Keep the reporter (if there is one) in the loop privately.
6. **Communicate** — for Sev 0/1, ship the public advisory ≥ 7 days after the patched release per the [`SECURITY.md` disclosure timeline](../SECURITY.md#disclosure-timeline). For Sev 2, optional advisory at maintainer's call.
7. **Review** — fill in *Section 5: Post-incident review template* in `docs/incidents/` (see *§5*). Updates this runbook if a gap was found.

**Communication channels:**

| Channel | Use |
|---|---|
| `security@paimos.com` | Inbound vulnerability disclosures (private) |
| GitHub Security Advisory | Outbound advisory after fix releases |
| Release CHANGELOG entry | Publicly visible "what landed" with `SEC-YYYY-NN` identifier per `SECURITY.md` |
| Status page (when configured) | Live outage notice for Sev 0/1 |
| `incident_log` table (PAI-116) | Internal canonical record per incident |

---

## 3 · Runbooks

Each runbook follows the same four-phase structure: **Detect · Contain · Eradicate · Recover**. Maintainers should read each runbook at least once before they need it; the goal is muscle memory, not document lookup at 3 am.

### 3.1 · Compromised API key

A `paimos_…` API key is suspected leaked (committed to a public repo, posted in a screenshot, sent over an unencrypted channel, found in a third-party log spill).

**Severity baseline:** Sev 1. Up-shift to Sev 0 if the key belongs to an admin user or has been observed in active exploitation.

**Detect**
- Inbound: leak reported by a third party, GitHub secret-scanning alert, or visible in `git log` of a public repo.
- Internal: anomalous activity in `audit: ai_action ...` or session-mutation logs (PAI-116) traced back to the key.

**Contain**
1. Revoke the key immediately: `DELETE /api/auth/api-keys/{id}` as the owning user, OR via the admin user-detail page if the user account is compromised wholesale.
2. Identify the user whose key it was. Check their `audit_log` rows for the last 24-72 h via `GET /api/sessions/{id}/activity` per session id.
3. If the key is admin-scope, also rotate any in-deployment secrets the admin had access to (OIDC client secret, SMTP credentials, OpenRouter API key, MinIO secret) — assume those have leaked too.

**Eradicate**
1. Identify the leak source (commit hash, screenshot, log file). The leak vector matters: a committed key in a public repo means the key is known to the whole internet; a single screenshot may be more contained.
2. If the source is a Git commit, **rewriting history is rarely worth it** (the key is already cached on every clone and fork). Treat the key as permanently public; revocation is the eradication.
3. Add a regression check if applicable: pre-commit hook, CI secret-scan step (PAI-128 makes this routine).

**Recover**
1. Issue a replacement key via `POST /api/auth/api-keys` and hand it to the user privately.
2. Document the leak source in an `incident_log` note for future audits.
3. If the leak was via Git history of a published repo, file a follow-up ticket to enable PAI-128's gitleaks/trufflehog step if not already on.

**Don't:**
- Don't try to revoke "all keys for this user" via `DELETE FROM api_keys WHERE user_id = ?` raw — use the existing endpoint, which preserves the audit trail. Manual SQL bypasses the audit invariant.
- Don't post the leaked key in any public channel (including bug-tracker comments) when describing the incident — the leaked artefact stays private.

---

### 3.2 · Suspected unauthorised access

A login from an unexpected location, a session created outside normal patterns, or admin actions that the legitimate admin did not perform.

**Severity baseline:** Sev 1. Up-shift to Sev 0 if the actor took persistence-establishing actions (created admin accounts, rotated secrets, added API keys, modified auth config).

**Detect**
- Sessions table shows IP/user-agent inconsistent with the user's typical pattern.
- `audit_log` shows actions outside the user's normal hours or from unfamiliar IP ranges.
- The user reports they did not perform action X.

**Contain**
1. Invalidate all active sessions for the affected user. The lazy approach is `DELETE FROM sessions WHERE user_id = ?` via admin DB access; the proper approach is logging the user out via `POST /api/auth/logout-all` (admin-as-user impersonation if needed).
2. Force a password reset on the affected account (`POST /api/auth/admin-password-reset/{user_id}` — admin-only).
3. If TOTP was active and the actor bypassed it, force `auth_reset_totp` on the user (admin endpoint).
4. If the user is admin and actions were taken, freeze admin role on every other admin account temporarily (`role = 'member'`) until you've confirmed those accounts are unaffected.

**Eradicate**
1. Audit every action the suspect session(s) performed via `GET /api/sessions/{session_id}/activity` (PAI-116). Roll back any malicious changes.
2. If new API keys were created during the session, revoke them.
3. Check the OIDC log if SSO is configured — the actor may have come in via a misconfigured trust relationship.

**Recover**
1. Reissue credentials privately to the user (out-of-band — phone/in-person, not the same email channel that may be compromised).
2. Help the user enable TOTP if they didn't have it; require it if you control account policy.
3. Document the suspected entry vector — credential reuse, OIDC mis-trust, session-token leak — in the incident_log note.

**Don't:**
- Don't disclose details of the detection vector (logs you watched, anomaly thresholds) in the public advisory — that's a roadmap for the next attacker.

---

### 3.3 · Data integrity / DB corruption

A corrupted SQLite database, missing rows after a backup restore, attachments that resolve to 404, or downstream effects of a botched migration.

**Severity baseline:** Sev 1 if the data loss is bounded (one project, one entity class) and recoverable from backups; Sev 0 if the data loss is unbounded or the live DB is unrecoverable.

**Detect**
- `sqlite3 paimos.db "PRAGMA integrity_check"` returns anything other than `ok`.
- API endpoints return 500 with FK violations or "no such column" errors.
- Users report data missing or wrong since a specific deploy.

**Contain**
1. **Stop the service immediately:** `docker compose stop paimos`. Every additional write into a corrupt DB makes the recovery harder.
2. Take a forensic copy of the live DB before any recovery attempt: `cp $DATA_DIR/paimos.db $DATA_DIR/paimos.db.corrupt-<UTC-timestamp>`. The original disk state is the only version of "what actually happened".
3. Identify the most recent good backup via the on-host backup root (default `/home/<user>/paimos-backups/<instance>/<UTC-timestamp>/`).

**Eradicate**
1. Per the rollback section of [`DEPLOY.md`](DEPLOY.md): restore from the last known-good `data.tar.gz`. Verify the restored DB with `PRAGMA integrity_check` and a smoke check on `GET /api/health`.
2. Walk the gap between the backup time and the corruption time. Anything mutated in that window is gone — communicate this to users honestly. Do not attempt to "merge" a backup with a corrupt DB; the merge will silently lose rows.
3. If the corruption was caused by a recent migration, **roll back the image too** to the version that wrote the last-good backup. Migrations are one-way; do not run a v-current binary against a v-prior schema.

**Recover**
1. Restart the service: `docker compose up -d paimos`. Watch logs for the migration step + `server starting`.
2. Communicate: post a CHANGELOG entry under the next release noting the data loss window. If users had work in flight during the window, ask them to re-enter it.
3. **File a follow-up ticket** to address the root cause (faulty migration, insufficient backup cadence, etc.). PAIMOS's PAI-117 retention sweep + PAI-132 backup proof-pack roadmap should converge on automated backup verification.

**Don't:**
- Don't run `VACUUM` on a suspected-corrupt DB. VACUUM rewrites the file in place; if the file is structurally damaged, you may convert recoverable data into unrecoverable data.
- Don't attempt manual `INSERT INTO …` recovery from the corrupt DB. Use the backup; the lost-window cost is preferable to silent data divergence.

---

### 3.4 · Inbound vulnerability disclosure

A security researcher emails `security@paimos.com` with a vulnerability report. Most common entry path; this runbook is the most-exercised in steady state.

**Severity baseline:** triage-dependent. Most reports are Sev 2 or Sev 3; the rest follow the table in §1.

**Detect**
- Email arrives at `security@paimos.com` with `[PAIMOS security]` subject.
- Public proof-of-concept appears on social media or a bug-bounty platform (rare but possible — escalate severity).

**Contain**
1. Acknowledge the report within 72 h per [`SECURITY.md`](../SECURITY.md). Even a "received, will investigate" reply is enough to start the disclosure clock.
2. Open an `incident_log` row immediately, even before reproducing — the row is the audit anchor.
3. If the report includes a working PoC, treat the underlying issue as confirmed (Sev ≥ 2) until proven otherwise. Don't wait for full reproduction before containment if a viable PoC is in hand.

**Eradicate**
1. Reproduce locally on the reported version.
2. Branch the fix (`feat/sec-NN-<short>`), develop in private. **Don't push the branch to the public remote** until the patched release lands; pre-patch branches are themselves a leak vector.
3. Test the fix against the PoC. Add a regression test (typically in `security_regression_test.go`).
4. Cut a patch release per `DEPLOY.md` with the `SEC-YYYY-NN` tag in the CHANGELOG entry.

**Recover**
1. Notify the reporter that the fix is live, with the release tag.
2. Wait the disclosure-window per `SECURITY.md` (default 7 days post-release for operators to update).
3. Publish the GitHub Security Advisory with the reporter's preferred attribution.
4. Update `claim-matrix.md` if the disclosed issue intersects a public claim.

**Don't:**
- Don't publish the PoC in the advisory body — link to the GitHub Security Advisory's controlled-disclosure surface or to the reporter's blog post if they have one.
- Don't backport silently to old releases without disclosure. Per `SECURITY.md` § Supported versions, only the most recent release gets fixes; users on older releases must upgrade.

---

### 3.5 · Provider outage or upstream data leak

PAIMOS depends on external providers: OpenRouter (AI assist), MinIO/S3 (attachments, optional), SMTP (password reset, optional), OpenID provider (SSO, optional). When one fails or leaks, PAIMOS is downstream.

**Severity baseline:** Sev 2 for outage with degraded-but-working PAIMOS (e.g., AI assist down but issues still editable). Sev 1 for an upstream secret/data leak that requires PAIMOS-side rotation. Sev 0 only if the upstream leak exposed customer data PAIMOS had stored at the provider.

**Detect**
- Provider's status page; emails from the provider; news reports.
- PAIMOS-side: provider 5xx rates spike; specific feature errors ("OpenRouter timeout" in audit lines); SMTP queue backs up.
- For data-leak scenarios: provider's incident report mentions PAIMOS's project ID, account, or data class.

**Contain**
1. **Outage:** flip the affected feature off so users see graceful degradation rather than 500s. Settings → AI → enabled = false (admin); for SMTP, the password-reset endpoint already degrades gracefully.
2. **Leak:** rotate the credential the provider held immediately, even before reading their full incident report. Treat the credential as compromised on first announcement.
3. Communicate to PAIMOS users: "external provider X is down/leaked, here's what's affected, here's what we've done."

**Eradicate**
- Outage: nothing PAIMOS can do; wait for upstream recovery. Use the time to verify monitoring caught it.
- Leak: read the provider's full incident report once available. Identify exactly what they held that you rotated, and whether anything else (logs, billing data, audit traces) needs handling.

**Recover**
- Outage: re-enable the feature once upstream is recovered. Verify no queued operations got dropped (e.g., AI calls that hit the timeout window — PAI-208 paper trail makes this auditable per call).
- Leak: log the rotation in `incident_log`, file a ticket if the dependency model needs revisiting (e.g., should PAI-122's local-AI roadmap accelerate?).

**Don't:**
- Don't re-enable a feature whose backing provider just leaked, until you've confirmed the new credential is in place and the old credential was actually invalidated upstream (the provider's "we've rotated all keys" doesn't always reach legacy long-lived ones).

---

## 4 · Tabletop exercise — captured

A tabletop is a 30-90 minute walkthrough of a hypothetical incident. The goal is to find gaps in this runbook (or in the surrounding infrastructure) before a real incident does. The exercise below was run on **2026-04-26**, against scenario **§3.1 Compromised API key**, by the maintainer.

### Scenario

> An open-source contributor pushes a PR that includes a `paimos_…` admin API key in a `.env.example` file by mistake. The PR is opened against the public `markus-barta/paimos` repo. The key belongs to the maintainer's primary admin account on the `pm.barta.cm` (ppm) instance. GitHub secret-scanning fires within 30 seconds.

### Walkthrough — first hour

| t (min) | Action | Notes / friction |
|---|---|---|
| 0 | GitHub secret-scanning alert lands in maintainer's inbox. | ✓ Detection works — secret scanning is enabled on the public repo. |
| 1 | Open the alert; read the PR; identify the key prefix. | The first 10 chars of the key (`paimos_1a2b...`) are enough to identify the row in the `api_keys` table. |
| 2 | Open ppm in the browser, log in, navigate to **Settings → Account → API keys**. Spot the key by prefix. **Click Revoke.** | Revoke is one click and confirmed. Audit row produced. |
| 4 | Verify revocation propagated: try to use the revoked key from another shell — `curl -H "Authorization: Bearer paimos_…" $URL/api/auth/me` — expect 401. | ✓ Confirmed. The key is dead. |
| 6 | Check `audit_log` for actions taken by that key in the last 7 days via the admin Audit view. Skim for unfamiliar entries. | Nothing unfamiliar. The key was created last week, used only by the maintainer's local script. |
| 12 | Decide: this was a self-inflicted disclosure (the maintainer's own account, leaked by accident in a public PR). No third-party exploitation, no signs of misuse. Severity stays at **Sev 1** baseline. | |
| 14 | Issue a replacement API key, update the local script's environment, verify the script still works. | Local-script update was three lines; would have been more work if shared CI. |
| 16 | Open an `incident_log` entry via the admin UI: `IR-2026-001 — Self-disclosed admin API key in public PR`. Status: investigating → resolved. | The form was straightforward; the entry-time field defaulted helpfully. |
| 25 | Force-close the PR on GitHub (don't merge). Comment publicly: "Thanks; this PR included a real API key — I've revoked it. Re-open with the example value `paimos_REPLACE_ME` instead." | Public comment is OK because the key is already revoked. |
| 30 | Write a 3-line note in the `incident_log` row: leaked key, revoked, replacement issued, no exploitation observed. Mark resolved. | The exercise ends here. |

**Total real-time:** ~30 minutes from alert to resolved state.

### Gaps found

1. **No CI step refuses commits that contain `paimos_` prefixes.** PAI-128 already plans to add gitleaks/trufflehog with this exact pattern in its rule set. **Action: keep PAI-128 high-priority.** Filed as a note on PAI-128 to include the `paimos_` prefix specifically.
2. **The admin Audit view is paginated by time, not by API key.** Filtering by `api_key_id` would have been faster than the time-scan I did at t=6. **Action:** small frontend filter add; not worth a ticket on its own — fold into the next admin-UI sweep.
3. **No automated "key has been unused for N days" cleanup.** The leaked key had been unused for 5 days; the maintainer's local script had moved to env-var-only. An idle-key sweeper would have prevented this exact incident. **Action:** filed as PAI-followup.
4. **The replacement-key copy step happens on the maintainer's clipboard.** Acceptable solo-maintainer trade-off; would be a Sev 1 process gap at team scale. **Action:** none for now; revisit when team grows.
5. **The `incident_log` table doesn't auto-link to `audit_log` rows the incident references.** A maintainer reading IR-2026-001 six months later has to re-grep `audit_log` themselves. **Action:** filed as PAI-followup — small backend join + UI surface.

### Outcome

The runbook held up. Detection → containment was 4 minutes wall-clock. The most valuable artefact produced is *Gap 3 (idle-key sweeper)* — a real product improvement uncovered by walking through a hypothetical. **Re-run this exercise on a different scenario in 6 months** (target: 2026-10-26, against §3.3 *Data integrity / DB corruption* using a deliberately-corrupted dev DB).

---

## 5 · Post-incident review template

After every real Sev 0 / Sev 1 incident, fill in a fresh `docs/incidents/<UTC-date>-<short-slug>.md` from the template below. **Sev 2/3 are optional**, but write one when the incident surfaced anything worth remembering.

The review should be readable cold by future-maintainer six months on. Don't write defensive prose; write what actually happened.

```markdown
# Incident review — IR-<YYYY-NN> · <short title>

**Severity:** Sev <N>
**Detected:** <UTC datetime>
**Resolved:** <UTC datetime>
**Reporter:** <internal | external (name/handle/anonymous) | automated>
**incident_log id:** <id>
**Public advisory:** <SEC-YYYY-NN | none>

## Summary
<1-3 sentences. The "if you only read this section" version.>

## Timeline
| t (UTC) | What happened |
|---|---|
| HH:MM   | …             |

## Root cause
<What broke. Cite specific code / config / process. Resist the urge to diffuse blame across "complex factors"; pick the one or two real causes.>

## Impact
- Users affected: <count or scope>
- Data exposure: <yes/no, what>
- Service downtime: <minutes; nominal traffic affected only>
- Financial: <if applicable, e.g., excess provider spend>

## What worked
- <what the team / runbook / monitoring caught early>

## What didn't
- <what slowed us down; what we wished we had>

## Follow-ups
- [ ] <ticket key> — <action item>
- [ ] <ticket key> — <action item>

## Runbook deltas
<If this incident exposed a missing or wrong runbook section, list the change here. Update §3 of `docs/INCIDENT_RESPONSE.md` in the same PR that ships the fix.>
```

**Discipline:** the post-incident review is **not** an exercise in self-criticism. It's the one chance to convert the cost of an incident into a runbook delta. Skip it and the next maintainer pays the same cost a year later.

---

## 6 · Operational notes

- **Where this runbook lives:** `docs/INCIDENT_RESPONSE.md` in the main repo. Keep it in-repo, not in a wiki — wikis bit-rot; in-repo docs ship with every clone and survive deploy churn.
- **Who edits it:** the maintainer. Open a PR that updates a runbook section in the same change that ships the fix. The runbook delta and the code delta belong together.
- **What's deliberately out of scope:** crisis communication beyond GitHub Security Advisory + CHANGELOG (PAIMOS doesn't have a press team and doesn't pretend to); legal handling of regulator notification (out of solo-maintainer scope; consult counsel if PAIMOS holds GDPR-controller-class data, which the default deployment doesn't).
- **Cross-references:**
  - Inbound disclosure policy: [`SECURITY.md`](../SECURITY.md)
  - Release + rollback runbook: [`DEPLOY.md`](DEPLOY.md)
  - Audit + retention controls: [`CONFIGURATION.md` § Audit & retention](CONFIGURATION.md#audit--retention-pai-116--pai-117)
  - Programme close-out audit: [`2.0_AUDIT.md`](2.0_AUDIT.md) §5 (Release process)
  - Trust evidence matrix: [`claim-matrix.md`](claim-matrix.md)
