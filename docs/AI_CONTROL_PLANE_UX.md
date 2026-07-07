# AI Control Plane UX

Design spec for `PAI-648`, under epic `PAI-646`.

The AI control-plane work turns the current AI and agent features into one
operational workflow. The user should not have to know whether a capability
started in the text-action menu, project agents, admin AI settings, or the
Implement-this runner. The product rule is:

> Execution location and model/provider choice must be visible, auditable, and
> capability-gated before a run starts.

The latest imagegen concept in
[`docs/brand/screenshots/paimos-ai-workbench-coherent-ux.png`](brand/screenshots/paimos-ai-workbench-coherent-ux.png)
is the visual direction: one issue workspace with a backlog table, a right-side
AI workbench, visible provider/model/effort/prompt/context controls, PPM
knowledge suggestions, and a bottom agent-activity rail. The earlier
[`docs/brand/screenshots/paimos-ai-control-plane-ux.png`](brand/screenshots/paimos-ai-control-plane-ux.png)
remains a drawer/panel exploration. Both are concept mockups, not promises that
every future-facing patch/review affordance shown there is implemented.
Knowledge and Project Agents feed the same run controls.

## Current Surface Map

| Surface | Current files | Current role | Target role |
|---|---|---|---|
| Text and issue AI menu | `frontend/src/components/ai/AiActionMenu.vue`, `frontend/src/composables/useAiAction.ts` | Runs registered AI actions with action/sub-action/field/text/params. | Entry point for action choice plus profile, effort, prompt preset, and context pack. |
| AI result review | `AiActionResultModal.vue`, `AiSurfaceFeedback.vue`, `AiResultStrip.vue` | Shows action-specific result bodies and basic metadata. | Shows result plus resolved execution metadata and safe request ID on failures. |
| Issue run panel | `frontend/src/components/issue/AgentRunPanel.vue` | Creates Implement-this runs from runner capabilities and shows run lifecycle. | AI Run panel for selected agent, runner/provider, context, deploy gate, and run history. |
| Row actions | `IssueList.vue`, `IssueRowActions.vue` | Offers provider-aware Implement-this actions when runners advertise them. | Compact launch affordance that mirrors the same choices as issue detail. |
| Project agents | `ProjectAgentsTab.vue`, `ProjectAgentsSection.vue` | Declares and edits project agents. | Operational launchpad: agent readiness, rendered artifact status, runner status, recent runs, and session commands. |
| AI settings | `SettingsAITab.vue`, `SettingsAIPromptsTab.vue` | Admin-global provider/model/prompt configuration. | Admin defaults for profiles, models, effort, and prompt catalog health. |
| Backend action dispatcher | `backend/handlers/ai_action.go`, `ai_action_helpers.go`, action handlers | Resolves global AI settings and records action audit rows. | Resolves execution options and records safe provenance metadata. |
| Agent run API | `backend/handlers/agent_runs.go`, `cmd/paimos/cmd_run_agent.go` | Creates local runner jobs and reports lifecycle. | Accepts selected agent/context/options for local runners and hosted/local draft providers, with safe provenance. |

## Target Objects

### AI Control

The reusable control set for any AI action or agent run:

- **Profile / Model**: user-facing profile such as Fast, Balanced, or Deep,
  optionally revealing the resolved provider/model.
- **Effort**: low, standard, or deep reasoning effort where the selected
  provider supports it.
- **Prompt**: Default or a project knowledge prompt preset.
- **Context**: issue-only, issue plus project knowledge, retrieved context, or
  repo-aware bundle.
- **Agent**: optional project agent that scopes lane, rules, and bootstrap
  context.
- **Runner / Provider**: local CLI runner, local model draft, or hosted model
  draft.
- **Deploy**: local-runner-only gate, never available for hosted draft runs.

The same labels appear in menus, detail panels, run history, and audit metadata.
Do not invent a separate vocabulary per surface.

### Knowledge Prompt Presets

`PAI-652` uses regular project knowledge entries as selectable prompt presets.
Only `memory`, `runbook`, and `guideline` entries can be exposed this way.
Authors opt in by adding `metadata.ai_prompt_preset`:

```json
{
  "ai_prompt_preset": {
    "enabled": true,
    "label": "Spec Writer",
    "status": "active",
    "actions": ["spec_out"]
  }
}
```

The AI menu lists active in-scope presets as `kb:<type>:<slug>`. Action
responses and audit metadata record the selected reference with its content
revision, for example `kb:memory:spec_writer@a1b2c3d4e5f6`, plus the safe label.
The prompt body is never returned in `/api/ai/execution-options`, action
responses, or parser-error payloads.

Invalid, archived, empty, oversized, unauthorized, or out-of-scope presets are
rejected by the backend resolver. The frontend also disables menu actions that
do not match the currently selected preset's `actions` list.

`PAI-662` adds `knowledge_suggestions` to the same execution-options catalog.
The issue AI Workbench renders those safe refs as compact choices: prompt-ready
entries set the Prompt selector, while regular knowledge entries switch Context
to Project knowledge. The Workbench still receives no knowledge body text.

### Context Packs

`PAI-653` makes context explicit in the same execution-options catalog as
profile, effort, and prompt:

- `issue` / "Issue only" — the current issue and selected field.
- `knowledge` / "Project knowledge" — issue context plus active project
  memory, runbooks, and guidelines.
- `retrieve` / "Retrieved context" — issue context plus bounded mixed
  retrieval from the project context index.
- `repo` / "Repo-aware bundle" — issue context plus retrieval and uploaded code
  anchors. This option is listed only when the project has anchor context.

The backend enforces the pack choice, assembles the prompt-side context with a
bounded byte budget, and reports only safe provenance in the action envelope:
`context_pack`, optional `context_pack_label`, `context_sources`, and
`context_truncated`. Context bodies, retrieved snippets beyond the compact
prompt pack, local file contents, secrets, and raw prompt text are not returned.

### Implement-this Agent Selection

`PAI-654` bridges project agents into Implement-this run creation. The run API
accepts an optional `agent_name`; the backend validates that the named agent is
declared on the issue's project and stores it on `agent_runs` when the run is
queued. Because `project_agents` currently has no disabled/archive state,
presence on the project is the active declaration.

The issue run panel loads project agents into the same control row as provider,
runner, and deploy target. The project issue list has one compact agent picker
for row launches, so row actions can carry the selected project agent without
adding a selector to every row.

### AI Run Panel

Issue detail owns the full run workflow. `AgentRunPanel.vue` should evolve into
the AI Run panel:

- Header: selected action label and current status.
- Control row: profile, effort, prompt, context, agent, runner/provider.
- Capability row: online runner, can test, can deploy, hosted/local draft mode.
- Primary command: Run, Draft, or Implement depending on selected provider mode.
- Timeline: queued, claimed, working, tests/report, deploy if selected.
- History: previous AI actions and agent runs with the same provenance fields.

On the row list, keep the action compact. If more than one provider/action is
available, the row action opens a small chooser that uses the same labels and
defaults as the AI Run panel.

### AI Control Drawer

The drawer is the detailed configuration view. On desktop it appears as a
right-side drawer from issue detail. On narrow screens it becomes a bottom
sheet. It is not a separate destination page.

The drawer contains:

- Profile and model details.
- Effort selector with provider capability hints.
- Prompt preset selector from project knowledge.
- Context pack selector with provenance and truncation preview.
- Agent selector with rendered artifact status.
- Runner/provider health and capability explanation.

The issue should remain visible while the drawer is open. The drawer changes
configuration; it does not hide the work item.

### Project Agents Launchpad

Project Agents should remain editable, but it also needs to explain readiness:

- Declared agents with active/inactive state.
- Rendered skill or artifact revision status.
- Compatible adapter or harness.
- Online runner availability for that agent.
- Recent runs and failed handoffs.
- Commands for `paimos skill render`, `paimos session start`, and
  `paimos run-agent watch` when relevant.

The launchpad should not claim that an agent is runnable unless a matching
runner/provider capability is present.

## States

All AI control surfaces must cover these states.

| State | User-facing behavior | Backend/source signal |
|---|---|---|
| No provider configured | Controls disabled with a route to admin AI settings for admins; non-admins see unavailable state. | AI status/settings unavailable. |
| Provider offline | Keep selections visible, disable Run, show retryable provider status. | Provider health or runner heartbeat missing/stale. |
| Model missing | Profile/model row requires a valid model before Run. | Resolver rejects missing/unavailable model. |
| Prompt missing | Prompt row falls back to Default or blocks only when the selected preset is invalid. | Knowledge prompt preset absent, archived, or out of scope. |
| Context too large | Show truncation before Run; allow narrower context pack. | Context assembler budget exceeded or truncated. |
| Run queued | Timeline active on Queued; controls lock except cancel when supported. | `agent_runs.status=queued` or action activity started. |
| Run active | Timeline active on Claimed/Working; show runner/provider and request ID. | `running` status, AI action activity, or polling state. |
| Completed | Result card includes model/profile, effort, prompt ref, context pack, agent, runner/provider, tokens if available. | Action envelope or run terminal status. |
| Failed | Safe error, request ID, retry affordance. No raw model output, prompts, secrets, or local environment values. | Problem Details, run error summary, audit row. |

## Safe Provenance

Every AI action and run should record the same metadata shape:

- request/run ID
- action key or run action key
- provider/profile/model
- effort
- prompt preset reference and revision
- context pack name and truncation/provenance summary
- project agent name when selected
- runner/device for local CLI runs
- status, timestamps, token counts when available

Never store or display prompt bodies, raw model output on parser failures, API
keys, shell environment, or local secret values. `PAI-647` is the safety floor
for this rule.

## Component Plan

1. `PAI-649`: add the backend options envelope and resolver.
2. `PAI-650`: define profiles and per-action defaults.
3. `PAI-651`: add compact profile and effort controls to `AiActionMenu`.
4. `PAI-652`: mark PPM knowledge entries as prompt presets and expose them to
   AI actions. Shipped for action menu presets and safe provenance.
5. `PAI-653`: add context-pack selection and provenance. Shipped for AI
   actions; agent-run handoff follows in PAI-654/655.
6. `PAI-654`: let Implement-this creation accept a selected project agent.
   Shipped for run creation, issue detail, and row-action launch payloads.
7. `PAI-655`: pass selected agent context through `paimos run-agent watch`.
   Shipped for selected-agent artifact fetch, bounded/redacted prompt context,
   child env metadata, and durable selected-agent attribution.
8. `PAI-656`: turn Project Agents into the operational launchpad. Shipped first
   slice for artifact links, compatible online adapters, runner availability,
   setup commands, and recent agent-attributed runs.
9. `PAI-657` and `PAI-658`: add draft provider modes. Shipped for
   OpenRouter Draft and Local Model Draft capabilities, run metadata,
   issue-detail controls, row quick actions, and internal provenance comments.
10. `PAI-659`: unify AI action and run history into one provenance view.
    Shipped first slice for issue activity: AI actions and agent runs now share
    action/run type, provider/profile, effort, prompt preset, context pack,
    agent, runner, timestamps, status, and safe filtering.
11. `PAI-660`: update docs, README, claim matrix, and website claims. Shipped
    for the repo docs and public-site copy that describe the current
    control-plane state without overstating autonomous repo mutation.
12. `PAI-661`: centralize AI selector defaults across actions, runs, and row
    launches. Shipped first slice through `/api/ai/execution-options`
    `selector_defaults`, consumed by `AiActionMenu`, `AgentRunPanel`, and row
    quick actions without persisting transient selector changes.
13. `PAI-662`: make PPM knowledge suggestions actionable in the AI Workbench.
    Shipped first slice through execution-options `knowledge_suggestions` and
    the issue Workbench PPM knowledge lane; suggestions can set the prompt
    preset or Project knowledge context without exposing prompt/context bodies.
14. `PAI-663`: consolidate issue detail into the coherent AI Workbench layout
    shown in the latest imagegen concept. Shipped first slice as one
    `AI Workbench` region on issue detail: selected issue context stays visible,
    run controls and PPM knowledge suggestions live together, and the unified
    AI activity feed opens directly below the controls.
15. `PAI-664`: integrate Project Agents readiness into the AI Workbench.
    Shipped first slice by surfacing selected-agent artifact status, compatible
    online adapters, and setup commands inside the issue Workbench; Project
    Agents recent-run links now deep-link back to the Workbench activity view.
16. `PAI-665`: add a draft-output review and trusted-runner handoff.
    Shipped first slice with draft review pills for no-tests/no-repo-mutation
    provenance, token/truncation metadata, `source_draft_run_id` /
    `followup_run_id` linkage, and a Workbench handoff button that creates a
    trusted local-runner follow-up.
17. `PAI-666`: add project AI defaults and policy management.
    Shipped first slice through project settings for global/scoped AI defaults
    and draft-provider policy, plus execution-options policy/default resolution
    so selectors can show whether a value came from project or inherited
    defaults.

## Copy Rules

- Use the terms Profile, Effort, Prompt, Context, Agent, Runner, Provider, Run.
- Use "Draft" for hosted/local-model output that does not edit the repo.
- Use "Implement" only for trusted local runner flows that may edit a checkout.
- Use "Deploy" only when the local runner advertises deploy capability and the
  run explicitly selects a deploy target.
- Do not call prompt presets "custom actions"; actions are registered backend
  capabilities, while prompts are selectable instruction sources.

## Acceptance Checklist

- The AI Control drawer, AI Run panel, row actions, and Project Agents launchpad
  are mapped to existing Vue components.
- No-provider, provider-offline, model-missing, prompt-missing,
  context-too-large, queued, active, completed, and failed states are defined.
- Desktop and narrow-screen behavior is specified.
- Product language distinguishes prompts, context, agents, providers, runners,
  and runs.
