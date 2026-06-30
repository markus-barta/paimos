# PAI-625 Local Implement-This Demo

This is the local, non-production harness used for the PAI-625 workstation
end-to-end pass. It exercises the "Implement this" runner against a tiny repo
with a safe local deploy target.

## Demo Repo

The demo repository lives at:

```sh
/Users/mba/Code/paimos/.paimos/cache/pai-625-demo
```

It has three useful commands:

```sh
cd /Users/mba/Code/paimos/.paimos/cache/pai-625-demo
npm test
npm run deploy:local
cat .deploy/local.txt
```

`npm run deploy:local` only writes `.deploy/local.txt`; it has no production
side effects.

## Report-Back Only Runner

Use this to prove a real Claude Code edit, test execution, run status update,
and auto-comment without deploying:

```sh
paimos --instance local-dev --agent-name claude-pai625-demo \
  run-agent watch \
  --project P625D \
  --repo-root /Users/mba/Code/paimos/.paimos/cache/pai-625-demo \
  --yes \
  --test-exec 'npm test'
```

Trigger a safe issue from the UI or API:

```sh
paimos --instance local-dev curl \
  -X POST /api/issues/P625D-<issue-number>/implement \
  -H 'Content-Type: application/json' \
  -d '{}'
```

Expected result:

- run transitions `queued -> running -> tests_passed`
- `tests_summary` includes `npm test`
- `version` is read from `VERSION`
- no log attachment is created unless `--attach-logs` is passed

## Local Deploy Runner

Use this after report-back-only has passed:

```sh
paimos --instance local-dev --agent-name claude-pai625-demo-deploy \
  run-agent watch \
  --project P625D \
  --repo-root /Users/mba/Code/paimos/.paimos/cache/pai-625-demo \
  --yes \
  --test-exec 'npm test' \
  --allow-deploy \
  --deploy-exec 'npm run deploy:local' \
  --yes-deploy
```

Trigger with the explicit non-production deploy target:

```sh
paimos --instance local-dev curl \
  -X POST /api/issues/P625D-<issue-number>/implement \
  -H 'Content-Type: application/json' \
  -d '{"deploy_target":"local-dev"}'
```

Expected result:

- run transitions `queued -> running -> deployed`
- `deploy_target` is `local-dev`
- `tests_summary` includes `npm test`
- `.deploy/local.txt` records the deployed version
- the auto-comment includes version, deploy target, tests, and device id

## Safety Checks

Run without `--yes` and answer `n` to prove a declined job cancels before
spawn:

```sh
paimos --instance local-dev --agent-name claude-pai625-confirm \
  run-agent watch \
  --project P625D \
  --repo-root /Users/mba/Code/paimos/.paimos/cache/pai-625-demo
```

Run two watchers against the same project to prove only one wins the atomic
claim; the loser should log that the run was already claimed.

Keep `--attach-logs` off unless deliberately testing log attachment behavior.
The default should not upload command output as a ticket attachment.

## Cleanup

Demo tickets should be soft-deleted when they are no longer useful:

```sh
paimos --instance local-dev issue delete P625D-<issue-number> --yes
```

Leave the demo repository itself in a clean git state:

```sh
git -C /Users/mba/Code/paimos/.paimos/cache/pai-625-demo status --short
```
