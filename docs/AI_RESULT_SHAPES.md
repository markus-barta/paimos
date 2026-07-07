# AI Result Shapes

`PAI-204` reference for the frontend result strip and deep-detail viewers.

## Envelope

All action responses continue to use the existing action envelope:

```json
{
  "action": "estimate_effort",
  "sub_action": "",
  "model": "anthropic/claude-sonnet-4.5",
  "request_id": "01968f5f-....",
  "options": {
    "profile_id": "default",
    "model": "anthropic/claude-sonnet-4.5",
    "effort": "standard",
    "prompt_preset_ref": "kb:memory:spec_writer@a1b2c3d4e5f6",
    "prompt_preset_label": "Spec Writer",
    "context_pack": "knowledge",
    "context_pack_label": "Project knowledge",
    "context_truncated": false,
    "context_sources": [
      { "kind": "knowledge", "label": "Project knowledge", "count": 4 }
    ]
  },
  "result": {}
}
```

The `result` payload is action-specific. Optional counters live under
`result.counters`.

`options` is safe provenance metadata from the AI action execution resolver.
It stores IDs/names only. It must not contain prompt bodies, raw model output,
API keys, local environment values, or other secret material.

`prompt_preset_ref` is `"default"` unless the user selected a project knowledge
prompt preset. Non-default refs use `kb:<type>:<slug>@<revision>`; the optional
`prompt_preset_label` is display-only and safe to render.

`context_pack` is `"issue"` unless the user selected a broader pack. Non-issue
packs may include `context_pack_label`, compact `context_sources`, and
`context_truncated`. The action envelope must never include the assembled
context body.

## Execution Selector Defaults

`GET /api/ai/execution-options` returns `selector_defaults` so AI menus,
row launches, and the issue AI Workbench can use the same safe defaults:

```json
{
  "selector_defaults": {
    "actions": {
      "estimate_effort": {
        "action_key": "estimate_effort",
        "profile_id": "balanced",
        "profile_label": "Balanced",
        "model": "anthropic/claude-sonnet-4.5",
        "effort": "standard",
        "prompt_preset_ref": "default",
        "context_pack": "issue",
        "context_pack_label": "Issue only",
        "provider_id": "openrouter",
        "provider_label": "OpenRouter",
        "source": "global"
      }
    },
    "runs": {
      "openrouter_draft.implement": {
        "action_key": "openrouter_draft.implement",
        "provider_id": "openrouter",
        "provider_label": "OpenRouter Draft",
        "profile_id": "balanced",
        "effort": "standard",
        "prompt_preset_ref": "default",
        "context_pack": "issue",
        "source": "global"
      }
    },
    "row_launch": {
      "action_key": "claude_cli.implement",
      "provider_id": "claude_cli",
      "provider_label": "Claude Code",
      "agent_name": "codex",
      "source": "project"
    },
    "workbench": {
      "action_key": "claude_cli.implement",
      "provider_id": "claude_cli",
      "provider_label": "Claude Code",
      "agent_name": "codex",
      "source": "project"
    }
  },
  "project_policy": {
    "disable_hosted_draft": true,
    "disable_local_model_draft": false
  }
}
```

The object is IDs and labels only. It does not persist user changes, does not
include prompt bodies, and does not expose provider secrets or local endpoint
credentials.

Project AI defaults are stored on the project as global defaults plus optional
`actions`, `runs`, and `agents` scopes. Selector entries use `source:
"project"` when a project default contributes to the resolved value; otherwise
they remain `global` / inherited. Project policy can mark hosted or local-model
draft providers unavailable before a run starts.

The same endpoint also returns `knowledge_suggestions` for the issue AI
Workbench:

```json
{
  "knowledge_suggestions": [
    {
      "ref": "kb:runbook:draft",
      "type": "runbook",
      "slug": "draft",
      "title": "Draft Runbook",
      "status": "backlog",
      "revision": "a1b2c3d4e5f6",
      "suggested_use": "prompt",
      "prompt_preset": true,
      "prompt_preset_ref": "kb:runbook:draft",
      "prompt_preset_label": "Draft runbook",
      "prompt_preset_status": "active",
      "actions": ["openrouter_draft.implement"]
    }
  ]
}
```

Suggestions are also IDs, labels, status, revision, and action scope only. The
Workbench can set a prompt preset directly when `suggested_use` is `prompt`; for
context suggestions it switches the Context selector to Project knowledge.
Knowledge entry bodies are never returned by the execution-options endpoint.

## Agent Run Drafts

Draft Implement-this providers store the same safe option metadata on
`agent_runs`:

```json
{
  "status": "drafted",
  "action_key": "openrouter_draft.implement",
  "provider_label": "OpenRouter Draft",
  "run_mode": "draft",
  "model": "anthropic/claude-sonnet-4.5",
  "profile_id": "balanced",
  "effort": "standard",
  "prompt_preset_ref": "default",
  "context_pack": "issue",
  "context_truncated": false,
  "prompt_tokens": 1200,
  "completion_tokens": 600,
  "finish_reason": "stop",
  "source_draft_run_id": null,
  "followup_run_id": 945,
  "tests_summary": "AI draft generated; no local tests were run and no deployment was attempted."
}
```

The draft text itself is stored as an internal issue comment with provenance.
Draft runs must not carry `device_id`, `deploy_target`, repository mutation
claims, local test claims, prompt bodies, API keys, endpoint credentials, or
local environment values.

When a human approves a draft for local implementation, the Workbench creates a
trusted runner follow-up by posting to `POST /api/issues/{id}/implement` with a
local-runner `action_key` and `source_draft_run_id`. The new local run stores
`source_draft_run_id`; the draft row stores `followup_run_id`. A draft can have
only one follow-up, and draft-provider actions cannot be used as follow-ups.

## Actions

### `optimize` / `optimize_customer`

```json
{
  "optimized": "..."
}
```

Frontend summary: char / sentence delta from `source_text` vs. `optimized`.

### `translate`

```json
{
  "optimized": "..."
}
```

Frontend summary: translated copy length and sentence delta.

### `tone_check`

```json
{
  "optimized": "...",
  "counters": {
    "phrases_removed": 4
  }
}
```

Frontend details: current text vs. neutralized text inline, plus a replace-text apply path.

### `suggest_enhancement`

```json
{
  "suggestions": [
    {
      "title": "...",
      "body": "...",
      "impact": "high",
      "target_field": "ac"
    }
  ],
  "counters": {
    "items": 4,
    "categories": 2
  }
}
```

### `spec_out`

```json
{
  "items": [
    { "category": "behavior", "text": "..." }
  ],
  "counters": {
    "items": 6
  }
}
```

### `find_parent`

```json
{
  "candidates": [
    {
      "issue_key": "PAI-83",
      "title": "...",
      "score": 0.87,
      "confidence": "high",
      "rationale": "..."
    }
  ]
}
```

### `generate_subtasks`

```json
{
  "suggestions": [
    { "title": "...", "description": "..." }
  ],
  "counters": {
    "items": 5
  }
}
```

### `estimate_effort`

```json
{
  "hours": 6,
  "lp": 1,
  "confidence": "medium",
  "reasoning": "...",
  "counters": {
    "hours": 6,
    "lp": 1
  }
}
```

### `detect_duplicates`

```json
{
  "matches": [
    {
      "issue_key": "PAI-19",
      "title": "...",
      "score": 0.82
    }
  ],
  "counters": {
    "matches": 3
  }
}
```

### `ui_generation`

```json
{
  "spec_markdown": "...",
  "counters": {
    "words": 142
  }
}
```
