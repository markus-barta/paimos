# PAIMOS Release + Deploy

Single-source-of-truth rule: **the git tag is the version.** Everything else
(`VERSION` file, `docs/CHANGELOG.md`, Docker tags, running containers) is
derived from or pinned to it.

> Bringing a fresh PAIMOS deployment online? Walk
> [`HARDENING.md`](HARDENING.md) before exposing it to users. This
> document covers release / rollback / image lifecycle; the hardening
> guide covers TLS / auth / files / audit / secrets / backups against
> the [`THREAT_MODEL.md`](THREAT_MODEL.md) invariants.

Two instances, both pulling from the same registry:

| Instance | Host                  | Auth                | Storage           |
| -------- | --------------------- | ------------------- | ----------------- |
| **ppm**  | `pm.barta.cm` (csb1)  | SSH key (`csb1`)    | named volume      |
| **pmo**  | `pm.bytepoets.com`    | SSH password (env)  | bind mount        |

Registry: `ghcr.io/markus-barta/paimos`. Images produced per-commit on `main`
(`:latest`, `:sha-<short>`) and per semver tag (`:X.Y.Z`, `:X.Y`, `:X`).
CI source of truth: `.github/workflows/ci.yml`.

---

## The four commands

```
just release [patch|minor|major|x.y.z]   # cut a release (VERSION + CHANGELOG + tag + push)
just deploy-ppm [tag]                    # deploy a tag to ppm (default: latest)
just deploy-pmo [tag]                    # deploy a tag to pmo (default: latest)
just doc-sync [tag]                      # file a "doc/site sync follow-up" ticket in PAIMOS
```

Plus a read-only status helper:

```
just status                              # last 5 tags + commits since last tag
```

The standard sequence after a feature lands on `main` is **release → deploy
→ doc-sync**. The first two cut and roll out the new build; `doc-sync`
files a single PAIMOS ticket with a four-item checklist (README, `docs/`,
the `../paimos-site` repo, brand/screenshots) so the user-facing surfaces
don't drift out of sync with the code. `release.sh` prints the
`just doc-sync` reminder as part of its "Next:" output to make the step
hard to miss.

## `just release`

1. Refuses to run if working tree dirty, not on `main`, or not in sync with
   `origin/main`.
2. If no argument: dumps commits since the last tag (all + runtime-only) and
   exits. Look at the output, decide patch/minor/major, re-run.
3. Computes the new version from the last git tag (not from `VERSION` —
   that's why `VERSION` can never drift again).
4. Updates `VERSION`, prepends a draft entry to `docs/CHANGELOG.md` pre-seeded
   from commit subjects, opens `$EDITOR` so you can clean it up before
   committing. If an entry for that version already exists, just its date
   is refreshed (needed once, for the 1.2.2–1.5.1 drift catch-up).
5. Commits (`release: vX.Y.Z`), tags `vX.Y.Z`, pushes both.
6. Polls ghcr for up to 10 minutes until the new image tag is visible, then
   prints the next-step deploy commands.

**Picking the level (what the AI looks at):** if `git log vLAST..HEAD` contains
commits that touch files under `backend/` or `frontend/src/`, lean **minor**.
Breaking API or schema changes → **major**. Pure docs / brand / scripts →
**patch**. The `release.sh` no-arg output breaks this down for you.

## `just deploy-{ppm,pmo}`

For each instance:

1. Resolves the tag (arg or `git tag --sort=-creatordate | head -1`). Aborts
   if that image isn't on ghcr yet.
2. SSH pre-flight: reads current image + image digest from the running
   container, aborts if target == current.
3. `docker compose stop <service>`.
4. Backup:
   - **bind storage** → `tar -czf` on the bind path.
   - **volume storage** → throwaway `alpine` container tarring the volume.
5. Validate: `gzip -t`, count archive entries, verify the DB file is
   present.
6. Write a `manifest.yaml` next to the tarball (pre-image, pre-image-id,
   target-image, paths).
7. `sed` the compose image pin from old → new tag, `docker compose pull`,
   `up -d`.
8. Tail logs for 5 seconds (surfaces migration output + "server starting").
9. External `curl /api/health` from your laptop, up to 24s of retries.

**On any failure in steps 2–8**, the script prints the exact rollback
command for the host and exits non-zero. It does **not** auto-rollback.

Artifacts produced on the remote host:

```
$BACKUP_ROOT/<UTC-timestamp>/
  data.tar.gz                  # authoritative rollback state
  docker-compose.yml.pre       # compose file from before the deploy
  manifest.yaml                # pre/post images, ids, paths
$COMPOSE_DIR/
  docker-compose.yml.bak.<ts>  # compose file before the sed edit
```

## Per-instance config

Each instance has a small conf file in `scripts/`:

- `scripts/deploy.ppm.conf` — 10 lines: ssh target, compose dir, service,
  volume name, DB filename, backup root, instance URL.
- `scripts/deploy.pmo.conf` — same shape, plus password-auth pointers into
  `~/Secrets/PMO/`.

If you spin up a third instance, copy one of these and change the values.

## Rollback (if a deploy goes sideways)

Each successful deploy prints the rollback one-liner as the last step. It
restores the tarball and repins compose to the previous image. Paraphrased:

> For full restore scenarios beyond a recent-deploy rollback (forensic /
> partial restore, captured drill timing, RPO/RTO targets, common
> failure modes), see [`BACKUP_RESTORE.md`](BACKUP_RESTORE.md).

```bash
# on the host
cd $COMPOSE_DIR
docker compose stop <service>
# bind storage:
tar -xzf <backup>/data.tar.gz -C $(dirname $DATA_PATH)/ --overwrite
# or volume storage:
docker run --rm -v <volume>:/dst -v <backup>:/src alpine \
  sh -c 'cd /dst && rm -rf ./* && tar -xzf /src/data.tar.gz'
sed -i 's|paimos:[^ ]*|<previous-image>|' docker-compose.yml
docker compose up -d <service>
```

**Critical:** schema migrations in `backend/db/db.go` are one-way. Rolling
back the image without restoring the tarball may leave the old binary
staring at a schema it doesn't understand. Always restore the DB too.

## What this replaces

- Any out-of-repo `just deploy-ppm` scripts on your laptop: move to
  `scripts/deploy.sh ppm`.
- Ad-hoc `ssh csb1 'docker compose pull && up -d'`: replaced by the
  backup-first flow in `scripts/_deploy-lib.sh`.
- Manual `VERSION` + `CHANGELOG` edits: replaced by `just release`, which
  does them atomically with the tag.

## What this deliberately leaves out

- **Staging environment.** There isn't one. ppm acts as a soft canary
  because you use it yourself before PMO sees a tag.
- **MinIO attachment snapshots.** A version bump doesn't touch stored
  attachments, so the backup is DB + data dir only. If you need bucket
  snapshots, `docker exec minio mc mirror …` handles it separately.
- **Secrets rotation, Cloudflare config, TLS certs.** Out of scope for
  image bumps; treat as infra work.
