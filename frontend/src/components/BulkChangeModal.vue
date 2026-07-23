<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import AppModal from '@/components/AppModal.vue'
import type { Issue, Sprint } from '@/types'
import { api, errMsg } from '@/api/client'
import { useIssueContext } from '@/composables/useIssueContext'
import { assignableIssueUsers } from '@/utils/users'
import { formatInteger } from '@/composables/useNumberFormat'

const { users, sprints, costUnits, releases } = useIssueContext()

const props = defineProps<{
  open: boolean
  selectedIds: Set<number>
  issues: Issue[]
  loadedSprints: Sprint[]
}>()

const emit = defineEmits<{
  close: []
  updated: [issue: Issue]
  done: []
}>()

const UNSET_VALUE = '__paimos_bulk_unset__'

// Chunk size for the modal's PATCH /issues calls. Backend MaxBatchSize
// is 100; staying under that with smaller chunks gives smoother progress
// without bloating any single request payload.
const BULK_CHUNK_SIZE = 50

const bulkField          = ref<string>('')
const bulkValue          = ref<string>(UNSET_VALUE)
const bulkSprintIds      = ref<number[]>([])
const bulkSprintMode     = ref<'add' | 'set' | 'remove'>('add')
const bulkSprintSearch   = ref('')
const bulkChanging       = ref(false)
const bulkChangeError    = ref('')
const bulkProgress       = ref<{ done: number; total: number }>({ done: 0, total: 0 })
// Owned per-execution AbortController. cancelBulk() aborts in-flight
// chunks so closing the modal doesn't keep firing PATCH calls behind
// the user's back. The currently-committed chunks stay committed
// (each was its own atomic batch) — that's expected.
let bulkAbort: AbortController | null = null

const bulkProgressPct = computed(() =>
  bulkProgress.value.total > 0
    ? Math.round((bulkProgress.value.done / bulkProgress.value.total) * 100)
    : 0,
)

const assignableUsers = computed(() => assignableIssueUsers(users.value))

const BULK_FIELDS = computed(() => {
  const fields = [
    { value: 'status',   label: 'Status'   },
    { value: 'priority', label: 'Priority' },
    { value: 'assignee', label: 'Assignee' },
    { value: 'parent',   label: 'Parent'   },
  ]
  if (sprints.value.length) fields.push({ value: 'sprint', label: 'Sprint' })
  fields.push({ value: 'cost_unit', label: 'Cost Unit' })
  fields.push({ value: 'release',  label: 'Release' })
  return fields
})

const BULK_STATUS_OPTIONS  = [
  { value: 'new',         label: 'New' },
  { value: 'backlog',     label: 'Backlog' },
  { value: 'in-progress', label: 'In Progress' },
  { value: 'qa',          label: 'QA' },
  { value: 'done',        label: 'Done' },
  { value: 'delivered',   label: 'Delivered' },
  { value: 'accepted',    label: 'Accepted' },
  { value: 'invoiced',    label: 'Invoiced' },
  { value: 'cancelled',   label: 'Cancelled' },
]
const BULK_PRIORITY_OPTIONS = [
  { value: 'low',    label: 'Low'    },
  { value: 'medium', label: 'Medium' },
  { value: 'high',   label: 'High'   },
]

const bulkValueOptions = computed(() => {
  switch (bulkField.value) {
    case 'status':   return BULK_STATUS_OPTIONS
    case 'priority': return BULK_PRIORITY_OPTIONS
    case 'assignee': return [
      { value: '', label: 'Unassigned' },
      ...assignableUsers.value.map(u => ({ value: String(u.id), label: u.username })),
    ]
    case 'parent': return [
      { value: '', label: '— No parent (orphan) —' },
      ...props.issues
        .filter(i => !props.selectedIds.has(i.id) && (i.type === 'epic' || i.type === 'ticket'))
        .map(i => ({ value: String(i.id), label: `${i.issue_key} ${i.title}` })),
    ]
    case 'sprint': return (props.loadedSprints.length ? props.loadedSprints : sprints.value).map(s => ({ value: String(s.id), label: s.title }))
    case 'cost_unit': return [
      { value: '', label: '— None —' },
      ...costUnits.value.map(cu => ({ value: cu, label: cu })),
    ]
    case 'release': return [
      { value: '', label: '— None —' },
      ...releases.value.map(r => ({ value: r, label: r })),
    ]
    default: return []
  }
})

const bulkFieldLabel  = computed(() => BULK_FIELDS.value.find(f => f.value === bulkField.value)?.label ?? '')
const bulkValueSelected = computed(() => bulkField.value !== 'sprint' && bulkValue.value !== UNSET_VALUE)

const bulkSprintOptions = computed(() => {
  const allSprints = props.loadedSprints.length ? props.loadedSprints : sprints.value
  const q = bulkSprintSearch.value.toLowerCase()
  return allSprints
    .filter(s => !s.archived)
    .filter(s => !q || s.title.toLowerCase().includes(q))
    .sort((a, b) => (a.start_date ?? '').localeCompare(b.start_date ?? ''))
})

function wouldCreateCycle(issueId: number, parentId: number): boolean {
  if (issueId === parentId) return true
  const visited = new Set<number>()
  let current: number | null = parentId
  while (current !== null) {
    if (visited.has(current)) break
    visited.add(current)
    if (current === issueId) return true
    const parent = props.issues.find(i => i.id === current)
    current = parent?.parent_id ?? null
  }
  return false
}

function reset() {
  cancelBulk('user closed/reset modal')
  bulkField.value       = ''
  bulkValue.value       = UNSET_VALUE
  bulkSprintIds.value   = []
  bulkSprintMode.value  = 'add'
  bulkSprintSearch.value = ''
  bulkChangeError.value = ''
  bulkProgress.value    = { done: 0, total: 0 }
}

function cancelBulk(_reason: string) {
  if (bulkAbort) {
    bulkAbort.abort()
    bulkAbort = null
  }
}

function isAbortError(e: unknown): boolean {
  return e instanceof DOMException && e.name === 'AbortError'
}

// If the parent hides the modal mid-bulk (close button, escape key,
// backdrop click), abort the in-flight chunked loop. Otherwise the
// loop keeps firing PATCHes after the user thinks they cancelled.
watch(() => props.open, (open, prev) => {
  if (prev && !open && bulkChanging.value) {
    cancelBulk('modal closed mid-bulk')
  }
})

async function executeBulkChange() {
  if (!bulkField.value) { bulkChangeError.value = 'Select a field.'; return }
  if (bulkField.value !== 'sprint' && bulkValue.value === UNSET_VALUE) {
    bulkChangeError.value = 'Select a new value.'
    return
  }
  bulkChanging.value    = true
  bulkChangeError.value = ''
  const ids = [...props.selectedIds]
  try {
    if (bulkField.value === 'sprint') {
      if (!bulkSprintIds.value.length) { bulkChangeError.value = 'Select at least one sprint.'; bulkChanging.value = false; return }
      // Sprint relations go through /issues/{id}/relations (one POST/DELETE
      // per sprint per issue). The endpoint isn't a batch shape, so we
      // surface progress per-issue and bail on first failure — silent
      // .catch() used to swallow FK violations and perm denials, leaving
      // the user thinking the bulk succeeded when it hadn't (PAI-314).
      bulkProgress.value = { done: 0, total: ids.length }
      for (let i = 0; i < ids.length; i++) {
        const id = ids[i]
        const issue = props.issues.find(it => it.id === id)
        if (!issue) {
          bulkProgress.value = { done: i + 1, total: ids.length }
          continue
        }
        const currentIds = issue.sprint_ids ?? []
        try {
          if (bulkSprintMode.value === 'remove') {
            for (const sid of bulkSprintIds.value) {
              if (currentIds.includes(sid)) {
                await api.delete(`/issues/${id}/relations`, { target_id: sid, type: 'sprint' })
              }
            }
          } else if (bulkSprintMode.value === 'set') {
            for (const sid of currentIds) {
              await api.delete(`/issues/${id}/relations`, { target_id: sid, type: 'sprint' })
            }
            for (const sid of bulkSprintIds.value) {
              await api.post(`/issues/${id}/relations`, { target_id: sid, type: 'sprint' })
            }
          } else {
            for (const sid of bulkSprintIds.value) {
              if (!currentIds.includes(sid)) {
                await api.post(`/issues/${id}/relations`, { target_id: sid, type: 'sprint' })
              }
            }
          }
          bulkProgress.value = { done: i + 1, total: ids.length }
        } catch (e: unknown) {
          bulkChangeError.value =
            `${i}/${ids.length} updated before failure (issue ${issue.issue_key}): ${errMsg(e, 'Sprint update failed.')}`
          bulkChanging.value = false
          return
        }
      }
      const updated = await Promise.all(ids.map(id => api.get<Issue>(`/issues/${id}`).catch(() => null)))
      for (const u of updated) { if (u) emit('updated', u) }
      emit('done')
    } else {
      // Build the per-issue field payload (same shape for parent/assignee/
      // status/priority/cost_unit/release).
      const fields: Record<string, unknown> = {}
      if (bulkField.value === 'assignee') {
        fields.assignee_id = bulkValue.value === '' ? null : Number(bulkValue.value)
      } else if (bulkField.value === 'parent') {
        fields.parent_id = bulkValue.value === '' ? null : Number(bulkValue.value)
      } else {
        fields[bulkField.value] = bulkValue.value
      }

      // For parent updates, pre-filter ids that would create a cycle so
      // the batch only carries safe rows. The batch endpoint is atomic —
      // mixing safe + unsafe ids would roll back everyone.
      let targetIds = ids
      const cycleIssues: string[] = []
      if (bulkField.value === 'parent') {
        const newParentId = fields.parent_id as number | null
        const safeIds: number[] = []
        for (const id of ids) {
          if (newParentId !== null && wouldCreateCycle(id, newParentId)) {
            const issue = props.issues.find(i => i.id === id)
            cycleIssues.push(issue?.issue_key ?? String(id))
          } else {
            safeIds.push(id)
          }
        }
        targetIds = safeIds
      }

      if (targetIds.length > 0) {
        // Chunked loop. Each chunk is one atomic PATCH /issues that
        // shares a batch_id on the server, so within a chunk the bulk op
        // is undoable as one action. Across chunks, already-committed
        // chunks stay committed if a later one fails — we surface the
        // partial-progress count in the error.
        bulkProgress.value = { done: 0, total: targetIds.length }
        const chunks: number[][] = []
        for (let i = 0; i < targetIds.length; i += BULK_CHUNK_SIZE) {
          chunks.push(targetIds.slice(i, i + BULK_CHUNK_SIZE))
        }
        bulkAbort = new AbortController()
        const signal = bulkAbort.signal
        const total = targetIds.length
        for (let i = 0; i < chunks.length; i++) {
          // done = count of items committed by previous (resolved) chunks.
          // Computing from i (not bulkProgress.value) makes the message
          // robust to reset() running between fetch reject and catch.
          const done = i * BULK_CHUNK_SIZE
          if (signal.aborted) {
            bulkChangeError.value = `Cancelled — ${done}/${total} issues updated.`
            bulkChanging.value = false
            return
          }
          const chunk = chunks[i]
          const items = chunk.map(id => ({ ref: String(id), fields }))
          try {
            const resp = await api.patch<{ issues: Issue[] }>('/issues', items, { signal })
            for (const u of resp?.issues ?? []) { if (u) emit('updated', u) }
            bulkProgress.value = {
              done:  done + chunk.length,
              total,
            }
          } catch (e: unknown) {
            if (isAbortError(e) || signal.aborted) {
              bulkChangeError.value = `Cancelled — ${done}/${total} issues updated.`
            } else {
              bulkChangeError.value =
                `${done}/${total} updated before failure (chunk ${i + 1}/${chunks.length}): ${errMsg(e, 'Bulk change failed.')}`
            }
            bulkChanging.value = false
            return
          }
        }
        bulkAbort = null
      }

      if (cycleIssues.length > 0) {
        bulkChangeError.value = `Skipped ${cycleIssues.length} issue(s) to prevent hierarchy loops: ${cycleIssues.join(', ')}`
        bulkChanging.value = false
        return
      }
      emit('done')
    }
  } catch (e: unknown) {
    bulkChangeError.value = errMsg(e, 'Bulk change failed.')
  } finally {
    bulkChanging.value = false
  }
}

defineExpose({ reset })
</script>

<template>
  <AppModal title="Bulk Change" :open="open" @close="emit('close')">
    <div class="form">
      <p class="bulk-change-desc">
        Change <strong>{{ formatInteger(selectedIds.size) }} issue{{ selectedIds.size !== 1 ? 's' : '' }}</strong>:
      </p>
      <div class="field">
        <label>Field</label>
        <select v-model="bulkField" class="v2-select" @change="bulkValue = UNSET_VALUE; bulkSprintIds = []; bulkSprintMode = 'add'; bulkSprintSearch = ''">
          <option value="">— Select field —</option>
          <option v-for="f in BULK_FIELDS" :key="f.value" :value="f.value">{{ f.label }}</option>
        </select>
      </div>
      <div class="field" v-if="bulkField && bulkField !== 'sprint'">
        <label>New value for {{ bulkFieldLabel }}</label>
        <select v-if="bulkValueOptions.length" v-model="bulkValue" class="v2-select">
          <option :value="UNSET_VALUE" disabled>— Select value —</option>
          <option v-for="o in bulkValueOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </div>
      <div v-if="bulkField === 'sprint'" class="bulk-sprint-section">
        <label class="field-label">Mode</label>
        <div class="bulk-sprint-modes">
          <label v-for="m in [{ v: 'add', l: 'Add' }, { v: 'set', l: 'Set (replace)' }, { v: 'remove', l: 'Remove' }]" :key="m.v" :class="['bulk-sprint-mode', { active: bulkSprintMode === m.v }]">
            <input type="radio" v-model="bulkSprintMode" :value="m.v" /> {{ m.l }}
          </label>
        </div>
        <label class="field-label" style="margin-top:.75rem">Sprints</label>
        <input v-model="bulkSprintSearch" class="v2-input" placeholder="Search sprints..." style="margin-bottom:.5rem" />
        <div class="bulk-sprint-list">
          <label v-for="s in bulkSprintOptions" :key="s.id" class="bulk-sprint-opt">
            <input type="checkbox" :checked="bulkSprintIds.includes(s.id)" @change="bulkSprintIds.includes(s.id) ? (bulkSprintIds = bulkSprintIds.filter(x => x !== s.id)) : (bulkSprintIds = [...bulkSprintIds, s.id])" />
            <span class="bulk-sprint-title">{{ s.title }}</span>
            <span v-if="s.start_date" class="bulk-sprint-date">{{ s.start_date }}</span>
          </label>
        </div>
        <div v-if="bulkSprintIds.length" class="bulk-sprint-summary">{{ formatInteger(bulkSprintIds.length) }} sprint{{ bulkSprintIds.length !== 1 ? 's' : '' }} selected</div>
      </div>
      <div v-if="bulkChanging && bulkProgress.total > 0" class="bulk-progress">
        <div class="bulk-progress-label">
          Applying {{ formatInteger(bulkProgress.done) }} of {{ formatInteger(bulkProgress.total) }} issue{{ bulkProgress.total !== 1 ? 's' : '' }}…
        </div>
        <div class="bulk-progress-track">
          <div class="bulk-progress-fill" :style="{ width: bulkProgressPct + '%' }"></div>
        </div>
      </div>
      <div v-if="bulkChangeError" class="form-error">{{ bulkChangeError }}</div>
      <div class="form-actions">
        <button class="btn btn-ghost" @click="emit('close')" :disabled="bulkChanging">Cancel</button>
        <button class="btn btn-primary" @click="executeBulkChange" :disabled="bulkChanging || !bulkField || (bulkField === 'sprint' ? !bulkSprintIds.length : !bulkValueSelected)">
          <template v-if="bulkChanging && bulkProgress.total > 0">
            Applying {{ formatInteger(bulkProgress.done) }}/{{ formatInteger(bulkProgress.total) }}…
          </template>
          <template v-else-if="bulkChanging">Applying…</template>
          <template v-else>Apply to {{ formatInteger(selectedIds.size) }} issue{{ selectedIds.size !== 1 ? 's' : '' }}</template>
        </button>
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.form { display: flex; flex-direction: column; gap: .85rem; }
.field { display: flex; flex-direction: column; gap: .3rem; }
.field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
.form-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }
.bulk-change-desc { font-size: 13px; color: var(--text); margin-bottom: .25rem; }

.v2-select {
  border: 1px solid var(--border); border-radius: var(--radius);
  padding: .35rem .55rem; font-size: 13px; font-family: inherit;
  background: var(--bg); color: var(--text);
  outline: none; width: 100%;
}
.v2-select:focus { border-color: var(--brand-blue); }
.v2-input {
  border: 1px solid var(--border); border-radius: var(--radius);
  padding: .35rem .55rem; font-size: 13px; font-family: inherit;
  background: var(--bg); color: var(--text); outline: none; width: 100%;
}

.bulk-sprint-section { margin-top: .5rem; }
.bulk-sprint-modes { display: flex; gap: .5rem; margin-bottom: .25rem; }
.bulk-sprint-mode {
  display: inline-flex; align-items: center; gap: .3rem;
  padding: .25rem .6rem; font-size: 12px; font-weight: 500;
  border: 1px solid var(--border); border-radius: var(--radius);
  cursor: pointer; transition: all .1s;
}
.bulk-sprint-mode input[type="radio"] { display: none; }
.bulk-sprint-mode.active { background: var(--brand-blue); color: #fff; border-color: var(--brand-blue-dark); }
.bulk-sprint-list {
  max-height: 200px; overflow-y: auto;
  border: 1px solid var(--border); border-radius: var(--radius);
  padding: .25rem;
}
.bulk-sprint-opt {
  display: flex; align-items: center; gap: .5rem;
  padding: .3rem .5rem; border-radius: var(--radius);
  font-size: 12px; cursor: pointer;
}
.bulk-sprint-opt:hover { background: var(--bg); }
.bulk-sprint-opt input[type="checkbox"] { accent-color: var(--brand-blue); }
.bulk-sprint-title { font-weight: 500; color: var(--text); }
.bulk-sprint-date { font-size: 10px; color: var(--text-muted); margin-left: auto; }
.bulk-sprint-summary { font-size: 11px; color: var(--text-muted); margin-top: .35rem; }
.field-label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }

.bulk-progress { display: flex; flex-direction: column; gap: .35rem; margin-top: .25rem; }
.bulk-progress-label { font-size: 12px; color: var(--text-muted); }
.bulk-progress-track {
  width: 100%; height: 6px;
  background: var(--border);
  border-radius: 3px;
  overflow: hidden;
}
.bulk-progress-fill {
  height: 100%;
  background: var(--brand-blue);
  transition: width .2s ease-out;
}
</style>
