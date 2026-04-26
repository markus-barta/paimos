<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-165–172 / PAI-173 result rendering. One modal that switches on
 the action key and renders the right shape:

   suggest_enhancement → checklist of suggestions, append-to-AC/notes
   spec_out             → categorized AC checklist, append/replace
   find_parent          → 1-3 candidate cards, "Move under"
   generate_subtasks    → editable suggestion list, batch-create
   estimate_effort      → small inline card with hours + LP + apply
   detect_duplicates    → top-5 cards, link out / mark-as-duplicate
   ui_generation        → rendered markdown preview, append/replace

 The modal is mounted ONCE (typically inside AiActionMenu host
 surfaces). It listens to useAiAction().result and opens when set.

 The "apply" path for each action is a per-action callback —
 the host page provides it via a prop, scoped to the editor it
 belongs to. This avoids a giant switch in this file that would
 know how to mutate every editor surface; instead each surface
 wires up only the actions it cares about.
-->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import AppModal from '@/components/AppModal.vue'
import { useAiAction } from '@/composables/useAiAction'
import type { ActionEnvelope } from '@/composables/useAiAction'

const aiAction = useAiAction()

// `apply` is called when the user accepts a structured action's
// result. The host receives the action key, sub-action, and the
// already-shaped body so it can route to the right form mutation.
// Hosts that don't supply a callback get a no-op — useful for the
// drop-in case where actions are mostly diff-overlay-driven.
interface ActionApplyArgs {
  requestId?: string
  action: string
  subAction?: string
  field: string
  fieldLabel: string
  issueId?: number
  body: unknown
  intent?: string
  selection?: number[]
  values?: Record<string, unknown>
}
interface ActionApplyResult {
  undoLabel?: string
  undo?: () => void | Promise<void>
}
const props = defineProps<{
  hostKey?: string
  open?: boolean
  manual?: boolean
  apply?: (info: ActionApplyArgs) => void | Promise<void> | ActionApplyResult | Promise<ActionApplyResult | void>
}>()

const activeResult = computed(() => {
  const r = aiAction.result.value
  if (!r) return null
  if (props.hostKey && r.hostKey !== props.hostKey) return null
  return r
})
const MODAL_ACTIONS = new Set(['suggest_enhancement', 'spec_out', 'generate_subtasks', 'ui_generation'])
const shouldUseInlineSurface = computed(() => {
  const a = activeResult.value?.action
  return a === 'find_parent' || a === 'estimate_effort' || a === 'detect_duplicates'
})
const open = computed(() => {
  if (props.manual) return !!props.open && activeResult.value !== null
  if (shouldUseInlineSurface.value) return false
  return activeResult.value !== null && !shouldUseDiffOverlay.value && MODAL_ACTIONS.has(action.value)
})
// Close = clear the result; the composable's reset() is the canonical path.
function close() { aiAction.reset() }

// We don't render the modal for actions that drive the diff overlay —
// those have their own UX (see useAiOptimize). The composable already
// avoids storing a `result` for them, but keep the guard for safety.
const shouldUseDiffOverlay = computed(() => {
  const a = activeResult.value?.action
  return a === 'optimize' || a === 'optimize_customer' || a === 'translate' || a === 'tone_check'
})

const action = computed(() => activeResult.value?.action ?? '')
const subAction = computed(() => activeResult.value?.subAction)
const fieldLabel = computed(() => activeResult.value?.fieldLabel ?? '')
const sourceText = computed(() => activeResult.value?.sourceText ?? '')
const body = computed<any>(() => activeResult.value?.body ?? null)

// Per-action selection state. Each action that needs the user to
// pick which suggestions to apply uses `selected` as a Set of
// indices into the body's list. Reset whenever the result changes.
const selected = ref<Set<number>>(new Set())
function toggle(i: number) {
  if (selected.value.has(i)) selected.value.delete(i)
  else selected.value.add(i)
  selected.value = new Set(selected.value)
}
function reset() { selected.value = new Set() }
function selectAll(n: number) {
  selected.value = new Set(Array.from({ length: n }, (_, i) => i))
}

// ── per-action title shown in the modal header ──────────────────
const titleByAction: Record<string, string> = {
  suggest_enhancement: 'AI suggestions',
  spec_out:            'AC checklist',
  find_parent:         'Suggested parents',
  generate_subtasks:   'Suggested sub-tasks',
  estimate_effort:     'Estimate',
  detect_duplicates:   'Possible duplicates',
  ui_generation:       'UI spec',
}
const headerTitle = computed(() => {
  const base = titleByAction[action.value] ?? 'AI result'
  if (action.value === 'suggest_enhancement' && subAction.value) {
    return `${base} — ${subAction.value}`
  }
  return base
})

// ── apply paths ─────────────────────────────────────────────────

function emitApply(intent: string, values?: Record<string, unknown>) {
  const r = aiAction.result.value
  if (props.hostKey && r?.hostKey !== props.hostKey) return
  if (!r) return
  const sel = Array.from(selected.value).sort((a, b) => a - b)
  props.apply?.({
    requestId: r.requestId,
    action: r.action,
    subAction: r.subAction,
    field: r.field,
    fieldLabel: r.fieldLabel,
    issueId: r.issueId,
    body: r.body,
    intent,
    selection: sel,
    values,
  })
  close()
}

// ── editable subtask titles (PAI-169) ─────────────────────────
// Keep a parallel array of overrides so the user can rename a
// suggestion before creating it. Indices align 1:1 with body.suggestions.
const subtaskOverrides = ref<Record<number, string>>({})

// Reset selection / overrides whenever a fresh result lands.
function resetResultState() {
  reset()
  subtaskOverrides.value = {}
}
watch(
  () => [activeResult.value?.requestId ?? '', action.value, activeResult.value?.model ?? ''].join('|'),
  () => resetResultState(),
  { immediate: true },
)
</script>

<template>
  <AppModal
    v-if="open"
    :open="open"
    :title="headerTitle"
    @close="close"
  >
    <!-- ── suggest_enhancement ─────────────────────────────────── -->
    <div v-if="action === 'suggest_enhancement'" class="ar">
      <p class="ar-hint">Pick which suggestions to apply. Each is appended to <strong>{{ fieldLabel }}</strong> with a "(suggested by AI)" marker.</p>
      <ul class="ar-list">
        <li v-for="(s, i) in body?.suggestions ?? []" :key="i" class="ar-item">
          <label class="ar-row">
            <input type="checkbox" :checked="selected.has(i)" @change="toggle(i)" />
            <div class="ar-row-body">
              <div class="ar-row-headline">
                <strong>{{ s.title }}</strong>
                <span :class="['ar-impact', `ar-impact--${s.impact}`]">{{ s.impact }}</span>
                <span class="ar-target">→ {{ s.target_field === 'ac' ? 'Acceptance Criteria' : 'Notes' }}</span>
              </div>
              <p class="ar-row-text">{{ s.body }}</p>
            </div>
          </label>
        </li>
        <li v-if="!body?.suggestions?.length" class="ar-empty">The model returned no suggestions for this issue.</li>
      </ul>
      <div class="ar-actions">
        <button type="button" class="btn btn-ghost" @click="close">Cancel</button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!selected.size"
          @click="emitApply('append-suggestions')"
        >
          Apply {{ selected.size || '' }} suggestion{{ selected.size === 1 ? '' : 's' }}
        </button>
      </div>
    </div>

    <!-- ── spec_out ────────────────────────────────────────────── -->
    <div v-else-if="action === 'spec_out'" class="ar">
      <p class="ar-hint">Each item is appended to the existing AC. Click "Replace" to overwrite it instead.</p>
      <div v-for="cat in ['outcome','behavior','edge','regression']" :key="cat">
        <h4 v-if="(body?.items ?? []).some((it: any) => it.category === cat)" class="ar-cat-title">{{ catLabel(cat) }}</h4>
        <ul class="ar-list">
          <li v-for="(it, i) in (body?.items ?? []).filter((x: any) => x.category === cat)" :key="i" class="ar-item">
            <label class="ar-row">
              <input
                type="checkbox"
                :checked="selected.has(globalIndex(body, cat, i))"
                @change="toggle(globalIndex(body, cat, i))"
              />
              <span class="ar-row-text">{{ it.text }}</span>
            </label>
          </li>
        </ul>
      </div>
      <div class="ar-actions">
        <button type="button" class="btn btn-ghost" @click="close">Cancel</button>
        <button type="button" class="btn btn-ghost" @click="selectAll(body?.items?.length ?? 0)">Select all</button>
        <button type="button" class="btn btn-primary" :disabled="!selected.size" @click="emitApply('append-spec')">
          Append {{ selected.size }} item{{ selected.size === 1 ? '' : 's' }}
        </button>
      </div>
    </div>

    <!-- ── find_parent ─────────────────────────────────────────── -->
    <div v-else-if="action === 'find_parent'" class="ar">
      <p class="ar-hint">
        Top {{ body?.candidates?.length ?? 0 }} parent candidate{{ body?.candidates?.length === 1 ? '' : 's' }}
        for this issue<span v-if="body?.truncated"> (project truncated to a partial view)</span>.
      </p>
      <div v-for="(c, i) in body?.candidates ?? []" :key="i" class="ar-card">
        <div class="ar-card-headrow">
          <strong class="ar-card-title">{{ c.issue_key }} — {{ c.title }}</strong>
          <span :class="['ar-conf', `ar-conf--${c.confidence}`]">{{ c.confidence }} confidence</span>
        </div>
        <p class="ar-card-text">{{ c.rationale }}</p>
        <div class="ar-card-actions">
          <button type="button" class="btn btn-ghost btn-sm" @click="emitApply('move-under', { issue_key: c.issue_key })">Move under {{ c.issue_key }}</button>
        </div>
      </div>
      <p v-if="!body?.candidates?.length" class="ar-empty">No obvious parent for this issue.</p>
      <div class="ar-actions">
        <button type="button" class="btn btn-ghost" @click="close">Close</button>
      </div>
    </div>

    <!-- ── generate_subtasks ───────────────────────────────────── -->
    <div v-else-if="action === 'generate_subtasks'" class="ar">
      <p class="ar-hint">Pick which to create as children. Edit titles inline before applying.</p>
      <ul class="ar-list">
        <li v-for="(s, i) in body?.suggestions ?? []" :key="i" class="ar-item">
          <label class="ar-row">
            <input type="checkbox" :checked="selected.has(i)" @change="toggle(i)" />
            <div class="ar-row-body">
              <input
                class="ar-subtask-title"
                :value="subtaskOverrides[i] ?? s.title"
                @input="(e: any) => subtaskOverrides[i] = e.target.value"
                placeholder="Sub-task title"
              />
              <p class="ar-row-text">{{ s.description }}</p>
              <span class="ar-target">→ {{ s.type }}</span>
            </div>
          </label>
        </li>
        <li v-if="!body?.suggestions?.length" class="ar-empty">No sub-tasks were suggested.</li>
      </ul>
      <div class="ar-actions">
        <button type="button" class="btn btn-ghost" @click="close">Cancel</button>
        <button
          type="button" class="btn btn-primary" :disabled="!selected.size"
          @click="emitApply('create-subtasks', { titleOverrides: { ...subtaskOverrides } })"
        >
          Create {{ selected.size }} sub-task{{ selected.size === 1 ? '' : 's' }}
        </button>
      </div>
    </div>

    <!-- ── estimate_effort ─────────────────────────────────────── -->
    <div v-else-if="action === 'estimate_effort'" class="ar">
      <div class="ar-estimate">
        <div class="ar-estimate-row">
          <div>
            <strong class="ar-estimate-num">{{ Number(body?.hours ?? 0).toFixed(1) }} h</strong>
          </div>
          <div>
            <strong class="ar-estimate-num">{{ Number(body?.lp ?? 0).toFixed(1) }} LP</strong>
          </div>
        </div>
        <p class="ar-card-text" v-if="body?.reasoning">{{ body.reasoning }}</p>
      </div>
      <div class="ar-actions">
        <button type="button" class="btn btn-ghost" @click="close">Dismiss</button>
        <button type="button" class="btn btn-primary" @click="emitApply('apply-estimate', { hours: body?.hours, lp: body?.lp })">
          Apply estimate
        </button>
      </div>
    </div>

    <!-- ── detect_duplicates ───────────────────────────────────── -->
    <div v-else-if="action === 'detect_duplicates'" class="ar">
      <p class="ar-hint">
        Top {{ body?.matches?.length ?? 0 }} candidate{{ body?.matches?.length === 1 ? '' : 's' }}
        in the same project<span v-if="body?.truncated"> (project truncated to a partial view)</span>.
      </p>
      <div v-for="(m, i) in body?.matches ?? []" :key="i" class="ar-card">
        <div class="ar-card-headrow">
          <strong class="ar-card-title">{{ m.issue_key }} — {{ m.title }}</strong>
          <span :class="['ar-conf', `ar-conf--${m.similarity}`]">{{ m.similarity }} match</span>
        </div>
        <p class="ar-card-text">{{ m.rationale }}</p>
        <div class="ar-card-actions">
          <button type="button" class="btn btn-ghost btn-sm" @click="emitApply('mark-duplicate', { issue_key: m.issue_key })">Mark as duplicate of {{ m.issue_key }}</button>
        </div>
      </div>
      <p v-if="!body?.matches?.length" class="ar-empty">No similar issues found.</p>
      <div class="ar-actions">
        <button type="button" class="btn btn-ghost" @click="close">Close</button>
      </div>
    </div>

    <!-- ── ui_generation ───────────────────────────────────────── -->
    <div v-else-if="action === 'ui_generation'" class="ar">
      <p class="ar-hint">Generated UI spec — markdown.</p>
      <pre class="ar-markdown">{{ body?.spec_markdown ?? '' }}</pre>
      <div class="ar-actions">
        <button type="button" class="btn btn-ghost" @click="close">Dismiss</button>
        <button type="button" class="btn btn-ghost" @click="emitApply('append-to-notes')">Append to notes</button>
        <button type="button" class="btn btn-primary" @click="emitApply('replace-description')">Replace description</button>
      </div>
    </div>

    <!-- ── fallback ─────────────────────────────────────────────── -->
    <div v-else class="ar">
      <pre class="ar-markdown">{{ JSON.stringify(body, null, 2) }}</pre>
      <div class="ar-actions">
        <button type="button" class="btn btn-ghost" @click="close">Close</button>
      </div>
    </div>
  </AppModal>
</template>

<script lang="ts">
function catLabel(k: string): string {
  return ({
    outcome: 'Product outcome',
    behavior: 'Behavioural guarantees',
    edge: 'Edge cases',
    regression: 'Regression checks',
  } as Record<string, string>)[k] ?? k
}
function globalIndex(body: any, cat: string, localIdx: number): number {
  // Find the global array index of the localIdx-th item with the given category.
  if (!body?.items) return -1
  let count = 0
  for (let i = 0; i < body.items.length; i++) {
    if (body.items[i].category !== cat) continue
    if (count === localIdx) return i
    count++
  }
  return -1
}
export { catLabel, globalIndex }
</script>

<style scoped>
.ar { display: flex; flex-direction: column; gap: 1rem; min-width: 0; }
.ar-hint { margin: 0; font-size: 13px; color: var(--text-muted); line-height: 1.5; }
.ar-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: .35rem; }
.ar-item { padding: .5rem .65rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg-card); }
.ar-row { display: flex; align-items: flex-start; gap: .65rem; cursor: pointer; }
.ar-row > input[type="checkbox"] { margin-top: .3rem; }
.ar-row-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: .2rem; }
.ar-row-headline { display: flex; flex-wrap: wrap; align-items: baseline; gap: .5rem; }
.ar-row-text { margin: 0; font-size: 12.5px; color: var(--text); line-height: 1.5; }
.ar-empty { font-size: 12px; color: var(--text-muted); padding: .5rem .65rem; }
.ar-impact, .ar-conf, .ar-target {
  display: inline-flex; align-items: center;
  font-size: 9.5px; font-weight: 700;
  letter-spacing: .08em; text-transform: uppercase;
  padding: .12rem .45rem; border-radius: 999px;
}
.ar-impact--high { background: #fee2e2; color: #991b1b; }
.ar-impact--med  { background: #fef3c7; color: #92400e; }
.ar-impact--low  { background: #ecfdf5; color: #166534; }
.ar-conf--high   { background: #ecfdf5; color: #166534; }
.ar-conf--med    { background: #fef3c7; color: #92400e; }
.ar-conf--low    { background: #e2e8f0; color: #475569; }
.ar-target { background: var(--bp-blue-pale); color: var(--bp-blue-dark); }
.ar-cat-title { margin: .65rem 0 .25rem; font-size: 11px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; color: var(--text-muted); }
.ar-card { border: 1px solid var(--border); border-radius: 10px; padding: .75rem .9rem; background: var(--bg-card); display: flex; flex-direction: column; gap: .35rem; }
.ar-card-headrow { display: flex; align-items: baseline; gap: .55rem; flex-wrap: wrap; }
.ar-card-title { font-size: 13px; color: var(--text); }
.ar-card-text { margin: 0; font-size: 12.5px; color: var(--text-muted); line-height: 1.5; }
.ar-card-actions { display: flex; justify-content: flex-end; gap: .35rem; }
.ar-actions { display: flex; justify-content: flex-end; gap: .5rem; padding-top: .25rem; }
.ar-estimate { display: flex; flex-direction: column; gap: .65rem; padding: .85rem 1rem; background: var(--bg-card); border: 1px solid var(--border); border-radius: 10px; }
.ar-estimate-row { display: flex; gap: 1.5rem; }
.ar-estimate-num { font-family: 'DM Mono', monospace; font-size: 22px; color: var(--bp-blue-dark); }
.ar-markdown {
  font-family: 'DM Mono', monospace;
  font-size: 12px;
  line-height: 1.5;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: .85rem 1rem;
  white-space: pre-wrap;
  max-height: 480px;
  overflow: auto;
  margin: 0;
}
.ar-subtask-title {
  width: 100%;
  font-family: 'DM Sans', sans-serif;
  font-size: 13px;
  padding: .25rem .4rem;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg);
}
</style>
