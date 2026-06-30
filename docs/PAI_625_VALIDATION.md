# PAI-625 Validation Record

Date: 2026-06-30

This records the current PAI-625 audit evidence for the Implement-this runner.
It is intentionally conservative: local branch evidence is separated from live
`ppm` production proof.

## Verdict

v4.6.3 was not fully production-quality for the PAI-605 Implement-this runner.
The third audit found real correctness and auditability gaps:

- a stale `running` run with `started_at=NULL` could wedge future runs
- report-back-only `tests_passed` runs looked complete but lacked finished/test
  evidence and kept the UI polling
- non-status run PATCHes could race terminal immutability
- the default `--exec "claude"` runner path did not pass issue context and could
  open an interactive TUI
- the UI had no deploy-target path for staging/local deploy
- open-run claims did not record the actual claiming device
- purge-after-soft-delete CLI cleanup used a command path that 404ed

The working branch fixes or documents each finding and adds regression coverage.
The branch is locally green. The remaining unproved item is the final live
production click/deploy proof after these branch fixes are committed, pushed,
deployed to `ppm`, and triggered from the web UI.

## Findings Filed

- PAI-626: NULL-started running agent run can wedge Implement this
- PAI-627: AgentRunPanel polls forever after tests_passed result
- PAI-628: non-status run PATCH can race terminal immutability
- PAI-633: run-agent does not pass issue context to Claude by default
- PAI-634: tests_passed run lacks finished_at and test evidence
- PAI-635: Expose deploy target in Implement-this UI
- PAI-637: CLI trash cleanup: purge-after-soft-delete command path 404s
- PAI-638: open-run claims do not stamp the actual runner device

Requested follow-ups were also filed:

- PAI-629: Generalize Implement this into Claude and Codex issue actions
- PAI-630: Plan AI worker providers for local models and OpenRouter
- PAI-631: Surface AI work status per issue
- PAI-632: Create local demo project for Implement-this end-to-end testing

## Local Branch Evidence

Backend:

- `go test ./... -count=1` passed.
- Targeted runner race suite passed:
  `go test -race ./cmd/paimos -run 'TestAgentRunner(QueuedRunIDs|ClaimLost|Deploy|DefaultDoesNotAttachLog|TestExec)|TestClaudeDefault|TestDefaultSpawn|TestDeleteIssueByRef|TestIssueDeleteCLI_PurgeAlreadyTrashedNumericRef' -count=1`
- Targeted handler race suite passed:
  `go test -race ./handlers -run 'TestImplement|TestPatchAgentRun|TestIssueResponsesIncludeLatestAIWorkStatus|TestIssueListV2AIWorkStatusFilterAndSort|TestAgentRun' -count=1`
- `jq empty backend/handlers/openapi.json` passed.
- `git diff --check` passed.

Frontend:

- `npm test -- --run` passed: 75 files, 337 tests.
- `npm run typecheck` passed.
- `npm run build` passed.

Regression coverage anchors:

- stale NULL-started reaper: `TestImplementReapsRunningWithNullStartedAt`
- `tests_passed` finished timestamp: agent run tests around report-back-only
  lifecycle
- non-status PATCH compare-and-set: agent run PATCH tests
- device claim stamping/retargeting: agent run PATCH and runner tests
- Claude default prompt mode: `TestClaudeDefaultIsNonInteractivePromptMode`
- test summary and deploy gates: `TestAgentRunnerTestExec*`,
  `TestAgentRunnerDeploy*`
- catch-up and missed queued runs: `TestAgentRunnerQueuedRunIDs*`
- issue AI work projection: `TestIssueResponsesIncludeLatestAIWorkStatus`
- AI work filter/sort: `TestIssueListV2AIWorkStatusFilterAndSort`
- frontend AI row badge and run-panel polling/deploy-target tests
- purge-after-trash CLI cleanup tests:
  `TestDeleteIssueByRef_PurgeAlreadyTrashedNumericRef*`

## End-To-End Evidence

Already proven before this validation record:

- A live `ppm` v4.6.3 UI click created a throwaway run that the local runner
  claimed and completed with real Claude Code. That proof also exposed the
  `finished_at`/`tests_summary` gaps now fixed on the branch.
- The local demo repo at `.paimos/cache/pai-625-demo` proved report-back-only
  and local-deploy flows with real Claude Code and a safe deploy command.

Durable demo instructions live in `docs/PAI_625_DEMO.md`:

- report-back-only runner command
- local deploy runner command
- explicit non-production `deploy_target`
- confirmation, two-runner, attach-log, and cleanup checks

## Provider And Status Follow-Ups

`docs/IMPLEMENT_THIS_PROVIDERS.md` records the PAI-629/PAI-630 design:

- explicit `Do this with Claude` and `Do this with Codex` actions
- local CLI, local model, and hosted OpenRouter provider classes
- provider/action fields for future `agent_runs`
- runner capability advertisement
- hosted-provider safety boundary: draft/patch/comment only, no shell or deploy

The branch also adds issue-level AI work status derived from latest agent runs,
including list-row badges, filters/sort, and run-history links.

## Remaining Live Gate

This branch has not been committed, pushed, or deployed. Current `ppm` is still
v4.6.3 until an explicit commit/push/deploy is approved.

PAI-625 cannot be marked 100% complete until the deployed build is proven from
the live UI:

1. Deploy this branch to `ppm` or an approved staging target.
2. Start a local runner with the documented default Claude command and
   `--test-exec`.
3. Click the live web UI action on a safe throwaway ticket.
4. Confirm queued -> running -> tests_passed/deployed in the UI with version,
   tests summary, device id, and auto-comment.
5. Prove the local/staging deploy target from the UI path.
6. Clean up the throwaway ticket through soft-delete and purge.

## Approved Live-Proof Checklist

After explicit approval to ship this branch, use the normal deploy runbook in
`docs/DEPLOY.md`. The checkout is expected to be on `main`; for a canary of the
exact pushed commit:

```sh
# 1. Commit and push the reviewed branch.
git status --short
git add \
  backend/cmd/paimos \
  backend/handlers \
  backend/models \
  frontend/src \
  docs/AGENT_INTEGRATION.md \
  docs/IMPLEMENT_THIS_PROVIDERS.md \
  docs/PAI_625_DEMO.md \
  docs/PAI_625_VALIDATION.md
git commit -m "fix implement-this runner audit findings"
git push origin main

# 2. Wait for CI to publish the immutable main-commit image, then preflight.
short_sha="$(git rev-parse --short HEAD)"
just deploy-ppm-preflight "sha-${short_sha}"

# 3. Deploy the pushed image and verify the public health version.
just deploy-ppm "sha-${short_sha}"
curl -fsS https://pm.barta.cm/api/health
paimos --instance ppm doctor
```

Then run the live Implement-this proof:

```sh
paimos --instance ppm --agent-name claude-pai625-live \
  run-agent watch \
  --project PAI \
  --repo-root /Users/mba/Code/paimos/.paimos/cache/pai-625-demo \
  --yes \
  --test-exec 'npm test'
```

Use the signed-in web UI to create or open a safe throwaway ticket and click the
issue action. The fixed build should show queued/running/result status, a test
summary, version, device id, and the auto-comment. For the deploy path, use the
same safe demo repo with the UI deploy target set to `local-dev` and the runner
started with:

```sh
paimos --instance ppm --agent-name claude-pai625-live-deploy \
  run-agent watch \
  --project PAI \
  --repo-root /Users/mba/Code/paimos/.paimos/cache/pai-625-demo \
  --yes \
  --test-exec 'npm test' \
  --allow-deploy \
  --deploy-exec 'npm run deploy:local' \
  --yes-deploy
```

Do not use a production deploy command for this proof. `npm run deploy:local`
only writes `.deploy/local.txt`.
