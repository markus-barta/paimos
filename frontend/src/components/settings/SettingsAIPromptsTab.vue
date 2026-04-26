<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-176. Settings → AI → Prompts: list + edit modal for the
 ai_prompts table (M78, PAI-175). Built-in rows are listed first;
 custom rows live below. Each row exposes Edit + (when relevant)
 Reset / Delete.

 Edit modal contents:
   - prompt_template textarea (monospace)
   - variable picker (per-surface)
   - dry-run launcher (PAI-177; stub today)
-->
<script setup lang="ts">
import { ref, computed, onMounted, reactive } from 'vue'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import AppModal from '@/components/AppModal.vue'

interface PromptRow {
  id: number
  key: string
  label: string
  surface: 'issue' | 'customer'
  // PAI-179: '' means "use the registry default" (default_placement
  // tells you what that default is). 'text' / 'issue' / 'both'
  // override it.
  placement: '' | 'text' | 'issue' | 'both'
  default_placement?: string
  parent_action?: string
  sub_action?: string
  prompt_template: string
  enabled: boolean
  is_builtin: boolean
  default_template_hash?: string
  updated_at: string
}

const prompts = ref<PromptRow[]>([])
const loading = ref(true)
const loadError = ref('')
const saveError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const r = await api.get<{ prompts: PromptRow[] }>('/ai/prompts')
    prompts.value = r.prompts ?? []
  } catch (e) {
    loadError.value = errMsg(e, 'Failed to load AI prompts.')
  } finally {
    loading.value = false
  }
}
onMounted(load)

const groupedBuiltin = computed(() => prompts.value.filter(p => p.is_builtin))
const groupedCustom = computed(() => prompts.value.filter(p => !p.is_builtin))

// ── Edit modal state ────────────────────────────────────────────
interface EditState {
  open: boolean
  row: PromptRow | null
  draftTemplate: string
  draftLabel: string
  draftSurface: 'issue' | 'customer'
  draftKey: string
  draftEnabled: boolean
  draftPlacement: '' | 'text' | 'issue' | 'both'
  isCreate: boolean
}
const edit = reactive<EditState>({
  open: false,
  row: null,
  draftTemplate: '',
  draftLabel: '',
  draftSurface: 'issue',
  draftKey: '',
  draftEnabled: true,
  draftPlacement: '',
  isCreate: false,
})
const saving = ref(false)

function openEdit(row: PromptRow) {
  edit.row = row
  edit.draftTemplate = row.prompt_template
  edit.draftLabel = row.label
  edit.draftSurface = row.surface
  edit.draftKey = row.key
  edit.draftEnabled = row.enabled
  edit.draftPlacement = row.placement ?? ''
  edit.isCreate = false
  edit.open = true
}
function openCreate() {
  edit.row = null
  edit.draftTemplate = ''
  edit.draftLabel = ''
  edit.draftSurface = 'issue'
  edit.draftKey = ''
  edit.draftEnabled = true
  edit.draftPlacement = 'text'
  edit.isCreate = true
  edit.open = true
}
function closeEdit() {
  edit.open = false
  saveError.value = ''
}

async function save() {
  saving.value = true
  saveError.value = ''
  try {
    if (edit.isCreate) {
      await api.post<PromptRow>('/ai/prompts', {
        key: edit.draftKey.trim(),
        label: edit.draftLabel.trim(),
        surface: edit.draftSurface,
        placement: edit.draftPlacement,
        prompt_template: edit.draftTemplate,
        enabled: edit.draftEnabled,
      })
    } else if (edit.row) {
      const body: Record<string, unknown> = {
        prompt_template: edit.draftTemplate,
        enabled: edit.draftEnabled,
        // PAI-179: placement is mutable on built-in rows too.
        placement: edit.draftPlacement,
      }
      if (!edit.row.is_builtin) {
        body.label = edit.draftLabel.trim()
        body.surface = edit.draftSurface
      }
      await api.put<PromptRow>(`/ai/prompts/${edit.row.id}`, body)
    }
    closeEdit()
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Save failed.')
  } finally {
    saving.value = false
  }
}

async function reset(row: PromptRow) {
  if (!confirm(`Reset "${row.label}" to its default prompt?`)) return
  try {
    await api.post(`/ai/prompts/${row.id}/reset`, {})
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Reset failed.')
  }
}

async function remove(row: PromptRow) {
  if (!confirm(`Delete custom action "${row.label}"? This cannot be undone.`)) return
  try {
    await api.delete(`/ai/prompts/${row.id}`)
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Delete failed.')
  }
}

// ── Variable picker ─────────────────────────────────────────────
// Per-surface variables exposed by the dispatcher. The list lives
// here so admins see a usable autocomplete instead of memorising
// the schema.
const VARS_BY_SURFACE: Record<string, string[]> = {
  issue:    ['Title', 'Description', 'AcceptanceCriteria', 'Notes', 'Type', 'Status', 'IssueKey', 'ProjectName', 'ParentEpic'],
  customer: ['CustomerName', 'Industry', 'Notes', 'CooperationType', 'SLADetails', 'CooperationNotes'],
}
const surfaceVars = computed<string[]>(() => VARS_BY_SURFACE[edit.draftSurface] ?? [])
function insertVar(varName: string) {
  edit.draftTemplate += `{{.${varName}}}`
}
// Pre-render the display string for each variable; the literal
// `{{...}}` would otherwise collide with Vue's interpolation parser
// inside templates.
function varDisplay(varName: string): string {
  return '{{.' + varName + '}}'
}
const titleVarExample = '{{.Title}}'

// PAI-179: resolve the effective placement for a row — admin
// override if set, otherwise the registry default. Surfaces in
// the list-row pill so admins see at a glance where the action
// will appear.
function effectivePlacement(p: PromptRow): string {
  if (p.placement) return p.placement
  if (p.default_placement) return p.default_placement
  return 'text'
}
function placementTitle(p: PromptRow): string {
  const eff = effectivePlacement(p)
  if (p.placement) return `Admin override: ${eff}`
  return `Default: ${eff}`
}

// ── Dry-run (PAI-177 stub) ───────────────────────────────────────
const dryRunIssueId = ref<number | null>(null)
const dryRunResult = ref<unknown>(null)
const dryRunRunning = ref(false)
const dryRunError = ref('')
async function dryRun() {
  if (!edit.row || dryRunRunning.value) return
  dryRunRunning.value = true
  dryRunError.value = ''
  dryRunResult.value = null
  try {
    const r = await api.post<unknown>(`/ai/prompts/${edit.row.id}/dry-run`, {
      issue_id: dryRunIssueId.value ?? 0,
    })
    dryRunResult.value = r
  } catch (e) {
    dryRunError.value = errMsg(e, 'Dry-run failed.')
  } finally {
    dryRunRunning.value = false
  }
}
</script>

<template>
  <div class="ap-tab">
    <header class="ap-hero">
      <div class="ap-hero-iconwrap"><AppIcon name="pen-line" :size="22" /></div>
      <div>
        <h2 class="ap-hero-title">AI prompt templates</h2>
        <p class="ap-hero-desc">
          Override the prompt for each AI action, or define your own
          custom actions. Built-in actions keep their label, surface,
          and target field — only the prompt text is yours to edit.
        </p>
      </div>
    </header>

    <p v-if="loadError" class="ap-banner ap-banner--error">
      <AppIcon name="alert-triangle" :size="14" /> {{ loadError }}
    </p>

    <section class="ap-section" v-if="!loading">
      <header class="ap-section-headrow">
        <h3 class="ap-section-title">Built-in actions</h3>
        <span class="ap-section-meta">{{ groupedBuiltin.length }}</span>
      </header>
      <p v-if="!groupedBuiltin.length" class="ap-empty">
        No built-in actions yet — once an action ships its handler, it
        appears here.
      </p>
      <ul class="ap-list">
        <li v-for="p in groupedBuiltin" :key="p.id" class="ap-row">
          <div class="ap-row-meta">
            <strong class="ap-row-label">{{ p.label }}</strong>
            <code class="ap-row-key">{{ p.key }}</code>
            <span :class="['ap-row-surface', `ap-row-surface--${p.surface}`]">{{ p.surface }}</span>
            <span :class="['ap-row-place', `ap-row-place--${effectivePlacement(p)}`]" :title="placementTitle(p)">
              {{ effectivePlacement(p) }}
            </span>
            <span v-if="p.prompt_template" class="ap-row-tag" title="Admin override is set; click reset to revert.">overridden</span>
          </div>
          <div class="ap-row-actions">
            <button class="btn btn-ghost btn-sm" @click="openEdit(p)">Edit</button>
            <button v-if="p.prompt_template" class="btn btn-ghost btn-sm" @click="reset(p)">Reset</button>
          </div>
        </li>
      </ul>
    </section>

    <section class="ap-section" v-if="!loading">
      <header class="ap-section-headrow">
        <h3 class="ap-section-title">Custom actions</h3>
        <span class="ap-section-meta">{{ groupedCustom.length }}</span>
        <button class="btn btn-primary btn-sm ap-add" @click="openCreate">
          <AppIcon name="plus" :size="13" /> New custom action
        </button>
      </header>
      <ul v-if="groupedCustom.length" class="ap-list">
        <li v-for="p in groupedCustom" :key="p.id" class="ap-row">
          <div class="ap-row-meta">
            <strong class="ap-row-label">{{ p.label }}</strong>
            <code class="ap-row-key">{{ p.key }}</code>
            <span :class="['ap-row-surface', `ap-row-surface--${p.surface}`]">{{ p.surface }}</span>
            <span :class="['ap-row-place', `ap-row-place--${effectivePlacement(p)}`]" :title="placementTitle(p)">
              {{ effectivePlacement(p) }}
            </span>
          </div>
          <div class="ap-row-actions">
            <button class="btn btn-ghost btn-sm" @click="openEdit(p)">Edit</button>
            <button class="btn btn-ghost btn-sm ap-delete" @click="remove(p)">Delete</button>
          </div>
        </li>
      </ul>
      <p v-else class="ap-empty">No custom actions yet — click "+ New custom action" to add one.</p>
    </section>

    <div v-if="loading" class="ap-loading">Loading prompts…</div>

    <!-- ── Edit modal ────────────────────────────────────────────── -->
    <AppModal
      v-if="edit.open"
      :open="edit.open"
      :title="edit.isCreate ? 'New custom AI action' : `Edit ${edit.row?.label}`"
      @close="closeEdit"
      max-width="780px"
    >
      <div class="ap-edit">
        <template v-if="edit.isCreate || !edit.row?.is_builtin">
          <label class="ap-field">
            <span class="ap-field-label">Key</span>
            <input
              v-model="edit.draftKey"
              type="text"
              class="ap-input ap-input-mono"
              placeholder="my_custom_action"
              :disabled="!edit.isCreate"
            />
            <span class="ap-field-hint">Lowercase letters, digits, underscore. 3-32 chars. Used in audit logs.</span>
          </label>
          <label class="ap-field">
            <span class="ap-field-label">Label</span>
            <input v-model="edit.draftLabel" type="text" class="ap-input" placeholder="My custom action" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">Surface</span>
            <select v-model="edit.draftSurface" class="ap-input">
              <option value="issue">Issue editor</option>
              <option value="customer">Customer fields</option>
            </select>
          </label>
        </template>
        <!-- PAI-179: placement editor — built-in and custom rows.
             Empty string = "use the registry default", which is
             surfaced beside the radio so admins know what the
             fall-through is. Three concrete values: text (textarea
             menu), issue (record-level menu), both (everywhere). -->
        <fieldset class="ap-field ap-placement">
          <legend class="ap-field-label">Placement</legend>
          <span class="ap-field-hint">
            Where this action appears in the UI.
            <strong>Text</strong> = inline next to text fields (textareas).
            <strong>Issue</strong> = in the issue header / sidebar / ellipsis.
            <strong>Both</strong> = everywhere.
          </span>
          <div class="ap-placement-row">
            <label class="ap-placement-opt" :class="{ 'ap-placement-opt--active': edit.draftPlacement === '' }">
              <input type="radio" v-model="edit.draftPlacement" value="" />
              <span>Default<span v-if="edit.row?.default_placement" class="ap-placement-default">({{ edit.row.default_placement }})</span></span>
            </label>
            <label class="ap-placement-opt" :class="{ 'ap-placement-opt--active': edit.draftPlacement === 'text' }">
              <input type="radio" v-model="edit.draftPlacement" value="text" />
              <span>Text fields</span>
            </label>
            <label class="ap-placement-opt" :class="{ 'ap-placement-opt--active': edit.draftPlacement === 'issue' }">
              <input type="radio" v-model="edit.draftPlacement" value="issue" />
              <span>Issue menu</span>
            </label>
            <label class="ap-placement-opt" :class="{ 'ap-placement-opt--active': edit.draftPlacement === 'both' }">
              <input type="radio" v-model="edit.draftPlacement" value="both" />
              <span>Both</span>
            </label>
          </div>
        </fieldset>

        <label class="ap-field ap-toggle">
          <input v-model="edit.draftEnabled" type="checkbox" />
          <span>Enabled (the action appears in the dropdown menu)</span>
        </label>
        <div class="ap-field">
          <span class="ap-field-label">Prompt template</span>
          <span class="ap-field-hint">
            Empty for built-in rows means "use the code-defined default". Variables: <code>{{ titleVarExample }}</code>, etc.
          </span>
          <div class="ap-vars">
            <button
              v-for="v in surfaceVars" :key="v"
              type="button" class="ap-var" @click="insertVar(v)"
            >{{ varDisplay(v) }}</button>
          </div>
          <textarea
            v-model="edit.draftTemplate"
            rows="14"
            class="ap-textarea"
            spellcheck="false"
            placeholder="You are an editor inside PAIMOS. Rewrite the field below…"
          ></textarea>
        </div>

        <p v-if="saveError" class="ap-banner ap-banner--error">
          <AppIcon name="alert-triangle" :size="14" /> {{ saveError }}
        </p>

        <div class="ap-edit-actions">
          <button class="btn btn-ghost" @click="closeEdit">Cancel</button>
          <details v-if="edit.row && !edit.isCreate" class="ap-dryrun">
            <summary>Dry-run preview</summary>
            <div class="ap-dryrun-body">
              <label class="ap-field">
                <span class="ap-field-label">Issue ID</span>
                <input v-model.number="dryRunIssueId" type="number" class="ap-input ap-input-mono" placeholder="e.g. 553" />
              </label>
              <button type="button" class="btn btn-ghost btn-sm" :disabled="dryRunRunning" @click="dryRun">
                <AppIcon :name="dryRunRunning ? 'loader-circle' : 'play'" :size="12" :class="{ spin: dryRunRunning }" />
                {{ dryRunRunning ? 'Running…' : 'Run' }}
              </button>
              <p v-if="dryRunError" class="ap-banner ap-banner--error">
                <AppIcon name="alert-triangle" :size="14" /> {{ dryRunError }}
              </p>
              <pre v-if="dryRunResult" class="ap-dryrun-result">{{ JSON.stringify(dryRunResult, null, 2) }}</pre>
            </div>
          </details>
          <button class="btn btn-primary" :disabled="saving" @click="save">
            <AppIcon v-if="saving" name="loader-circle" :size="13" class="spin" />
            {{ saving ? 'Saving…' : (edit.isCreate ? 'Create' : 'Save') }}
          </button>
        </div>
      </div>
    </AppModal>
  </div>
</template>

<style scoped>
.ap-tab { display: flex; flex-direction: column; gap: 1rem; max-width: 920px; }
.ap-hero {
  display: flex; align-items: flex-start; gap: 1.1rem;
  padding: 1.4rem 1.6rem;
  border: 1px solid var(--border);
  border-radius: 14px;
  background: var(--bg-card);
}
.ap-hero-iconwrap {
  flex-shrink: 0; width: 44px; height: 44px;
  display: flex; align-items: center; justify-content: center;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  border-radius: 11px;
}
.ap-hero-title { margin: 0 0 .25rem; font-size: 17px; font-weight: 700; }
.ap-hero-desc { margin: 0; font-size: 13px; color: var(--text-muted); line-height: 1.55; max-width: 680px; }

.ap-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 1.1rem 1.25rem;
  display: flex; flex-direction: column; gap: .65rem;
}
.ap-section-headrow {
  display: flex; align-items: center; gap: .55rem;
}
.ap-section-title {
  margin: 0;
  font-size: 11px; font-weight: 700; letter-spacing: .075em;
  text-transform: uppercase; color: var(--text);
}
.ap-section-meta {
  font-family: 'DM Mono', monospace;
  font-size: 11px;
  color: var(--text-muted);
}
.ap-add { margin-left: auto; }

.ap-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: .35rem; }
.ap-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .65rem; padding: .55rem .75rem;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.ap-row-meta { display: flex; align-items: center; gap: .55rem; flex-wrap: wrap; min-width: 0; }
.ap-row-label { font-size: 13px; color: var(--text); letter-spacing: -.005em; }
.ap-row-key { font-family: 'DM Mono', monospace; font-size: 11.5px; color: var(--text-muted); background: white; padding: .1rem .4rem; border-radius: 5px; border: 1px solid var(--border); }
.ap-row-surface { font-size: 9.5px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; padding: .12rem .45rem; border-radius: 999px; }
.ap-row-surface--issue    { background: var(--bp-blue-pale); color: var(--bp-blue-dark); }
.ap-row-surface--customer { background: #ede9fe; color: #5b21b6; }
/* PAI-179: placement pill — at-a-glance "where does this action
   appear in the UI". Same shape as the surface pill, distinct
   palette so the eye can separate the two dimensions. */
.ap-row-place {
  font-size: 9.5px; font-weight: 700;
  letter-spacing: .08em; text-transform: uppercase;
  padding: .12rem .45rem; border-radius: 999px;
  font-family: 'DM Sans', sans-serif;
}
.ap-row-place--text  { background: #d1fae5; color: #065f46; }
.ap-row-place--issue { background: #fef3c7; color: #92400e; }
.ap-row-place--both  { background: #fce7f3; color: #9d174d; }
/* Placement editor — radio cluster inside the modal. */
.ap-placement { border: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: .35rem; }
.ap-placement-row {
  display: flex; gap: .35rem; flex-wrap: wrap;
  margin-top: .15rem;
}
.ap-placement-opt {
  display: inline-flex; align-items: center; gap: .35rem;
  padding: .35rem .65rem;
  background: var(--bg);
  border: 1.5px solid var(--border);
  border-radius: 8px;
  font-size: 12.5px; color: var(--text);
  cursor: pointer;
  transition: border-color .12s, background .12s;
}
.ap-placement-opt:hover { border-color: var(--bp-blue-light); }
.ap-placement-opt > input[type="radio"] { accent-color: var(--bp-blue); margin: 0; }
.ap-placement-opt--active {
  border-color: var(--bp-blue);
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
  font-weight: 600;
}
.ap-placement-default {
  margin-left: .25rem;
  font-family: 'DM Mono', monospace;
  font-size: 10.5px;
  color: var(--text-muted);
}
.ap-row-tag {
  font-size: 9.5px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase;
  padding: .12rem .45rem; border-radius: 999px;
  background: #fef3c7; color: #92400e;
}
.ap-row-actions { display: flex; gap: .35rem; flex-shrink: 0; }
.ap-delete { color: #b91c1c; }
.ap-delete:hover { background: #fef2f2; border-color: #fecaca; }

.ap-empty { font-size: 12.5px; color: var(--text-muted); padding: .25rem 0; }
.ap-loading { color: var(--text-muted); padding: .5rem 0; font-size: 13px; }

.ap-edit { display: flex; flex-direction: column; gap: .85rem; min-width: 0; }
.ap-field { display: flex; flex-direction: column; gap: .25rem; }
.ap-field-label { font-size: 11px; font-weight: 700; letter-spacing: .07em; text-transform: uppercase; color: var(--text-muted); }
.ap-field-hint { font-size: 11px; color: var(--text-muted); line-height: 1.45; }
.ap-toggle { flex-direction: row; align-items: center; gap: .5rem; font-size: 13px; color: var(--text); }
.ap-input {
  font-family: 'DM Sans', sans-serif;
  font-size: 13px; padding: .5rem .65rem;
  border: 1.5px solid var(--border); border-radius: 8px;
  background: white; color: var(--text);
}
.ap-input:focus { outline: none; border-color: var(--bp-blue); box-shadow: 0 0 0 3px var(--bp-blue-pale); }
.ap-input-mono { font-family: 'DM Mono', monospace; }
.ap-textarea {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 12px; line-height: 1.6;
  padding: .8rem .95rem;
  border: 1.5px solid var(--border); border-radius: 8px;
  background: white; color: var(--text);
  resize: vertical; min-height: 240px; width: 100%; box-sizing: border-box;
}
.ap-textarea:focus { outline: none; border-color: var(--bp-blue); box-shadow: 0 0 0 3px var(--bp-blue-pale); }

.ap-vars { display: flex; flex-wrap: wrap; gap: .25rem; }
.ap-var {
  font-family: 'DM Mono', monospace;
  font-size: 10.5px;
  color: var(--bp-blue-dark);
  background: var(--bp-blue-pale);
  border: 1px solid transparent;
  border-radius: 6px;
  padding: .15rem .45rem;
  cursor: pointer;
}
.ap-var:hover { background: var(--bp-blue); color: white; }

.ap-edit-actions { display: flex; justify-content: flex-end; gap: .5rem; flex-wrap: wrap; align-items: flex-end; }
.ap-dryrun { flex: 1; min-width: 0; }
.ap-dryrun > summary { font-size: 12.5px; cursor: pointer; padding: .35rem 0; color: var(--text); }
.ap-dryrun-body { display: flex; flex-direction: column; gap: .55rem; padding: .5rem 0; }
.ap-dryrun-result {
  font-family: 'DM Mono', monospace;
  font-size: 11.5px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: .65rem;
  max-height: 240px;
  overflow: auto;
  margin: 0;
}

.ap-banner {
  margin: 0; padding: .55rem .85rem;
  border-radius: 8px; font-size: 12.5px;
  display: inline-flex; align-items: center; gap: .45rem;
}
.ap-banner--error { background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca; }

.spin { animation: ap-spin 1s linear infinite; }
@keyframes ap-spin { to { transform: rotate(360deg); } }
</style>
