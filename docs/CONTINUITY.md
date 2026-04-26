# PAIMOS — Solo-Maintainer Continuity Plan

**Owner:** the maintainer (single-person operation as of v2.0).
**Companion docs:** [`SECURITY.md`](../SECURITY.md), [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md), [`DEPLOY.md`](DEPLOY.md), [`2.0_AUDIT.md`](2.0_AUDIT.md).
**Status:** v1 — review every six months, or after any material infrastructure change.

---

## 0 · What this document is, and isn't

This is the bus-factor plan for a solo-maintained FOSS project. It says **what survives, what doesn't, and what someone else can do if the maintainer is out of action** — temporarily or permanently.

It is **not**:

- An enterprise BCP / SLA. PAIMOS makes no uptime promises; users self-host.
- A succession contract. AGPL-3.0 is the legal continuity layer; this document is operational guidance, not a binding handover.
- A guarantee that "the project keeps running automatically forever." It guarantees that a competent stranger reading this document plus the rest of the repo can pick up the pieces.

The bar to clear: a contributor or external auditor reading this on day 1 of an outage should be able to take meaningful action on day 2 without having to reconstruct intent from chat logs or GitHub history.

---

## 1 · What survives without the maintainer

The single best fact about a solo-maintained FOSS project is that **most of the value isn't on the maintainer's laptop**. Specifically:

| Artefact | Where it lives | Survives loss of maintainer? |
|---|---|---|
| Source code | GitHub `markus-barta/paimos` + `markus-barta/paimos-site` (and every clone, fork, and CI runner cache) | ✓ AGPL-3.0; cannot be retracted. |
| Release artefacts | `ghcr.io/markus-barta/paimos:<x.y.z>` (immutable per tag) + GitHub Releases page (SBOMs, signatures) | ✓ Immutable on registry; survives indefinitely unless the registry itself goes dark. See §3.6. |
| User data | Each operator's own `$DATA_DIR/paimos.db` + their MinIO/S3 bucket if attachments | ✓ Always — self-hosted; the maintainer never had a copy. |
| Documentation | In the same repo as the code, including this document. | ✓ Travels with the code. |
| Disclosure history | GitHub Security Advisories (per release tag) + CHANGELOG entries with `SEC-YYYY-NN` identifiers | ✓ Public, durable. |

What does **not** survive without the maintainer, and is the focus of the rest of this document:

- The **paimos.com** domain (registered by the maintainer; renews against a personal billing account).
- **Outbound communication channels** (the `security@paimos.com` inbox, social presence).
- The **chosen-successor or fork pathway** announcement to existing operators.
- Any **operational secret** (registry tokens, OIDC client secrets, SMTP credentials at the reference deployment, etc.) — these are private to the maintainer and the deployments they run; loss of access requires rotation, not recovery.

---

## 2 · Critical operational knowledge — recoverable without the maintainer

The information below has to be findable from this document, this repo, or the maintainer's documented secret-store. The maintainer's *memory* must not be a single point of failure.

### 2.1 · Where everything lives

| Asset | Canonical location |
|---|---|
| App code | `https://github.com/markus-barta/paimos` |
| Site code | `https://github.com/markus-barta/paimos-site` |
| Release images | `ghcr.io/markus-barta/paimos:<tag>` |
| Reference deployment | `pm.barta.cm` (ppm), runs the maintainer's primary instance |
| Secondary reference deployment | `pm.bytepoets.com` (pmo) |
| Public website | `paimos.com` (Caddy server on `cs1.barta.cm`, rsync deploys per [paimos-site/README.md](https://github.com/markus-barta/paimos-site/blob/main/README.md)) |
| Disclosure inbox | `security@paimos.com` |
| Security advisories | `https://github.com/markus-barta/paimos/security/advisories` |
| GitHub Org access list | `https://github.com/orgs/markus-barta/people` (admin-only view) |

### 2.2 · How to do each critical operation

Cross-references — every routine operation has a runbook in the repo. Do not reinvent these from memory; follow the doc.

| Operation | Runbook |
|---|---|
| Cut a release | [`DEPLOY.md` § The four commands](DEPLOY.md#the-four-commands) — `just release patch\|minor\|major` |
| Deploy a tag to ppm / pmo | `just deploy-ppm <tag>` / `just deploy-pmo <tag>` |
| Doc-sync follow-up after release | `just doc-sync` |
| Restore a deployment from backup | [`DEPLOY.md` § Rollback](DEPLOY.md#rollback-if-a-deploy-goes-sideways) |
| Verify a release artefact (signature + SBOM) | [`RELEASE.md` § How to verify](RELEASE.md#how-to-verify-a-release) |
| Handle inbound vulnerability disclosure | [`INCIDENT_RESPONSE.md` § 3.4](INCIDENT_RESPONSE.md) |
| Investigate suspected unauthorised access | [`INCIDENT_RESPONSE.md` § 3.2](INCIDENT_RESPONSE.md) |
| Respond to data corruption | [`INCIDENT_RESPONSE.md` § 3.3](INCIDENT_RESPONSE.md) |
| Update the public claim matrix | [`docs/claim-matrix.md`](claim-matrix.md) — checked at release time by `scripts/check-claims.sh` |

### 2.3 · Where the secrets live

This document does **not** name secret locations or list credentials. That information lives in the maintainer's password-manager vault, with two-of-three recovery share (the maintainer + two trusted recovery contacts; the contacts are named in the maintainer's personal estate document, not here).

What an emergency handover **does** find in this repo:

- The list of *kinds* of secret that exist (registry tokens, OIDC client secrets, SMTP credentials, OpenRouter API key, deploy SSH keys). See `docs/CONFIGURATION.md` for the env-var inventory.
- The rotation procedure for each (each operator rotates their own; the maintainer does not hold any operator's secrets).
- The DNS / domain registrar relationship description (not the credentials).

A would-be successor inheriting administrative duties needs to:

1. Establish access to the password-manager recovery share.
2. From the share, retrieve registrar + DNS + GitHub-org credentials.
3. Rotate every credential before resuming public-facing operations — **assume the prior maintainer's session is the threat model** unless the handover is voluntary and verified.

### 2.4 · Domain + DNS posture

`paimos.com` is registered with a personal billing account. Auto-renewal is on. The first sign of a problem is a renewal-failure email; the second is the WHOIS expiry date crossing into the present.

If `paimos.com` lapses:

- The site goes dark within the registrar's grace window (typically 30-60 days; varies by TLD policy).
- Existing operators' deployments **are not affected** — they pull images from `ghcr.io` and serve from their own domains.
- The disclosure inbox `security@paimos.com` stops resolving. Reports route to a backup alias (documented in the maintainer's vault, not here).
- The trust page (`paimos.com/trust.html`) goes dark, but the same content exists in this repo as [`docs/2.0_AUDIT.md`](2.0_AUDIT.md), [`docs/INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md), [`SECURITY.md`](../SECURITY.md), [`docs/claim-matrix.md`](claim-matrix.md), and this document.

---

## 3 · Emergency scenarios

Each subsection below is a runbook for "what happens when X". The structure mirrors [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md): **Detect · Contain · Eradicate · Recover**, plus a **Don't** list.

### 3.1 · Temporary maintainer unavailability (days to weeks)

The maintainer is out for vacation, illness, or comparable short-term reasons.

**Detect** — calendar event, automated bounceback on `security@paimos.com` (when configured), social-media absence past expected duration.

**Contain** — nothing required. Existing operators run their own deployments; new releases simply don't ship for the duration.

**Eradicate** — n/a; this isn't a defect, it's normal life.

**Recover** — the maintainer returns and resumes the regular release cadence. Inbound vulnerability disclosures that arrived during the absence are triaged on return; the 72h-acknowledge target slips, which is acknowledged in the eventual reply.

**Don't:**
- Don't post a "the maintainer is out" notice on the public site for routine absences. It invites attempts to exploit the window. A persistent absence (multi-week) warrants a status note; routine vacations don't.
- Don't auto-acknowledge security disclosures with a vacation message. The reporter should reach a real human or no one; an automated reply that says "I'm back on date X" is roadmap for an attacker.

### 3.2 · Long-term or permanent maintainer unavailability

The maintainer is unable or unwilling to continue. Could be permanent (life event) or extended (multi-month sabbatical with no clear return).

**Detect** — recovery contacts named in the maintainer's estate document are notified by family or legal counsel; community surfaces (GitHub, social media) show extended silence.

**Contain**:
1. The recovery contacts coordinate with the maintainer's estate (if applicable) to access the password-manager vault.
2. A holding announcement is posted at `paimos.com` and on the GitHub repo: "PAIMOS is in a planned succession period. Operators continue to self-host; security disclosures should pause until further notice or be addressed to <designated successor>."
3. All registry tokens, deploy keys, and the disclosure inbox are rotated. The reference deployment (ppm) is either kept running by the recovery contacts (if they have the appetite) or shut down with notice — operators are unaffected since they run their own.

**Eradicate** — there's nothing to eradicate. The project simply enters a transition.

**Recover** — three legitimate paths:

1. **A designated successor steps in.** The successor has GitHub-org admin transferred to them, takes over the domain renewal, and continues the release cadence. Communicate via a single GitHub Discussions post pinned at the repo top.
2. **The community forks.** Per AGPL-3.0, anyone may fork. The fork takes a new domain (`paimos-community.org` or similar — name doesn't matter), publishes a "we are the active fork" notice, and signals to operators where to point their next image pull. The original `paimos.com` becomes archival.
3. **Project enters honest dormancy.** The repo is marked archived on GitHub. Existing images on `ghcr.io` continue to work indefinitely; no new releases ship. Operators can keep running until they choose to migrate. The trust page is updated to say "no longer maintained" and link to the AGPL fork rights.

**Don't:**
- Don't pretend the project is still active when it isn't. Honest dormancy preserves the tool's usefulness for current operators; pretended activity erodes trust.
- Don't transfer the GitHub org to anyone who hasn't demonstrated commitment to the AGPL-3.0 stance and to the brand framework in [`docs/brand/BRAND.md`](brand/BRAND.md). The legal stance is part of what users adopted; a hostile re-license would betray that.

### 3.3 · Domain expiry or DNS lapse

`paimos.com` is unreachable. Either WHOIS shows expiry, or DNS responds with NXDOMAIN, or HTTPS certificates fail.

**Detect** — registrar email; external monitor (when configured); user reports.

**Contain**:
1. Check the registrar account — is this a billing failure or a deliberate lapse?
2. If billing failure: pay the renewal. Most registrars allow renewal-after-expiry within a 30-day grace window. The site comes back without DNS migration.
3. If credentials are lost: see §3.2 (long-term unavailability) — the registrar is part of the bus-factor surface.

**Eradicate** — fix the cause: enable auto-renewal, ensure the billing account is funded, ensure the renewal-failure emails reach a checked address.

**Recover** — domain is live again; site rsyncs through the existing GitHub Actions deploy workflow; trust page is back. Existing operators were never affected (their images pull from `ghcr.io`, not from `paimos.com`).

**Don't:**
- Don't rely on the maintainer's personal email for renewal-failure alerts as the only channel. Add an auto-forward to a second address checked by recovery contacts.
- Don't transfer the domain to a different registrar mid-incident. Stabilise first; reorganise later.

### 3.4 · GitHub account or org compromise

The maintainer's GitHub account is taken over, or the org's permissions are tampered with.

**Detect** — unexpected commits, unexpected releases, missing releases, unfamiliar collaborators, billing changes, two-factor reset notifications.

**Contain**:
1. **Lock the org**: GitHub Support has an "I think my account is compromised" path that disables sensitive operations pending verification.
2. Force-rotate every secret derived from GitHub (CI tokens, image-registry write tokens, deploy SSH keys).
3. **Pull a forensic clone** of every PAIMOS repo to local disk *immediately*. The attacker cannot retract a clone you already have.
4. Treat any release tagged during the compromise window as untrusted. Operators should pin to the last known-good signed image until the situation is cleared.

**Eradicate**:
1. Recover the GitHub account via 2FA recovery codes (stored in the password-manager vault).
2. Audit the repo: revert any malicious commits, remove any unauthorised collaborators, regenerate the cosign keyless-signing trust path (the OIDC subject identity is what cosign signs against; if the GitHub account identity changed, every signature after the change is suspect).
3. Issue a public Security Advisory documenting the incident and the affected release range.

**Recover**:
1. Cut a fresh patch release with the rotated secrets. The new release's cosign signature carries the recovered identity.
2. Re-publish the trust page and CHANGELOG with the incident note.
3. Run a tabletop exercise of this scenario in [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) within 30 days to capture lessons learned.

**Don't:**
- Don't assume the cloud-hosted Git history is the canonical record. The attacker may have force-pushed; your local clone (which you took at containment-step 3) is the source of truth for "what should the history look like".
- Don't disclose the compromise vector publicly until the recovery is complete. Doing so accelerates exploitation against operators who haven't yet rotated.

### 3.5 · Maintainer's personal machines lost or compromised

Laptop stolen / disk encrypted by ransomware / drive failure with no recent backup.

**Detect** — the moment it happens; usually no detection delay needed.

**Contain**:
1. Revoke active SSH keys, GPG keys, GitHub PATs, registry tokens — anything keyed to the lost machine.
2. Force-end every active session on critical accounts (GitHub, registrar, DNS, email).
3. Rotate the password-manager master password from a known-good device; verify recovery shares are intact.

**Eradicate** — restore from the password-manager vault to a fresh machine. Re-establish 2FA, re-clone the repos, re-create local SSH keys, re-authenticate to deploy targets.

**Recover** — resume normal operations. The repository state is unaffected (everything was on GitHub); the local working tree is rebuilt from clone.

**Don't:**
- Don't store unencrypted secrets on the working machine. Every secret PAIMOS depends on lives in a managed password vault, not in `~/.bashrc` or `~/.zshrc`.
- Don't rely on a single physical 2FA device. The recovery codes in the password vault are the second factor for the second factor.

### 3.6 · Container registry shutdown or pull restriction

GitHub Container Registry (`ghcr.io`) becomes unavailable to operators. Could be a service-level outage, a policy change, or a takedown.

**Detect** — `docker pull` returns 503/404 / authentication errors; GitHub status page acknowledges.

**Contain**:
- Operators continue running existing deployments (image already on local Docker daemon). New deploys to other hosts may fail until a mirror is established.
- Maintainer publishes a status note pointing to a fallback registry if available.

**Eradicate** — n/a from the maintainer side; this is upstream infrastructure.

**Recover** — multi-pronged:

1. **Rebuild from source.** Every release tag in Git is reproducible: `git checkout v<x.y.z>`, `docker build -t paimos:<x.y.z> .`. The Dockerfile is in the repo.
2. **Mirror the registry.** Operators can `docker pull` once and `docker save` to a tarball for re-distribution; the SBOM attestations stay valid against the digest.
3. **Federate to a second registry.** If `ghcr.io` becomes unreliable, the project documents pushing to a second registry (Docker Hub, Quay) as a one-off, with the cosign signature reissued against the new identity.

**Don't:**
- Don't depend on a single registry as the only path to images. Document the "build from source" path explicitly so it's not a panic-time surprise.
- Don't migrate registries silently. The cosign trust identity changes when the registry identity changes; a silent migration breaks every operator's verify command.

---

## 4 · Designated successor / fork pathway

The legal continuity layer is **AGPL-3.0**: anyone with a copy of the source can continue the project. The project does not have a single named successor in this public document, by design — naming a successor here is a soft promise the project can't keep, since people change roles.

What the project **does** commit to:

- The maintainer will name recovery contacts privately (in their estate document, in their password-manager vault metadata, and in a single sealed envelope held by a trusted friend) so that long-term unavailability does not strand the project's operational surface.
- If a successor steps in, the trust page and this document will be updated to reflect that — explicitly, not implicitly.
- If no successor steps in, dormancy will be declared honestly per §3.2 path 3.

Forking is encouraged when no successor materialises. The AGPL-3.0 protects users against dead-project surprise: the source remains forkable, the brand is forkable (paimos's brand framework lives entirely in [`docs/brand/BRAND.md`](brand/BRAND.md)), and the deployment runbooks survive.

A community fork should:

1. Take a new domain. Don't squat on `paimos.com` — that's a separate trust handover.
2. Mark itself clearly as a fork on first commit ("this is a community continuation of paimos by markus-barta after <date>").
3. Re-issue the cosign trust identity. The original signatures stay valid against the original digest; the fork's signatures attest to the fork's commits, not retroactively to the original's.

---

## 5 · User self-care during outages

Operators can do a lot for themselves. The reference deployment going dark does not mean their data is at risk.

**For operators running their own PAIMOS instance:**

- **Backup discipline:** existing `DEPLOY.md` describes the backup-on-deploy flow. Keep at least one off-site backup.
- **Image pinning:** pin the running deployment to a specific `<x.y.z>` digest, not `:latest`. Survives a registry pull.
- **Source mirroring:** maintain a private mirror of the GitHub repo if you depend on PAIMOS for production. `git clone --mirror` weekly works.
- **Watch for advisories:** subscribe to `https://github.com/markus-barta/paimos/security/advisories` (RSS feed available). Don't rely on email from the maintainer.
- **If you discover a security issue and the maintainer is unreachable:** post a coordinated-disclosure ticket via GitHub's "Report a vulnerability" surface (built into Security Advisories). It does not require email.

**For prospective operators evaluating PAIMOS during an extended absence:**

- Read [`paimos.com/trust.html`](https://paimos.com/trust.html). It documents the project's posture honestly, including the limits.
- Read this document. If §3.2 has been triggered and the trust page reflects dormancy, factor that into your adoption decision.
- Build from source if you can't reach the registry. The Dockerfile + the `just` recipes are designed to work offline-from-source.

---

## 6 · Update cadence

Review and update this document:

- Every six months on a fixed calendar reminder (next: 2026-10-26).
- After any material infrastructure change: domain registrar move, new deploy target, password-manager change, new recovery contact, new GitHub org member.
- After a real continuity event (per §3) — the post-incident review template in [`INCIDENT_RESPONSE.md` §5](INCIDENT_RESPONSE.md#5--post-incident-review-template) covers this; runbook deltas land here, not in the incident log.

The trigger for revisiting **this entire plan structurally** (not just the specific entries) is project growth past a single maintainer. Once paimos has more than one full-time maintainer, the bus-factor framing in §0 changes; this document becomes part of the team's BCP rather than a solo-handover plan.

---

## 7 · Tabletop exercise — captured

A 30-minute walkthrough run on **2026-04-26**, against scenario **§3.2 Long-term maintainer unavailability**, by the maintainer.

### Scenario

> The maintainer is in a serious traffic accident, hospitalised, and unable to communicate for an estimated 6-12 weeks. Family contacts the named recovery contact (per estate document) on day 3.

### Walkthrough — first month

| Day | Action | Notes / friction |
|---|---|---|
| 0 | Accident. Maintainer unavailable. | |
| 3 | Family reaches a recovery contact. Recovery contact accesses password-manager via the two-of-three share (recovery contact + one other share-holder). | The two-of-three share survives the loss of one keyholder; the maintainer + both contacts losing access simultaneously is a separate, lower-likelihood failure mode (treat as accepted residual risk). |
| 5 | Recovery contact verifies legitimacy via medical confirmation and posts a holding announcement at `github.com/markus-barta/paimos` discussions: "The maintainer is temporarily unavailable. New releases pause; existing operators continue to self-host; security disclosures route to <recovery contact alias> until further notice." | The announcement intentionally does **not** detail the medical situation — that's family privacy, not project transparency. |
| 7 | Recovery contact rotates registry tokens, deploy keys, the `security@paimos.com` inbox forwarding rule. Existing tokens that the maintainer might have written down on a sticky note are now revoked. | This is paranoia-by-default per §3.2 contain step 3. Ten minutes of work. |
| 14 | First inbound security disclosure since the unavailability. Recovery contact triages it but cannot ship a patch (no engineering coverage). Reply to reporter: "Acknowledged; the maintainer is unavailable until approximately <date>. If this is high-severity and time-sensitive, please consider responsible coordinated disclosure via the AGPL-3.0 fork pathway documented at [link]." | The reply is honest. Reporters can choose whether to wait or escalate. |
| 21 | Maintainer's `paimos.com` domain renewal email arrives. Recovery contact pays it from the share's billing card on file. | Auto-renewal would have caught this; the manual confirmation is belt-and-braces. |
| 28 | One operator reaches out via GitHub Issues asking "is paimos still alive". Recovery contact replies linking the holding announcement and this document. | The operator is satisfied; they continue running their pinned deployment. |
| 30 | End of the exercise window. Maintainer's expected return is week 8-10. | |

### Gaps found

1. **The `security@paimos.com` forwarding rule is configured at the registrar level, not in this document.** A recovery contact opening this document would not know how to reroute the inbox without poking around. **Action:** add a note to the maintainer's vault that names the registrar's "email forwarding" config page; do not name the registrar in this public doc.
2. **No backup billing card on file at the domain registrar.** If the maintainer's primary card declines (compromised + frozen), the registrar's renewal fails. **Action:** add a backup card to the registrar account; tracked as a maintainer-personal task, not a PAIMOS ticket.
3. **The recovery contact has no GitHub-org admin role today.** Establishing it post-incident requires a 2FA recovery flow, which slows day-3 actions to day-5+. **Action:** add the recovery contact to the GitHub org with admin scope, gated by their own 2FA; document this in the vault metadata, not here.
4. **The tabletop assumed AGPL-3.0 fork rights are well-known to operators.** They are *legally* well-known; *operationally* well-known is a stretch. **Action:** add an "If we go dark, this is what you do" section to the public trust page so operators see the path before they need it.

### Outcome

The plan held. The recovery contact-driven flow is workable in 6-12 week absences. The main soft spots are operational details that belong in the vault (gaps 1, 2, 3) plus one user-facing surface (gap 4 — already covered by §5 of this document and the trust-page §05 limits section).

**Re-run this exercise on a different scenario in 12 months** (target: 2027-04-26, against §3.4 *GitHub org compromise* using a deliberately-revoked test token).

---

## 8 · Cross-references

- [`SECURITY.md`](../SECURITY.md) — disclosure policy.
- [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) — incident severity, runbooks, post-incident review template.
- [`DEPLOY.md`](DEPLOY.md) — release + deploy + rollback runbooks.
- [`RELEASE.md`](RELEASE.md) — what each tag publishes, how to verify it.
- [`2.0_AUDIT.md`](2.0_AUDIT.md) — programme-scope audit + decisions log.
- [`brand/BRAND.md`](brand/BRAND.md) — name, mark, voice, phasing plan.
- [`paimos.com/trust.html`](https://paimos.com/trust.html) — public trust posture (mirrors content from this doc + claim-matrix.md + 2.0_AUDIT.md).
- [`THREAT_MODEL.md`](THREAT_MODEL.md) — threat actors, trust boundaries, named security invariants per domain. This continuity plan handles the maintainer being out; the threat model handles the system being under attack.
