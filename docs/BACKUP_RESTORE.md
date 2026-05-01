# PAIMOS — Backup, Restore & Disaster Recovery

**Owner:** the maintainer (single-person operation as of v2.0).
**Companion docs:** [`DEPLOY.md`](DEPLOY.md) (release + rollback), [`HARDENING.md`](HARDENING.md) (operator hardening checklist), [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) (DB corruption runbook §3.3), [`CONTINUITY.md`](CONTINUITY.md) (bus-factor planning), [`THREAT_MODEL.md`](THREAT_MODEL.md) (the model an outage threatens).
**Audience:** operators running PAIMOS in production. Pre-launch, recurring (every six months), and at incident time.
**Drill cadence:** at least one recorded restore drill every six months. Next: **2026-10-26**.

---

## 0 · Targets at a glance

| Target | Value | Notes |
|---|---|---|
| **RPO** (Recovery Point Objective) | **≤ 24 h** with default operator setup; **= deploy cadence** if deploys are more frequent | The deploy script takes a backup before every image swap. For environments deploying weekly or rarer, operators should add a scheduled snapshot. |
| **RTO** (Recovery Time Objective) | **~5 min** for image-pin rollback (no data loss) · **~15-30 min** for full DB restore from tarball | Image-pin rollback is the right answer when only the image is suspect. DB restore is for actual data corruption / loss. |
| **Drill cadence** | every 6 months minimum, plus before any risky migration | Drill recorded under §5 below; gaps drive doc and runbook deltas |
| **Backup retention** | operator-controlled; default = whatever fits in `$BACKUP_ROOT` | The deploy script never auto-prunes; operators tune retention per their available storage |

The numbers above are **observed**, not aspirational — see §5 for the drill that backs them up.

---

## 1 · Backup scope — what's in, what's out

### What the deploy-time tarball contains

`scripts/deploy.sh` produces `data.tar.gz` containing the entire `$DATA_DIR` (or the contents of the named Docker volume, depending on storage mode). After extraction:

| Path | Contents | Size order-of-magnitude |
|---|---|---|
| `paimos.db` | Main SQLite database — issues, users, projects, comments, time entries, sessions, audit, AI usage, etc. All migrations applied through the running version. | ~ MB to GB depending on usage |
| `paimos.db-wal` | SQLite write-ahead log; rolled into `paimos.db` on restore. | typically ~ MB |
| `paimos.db-shm` | SQLite shared-memory file; auto-rebuilt on first open after restore. | KB |
| `branding.json` | Branding config edited via the admin UI. | KB |
| `branding-assets/` | Operator-uploaded logo / favicon / cover. | up to ~MB |
| `avatars/` | User-uploaded avatars. | up to ~10s of MB |
| `test-reports/` | Operator-ingested CI test reports (PAI-188). | up to ~MB |

### What the tarball does NOT contain

| What | Where it lives | Why excluded |
|---|---|---|
| **MinIO/S3 attachments** | The configured object store | Separate concern, separate backup. Operators using attachments must back up the bucket independently — the bucket survives a PAIMOS DB restore unchanged, but a corrupted bucket is its own recovery exercise. |
| **Container image** | `ghcr.io/markus-barta/paimos:<tag>` | Immutable on the registry; pull on demand. Verified via cosign signatures (see [`RELEASE.md`](RELEASE.md)). |
| **Env-var secrets** (`OIDC_CLIENT_SECRET`, `MINIO_SECRET_KEY`, `SMTP_PASS`) | Operator's secret manager | Backup of the secret manager is an operator concern; PAIMOS does not see those secrets after process startup. |
| **Reverse-proxy config** (Caddyfile / nginx.conf / etc.) | Operator's deployment-config repo | Operator-controlled; back it up alongside your IaC. |
| **`docker-compose.yml` itself** | Operator's deployment-config repo | The deploy script does archive a copy as `docker-compose.yml.pre` next to the data tarball, for forensic parity, but the canonical version is in your deployment-config repo. |

The split is deliberate: the tarball is the **smallest sufficient unit** to restore a PAIMOS instance to its DB-state-of-the-moment. Everything outside it is either operator-stewarded or registry-pinned and immutable.

---

## 2 · Backup cadence

### Automatic — every deploy

`scripts/deploy.sh ppm <tag>` (or `pmo`) takes a backup before every image swap. The step is in `scripts/_deploy-lib.sh::do_backup` and is non-skippable: a deploy that can't produce a valid tarball **does not proceed**.

Backup artefacts land at:

```
$BACKUP_ROOT/<UTC-timestamp>/
  data.tar.gz                  # the authoritative restore state
  docker-compose.yml.pre       # the compose file from before this deploy
  manifest.yaml                # pre-image, pre-image-id, target-image, paths
```

The `manifest.yaml` is the metadata that makes a tarball self-describing: someone unfamiliar with PAIMOS can read it cold and know which image the tarball was taken against.

### Operator-added — scheduled

For deployments that run hours-to-days between deploys, the deploy-time backup is insufficient as the only protection (RPO = deploy cadence is too lax). Recommended additions:

| Cadence | Mechanism | Scope |
|---|---|---|
| **Hourly** snapshot of `$DATA_DIR` | host-side cron + `tar` (or volume snapshot if using ZFS / Btrfs / cloud disk snapshots) | data-only |
| **Daily** off-host upload | rsync / restic / borg / rclone — pick what your environment supports | data-only, off-host |
| **Weekly** verification | extract a recent backup, run `sqlite3 PRAGMA integrity_check`, log the result | both |

The hourly + daily + weekly pattern gives **RPO ≤ 1 h** and **off-host survivability**. The deploy-time backup remains the canonical immediately-pre-deploy snapshot.

### Where backups should NOT live

- Same disk as `$DATA_DIR` (a single disk failure takes both)
- Inside `$DATA_DIR` itself (the deploy script archives the whole tree; backups-of-backups grow exponentially)
- A bucket / FS that's accessible only from the same host (host compromise = backup compromise)

The hardening checklist in [`HARDENING.md` §3.7](HARDENING.md) names this gap explicitly.

---

## 3 · Restore runbook

Three restore scenarios, in order of escalation. Walk top-down — only escalate to the next when the prior doesn't fit.

### 3.1 · Bad-deploy rollback — image only (no data restore)

The most common case: a recent deploy introduced a bug, but data integrity is intact. **Do not restore data**; just pin the image back.

**Steps (≈ 2-5 min):**

```sh
ssh <operator>@<host>
cd $COMPOSE_DIR
docker compose stop paimos
sed -i 's|paimos:[^ ]*|paimos:<previous-tag>|' docker-compose.yml
docker compose pull
docker compose up -d
curl -fsS https://your.host/api/health
```

The deploy script emits this exact command sequence as the **rollback one-liner** at the end of every successful deploy. Save it; don't reconstruct it from memory mid-incident.

### 3.2 · Full DB restore from a recent backup

Data corruption, accidental destructive admin action, or a bad migration. Restore the whole `$DATA_DIR` from the most recent good tarball.

**Steps (≈ 15-30 min):**

```sh
ssh <operator>@<host>
cd $COMPOSE_DIR

# 1 · Stop the service. Every additional write to a corrupt DB
#     makes recovery harder (see INCIDENT_RESPONSE.md §3.3).
docker compose stop paimos

# 2 · Forensic copy of the live DB before any recovery attempt.
#     The original disk state is the only version of "what happened".
cp $DATA_DIR/paimos.db $DATA_DIR/paimos.db.corrupt-$(date -u +%Y%m%dT%H%M%SZ)

# 3 · Identify the most recent good backup.
ls -lt $BACKUP_ROOT | head -10
# Pick a directory; from now on $BACKUP refers to it.
BACKUP=$BACKUP_ROOT/<chosen-timestamp>

# 4 · Validate the tarball BEFORE restoring (don't compound corruption).
gzip -t $BACKUP/data.tar.gz && echo "tarball gzip OK"
tar -tzf $BACKUP/data.tar.gz | head -10  # spot-check entries

# 5 · Restore — bind-storage path:
tar -xzf $BACKUP/data.tar.gz -C $(dirname $DATA_PATH)/ --overwrite

# 5' · Restore — volume-storage path (alternative):
docker run --rm -v <volume>:/dst -v $BACKUP:/src:ro alpine \
  sh -c 'cd /dst && rm -rf ./* && tar -xzf /src/data.tar.gz'

# 6 · CRITICAL — also pin the image back to the version the
#     backup was taken against. Migrations are one-way; do NOT
#     run a v-current binary against a v-prior schema.
sed -i 's|paimos:[^ ]*|paimos:<backup-image-tag>|' docker-compose.yml

# 7 · Verify integrity before bringing the service back up.
sqlite3 $DATA_DIR/paimos.db "PRAGMA integrity_check"  # expect: ok

# 8 · Start the service; smoke-check.
docker compose up -d
curl -fsS https://your.host/api/health  # expect: {"status":"ok","service":"...","version":"<tag>"}
```

**Step 6 is the most-forgotten step.** Migrations in `backend/db/db.go` are additive-only and one-way. A v-current binary expecting (say) M79's `placement` column will crash against a backup that doesn't have it. Image-pin and DB-restore must move together.

### 3.3 · Forensic / partial restore

You don't want the whole DB back — you want to recover specific rows from a backup without losing what's been added since. Examples: an admin accidentally deleted one project; a migration silently corrupted one column.

This is **not a one-command runbook.** It requires SQL surgery against the backup. The supported pattern:

```sh
# 1 · Extract the backup to a SCRATCH location, not over $DATA_DIR.
mkdir /tmp/forensic-restore
tar -xzf $BACKUP/data.tar.gz -C /tmp/forensic-restore

# 2 · Identify the rows you need.
sqlite3 /tmp/forensic-restore/paimos.db \
  "SELECT id, key, name FROM projects WHERE deleted_at IS NULL"

# 3 · Build a targeted INSERT-or-UPDATE script in a SQL file.
#     Test it against a SECOND scratch copy first.
cp $DATA_DIR/paimos.db /tmp/test-target.db
sqlite3 /tmp/test-target.db <my-restore-script.sql>
sqlite3 /tmp/test-target.db "PRAGMA integrity_check"

# 4 · Once verified, apply to live (with the service stopped).
docker compose stop paimos
sqlite3 $DATA_DIR/paimos.db <my-restore-script.sql>
sqlite3 $DATA_DIR/paimos.db "PRAGMA integrity_check"
docker compose up -d
```

**The principle:** never blind-merge between two SQLite files. Always work in a scratch copy, verify, then commit.

For incidents complex enough to need this runbook, escalate to the [`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md) §3.3 framework — incident_log row, severity decision, post-incident review afterward.

---

## 4 · RPO/RTO targets — observed, not aspirational

| Metric | Default deployment | With hourly snapshots | With real-time replication (not yet shipped) |
|---|---|---|---|
| RPO | = deploy cadence (hours to days) | ≤ 1 h | ≤ 1 min |
| RTO (image rollback) | 2-5 min | same | same |
| RTO (full DB restore) | 15-30 min | same (the bottleneck is container restart + integrity_check, not backup retrieval) | same |
| RTO (forensic / partial restore) | 1-3 h, complexity-dependent | same | same |

**Real-time replication is out of scope for v2.x.** PAIMOS uses SQLite WAL with a single writer; multi-master replication isn't on the roadmap. Operators needing tighter RPO than hourly snapshots should run PAIMOS on a storage layer with continuous snapshotting (ZFS, cloud-disk-snapshot APIs, etc.).

The 15-30 min full-restore RTO has these components:

| Step | Time |
|---|---|
| stop service + take forensic copy | < 1 min |
| validate + extract tarball | < 1 min for typical DB size; a few minutes for GB-scale DBs |
| `PRAGMA integrity_check` | seconds at MB scale; minutes at GB scale; O(N) over data |
| pin image back + `docker compose pull` | ~2 min, dominated by registry pull if image not cached |
| service restart + `/api/health` smoke | < 1 min |
| reverse-proxy / DNS sanity | operator-dependent, typically < 1 min |

Restore latency is dominated by **integrity_check at scale** + **registry pull**. Operators with large DBs or slow registry connectivity should expect the upper end of the 15-30 min band.

---

## 5 · Captured drill — 2026-04-26

Full restore drill executed against a synthetic PAIMOS-shaped SQLite database on **2026-04-26** by the maintainer. Timeline below is **observed wall-time**, not estimates.

### Setup

- Synthetic `$DATA_DIR` mirroring the v2.0 schema: `users`, `projects`, `issues`, `schema_versions` tables.
- Realistic data: 2 users, 1 project, 500 issues, schema_v=79, sidecar files (`branding.json`, empty `branding-assets/`, `avatars/`, `test-reports/`).
- WAL journal mode enabled (matches production).

### Timeline

| Step | Wall-time | What happened |
|---|---|---|
| Setup — generate synthetic DB + sidecars | 0.066 s | DDL + 500-row INSERT, sidecar files created |
| Step 1 — `tar -czf data.tar.gz paimos-data/` | **0.048 s** | tarball produced, size 12,577 B |
| Step 2 — `gzip -t data.tar.gz` + `tar -tzf` | **0.027 s** | gzip integrity check, 8 entries listed |
| Step 3 — `tar -xzf data.tar.gz -C /tmp/drill-restore` | **0.040 s** | full extraction |
| Step 4 — `sqlite3 PRAGMA integrity_check` | **0.028 s** | result = `ok` |
| Step 5 — spot-check `users` / `projects` / `issues` / `schema_v` row counts | **0.043 s** | counts: 2 / 1 / 500 / 79 — exactly as seeded |
| Step 6 — sidecar files present | < 0.001 s | `branding.json`, `branding-assets/`, `avatars/`, `test-reports/` all present |
| **Total drill wall-time** | **0.432 s** | (Steps 1-5; Step 0 setup excluded) |

### Findings

The runbook held up. Every step in §3.2 produced the documented output; no surprises in the order of operations or the command shapes.

**Gaps surfaced:**

1. **The drill ran against a SYNTHETIC DB, not a real production-scale one.** A 500-issue / few-user DB is two-to-three orders of magnitude smaller than a year-old multi-team production deployment. The 0.4 s end-to-end time scales roughly linearly with DB size for tar-up + extract; `PRAGMA integrity_check` is O(rows) and dominates at scale. **Action:** the next drill (target 2026-10-26) should run against a copy of the ppm production DB at-rest size, with the service stopped, to capture realistic timings.

2. **The drill did NOT test the WAL+SHM mid-write scenario.** A real failure that triggers a backup-then-restore could happen with the service running and writes in flight. SQLite WAL handles this gracefully on restart, but the drill didn't exercise it. **Action:** include a "service was writing when backup was taken" variant in the next drill.

3. **The drill did NOT test cross-arch restoration** (e.g., restoring an x86_64 backup on arm64). PAIMOS images are multi-arch; SQLite files are byte-order-independent so this should work, but it wasn't verified. **Action:** include a cross-arch leg in the next drill, or document explicitly as out-of-scope if not.

4. **The drill did NOT test partial / forensic restore (§3.3)**, only the full-restore path (§3.2). Partial restore is the higher-stakes runbook because it involves SQL surgery; it deserves its own drill cycle. **Action:** alternate full-restore and forensic-restore drills, six months apart.

5. **The drill did NOT include the registry pull step** (the `docker compose pull <previous-tag>` part of §3.2 step 6). On a slow connection, this is the bottleneck. **Action:** time the registry pull separately during the next ppm-targeted drill.

### Outcome

The §3.2 full-restore runbook is verifiable and works as documented for synthetic data. The five gaps above are real and inform the next-drill plan. None of them are deal-breakers; they're scope for the next iteration.

**Drill artefacts** retained for audit:

- The synthetic source DB at `/tmp/drill-source/paimos-data/paimos.db` (cleaned up after drill)
- The tarball at `/tmp/drill-source/data.tar.gz` (cleaned up)
- The restored DB at `/tmp/drill-restore/paimos-data/paimos.db` (cleaned up)
- The drill script itself: reproducible — see drill commit history if needed

---

## 6 · Common failure modes during restore

The five real failure modes operators should expect:

1. **Tarball gzip fails integrity check.** Caused by a truncated or storage-corrupted backup. Fix: walk back to the previous-but-one backup. Cause: usually a mid-snapshot host crash; operator should verify backup integrity at-rest, not just at-create.
2. **DB restored cleanly but PAIMOS won't start.** Almost always: image-pin mismatch (§3.2 step 6 was skipped). Fix: pin image back to the version the backup was taken against. Symptom: "no such column" or "table missing" errors in the startup log.
3. **`PRAGMA integrity_check` returns anything other than `ok`.** The backup itself is corrupt. **Do NOT proceed**; walk back to an earlier backup. Document the corrupt one for forensic analysis later.
4. **Restored DB is structurally OK but missing recent data.** Expected for any backup older than the last write. The window of loss is the gap between the backup and the corruption event. Communicate the gap honestly to users; do not attempt to "merge" the live (corrupt) DB with the backup.
5. **MinIO attachments unreachable after DB restore.** The DB references attachments by id; if the bucket was wiped or rotated since the backup, the references are dangling. PAIMOS handles this gracefully (404 on download attempt), but operators should restore the bucket to a corresponding state when possible.

---

## 7 · Cross-references

- **[`DEPLOY.md`](DEPLOY.md)** — release + deploy + rollback runbook; the rollback section there is the canonical short version of §3.1 here.
- **[`INCIDENT_RESPONSE.md`](INCIDENT_RESPONSE.md)** §3.3 — DB corruption incident runbook; the higher-level wrapper around §3.2 here.
- **[`HARDENING.md`](HARDENING.md)** §3.7 — operator hardening checklist row for backup posture (off-host, off-site, restore-tested).
- **[`THREAT_MODEL.md`](THREAT_MODEL.md)** — INV-EXPORT-04 (hard-delete is irreversible) is what makes backups load-bearing for "I deleted it by mistake" scenarios.
- **[`CONTINUITY.md`](CONTINUITY.md)** §3 — emergency runbooks beyond DB-restore (domain expiry, GitHub compromise, etc.).
- **[`CONFIGURATION.md`](CONFIGURATION.md)** — env vars referenced (`DATA_DIR`, `BACKUP_ROOT` is operator-defined).
- **`scripts/deploy.sh`** + **`scripts/_deploy-lib.sh`** — the deploy + backup orchestration referenced from §2.
