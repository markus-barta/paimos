<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-176 / PAI-180. Settings → AI → Prompts: list + edit modal for
 the ai_prompts table. Built-in rows listed first; custom rows below.
 The edit modal got a full UX rewrite in PAI-180 — see comments on
 the .ape-* classes for the design notes.
-->
<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, computed, onMounted, reactive, nextTick } from 'vue'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import AppModal from '@/components/AppModal.vue'
import AiActivityStrip from '@/components/ai/AiActivityStrip.vue'
import AiResultStrip from '@/components/ai/AiResultStrip.vue'
import IssueSearchInput from '@/components/ai/IssueSearchInput.vue'

interface PromptRow {
  id: number
  key: string
  label: string
  surface: 'issue' | 'customer'
  // PAI-179: '' = "use the registry default" (default_placement
  // tells the UI what that default is). 'text' / 'issue' / 'both'
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

interface DryRunResponse {
  rendered_system?: string
  rendered_user?: string
  response?: string
  model?: string
  latency_ms?: number
  prompt_tokens?: number
  completion_tokens?: number
  used_default?: boolean
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
const groupedCustom  = computed(() => prompts.value.filter(p => !p.is_builtin))

// ── Edit modal state ─────────────────────────────────────────────
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
  // Wipe any stale dry-run state from a previous edit session.
  dryRunIssueId.value = null
  dryRunResult.value = null
  dryRunError.value = ''
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

// ── Variable picker ──────────────────────────────────────────────
//
// Per-surface variables exposed by the dispatcher. The list lives in
// the frontend so admins see a usable picker instead of memorising
// the schema; it stays in lockstep with the backend's dryRunContext
// struct. Adding a new variable: one entry here + one field in
// backend/handlers/ai_prompts.go's dryRunContext.
const VARS_BY_SURFACE: Record<string, string[]> = {
  issue:    ['Title', 'Description', 'AcceptanceCriteria', 'Notes', 'Type', 'Status', 'IssueKey', 'ProjectName', 'ParentEpic'],
  customer: ['CustomerName', 'Industry', 'Notes', 'CooperationType', 'SLADetails', 'CooperationNotes'],
}
const surfaceVars = computed<string[]>(() => VARS_BY_SURFACE[edit.draftSurface] ?? [])
function varDisplay(varName: string): string {
  // The literal `{{...}}` would otherwise collide with Vue's
  // interpolation parser inside templates, so we render it via this
  // helper that returns a plain string.
  return '{{.' + varName + '}}'
}
const titleVarExample = '{{.Title}}'

// `insertVar` inserts at the cursor instead of appending — feels
// considerably better when an admin is composing a prompt and
// wants to drop a variable mid-sentence.
const promptTextarea = ref<HTMLTextAreaElement | null>(null)
function insertVar(varName: string) {
  const token = `{{.${varName}}}`
  const ta = promptTextarea.value
  if (!ta) {
    edit.draftTemplate += token
    return
  }
  const start = ta.selectionStart
  const end = ta.selectionEnd
  const before = edit.draftTemplate.slice(0, start)
  const after = edit.draftTemplate.slice(end)
  edit.draftTemplate = before + token + after
  // Focus + place the caret right after the inserted token so the
  // admin can keep typing in flow.
  nextTick(() => {
    ta.focus()
    const pos = start + token.length
    ta.setSelectionRange(pos, pos)
  })
}

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

// ── Dry-run console (PAI-177 / PAI-180) ──────────────────────────
const dryRunIssueId = ref<number | null>(null)
const dryRunResult = ref<DryRunResponse | null>(null)
const dryRunRunning = ref(false)
const dryRunError = ref('')
const dryRunStartedAt = ref<number | null>(null)

const dryRunSummary = computed(() => {
  if (!dryRunResult.value) return ''
  const tokens = (dryRunResult.value.prompt_tokens ?? 0) + (dryRunResult.value.completion_tokens ?? 0)
  const chars = String(dryRunResult.value.response ?? '').length
  return `${chars} response chars · ${tokens} tokens`
})

async function dryRun() {
  if (!edit.row || dryRunRunning.value) return
  dryRunRunning.value = true
  dryRunStartedAt.value = Date.now()
  dryRunError.value = ''
  dryRunResult.value = null
  try {
    const r = await api.post<DryRunResponse>(`/ai/prompts/${edit.row.id}/dry-run`, {
      issue_id: dryRunIssueId.value ?? 0,
    })
    dryRunResult.value = r
  } catch (e) {
    dryRunError.value = errMsg(e, 'Dry-run failed.')
  } finally {
    dryRunRunning.value = false
  }
}
function clearDryRun() {
  dryRunResult.value = null
  dryRunError.value = ''
  dryRunStartedAt.value = null
}
</script>

<template>
  <div class="ap-tab">
    <!-- ── Tab hero ─────────────────────────────────────────────── -->
    <header class="ap-hero">
      <div class="ap-hero-iconwrap"><AppIcon name="pen-line" :size="22" /></div>
      <div>
        <h2 class="ap-hero-title">AI prompt templates</h2>
        <p class="ap-hero-desc">
          Override the prompt for each AI action, or define your own
          custom actions. Built-in actions keep their key and surface —
          their prompt text and placement are yours to edit.
        </p>
      </div>
    </header>

    <p v-if="loadError" class="ap-banner ap-banner--error">
      <AppIcon name="alert-triangle" :size="14" /> {{ loadError }}
    </p>

    <!-- ── Built-in actions list ────────────────────────────────── -->
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

    <!-- ── Custom actions list ──────────────────────────────────── -->
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

    <LoadingText v-if="loading" class="ap-loading" label="Loading prompts…" />

    <!-- ──────────────────────────────────────────────────────────────
         PAI-180: Edit modal — full UX rewrite.

         Sections (top → bottom):
           1. Identity (custom rows only)         — key / label / surface
           2. Placement                           — segmented radio cards
           3. Status                              — iOS-style toggle row
           4. Prompt template                     — variable chips + textarea
           5. Dry-run console                     — issue picker + Run + result
           — sticky footer: Cancel + Save (consistent height + alignment)
    ────────────────────────────────────────────────────────────────── -->
    <AppModal
      v-if="edit.open"
      :open="edit.open"
      :title="edit.isCreate ? 'New custom AI action' : `Edit · ${edit.row?.label}`"
      @close="closeEdit"
      max-width="960px"
    >
      <div class="ape-form">
        <!-- ── 1. IDENTITY (custom-only) ───────────────────────── -->
        <section v-if="edit.isCreate || !edit.row?.is_builtin" class="ape-card">
          <header class="ape-card-head">
            <h4 class="ape-card-title">Identity</h4>
            <p class="ape-card-hint">
              Key + label + surface define how the action is wired into
              the dispatcher. Key is locked once the row exists.
            </p>
          </header>
          <div class="ape-grid-3">
            <label class="ape-field">
              <span class="ape-field-label">Key</span>
              <input
                v-model="edit.draftKey"
                type="text"
                class="ape-input ape-input--mono"
                placeholder="my_custom_action"
                :disabled="!edit.isCreate"
                spellcheck="false"
              />
              <span class="ape-field-hint">Lowercase, digits, underscore. 3–32 chars.</span>
            </label>
            <label class="ape-field">
              <span class="ape-field-label">Label</span>
              <input
                v-model="edit.draftLabel"
                type="text"
                class="ape-input"
                placeholder="My custom action"
              />
              <span class="ape-field-hint">Shown in the AI dropdown menu.</span>
            </label>
            <label class="ape-field">
              <span class="ape-field-label">Surface</span>
              <select v-model="edit.draftSurface" class="ape-input">
                <option value="issue">Issue editor</option>
                <option value="customer">Customer fields</option>
              </select>
              <span class="ape-field-hint">Which editor surface this targets.</span>
            </label>
          </div>
        </section>

        <!-- ── 2. PLACEMENT ─────────────────────────────────────── -->
        <section class="ape-card">
          <header class="ape-card-head">
            <h4 class="ape-card-title">Placement</h4>
            <p class="ape-card-hint">
              Where this action appears.
              <strong>Text fields</strong> = inline next to textareas.
              <strong>Issue menu</strong> = in the issue header / sidebar.
              <strong>Both</strong> = everywhere.
              <strong>Default</strong> = use the registry default<span v-if="edit.row?.default_placement"> (<code>{{ edit.row.default_placement }}</code>)</span>.
            </p>
          </header>
          <div class="ape-radio-grid">
            <label
              :class="['ape-radio', { 'ape-radio--active': edit.draftPlacement === '' }]"
            >
              <input type="radio" v-model="edit.draftPlacement" value="" />
              <span class="ape-radio-mark" aria-hidden="true" />
              <span class="ape-radio-text">
                <strong>Default</strong>
                <span v-if="edit.row?.default_placement" class="ape-radio-default">{{ edit.row.default_placement }}</span>
                <span v-else class="ape-radio-default">registry</span>
              </span>
            </label>
            <label
              :class="['ape-radio', { 'ape-radio--active': edit.draftPlacement === 'text' }]"
            >
              <input type="radio" v-model="edit.draftPlacement" value="text" />
              <span class="ape-radio-mark" aria-hidden="true" />
              <span class="ape-radio-text">
                <strong>Text fields</strong>
                <span class="ape-radio-default">inline</span>
              </span>
            </label>
            <label
              :class="['ape-radio', { 'ape-radio--active': edit.draftPlacement === 'issue' }]"
            >
              <input type="radio" v-model="edit.draftPlacement" value="issue" />
              <span class="ape-radio-mark" aria-hidden="true" />
              <span class="ape-radio-text">
                <strong>Issue menu</strong>
                <span class="ape-radio-default">record-level</span>
              </span>
            </label>
            <label
              :class="['ape-radio', { 'ape-radio--active': edit.draftPlacement === 'both' }]"
            >
              <input type="radio" v-model="edit.draftPlacement" value="both" />
              <span class="ape-radio-mark" aria-hidden="true" />
              <span class="ape-radio-text">
                <strong>Both</strong>
                <span class="ape-radio-default">everywhere</span>
              </span>
            </label>
          </div>
        </section>

        <!-- ── 3. STATUS — iOS-style toggle row ─────────────────── -->
        <section class="ape-card ape-toggle-card">
          <div class="ape-toggle-meta">
            <strong class="ape-toggle-label">Action enabled</strong>
            <span class="ape-toggle-hint">When off, the action is hidden from the AI dropdown menu — useful for parking an action without deleting it.</span>
          </div>
          <label class="ape-switch" :title="edit.draftEnabled ? 'Disable action' : 'Enable action'">
            <input v-model="edit.draftEnabled" type="checkbox" />
            <span class="ape-switch-track" />
          </label>
        </section>

        <!-- ── 4. PROMPT TEMPLATE ───────────────────────────────── -->
        <section class="ape-card">
          <header class="ape-card-head">
            <h4 class="ape-card-title">Prompt template</h4>
            <p class="ape-card-hint">
              Empty for built-in rows means "use the code-defined default".
              Click any chip to insert <code>{{ titleVarExample }}</code> at the cursor.
            </p>
          </header>
          <div class="ape-vars">
            <button
              v-for="v in surfaceVars" :key="v"
              type="button" class="ape-var" @click="insertVar(v)"
              :title="`Insert ${varDisplay(v)} at cursor`"
            >{{ varDisplay(v) }}</button>
          </div>
          <textarea
            ref="promptTextarea"
            v-model="edit.draftTemplate"
            rows="14"
            class="ape-textarea"
            spellcheck="false"
            placeholder="You are an editor inside PAIMOS. Rewrite the field below…"
          ></textarea>
        </section>

        <!-- ── 5. DRY-RUN CONSOLE ───────────────────────────────── -->
        <section v-if="edit.row && !edit.isCreate" class="ape-card ape-console">
          <header class="ape-card-head">
            <h4 class="ape-card-title">
              <AppIcon name="play-circle" :size="13" class="ape-card-title-icon" />
              Dry-run preview
            </h4>
            <p class="ape-card-hint">
              Render the template against a real issue and call the LLM.
              Strictly preview — nothing is mutated.
            </p>
          </header>
          <div class="ape-console-controls">
            <div class="ape-console-issue">
              <span class="ape-field-label">Against issue</span>
              <IssueSearchInput v-model="dryRunIssueId" />
            </div>
            <button
              type="button"
              class="btn btn-primary ape-run-btn"
              :disabled="dryRunRunning"
              @click="dryRun"
            >
              <AppIcon :name="dryRunRunning ? 'loader-circle' : 'play'" :size="13" :class="{ 'ape-spin': dryRunRunning }" />
              {{ dryRunRunning ? 'Running…' : 'Run preview' }}
            </button>
          </div>

          <AiActivityStrip
            v-if="dryRunRunning && dryRunStartedAt"
            action-key="dry_run"
            title="Prompt dry-run"
            :started-at="dryRunStartedAt"
          />

          <p v-if="dryRunError" class="ape-banner ape-banner--error">
            <AppIcon name="alert-triangle" :size="14" /> {{ dryRunError }}
          </p>

          <AiResultStrip
            v-if="dryRunResult"
            action-key="dry_run"
            title="Dry-run result"
            :summary="dryRunSummary"
            details-label="Details"
            :dismissable="true"
            @dismiss="clearDryRun"
          >
          <div class="ape-result">
            <div class="ape-result-meta">
              <span v-if="dryRunResult.model" class="ape-result-pill">
                <AppIcon name="cpu" :size="11" /> {{ dryRunResult.model }}
              </span>
              <span v-if="dryRunResult.latency_ms" class="ape-result-pill">
                <AppIcon name="zap" :size="11" /> {{ dryRunResult.latency_ms }} ms
              </span>
              <span v-if="(dryRunResult.prompt_tokens ?? 0) + (dryRunResult.completion_tokens ?? 0) > 0" class="ape-result-pill">
                {{ dryRunResult.prompt_tokens }}p + {{ dryRunResult.completion_tokens }}c tokens
              </span>
              <span v-if="dryRunResult.used_default" class="ape-result-pill ape-result-pill--default">
                code default (no override)
              </span>
              <button type="button" class="ape-result-clear" @click="clearDryRun" title="Clear preview">
                <AppIcon name="x" :size="12" /> Clear
              </button>
            </div>
            <div class="ape-result-grid">
              <div class="ape-result-pane">
                <div class="ape-result-pane-head">
                  <AppIcon name="file-text" :size="12" />
                  <span>Rendered prompt</span>
                </div>
                <pre class="ape-result-pane-body">{{ (dryRunResult.rendered_system ?? '') + (dryRunResult.rendered_user ? '\n\n— USER —\n\n' + dryRunResult.rendered_user : '') }}</pre>
              </div>
              <div class="ape-result-pane">
                <div class="ape-result-pane-head">
                  <AppIcon name="message-square" :size="12" />
                  <span>Model response</span>
                </div>
                <pre class="ape-result-pane-body">{{ dryRunResult.response ?? '' }}</pre>
              </div>
            </div>
          </div>
          </AiResultStrip>
        </section>

        <p v-if="saveError" class="ape-banner ape-banner--error ape-banner--save">
          <AppIcon name="alert-triangle" :size="14" /> {{ saveError }}
        </p>
      </div>

      <!-- ── Sticky footer: Cancel + Save, consistent buttons ───── -->
      <footer class="ape-footer">
        <span class="ape-footer-meta">
          <template v-if="edit.row?.is_builtin">
            <code>{{ edit.row.key }}</code>
            <span class="ape-footer-tag">built-in</span>
          </template>
          <template v-else-if="edit.isCreate">
            <span class="ape-footer-hint">A new custom action will be created.</span>
          </template>
          <template v-else-if="edit.row">
            <code>{{ edit.row.key }}</code>
            <span class="ape-footer-tag ape-footer-tag--custom">custom</span>
          </template>
        </span>
        <div class="ape-footer-buttons">
          <button class="ape-btn ape-btn--ghost" @click="closeEdit" :disabled="saving">Cancel</button>
          <button
            class="ape-btn ape-btn--primary"
            :disabled="saving"
            @click="save"
          >
            <AppIcon v-if="saving" name="loader-circle" :size="13" class="ape-spin" />
            {{ saving ? 'Saving…' : (edit.isCreate ? 'Create action' : 'Save changes') }}
          </button>
        </div>
      </footer>
    </AppModal>
  </div>
</template>

<style scoped>
/* ──────────────────────────────────────────────────────────────────
   TAB CHROME (list view) — left mostly intact from prior PAI-176;
   only minor harmonisation with the new modal's spacing language.
─────────────────────────────────────────────────────────────────── */
.ap-tab { display: flex; flex-direction: column; gap: 1rem; max-width: 1200px; }
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
.ap-hero-title {
  margin: 0 0 .25rem;
  font-size: 17px; font-weight: 700;
  font-family: 'Bricolage Grotesque', 'DM Sans', sans-serif;
  letter-spacing: -.018em;
}
.ap-hero-desc { margin: 0; font-size: 13px; color: var(--text-muted); line-height: 1.55; max-width: 720px; }

.ap-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 1.1rem 1.25rem;
  display: flex; flex-direction: column; gap: .65rem;
}
.ap-section-headrow { display: flex; align-items: center; gap: .55rem; }
.ap-section-title {
  margin: 0;
  font-size: 11px; font-weight: 700; letter-spacing: .075em;
  text-transform: uppercase; color: var(--text);
}
.ap-section-meta { font-family: 'DM Mono', monospace; font-size: 11px; color: var(--text-muted); }
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
.ap-row-place {
  font-size: 9.5px; font-weight: 700;
  letter-spacing: .08em; text-transform: uppercase;
  padding: .12rem .45rem; border-radius: 999px;
  font-family: 'DM Sans', sans-serif;
}
.ap-row-place--text  { background: #d1fae5; color: #065f46; }
.ap-row-place--issue { background: #fef3c7; color: #92400e; }
.ap-row-place--both  { background: #fce7f3; color: #9d174d; }
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

/* General banner styling shared between the tab and the modal. */
.ap-banner, .ape-banner {
  margin: 0; padding: .6rem .85rem;
  border-radius: 8px; font-size: 12.5px;
  display: inline-flex; align-items: center; gap: .45rem;
}
.ap-banner--error, .ape-banner--error {
  background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca;
}

/* ──────────────────────────────────────────────────────────────────
   PAI-180: EDIT MODAL.

   The class prefix moves from `.ap-edit-*` (the broken first cut) to
   `.ape-*` so a future ripgrep cleanly tags the redesigned styles.
   The body is a stack of "cards" with breathing room; section heads
   read like a quiet IDE settings panel; controls are deliberately
   uniform in height (40px standard) and spacing.
─────────────────────────────────────────────────────────────────── */
.ape-form {
  display: flex; flex-direction: column;
  gap: .85rem;
  /* Counteract AppModal's body padding (1.5rem) on the bottom so the
     sticky footer below can break flush against the modal edge. */
  margin-bottom: -1.5rem;
}

.ape-card {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 11px;
  padding: 1rem 1.15rem 1.05rem;
  display: flex; flex-direction: column; gap: .65rem;
}

.ape-card-head { display: flex; flex-direction: column; gap: .15rem; }
.ape-card-title {
  margin: 0;
  font-size: 11px; font-weight: 700;
  letter-spacing: .075em; text-transform: uppercase;
  color: var(--text);
  display: inline-flex; align-items: center; gap: .35rem;
  font-family: 'DM Sans', sans-serif;
}
.ape-card-title-icon { color: var(--bp-blue-dark); }
.ape-card-hint {
  margin: 0;
  font-size: 12px; line-height: 1.55;
  color: var(--text-muted);
  max-width: 80ch;
}
.ape-card-hint code {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 10.5px;
  background: white; border: 1px solid var(--border); border-radius: 4px;
  padding: 0 .3rem; color: var(--text);
}

/* Generic form field used inside cards. */
.ape-field { display: flex; flex-direction: column; gap: .25rem; min-width: 0; }
.ape-field-label {
  font-size: 10.5px; font-weight: 700;
  letter-spacing: .08em; text-transform: uppercase;
  color: var(--text-muted);
  font-family: 'DM Sans', sans-serif;
}
.ape-field-hint { font-size: 11.5px; color: var(--text-muted); line-height: 1.45; }

.ape-input {
  font-family: 'DM Sans', sans-serif;
  font-size: 13px;
  padding: .55rem .7rem;
  min-height: 40px;
  border: 1.5px solid var(--border);
  border-radius: 8px;
  background: white; color: var(--text);
  width: 100%; box-sizing: border-box;
  transition: border-color .14s, box-shadow .14s;
}
.ape-input:focus {
  outline: none;
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px var(--bp-blue-pale);
}
.ape-input--mono { font-family: 'DM Mono', 'JetBrains Mono', monospace; font-size: 12.5px; }
.ape-input:disabled { background: var(--bg-card); color: var(--text-muted); cursor: not-allowed; }

/* 3-column layout for the identity card; collapses below 720px. */
.ape-grid-3 {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: .75rem;
}
@media (max-width: 720px) { .ape-grid-3 { grid-template-columns: 1fr; } }

/* ── Placement: 4-up segmented radio cards ──────────────────────── */
.ape-radio-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: .55rem;
}
@media (max-width: 720px) { .ape-radio-grid { grid-template-columns: repeat(2, 1fr); } }

.ape-radio {
  display: flex; align-items: center;
  gap: .65rem;
  padding: .7rem .85rem;
  background: white;
  border: 1.5px solid var(--border);
  border-radius: 9px;
  cursor: pointer;
  transition: border-color .14s, background .14s, transform .12s;
}
.ape-radio:hover { border-color: var(--bp-blue-light); transform: translateY(-1px); }
.ape-radio--active {
  background: var(--bp-blue-pale);
  border-color: var(--bp-blue);
}
.ape-radio > input[type="radio"] {
  /* Visually hidden — the .ape-radio-mark is the visual control. */
  position: absolute; opacity: 0; width: 0; height: 0;
}
.ape-radio-mark {
  width: 16px; height: 16px;
  border-radius: 50%;
  border: 1.5px solid var(--border);
  flex-shrink: 0;
  position: relative;
  transition: border-color .14s, background .14s;
}
.ape-radio--active .ape-radio-mark {
  border-color: var(--bp-blue);
  background: var(--bp-blue);
}
.ape-radio--active .ape-radio-mark::after {
  content: '';
  position: absolute; top: 3px; left: 3px;
  width: 8px; height: 8px;
  border-radius: 50%;
  background: white;
}
.ape-radio-text {
  display: flex; flex-direction: column; gap: 2px;
  min-width: 0;
}
.ape-radio-text > strong {
  font-size: 12.5px; font-weight: 600;
  color: var(--text);
  letter-spacing: -.005em;
}
.ape-radio--active .ape-radio-text > strong { color: var(--bp-blue-dark); }
.ape-radio-default {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 10px;
  color: var(--text-muted);
  letter-spacing: 0;
}
.ape-radio--active .ape-radio-default { color: var(--bp-blue-dark); opacity: .8; }

/* ── Status: toggle-card ────────────────────────────────────────── */
.ape-toggle-card {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.ape-toggle-meta { display: flex; flex-direction: column; gap: .15rem; min-width: 0; flex: 1; }
.ape-toggle-label {
  font-size: 14px; font-weight: 600;
  color: var(--text); letter-spacing: -.005em;
}
.ape-toggle-hint { font-size: 12px; color: var(--text-muted); line-height: 1.5; }

/* iOS-style switch (mirrors SettingsAITab's switch language). */
.ape-switch {
  position: relative; display: inline-block;
  width: 42px; height: 24px;
  cursor: pointer;
  flex-shrink: 0;
}
.ape-switch input { opacity: 0; width: 0; height: 0; position: absolute; }
.ape-switch-track {
  position: absolute; inset: 0;
  background: #cbd5e1;
  border-radius: 999px;
  transition: background .2s ease;
}
.ape-switch-track::before {
  content: '';
  position: absolute;
  width: 20px; height: 20px;
  left: 2px; top: 2px;
  background: white;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgba(0,0,0,.22), 0 0 0 .5px rgba(0,0,0,.04);
  transition: transform .22s cubic-bezier(.4, 1.4, .6, 1);
}
.ape-switch input:checked + .ape-switch-track { background: var(--bp-blue); }
.ape-switch input:checked + .ape-switch-track::before { transform: translateX(18px); }

/* ── Prompt template ────────────────────────────────────────────── */
.ape-vars { display: flex; flex-wrap: wrap; gap: .3rem; }
.ape-var {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 10.75px;
  color: var(--bp-blue-dark);
  background: var(--bp-blue-pale);
  border: 1px solid transparent;
  border-radius: 6px;
  padding: .2rem .5rem;
  cursor: pointer;
  transition: background .12s, color .12s, transform .1s;
}
.ape-var:hover { background: var(--bp-blue); color: white; transform: translateY(-1px); }

.ape-textarea {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 12.5px; line-height: 1.65;
  padding: .9rem 1rem;
  border: 1.5px solid var(--border);
  border-radius: 9px;
  background: white;
  color: var(--text);
  resize: vertical;
  min-height: 240px;
  width: 100%; box-sizing: border-box;
  transition: border-color .14s, box-shadow .14s;
}
.ape-textarea:focus {
  outline: none;
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px var(--bp-blue-pale);
}

/* ── Dry-run console ────────────────────────────────────────────── */
.ape-console {
  background: linear-gradient(180deg, var(--bg) 0%, var(--bg-card) 100%);
}
.ape-console-controls {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: .75rem;
  align-items: end;
}
@media (max-width: 600px) { .ape-console-controls { grid-template-columns: 1fr; } }
.ape-console-issue { display: flex; flex-direction: column; gap: .25rem; min-width: 0; }
.ape-run-btn {
  /* Same height as the issue search input for clean alignment. */
  min-height: 40px;
  padding: 0 1.1rem;
  display: inline-flex; align-items: center; gap: .4rem;
  font-weight: 600;
}

.ape-result {
  display: flex; flex-direction: column;
  gap: .6rem;
  margin-top: .15rem;
}
.ape-result-meta {
  display: flex; flex-wrap: wrap; align-items: center;
  gap: .35rem;
}
.ape-result-pill {
  display: inline-flex; align-items: center; gap: .3rem;
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 10.75px;
  padding: .2rem .55rem;
  border-radius: 999px;
  background: white;
  border: 1px solid var(--border);
  color: var(--text);
}
.ape-result-pill--default {
  background: #fef3c7;
  border-color: #fde68a;
  color: #92400e;
}
.ape-result-clear {
  margin-left: auto;
  display: inline-flex; align-items: center; gap: .3rem;
  background: none; border: none;
  color: var(--text-muted);
  cursor: pointer; padding: .15rem .4rem;
  border-radius: 5px;
  font-size: 11px;
  font-family: 'DM Sans', sans-serif;
}
.ape-result-clear:hover { background: var(--bg-card); color: var(--text); }

.ape-result-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: .6rem;
}
@media (max-width: 720px) { .ape-result-grid { grid-template-columns: 1fr; } }

.ape-result-pane {
  display: flex; flex-direction: column;
  background: white;
  border: 1px solid var(--border);
  border-radius: 9px;
  overflow: hidden;
}
.ape-result-pane-head {
  display: flex; align-items: center; gap: .35rem;
  padding: .45rem .7rem;
  font-size: 10.5px; font-weight: 700;
  letter-spacing: .07em; text-transform: uppercase;
  color: var(--text-muted);
  background: var(--bg);
  border-bottom: 1px solid var(--border);
}
.ape-result-pane-body {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 11.75px; line-height: 1.6;
  margin: 0;
  padding: .75rem .85rem;
  color: var(--text);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 320px;
  overflow: auto;
}

/* ── Save banner spacing inside the modal body ──────────────────── */
.ape-banner--save {
  /* Keep the save error visible above the sticky footer, but break
     out of the negative-margin trick the form uses for the footer. */
  margin-bottom: 1.5rem;
}

/* ── Sticky footer ──────────────────────────────────────────────── */
.ape-footer {
  position: sticky;
  bottom: -1.5rem; /* counter the modal body's 1.5rem bottom padding */
  z-index: 1;
  display: flex; align-items: center; justify-content: space-between;
  gap: 1rem;
  padding: .9rem 1.5rem;
  /* Break out of body padding so the footer goes edge-to-edge. */
  margin: 1rem -1.5rem -1.5rem;
  background: var(--bg-card);
  border-top: 1px solid var(--border);
  /* Subtle blur so content scrolling beneath stays legible without
     bleeding straight through. */
  backdrop-filter: saturate(1.05) blur(6px);
  -webkit-backdrop-filter: saturate(1.05) blur(6px);
}
.ape-footer-meta {
  display: inline-flex; align-items: center;
  gap: .5rem;
  font-size: 12px;
  color: var(--text-muted);
  min-width: 0;
}
.ape-footer-meta > code {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 11.5px;
  background: white; border: 1px solid var(--border);
  border-radius: 5px; padding: .1rem .4rem;
  color: var(--text);
}
.ape-footer-tag {
  font-size: 9.5px; font-weight: 700;
  letter-spacing: .08em; text-transform: uppercase;
  padding: .1rem .42rem; border-radius: 999px;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
}
.ape-footer-tag--custom { background: #fce7f3; color: #9d174d; }
.ape-footer-hint { font-style: italic; }

.ape-footer-buttons { display: inline-flex; gap: .55rem; }

/* Buttons rebuilt locally so Cancel + Save are guaranteed identical
   shape regardless of what the global .btn rules end up doing. */
.ape-btn {
  display: inline-flex; align-items: center; justify-content: center;
  gap: .4rem;
  min-height: 40px;
  padding: 0 1.15rem;
  border-radius: 9px;
  font-family: 'DM Sans', sans-serif;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -.005em;
  cursor: pointer;
  border: 1.5px solid var(--border);
  background: white;
  color: var(--text);
  transition: background .14s, border-color .14s, transform .12s, box-shadow .14s;
}
.ape-btn:disabled { opacity: .55; cursor: not-allowed; }
.ape-btn:hover:not(:disabled) { transform: translateY(-1px); }

.ape-btn--ghost {
  background: white;
  border-color: var(--border);
  color: var(--text);
}
.ape-btn--ghost:hover:not(:disabled) { background: var(--bg); border-color: var(--bp-blue-light); }

.ape-btn--primary {
  background: var(--bp-blue);
  border-color: var(--bp-blue);
  color: white;
  box-shadow: 0 1px 0 rgba(0,0,0,.04), 0 4px 10px rgba(46, 109, 164, .18);
}
.ape-btn--primary:hover:not(:disabled) {
  background: var(--bp-blue-dark, #1f4d75);
  border-color: var(--bp-blue-dark, #1f4d75);
}

/* Spinner — namespaced so it doesn't collide with any global .spin. */
.ape-spin { animation: ape-spin 1s linear infinite; }
@keyframes ape-spin { to { transform: rotate(360deg); } }

/* Responsive: shrink the footer padding on narrow viewports so the
   buttons don't overflow on a 360px-wide popover. */
@media (max-width: 520px) {
  .ape-footer { flex-direction: column; align-items: stretch; gap: .5rem; padding: .85rem 1rem; }
  .ape-footer-buttons { justify-content: flex-end; }
}
</style>
