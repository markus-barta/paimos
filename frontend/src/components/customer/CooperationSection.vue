<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-62. Cooperation profile section on the project detail view.
 Two modes:
   - View mode (default): structured fields shown as labelled value
     pills, freeform fields rendered as markdown. Read-only-friendly.
   - Edit mode (admin only, opt-in): all fields editable inline; Save
     pushes the upsert; Cancel reverts.
 Empty state ("No cooperation profile set yet") gets a "Set up" button
 for admins so first-time use isn't a hidden form.
-->
<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { api, errMsg } from '@/api/client'
import { useMarkdown } from '@/composables/useMarkdown'
import type { CooperationMetadata } from '@/types'
// PAI-146 expansion: AI optimize on the cooperation freeform fields.
// SLA text gets a "preserve every number verbatim" reminder; the
// cooperation notes get a "preserve named systems and ownership
// boundaries" reminder. Both via dedicated field IDs in prompt.go.
import AiActionMenu from '@/components/ai/AiActionMenu.vue'
import AiSurfaceFeedback from '@/components/ai/AiSurfaceFeedback.vue'
function onSlaDetailsAccept(text: string) {
  if (draft.value) draft.value.sla_details = text
}
function onCooperationNotesAccept(text: string) {
  if (draft.value) draft.value.cooperation_notes = text
}

const props = defineProps<{ projectId: number; canWrite: boolean }>()

// Emit `populated` so a parent (ProjectDetailView's segmented control)
// can show an (i) badge on the Cooperation tab without inspecting the
// row itself.
const emit = defineEmits<{ populated: [v: boolean] }>()

const data = ref<CooperationMetadata | null>(null)
const loading = ref(true)
const loadError = ref('')

const editing = ref(false)
const draft = ref<CooperationMetadata | null>(null)
const saving = ref(false)
const saveError = ref('')

// ── Enum option tables ─────────────────────────────────────────────
// Single source of truth — both the dropdowns in edit mode and the
// human-readable labels in view mode read from here so a future enum
// change touches one place.
const ENGAGEMENT_OPTIONS = [
  { value: 'consultancy',       label: 'Consultancy' },
  { value: 'project_delivery',  label: 'Project delivery' },
  { value: 'managed_service',   label: 'Managed service' },
  { value: 'retainer',          label: 'Retainer' },
] as const
const CODE_OWNERSHIP_OPTIONS = [
  { value: 'client_repo', label: 'Client repo' },
  { value: 'own_repo',    label: 'Own repo' },
  { value: 'mixed',       label: 'Mixed' },
] as const
const ENV_OPTIONS = [
  { value: 'dev_staging',      label: 'Dev + Staging' },
  { value: 'dev_staging_prod', label: 'Dev + Staging + Prod' },
  { value: 'full_stack',       label: 'Full stack (incl. infra)' },
] as const

function labelFor(options: ReadonlyArray<{ value: string; label: string }>, value: string | null): string {
  if (!value) return ''
  return options.find(o => o.value === value)?.label ?? value
}

// `populated` = at least one structured field, SLA flag, or freeform
// note is set. Drives the empty-state vs. content branch — and feeds
// the parent's segmented-control badge via the `populated` emit below.
const populated = computed(() => {
  if (!data.value) return false
  const d = data.value
  return !!(d.engagement_type || d.code_ownership || d.env_responsibility
            || d.has_sla || d.uptime_sla || d.response_time_sla
            || d.backup_responsible || d.oncall
            || d.sla_details || d.cooperation_notes)
})

watch(populated, (v) => emit('populated', v), { immediate: true })

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    data.value = await api.get<CooperationMetadata>(`/projects/${props.projectId}/cooperation`)
  } catch (e: unknown) {
    loadError.value = errMsg(e, 'Failed to load cooperation profile.')
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(() => props.projectId, load)

function startEdit() {
  if (!data.value) return
  // Deep-enough clone — no nested objects, so spread is sufficient.
  draft.value = { ...data.value }
  saveError.value = ''
  editing.value = true
}
function cancelEdit() {
  editing.value = false
  draft.value = null
  saveError.value = ''
}
async function save() {
  if (!draft.value) return
  saving.value = true
  saveError.value = ''
  try {
    data.value = await api.put<CooperationMetadata>(
      `/projects/${props.projectId}/cooperation`,
      draft.value,
    )
    editing.value = false
    draft.value = null
  } catch (e: unknown) {
    saveError.value = errMsg(e, 'Save failed.')
  } finally {
    saving.value = false
  }
}

// ── Markdown rendering for view mode ────────────────────────────────
// Two reactive strings → two HTML refs. Parser stays cached/sanitised
// inside useMarkdown.
const slaSrc   = computed(() => data.value?.sla_details ?? '')
const notesSrc = computed(() => data.value?.cooperation_notes ?? '')
const mdEnabled = ref(true)
const { html: slaHtml }   = useMarkdown(slaSrc,   mdEnabled)
const { html: notesHtml } = useMarkdown(notesSrc, mdEnabled)
</script>

<template>
  <section class="coop-section">
    <header class="coop-header">
      <div>
        <h3 class="coop-title">Cooperation profile</h3>
        <p class="coop-hint">
          How this engagement runs — informational; doesn't affect billing or workflows.
        </p>
      </div>
      <div v-if="canWrite && populated && !editing" class="coop-actions">
        <button class="btn btn-ghost btn-sm" @click="startEdit">
          <AppIcon name="pencil" :size="14" /> Edit
        </button>
      </div>
    </header>

    <div v-if="loading" class="coop-loading">Loading…</div>
    <div v-else-if="loadError" class="coop-error">{{ loadError }}</div>

    <!-- ── Empty state ────────────────────────────────────────── -->
    <div v-else-if="!populated && !editing" class="coop-empty">
      <AppIcon name="file-text" :size="22" />
      <div>
        <strong>No cooperation profile set yet.</strong>
        <p>Capture engagement type, code ownership, SLA terms and other context here.</p>
        <button v-if="canWrite" class="btn btn-primary btn-sm" @click="startEdit">
          <AppIcon name="plus" :size="14" /> Set up profile
        </button>
      </div>
    </div>

    <!-- ── View mode ──────────────────────────────────────────── -->
    <div v-else-if="!editing && data" class="coop-view">
      <div class="coop-grid">
        <div class="coop-field" v-if="data.engagement_type">
          <span class="coop-field-label">Engagement</span>
          <span class="coop-pill">{{ labelFor(ENGAGEMENT_OPTIONS, data.engagement_type) }}</span>
        </div>
        <div class="coop-field" v-if="data.code_ownership">
          <span class="coop-field-label">Code ownership</span>
          <span class="coop-pill">{{ labelFor(CODE_OWNERSHIP_OPTIONS, data.code_ownership) }}</span>
        </div>
        <div class="coop-field" v-if="data.env_responsibility">
          <span class="coop-field-label">Environment</span>
          <span class="coop-pill">{{ labelFor(ENV_OPTIONS, data.env_responsibility) }}</span>
        </div>
      </div>

      <div v-if="data.has_sla" class="coop-sla">
        <header class="coop-subhead">
          <AppIcon name="shield-check" :size="14" />
          <span>SLA in place</span>
        </header>
        <div class="coop-sla-grid">
          <div v-if="data.uptime_sla" class="coop-field">
            <span class="coop-field-label">Uptime</span>
            <span class="coop-mono">{{ data.uptime_sla }}</span>
          </div>
          <div v-if="data.response_time_sla" class="coop-field">
            <span class="coop-field-label">Response time</span>
            <span class="coop-mono">{{ data.response_time_sla }}</span>
          </div>
          <div class="coop-field">
            <span class="coop-field-label">Backup</span>
            <span :class="['coop-flag', data.backup_responsible ? 'coop-flag--yes' : 'coop-flag--no']">
              {{ data.backup_responsible ? 'Our responsibility' : 'Customer' }}
            </span>
          </div>
          <div class="coop-field">
            <span class="coop-field-label">On-call</span>
            <span :class="['coop-flag', data.oncall ? 'coop-flag--yes' : 'coop-flag--no']">
              {{ data.oncall ? 'On-call rotation' : 'No on-call' }}
            </span>
          </div>
        </div>
        <div v-if="data.sla_details" class="coop-md" v-html="slaHtml" />
      </div>

      <div v-if="data.cooperation_notes" class="coop-notes">
        <header class="coop-subhead">
          <AppIcon name="notebook-pen" :size="14" />
          <span>Notes</span>
        </header>
        <div class="coop-md" v-html="notesHtml" />
      </div>
    </div>

    <!-- ── Edit mode ──────────────────────────────────────────── -->
    <form v-else-if="editing && draft" class="coop-form" @submit.prevent="save">
      <div class="coop-form-grid">
        <div class="coop-form-field">
          <label>Engagement type</label>
          <select v-model="draft.engagement_type">
            <option :value="null">— Not set —</option>
            <option v-for="o in ENGAGEMENT_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </div>
        <div class="coop-form-field">
          <label>Code ownership</label>
          <select v-model="draft.code_ownership">
            <option :value="null">— Not set —</option>
            <option v-for="o in CODE_OWNERSHIP_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </div>
        <div class="coop-form-field">
          <label>Environment responsibility</label>
          <select v-model="draft.env_responsibility">
            <option :value="null">— Not set —</option>
            <option v-for="o in ENV_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </div>
      </div>

      <div class="coop-form-sla">
        <label class="coop-toggle-label">
          <input type="checkbox" v-model="draft.has_sla" />
          <span>SLA in place</span>
        </label>

        <div v-if="draft.has_sla" class="coop-form-grid">
          <div class="coop-form-field">
            <label>Uptime SLA</label>
            <input v-model="draft.uptime_sla" type="text" placeholder="e.g. 99.9%" />
          </div>
          <div class="coop-form-field">
            <label>Response time SLA</label>
            <input v-model="draft.response_time_sla" type="text" placeholder="e.g. P1: 4h, P2: 8h" />
          </div>
          <div class="coop-form-field coop-form-toggles">
            <label class="coop-toggle-label">
              <input type="checkbox" v-model="draft.backup_responsible" />
              <span>Backup is our responsibility</span>
            </label>
            <label class="coop-toggle-label">
              <input type="checkbox" v-model="draft.oncall" />
              <span>On-call rotation</span>
            </label>
          </div>
          <div class="coop-form-field coop-form-fullwidth">
            <div class="coop-field-label-row">
              <label>SLA details <span class="label-hint">— markdown supported</span></label>
              <AiActionMenu surface="customer"
                host-key="cooperation:sla_details"
                field="cooperation_sla_details"
                field-label="SLA details"
                :issue-id="0"
                :text="() => draft?.sla_details ?? ''"
                :on-accept="onSlaDetailsAccept"
              />
            </div>
            <textarea v-model="draft.sla_details" rows="4" placeholder="Detailed SLA terms, escalation path…" />
            <AiSurfaceFeedback host-key="cooperation:sla_details" />
          </div>
        </div>
      </div>

      <div class="coop-form-field">
        <div class="coop-field-label-row">
          <label>Cooperation notes <span class="label-hint">— markdown supported</span></label>
          <AiActionMenu surface="customer"
            host-key="cooperation:notes"
            field="cooperation_notes"
            field-label="Cooperation notes"
            :issue-id="0"
            :text="() => draft?.cooperation_notes ?? ''"
            :on-accept="onCooperationNotesAccept"
          />
        </div>
        <textarea v-model="draft.cooperation_notes" rows="4"
                  placeholder="Data retention, special arrangements, anything else worth knowing." />
        <AiSurfaceFeedback host-key="cooperation:notes" />
      </div>

      <p v-if="saveError" class="coop-error">{{ saveError }}</p>

      <div class="coop-form-actions">
        <button type="button" class="btn btn-ghost" @click="cancelEdit"><u>C</u>ancel</button>
        <button type="submit" class="btn btn-primary" :disabled="saving">
          {{ saving ? 'Saving…' : 'Save profile' }}
        </button>
      </div>
    </form>
  </section>
</template>

<style scoped>
/* PAI-146: per-field label row holds the label + the AI optimize
   button. Namespaced (.coop-field-label-row) so it doesn't collide
   with similarly-purposed rules in sibling components. */
.coop-field-label-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
  margin-bottom: .25rem;
}
.coop-field-label-row > label { margin-bottom: 0; }

.coop-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 1.25rem 1.4rem;
  display: flex; flex-direction: column; gap: 1rem;
}
.coop-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 1rem; }
.coop-title { font-size: 14px; font-weight: 700; color: var(--text); margin: 0 0 .15rem; letter-spacing: -.01em; }
.coop-hint  { font-size: 12px; color: var(--text-muted); margin: 0; }

.coop-loading { color: var(--text-muted); font-size: 13px; }
.coop-error {
  background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca;
  padding: .5rem .75rem; border-radius: var(--radius); font-size: 13px;
}

/* ── Empty state ──────────────────────────────────────────────── */
.coop-empty {
  display: flex; gap: 1rem; align-items: flex-start;
  padding: 1.25rem; border: 1px dashed var(--border); border-radius: 8px;
  color: var(--text-muted);
}
.coop-empty strong { color: var(--text); display: block; margin-bottom: .15rem; }
.coop-empty p { margin: 0 0 .65rem; font-size: 13px; line-height: 1.55; }

/* ── View mode ────────────────────────────────────────────────── */
.coop-view { display: flex; flex-direction: column; gap: 1.25rem; }
.coop-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: .75rem 1.25rem;
}
.coop-field { display: flex; flex-direction: column; gap: .25rem; min-width: 0; }
.coop-field-label {
  font-size: 10px; font-weight: 700; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .07em;
}

.coop-pill {
  display: inline-flex; align-items: center;
  padding: .25rem .65rem;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  border-radius: 999px; font-size: 12px; font-weight: 600;
  align-self: flex-start;
}
.coop-mono {
  font-family: 'DM Mono', monospace; font-size: 13px; font-variant-numeric: tabular-nums;
  color: var(--text);
}

.coop-flag {
  display: inline-block;
  padding: .15rem .55rem;
  border-radius: 999px;
  font-size: 11px; font-weight: 600;
  align-self: flex-start;
}
.coop-flag--yes { background: #dcfce7; color: #166534; }
.coop-flag--no  { background: #f1f5f9; color: #64748b; }

.coop-sla, .coop-notes {
  background: #fafbfc;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: .85rem 1rem;
  display: flex; flex-direction: column; gap: .65rem;
}
.coop-subhead {
  display: flex; align-items: center; gap: .35rem;
  font-size: 11px; font-weight: 700; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .07em;
}
.coop-sla-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: .75rem 1.25rem;
}

.coop-md { font-size: 13px; line-height: 1.6; color: var(--text); }
.coop-md :deep(p) { margin: 0 0 .55rem; }
.coop-md :deep(p:last-child) { margin-bottom: 0; }
.coop-md :deep(code) {
  font-family: 'DM Mono', monospace; font-size: 12px;
  background: var(--bg); padding: .05rem .35rem; border-radius: 4px;
}

/* ── Form ─────────────────────────────────────────────────────── */
.coop-form { display: flex; flex-direction: column; gap: 1rem; }
.coop-form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: .85rem;
}
.coop-form-field { display: flex; flex-direction: column; gap: .35rem; }
.coop-form-field label {
  font-size: 12px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .05em;
}
.coop-form-fullwidth { grid-column: 1 / -1; }

.coop-form-sla {
  background: #fafbfc;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: .85rem 1rem;
  display: flex; flex-direction: column; gap: .85rem;
}
.coop-toggle-label {
  display: inline-flex; align-items: center; gap: .5rem;
  font-size: 13px; color: var(--text); cursor: pointer;
  user-select: none;
}
.coop-toggle-label input[type=checkbox] {
  width: 16px; height: 16px;
  accent-color: var(--bp-blue);
  cursor: pointer;
}
.coop-form-toggles { display: flex; flex-direction: column; gap: .5rem; justify-content: center; }
.coop-form-actions { display: flex; justify-content: flex-end; gap: .5rem; padding-top: .25rem; }
.label-hint { font-weight: 400; text-transform: none; letter-spacing: 0; font-size: 11px; color: var(--text-muted); }
textarea { resize: vertical; min-height: 70px; }
</style>
