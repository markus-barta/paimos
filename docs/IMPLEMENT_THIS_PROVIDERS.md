# Implement-This Provider Plan

PAI-605/608 introduced the first execution path: a web UI action creates an
`agent_runs` row, and a developer-owned `paimos run-agent watch` process claims
the run and executes a local command in a repo checkout. PAI-629 and PAI-630
extend that into an explicit provider/action model instead of a single generic
"Implement this" button.

The key product rule is: **execution location and model/provider choice must be
visible, auditable, and capability-gated before a run starts.**

## Current Baseline

The current runner is a local-shell worker:

- The server creates and owns the run record, lifecycle validation, terminal
  immutability, comments, and visibility rules.
- The workstation owns repo access, spawned command execution, tests, and
  optional deploy.
- The default command is Claude Code print mode; `--exec` can point at Codex or
  another local tool.
- Deploy is triple-gated by runner flags, deploy command, and per-run
  `deploy_target`.

This is deliberately not the same as the existing `backend/ai` provider layer.
That layer currently powers AI text optimization through admin-configured
OpenRouter settings. Implement-this providers can reuse its secret/config
surface for hosted model calls, but they must not inherit workstation shell or
deploy capabilities by implication.

## Provider Classes

### Local CLI Providers

Examples: Claude Code, Codex CLI, opencode, or another developer-installed
agent command.

- Execution: local developer workstation.
- Repo access: local checkout selected by `--repo-root`.
- Secrets: local shell environment, so use the same caution as any developer
  terminal.
- Output mode: may edit files directly, run tests, and optionally deploy when
  the runner's deploy gates are satisfied.
- First-class actions: `Do this with Claude`, `Do this with Codex`.

The existing `paimos run-agent watch --exec ...` is the implementation
foundation. The next step is to make provider identity explicit instead of
encoded only in `agent_name` or a free-form command.

### Local Model Providers

Examples: Ollama, LM Studio, llama.cpp, or an in-house local inference service.

- Execution: local developer workstation or trusted LAN host.
- Repo access: should be explicit; v1 can draft a plan/patch, while a local
  runner applies it only after the operator chooses that action.
- Secrets: generally none for local inference, but prompts may include private
  project context.
- Output mode: plan, patch, or controlled local edit depending on advertised
  capabilities.
- First-class actions: `Draft with local model`, later `Do this with local
  model` if the worker can safely edit and test.

Local model support should use the same provider names already reserved in the
AI settings surface where possible, but implement-this must still model the
worker capabilities separately from text-optimization availability.

### Hosted Model Providers

Example: OpenRouter through the existing encrypted `ai_settings` API key and
admin-selected model.

- Execution: PAIMOS server sends a model request; no developer workstation is
  implied.
- Repo access: no direct repo checkout, no local shell, no deploy.
- Secrets: provider key comes from admin AI settings; project prompt content is
  sent to the hosted provider and must be intentionally scoped.
- Output mode: v1 should produce a plan, suggested patch, or ticket comment.
  Applying the patch should be a separate explicit action.
- First-class action: `Draft with OpenRouter`.

Hosted providers should not use the local runner's deploy path. If a later
server-side patch flow exists, it needs its own permission and review boundary.

## API And Data Model

Add explicit provider/action fields to `agent_runs` while keeping existing rows
valid:

- `action_key`: stable UI/requested action, such as `claude_cli.implement`,
  `codex_cli.implement`, `openrouter.draft`, or `local_model.draft`.
- `provider_kind`: stable provider class, such as `local_cli`,
  `local_model`, or `hosted_model`.
- `provider_id`: specific provider, such as `claude_cli`, `codex_cli`,
  `openrouter`, `ollama`, or `lmstudio`.
- `provider_label`: display label captured at run creation for audit history.
- `model`: optional model identifier for hosted/local-model providers.
- `run_mode`: `edit`, `draft`, `patch`, or `deploy`.

`agent_name` remains executor attribution. It should answer "who reported this
run?", not "which provider did the requester choose?"

`POST /api/issues/{id}/implement` should accept an action key:

```json
{
  "action_key": "claude_cli.implement",
  "device_id": "dev-mba-mbp",
  "deploy_target": "ppm"
}
```

The default remains backward compatible: an omitted action key maps to the
current local Claude action while the old single-button UI still exists.

The latest issue-level AI work status should include the provider/action fields
so list badges, filters, and history can explain what is running without opening
the raw run record.

## Runner Capabilities

Online runners should advertise a structured capability set in addition to the
current `can_implement=1` bit:

```json
{
  "device_id": "dev-mba-mbp",
  "repo_root": "/Users/mba/Code/paimos",
  "actions": [
    {
      "action_key": "claude_cli.implement",
      "provider_id": "claude_cli",
      "label": "Claude Code",
      "run_modes": ["edit"],
      "can_test": true,
      "can_deploy": true
    },
    {
      "action_key": "codex_cli.implement",
      "provider_id": "codex_cli",
      "label": "Codex CLI",
      "run_modes": ["edit"],
      "can_test": true,
      "can_deploy": true
    }
  ]
}
```

The UI should filter actions by project, issue type, and online runner
capabilities before it offers an edit/deploy action. Draft actions can remain
available when no runner is online if the server has the required provider
settings.

## UI Model

Replace the ambiguous single action with explicit choices as soon as there is
more than one configured action:

- Row menu: `Do this with Claude`, `Do this with Codex`, `Draft with
  OpenRouter`, `Draft with local model`.
- Issue detail panel: same action list, plus provider-aware device/deploy
  controls and the existing run timeline.
- Badges: include provider label in the tooltip and run history, for example
  `Claude running`, `Codex tests ok`, or `OpenRouter draft ready`.

The existing `Implement this` button can remain as the one-action shorthand on
single-runner installs.

## Security And Audit Rules

- Keep project-editor gating on run creation and requester/admin gating on run
  reads and updates.
- Keep status compare-and-set, stale-running reaping, active-run uniqueness,
  and terminal immutability server-side.
- Capture requested action, provider, model, device, deploy target, version,
  tests, and error on the run record.
- Do not attach local command logs by default; preserve the explicit
  `--attach-logs` opt-in.
- Do not send repo secrets or shell environment to hosted providers.
- Redact or bound hosted-provider prompts; prefer retrieved context over raw
  whole-repo dumps.
- Treat hosted-provider patches as suggestions until a separate apply action
  exists.
- Keep deploy local-runner-only until a reviewed server-side deploy mechanism
  exists.

## Rollout

Status as of PAI-629: rollout steps 1-3 are implemented for local CLI actions.
Local model and hosted model draft/apply modes remain future work.

1. **Schema and API compatibility.** Add nullable provider/action fields,
   default omitted action requests to the current Claude local CLI action, and
   expose fields in OpenAPI. **Done in PAI-629.**
2. **Runner capability advertisement.** Let `paimos run-agent watch` advertise
   one or more local CLI actions; add tests for action matching and targeted
   device claims. **Done in PAI-629 for one local CLI action per runner
   connection; multiple runner connections can expose multiple actions.**
3. **Claude and Codex UI actions.** Replace the generic menu when both actions
   are available, while preserving the old single-action button for simple
   installs. **Done in PAI-629 for issue detail and row actions.**
4. **Local model draft mode.** Add a local-model action that creates a plan or
   patch without autonomous deploy.
5. **OpenRouter draft mode.** Reuse encrypted admin AI settings to produce a
   plan/comment/patch suggestion. Keep patch application explicit and separate.

## Acceptance Tests

- Backend migration preserves existing `agent_runs` and defaults old requests.
- `POST /implement` rejects unavailable action/device combinations.
- Runner claims include device and action, and status PATCHes cannot retarget a
  run to a different provider/action after creation.
- OpenAPI documents provider/action fields on create, run detail, and issue AI
  work status.
- UI tests cover single-action shorthand, multi-action menu labels, and provider
  badges.
- Hosted-provider tests prove no deploy path and no local shell execution.
- Threat-model docs are updated when hosted draft/apply or server-side patching
  lands.
