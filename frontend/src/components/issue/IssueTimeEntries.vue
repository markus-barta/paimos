<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useTimerStore } from '@/stores/timer'
import { useConfirm } from '@/composables/useConfirm'
import { formatDuration, parseDuration } from '@/composables/useDurationInput'
import { fmtShortDateTime } from '@/utils/formatTime'
import AppIcon from '@/components/AppIcon.vue'
import type { TimeEntry } from '@/types'

const props = defineProps<{
  issueId: number
}>()

const authStore  = useAuthStore()
const timerStore = useTimerStore()
const { confirm } = useConfirm()

const timeEntries    = ref<TimeEntry[]>([])
const teLoading      = ref(false)
const showTeForm     = ref(false)
const teSaving       = ref(false)
const teError        = ref('')
const teForm         = ref({ duration: '', comment: '' })
const teDurationRef  = ref<HTMLInputElement | null>(null)

const isTimerIssue = computed(() => timerStore.isRunning(props.issueId))

// Collapsible time bar state
const tePref = localStorage.getItem('paimos:te-expanded') === '1'
const teExpanded = ref(false)
watch(teExpanded, (v) => localStorage.setItem('paimos:te-expanded', v ? '1' : '0'))

// Per-user time totals for the collapsed bar
const perUserTotals = computed(() => {
  const map = new Map<string, number>()
  for (const e of timeEntries.value) {
    const u = e.username || '?'
    map.set(u, (map.get(u) ?? 0) + (e.hours ?? 0))
  }
  return Array.from(map, ([username, hours]) => ({ username, hours }))
})

const totalHours = computed(() =>
  timeEntries.value.reduce((sum, e) => sum + (e.hours ?? 0), 0)
)

async function load() {
  if (!props.issueId) return
  teLoading.value = true
  try {
    timeEntries.value = await api.get<TimeEntry[]>(`/issues/${props.issueId}/time-entries`)
  } catch { timeEntries.value = [] }
  finally {
    teLoading.value = false
    if (!teExpanded.value) {
      teExpanded.value = timeEntries.value.length > 0 ? tePref : false
    }
  }
}

defineExpose({ load, totalHours })

watch(() => props.issueId, () => load())

function openTeForm() {
  teForm.value = { duration: '', comment: '' }
  teError.value = ''
  showTeForm.value = true
  nextTick(() => teDurationRef.value?.focus())
}

async function submitTimeEntry() {
  teError.value = ''
  teSaving.value = true
  try {
    const hours = parseDuration(teForm.value.duration)
    if (hours == null || hours <= 0) {
      teError.value = 'Enter a valid duration (e.g. 1h, 30m, 1h30m)'
      teSaving.value = false
      return
    }
    const now = new Date().toISOString()
    const body: Record<string, any> = {
      comment: teForm.value.comment,
      override: hours,
      started_at: now,
      stopped_at: now,
    }
    await api.post(`/issues/${props.issueId}/time-entries`, body)
    await load()
    showTeForm.value = false
  } catch (e: unknown) { teError.value = errMsg(e, 'Failed to save.') }
  finally { teSaving.value = false }
}

async function toggleTimer() {
  if (isTimerIssue.value) {
    const entry = timerStore.getRunningEntry(props.issueId)
    if (entry) await timerStore.stop(entry.id)
  } else {
    await timerStore.start(props.issueId)
  }
  await load()
}

async function stopAndRefresh(entry: TimeEntry) {
  if (entry.user_id !== authStore.user?.id) {
    if (!await confirm({ message: `You are stopping ${entry.username}'s timer. Continue?`, confirmLabel: 'Stop' })) return
  }
  await timerStore.stop(entry.id)
  await load()
}

// Inline duration editing
const editingTeId = ref<number | null>(null)
const editingTeValue = ref('')

function canEditEntry(entry: TimeEntry): boolean {
  return authStore.user?.role === 'admin' || entry.user_id === authStore.user?.id
}

function startEditDuration(entry: TimeEntry) {
  if (!canEditEntry(entry)) return
  editingTeId.value = entry.id
  editingTeValue.value = formatDuration(entry.hours)
}

async function saveDuration(entry: TimeEntry) {
  const parsed = parseDuration(editingTeValue.value, entry.hours ?? 0)
  editingTeId.value = null
  if (parsed == null || parsed === entry.hours) return
  if (entry.user_id !== authStore.user?.id) {
    if (!await confirm({ message: `You are editing ${entry.username}'s time entry. Continue?`, confirmLabel: 'Edit' })) return
  }
  await api.put(`/time-entries/${entry.id}`, { override: parsed })
  await load()
}

// Inline comment editing
const editingTeCommentId = ref<number | null>(null)
const editingTeComment = ref('')

function startEditComment(entry: TimeEntry) {
  if (!canEditEntry(entry)) return
  editingTeCommentId.value = entry.id
  editingTeComment.value = entry.comment ?? ''
}

async function saveComment(entry: TimeEntry) {
  editingTeCommentId.value = null
  if (editingTeComment.value === (entry.comment ?? '')) return
  if (entry.user_id !== authStore.user?.id) {
    if (!await confirm({ message: `You are editing ${entry.username}'s time entry. Continue?`, confirmLabel: 'Edit' })) return
  }
  await api.put(`/time-entries/${entry.id}`, { comment: editingTeComment.value })
  await load()
}

async function clearOverride(entry: TimeEntry) {
  if (!await confirm({ message: 'Clear the manual time override? The original tracked duration will be restored.', confirmLabel: 'Clear', danger: true })) return
  await api.put(`/time-entries/${entry.id}`, { clear_override: true })
  await load()
}

async function deleteTimeEntry(entry: TimeEntry) {
  const isOther = entry.user_id !== authStore.user?.id
  const msg = isOther
    ? `You are deleting ${entry.username}'s time entry. This cannot be undone.`
    : 'Delete this time entry?'
  if (!await confirm({ message: msg, confirmLabel: 'Delete', danger: true })) return
  await api.delete(`/time-entries/${entry.id}`)
  timeEntries.value = timeEntries.value.filter(e => e.id !== entry.id)
}
</script>

<template>
  <div class="te-bar" :class="{ 'te-bar--expanded': teExpanded }">
    <div class="te-bar-collapsed" @click="teExpanded = !teExpanded">
      <div class="te-bar-users">
        <template v-for="u in perUserTotals" :key="u.username">
          <span class="te-bar-user">
            <span class="te-bar-avatar">{{ (u.username || '?').slice(0, 2).toUpperCase() }}</span>
            <span class="te-bar-hours">{{ formatDuration(u.hours) }}</span>
          </span>
        </template>
        <template v-if="!perUserTotals.length && !isTimerIssue">
          <AppIcon name="clock" :size="13" class="te-bar-clock" />
          <span class="te-bar-empty">No time entries</span>
        </template>
      </div>
      <div class="te-bar-right">
        <template v-if="!teExpanded && !perUserTotals.length && !isTimerIssue">
          <button class="te-ghost-btn" @click.stop="toggleTimer" title="Start timer">
            <AppIcon name="play" :size="12" /> Start
          </button>
          <button class="te-ghost-btn" @click.stop="teExpanded = true; openTeForm()" title="Log time manually">
            <AppIcon name="plus" :size="12" /> Log
          </button>
        </template>
        <span v-if="isTimerIssue" class="te-total te-total--running">{{ timerStore.formattedElapsed(timerStore.getRunningEntry(issueId)?.id ?? 0) }}</span>
        <span v-else-if="totalHours > 0" class="te-total">{{ formatDuration(totalHours) }}</span>
        <AppIcon :name="teExpanded ? 'chevron-up' : 'chevron-down'" :size="12" class="te-bar-chevron" />
      </div>
    </div>
    <Transition name="te-expand">
      <div v-if="teExpanded" class="te-bar-content">
        <div class="te-bar-toolbar">
          <button v-if="!isTimerIssue" class="te-ghost-btn" @click.stop="toggleTimer" title="Start timer">
            <AppIcon name="play" :size="12" /> Start
          </button>
          <button class="te-ghost-btn" @click.stop="openTeForm" title="Log time manually">
            <AppIcon name="plus" :size="12" /> Log
          </button>
        </div>

        <div v-if="showTeForm" class="te-form">
          <div class="te-form-row">
            <div class="field" style="flex:1">
              <label>Duration</label>
              <input ref="teDurationRef" v-model="teForm.duration" type="text" placeholder="e.g. 1h30m, 45m, +10m" />
            </div>
            <div class="field" style="flex:2">
              <label>Comment</label>
              <input v-model="teForm.comment" type="text" placeholder="What did you work on?" />
            </div>
          </div>
          <div v-if="teError" class="form-error">{{ teError }}</div>
          <div class="te-form-actions">
            <button class="btn btn-ghost btn-sm" @click="showTeForm=false">Cancel</button>
            <button class="btn btn-sm te-save-btn" @click="submitTimeEntry" :disabled="teSaving">
              {{ teSaving ? 'Saving…' : 'Save entry' }}
            </button>
          </div>
        </div>

        <table v-if="timeEntries.length" class="te-table">
          <thead>
            <tr>
              <th>Who</th><th>Start</th><th>Stop</th><th>Hours</th><th>Comment</th><th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="e in timeEntries" :key="e.id" :class="{ 'te-overridden': e.override != null, 'te-row-running': !e.stopped_at && timerStore.isRunning(e.issue_id) }">
              <td class="te-user">{{ e.username || '—' }}</td>
              <td class="te-date" :class="{ 'te-struck': e.override != null }">{{ fmtShortDateTime(e.started_at) }}</td>
              <td class="te-date" :class="{ 'te-struck': e.override != null }">{{ e.stopped_at ? fmtShortDateTime(e.stopped_at) : '—' }}</td>
              <td class="te-hours" :style="canEditEntry(e) ? { cursor: 'pointer' } : {}" @click="startEditDuration(e)">
                <template v-if="editingTeId === e.id">
                  <input class="te-duration-input" v-model="editingTeValue" @blur="saveDuration(e)" @keydown.enter="saveDuration(e)" @keydown.escape="editingTeId = null" autofocus />
                </template>
                <template v-else>
                  <span v-if="!e.stopped_at && timerStore.isRunning(e.issue_id)" class="te-live">{{ timerStore.formattedElapsed(e.id) }}</span>
                  <template v-else>{{ formatDuration(e.hours) }}</template>
                  <button v-if="e.override != null && canEditEntry(e)" class="te-clear-override" @click.stop="clearOverride(e)" title="Clear override"><AppIcon name="x" :size="9" /></button>
                </template>
              </td>
              <td class="te-comment" :style="canEditEntry(e) ? { cursor: 'pointer' } : {}" @click="startEditComment(e)">
                <input v-if="editingTeCommentId === e.id" v-model="editingTeComment" class="te-comment-input" @blur="saveComment(e)" @keydown.enter="saveComment(e)" @keydown.escape="editingTeCommentId = null" />
                <span v-else class="te-comment-text">{{ e.comment || '—' }}</span>
              </td>
              <td class="te-del">
                <button v-if="!e.stopped_at && timerStore.isRunning(e.issue_id) && canEditEntry(e)" class="te-row-stop" @click.stop="stopAndRefresh(e)" title="Stop">
                  <AppIcon name="square" :size="10" /> Stop
                </button>
                <button v-else-if="canEditEntry(e)" class="btn-icon-del te-row-act" @click="deleteTimeEntry(e)" title="Delete"><AppIcon name="x" :size="11" /></button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else-if="!teLoading && !showTeForm" class="rel-empty">No time entries yet.</div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.te-bar {
  border: 1px solid color-mix(in srgb, var(--bp-green, #16a34a) 15%, var(--border));
  border-left: 3px solid var(--bp-green, #16a34a);
  border-radius: 8px; margin: 1.25rem 1.5rem 1rem; overflow: hidden;
  background: color-mix(in srgb, var(--bp-green, #16a34a) 4%, var(--bg-card));
}
.te-bar-collapsed {
  display: flex; align-items: center; justify-content: space-between;
  padding: .5rem 1rem; cursor: pointer; min-height: 32px;
  transition: background .12s;
}
.te-bar-collapsed:hover { background: color-mix(in srgb, var(--bp-green, #16a34a) 6%, var(--bg-card)); }
.te-bar-users { display: flex; align-items: center; gap: .6rem; flex-wrap: wrap; }
.te-bar-user { display: inline-flex; align-items: center; gap: .25rem; font-size: 11px; }
.te-bar-avatar {
  width: 18px; height: 18px; border-radius: 50%;
  background: var(--bp-blue-pale); color: var(--bp-blue);
  font-size: 9px; font-weight: 700; display: flex; align-items: center; justify-content: center;
}
.te-bar-hours { font-weight: 600; color: var(--text); }
.te-bar-empty { font-size: 11px; color: var(--text-muted); font-style: italic; }
.te-bar-clock { color: var(--text-muted); flex-shrink: 0; }
.te-bar-right { display: flex; align-items: center; gap: .5rem; }
.te-bar-chevron { color: var(--text-muted); flex-shrink: 0; }
.te-bar-content {
  padding: .75rem 1rem 1rem; border-top: 1px solid color-mix(in srgb, var(--bp-green, #16a34a) 15%, var(--border));
}
.te-expand-enter-active, .te-expand-leave-active { transition: max-height .25s ease, opacity .2s; overflow: hidden; }
.te-expand-enter-from, .te-expand-leave-to { max-height: 0; opacity: 0; }
.te-expand-enter-to, .te-expand-leave-from { max-height: 600px; opacity: 1; }

.te-bar-toolbar {
  display: flex; gap: .35rem; margin-bottom: .6rem;
}
.te-ghost-btn {
  background: none; border: none; cursor: pointer;
  display: inline-flex; align-items: center; gap: .3rem;
  font-size: 11px; font-weight: 500; color: var(--text-muted);
  padding: .25rem .5rem; border-radius: 4px; font-family: inherit;
  line-height: 1; transition: color .1s, background .1s;
}
.te-ghost-btn:hover { color: var(--bp-green, #16a34a); background: color-mix(in srgb, var(--bp-green) 8%, transparent); }

.te-total {
  font-size: 12px; font-weight: 700; background: var(--bp-green, #16a34a);
  color: #fff; border-radius: 10px; padding: .05rem .5rem;
}
.te-total--running {
  background: #22c55e; animation: te-pulse 2s ease-in-out infinite;
}
@keyframes te-pulse { 0%,100% { opacity: 1; } 50% { opacity: .7; } }

.te-form {
  background: var(--surface-2); border: 1px solid var(--border);
  border-radius: var(--radius); padding: .75rem 1rem;
  margin-bottom: .75rem; display: flex; flex-direction: column; gap: .6rem;
}
.te-form-row { display: flex; gap: 1rem; flex-wrap: wrap; }
.te-form-row .field { flex: 1; min-width: 140px; }
.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 11px; font-weight: 700; color: var(--text-muted); text-transform: uppercase; letter-spacing: .06em; }
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
.te-form-actions { display: flex; gap: .5rem; justify-content: flex-end; margin-top: .5rem; padding: .25rem 0; }
.te-save-btn { background: var(--bp-green, #16a34a); color: #fff; border-color: #15803d; }
.te-save-btn:hover { background: #15803d; }

.te-table {
  width: 100%; border-collapse: collapse; font-size: 12px; margin-top: .5rem;
}
.te-table th {
  text-align: left; font-size: 11px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .04em;
  padding: .35rem .6rem; border-bottom: 1px solid var(--border);
}
.te-table tbody tr { border-bottom: 1px solid var(--border-subtle, var(--border)); }
.te-table tbody tr:hover { background: var(--surface-2); }
.te-table td { padding: .45rem .6rem; vertical-align: middle; }
.te-user { font-weight: 600; }
.te-date { color: var(--text-muted); white-space: nowrap; }
.te-hours { font-weight: 700; white-space: nowrap; cursor: pointer; position: relative; }
.te-hours:hover { color: var(--bp-blue); }
.te-comment { color: var(--text-muted); max-width: 240px; cursor: pointer; }
.te-comment-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; }
.te-comment-text:hover { color: var(--bp-blue); }
.te-comment-input {
  width: 100%; font-size: 12px; border: 1px solid var(--bp-blue);
  border-radius: 3px; padding: .1rem .3rem; outline: none; font-family: inherit;
  background: var(--bg-card);
}
.te-del { text-align: right; }
.te-overridden td.te-date { opacity: .4; }
.te-struck { text-decoration: line-through; }
.te-duration-input {
  width: 60px; font-size: 12px; font-weight: 700; border: 1px solid var(--bp-blue);
  border-radius: 3px; padding: .1rem .3rem; outline: none; font-family: inherit;
  background: var(--bg-card);
}
.te-clear-override {
  background: none; border: none; cursor: pointer; padding: 1px; margin-left: 2px;
  color: var(--text-muted); border-radius: 2px; display: inline-flex; vertical-align: middle;
}
.te-clear-override:hover { color: var(--danger); background: var(--bg); }
.te-live { color: var(--bp-green, #16a34a); font-weight: 700; }
.te-row-running {
  background: color-mix(in srgb, var(--bp-green, #16a34a) 6%, var(--bg-card));
  box-shadow: inset 3px 0 0 var(--bp-green, #16a34a);
}
.te-row-stop {
  background: none; border: none; cursor: pointer; padding: 2px 6px;
  color: var(--bp-green, #16a34a); border-radius: 4px; display: inline-flex;
  align-items: center; gap: .25rem; font-size: 11px; font-weight: 600; font-family: inherit;
  transition: background .1s;
}
.te-row-stop:hover { background: color-mix(in srgb, var(--bp-green) 15%, transparent); }
.te-row-act { opacity: 0; transition: opacity .12s; }
tr:hover .te-row-act { opacity: 1; }
.btn-icon-del {
  background: none; border: none; cursor: pointer; color: var(--text-muted);
  font-size: 15px; line-height: 1; padding: 0 .2rem; border-radius: 3px;
}
.btn-icon-del:hover { color: #c0392b; }
.rel-empty { font-size: 13px; color: var(--text-muted); padding: .5rem 0; }
</style>
