<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import { useTimerStore } from '@/stores/timer'
import { useConfirm } from '@/composables/useConfirm'
import { formatDuration, parseDuration } from '@/composables/useDurationInput'
import { fmtShortDateTime } from '@/utils/formatTime'
import AppIcon from '@/components/AppIcon.vue'
import type { TimeEntry } from '@/types'
import { LS_TIME_ENTRIES_EXPANDED } from '@/constants/storage'
import { createIssueTimeEntry, deleteTimeEntryById, loadIssueTimeEntries, updateTimeEntry, type CreateTimeEntryPayload } from '@/services/issueTimeEntries'

const props = withDefaults(defineProps<{
  issueId: number
  canEdit?: boolean
}>(), {
  canEdit: true,
})

const authStore  = useAuthStore()
const timerStore = useTimerStore()
const { confirm } = useConfirm()

const timeEntries    = ref<TimeEntry[]>([])
const teLoading      = ref(false)
const showTeForm     = ref(false)
const teSaving       = ref(false)
const teError        = ref('')
const teForm         = ref({ duration: '', material: '', comment: '', userId: 0 as number, date: '' })
const teDurationRef  = ref<HTMLInputElement | null>(null)

// PAI-478: localISODate / localDateFromISO / shiftDateInISO are used by
// both the create form (date field default + submit) and the inline
// date edit on existing rows. We deal with two representations:
//   - "YYYY-MM-DD" in the user's local timezone (what date pickers speak)
//   - "YYYY-MM-DDTHH:MM:SSZ" UTC ISO (what the API stores)
// Edits preserve the time-of-day in UTC and only shift the calendar
// date by the chosen delta. That keeps duration-by-subtraction intact
// for non-override timer entries and keeps the display stable.
function localISODate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${dd}`
}
function localDateFromISO(iso: string): string {
  const d = new Date(iso.endsWith('Z') || /[+-]\d{2}:\d{2}$/.test(iso) ? iso : iso.replace(' ', 'T') + 'Z')
  return localISODate(d)
}
function shiftDateInISO(iso: string, oldLocalDate: string, newLocalDate: string): string {
  const a = new Date(`${oldLocalDate}T00:00:00`).getTime()
  const b = new Date(`${newLocalDate}T00:00:00`).getTime()
  const delta = b - a
  const t = new Date(iso.endsWith('Z') || /[+-]\d{2}:\d{2}$/.test(iso) ? iso : iso.replace(' ', 'T') + 'Z').getTime()
  return new Date(t + delta).toISOString()
}
function isoOnLocalDate(localDate: string, now: Date): string {
  // Compose started_at/stopped_at for a new manual entry: chosen local
  // date + current local time-of-day, serialized as UTC ISO.
  const [y, m, d] = localDate.split('-').map(Number)
  const local = new Date(y, m - 1, d, now.getHours(), now.getMinutes(), now.getSeconds())
  return local.toISOString()
}

// PAI-335: super-admin can log time on behalf of any user. The picker
// only renders when authStore.isSuperAdmin so non-super-admins never
// see (or send) the user_id field. Lazy-loaded — first time the
// super-admin opens the create form.
const assignableUsers = ref<User[]>([])
let assignableUsersLoaded = false
async function ensureAssignableUsers() {
  if (assignableUsersLoaded) return
  if (!authStore.isSuperAdmin) return
  try {
    const all = await api.get<User[]>('/users')
    // Only active accounts make sense as time-entry owners.
    assignableUsers.value = all.filter(u => u.status === 'active')
    assignableUsersLoaded = true
  } catch { /* tolerate; picker just stays empty */ }
}
const pickedUser = computed<User | undefined>(() =>
  assignableUsers.value.find(u => u.id === teForm.value.userId),
)
const isActingAsOther = computed(() =>
  authStore.isSuperAdmin
  && teForm.value.userId !== 0
  && teForm.value.userId !== authStore.user?.id,
)

const isTimerIssue = computed(() => timerStore.isRunning(props.issueId))
const canEditTimeEntries = computed(() => props.canEdit !== false)

// Collapsible time bar state
const tePref = localStorage.getItem(LS_TIME_ENTRIES_EXPANDED) === '1'
const teExpanded = ref(false)
watch(teExpanded, (v) => localStorage.setItem(LS_TIME_ENTRIES_EXPANDED, v ? '1' : '0'))

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
    timeEntries.value = await loadIssueTimeEntries(props.issueId)
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
  if (!canEditTimeEntries.value) return
  teForm.value = {
    duration: '',
    material: '',
    comment: '',
    // PAI-335: default to caller; super-admin can change via picker.
    userId: authStore.user?.id ?? 0,
    // PAI-478: default to today (local). User can pick a past day for
    // retroactive bookings — started_at/stopped_at will be persisted on
    // that day rather than `now`.
    date: localISODate(new Date()),
  }
  teError.value = ''
  showTeForm.value = true
  // Fire-and-forget; the picker just stays empty until the fetch lands.
  void ensureAssignableUsers()
  nextTick(() => teDurationRef.value?.focus())
}

async function submitTimeEntry() {
  teError.value = ''
  teSaving.value = true
  try {
    const hours = parseDuration(teForm.value.duration)
    // PAI-581: material (LP / token cost) is optional and may be logged with or
    // without hours (e.g. an AI-dev cost with no human time).
    const rawMat = teForm.value.material.trim()
    const material = rawMat === '' ? null : Number(rawMat)
    if (material != null && (!Number.isFinite(material) || material < 0)) {
      teError.value = 'Enter a valid material value (>= 0)'
      teSaving.value = false
      return
    }
    const hasHours = hours != null && hours > 0
    const hasMaterial = material != null && material > 0
    if (!hasHours && !hasMaterial) {
      teError.value = 'Enter a valid duration (e.g. 1h, 30m, 1h30m) or a material value'
      teSaving.value = false
      return
    }
    // PAI-478: persist on the chosen date (default = today). The
    // override semantics are unchanged; only the calendar day on which
    // the entry "happened" reflects what the user actually entered.
    const stamp = teForm.value.date
      ? isoOnLocalDate(teForm.value.date, new Date())
      : new Date().toISOString()
    const body: CreateTimeEntryPayload = {
      comment: teForm.value.comment,
      override: hasHours ? (hours as number) : 0,
      started_at: stamp,
      stopped_at: stamp,
    }
    if (hasMaterial) body.material_lp = material as number
    // PAI-335: only attach user_id when the super-admin picked
    // someone OTHER than themselves. Sending the caller's own id is
    // harmless server-side, but omitting it keeps the wire shape
    // identical to the pre-PAI-335 client for the common case.
    if (isActingAsOther.value) {
      body.user_id = teForm.value.userId
    }
    await createIssueTimeEntry(props.issueId, body)
    await load()
    showTeForm.value = false
  } catch (e: unknown) { teError.value = errMsg(e, 'Failed to save.') }
  finally { teSaving.value = false }
}

async function toggleTimer() {
  if (!canEditTimeEntries.value) return
  if (isTimerIssue.value) {
    const entry = timerStore.getRunningEntry(props.issueId)
    if (entry) await timerStore.stop(entry.id)
  } else {
    await timerStore.start(props.issueId)
  }
  await load()
}

async function stopAndRefresh(entry: TimeEntry) {
  if (!canEditTimeEntries.value) return
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
  return canEditTimeEntries.value && (authStore.isSuperAdmin || entry.user_id === authStore.user?.id)
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
  await updateTimeEntry(entry.id, { override: parsed })
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
  await updateTimeEntry(entry.id, { comment: editingTeComment.value })
  await load()
}

// PAI-478: inline date edit. Click the Start cell to change which
// calendar day a retroactively-booked entry belongs to. We preserve
// each timestamp's time-of-day and shift only the calendar date by
// the chosen delta — keeps duration-by-subtraction intact for non-
// override timer entries (the user almost certainly doesn't want
// "move yesterday's 30min into today" to also resize it).
const editingTeDateId = ref<number | null>(null)
const editingTeDate = ref('')

function startEditDate(entry: TimeEntry) {
  if (!canEditEntry(entry)) return
  // A running timer has no fixed date yet; disallow until it's stopped.
  if (!entry.stopped_at) return
  editingTeDateId.value = entry.id
  editingTeDate.value = localDateFromISO(entry.started_at)
}

async function saveDate(entry: TimeEntry) {
  const newDate = editingTeDate.value
  editingTeDateId.value = null
  if (!newDate) return
  const oldDate = localDateFromISO(entry.started_at)
  if (newDate === oldDate) return
  if (entry.user_id !== authStore.user?.id) {
    if (!await confirm({ message: `You are editing ${entry.username}'s time entry. Continue?`, confirmLabel: 'Edit' })) return
  }
  const payload: Record<string, unknown> = {
    started_at: shiftDateInISO(entry.started_at, oldDate, newDate),
  }
  if (entry.stopped_at) {
    payload.stopped_at = shiftDateInISO(entry.stopped_at, oldDate, newDate)
  }
  await updateTimeEntry(entry.id, payload)
  await load()
}

async function clearOverride(entry: TimeEntry) {
  if (!canEditEntry(entry)) return
  if (!await confirm({ message: 'Clear the manual time override? The original tracked duration will be restored.', confirmLabel: 'Clear', danger: true })) return
  await updateTimeEntry(entry.id, { clear_override: true })
  await load()
}

async function deleteTimeEntry(entry: TimeEntry) {
  if (!canEditEntry(entry)) return
  const isOther = entry.user_id !== authStore.user?.id
  const msg = isOther
    ? `You are deleting ${entry.username}'s time entry. You can undo this from Recent activity.`
    : 'Delete this time entry? You can undo it from Recent activity.'
  if (!await confirm({ message: msg, confirmLabel: 'Delete', danger: true })) return
  await deleteTimeEntryById(entry.id)
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
        <template v-if="canEditTimeEntries && !teExpanded && !perUserTotals.length && !isTimerIssue">
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
          <button v-if="canEditTimeEntries && !isTimerIssue" class="te-ghost-btn" @click.stop="toggleTimer" title="Start timer">
            <AppIcon name="play" :size="12" /> Start
          </button>
          <button v-if="canEditTimeEntries" class="te-ghost-btn" @click.stop="openTeForm" title="Log time manually">
            <AppIcon name="plus" :size="12" /> Log
          </button>
        </div>

        <div v-if="canEditTimeEntries && showTeForm" class="te-form">
          <div class="te-form-row">
            <div class="field" style="flex:0 0 auto">
              <label>Date</label>
              <input v-model="teForm.date" type="date" class="te-date-input" />
            </div>
            <div class="field" style="flex:1">
              <label>Duration</label>
              <input ref="teDurationRef" v-model="teForm.duration" type="text" placeholder="e.g. 1h30m, 45m, +10m" />
            </div>
            <div class="field" style="flex:1">
              <label>Material (LP)</label>
              <input v-model="teForm.material" type="number" min="0" step="any" placeholder="e.g. 2.5" />
            </div>
            <div class="field" style="flex:2">
              <label>Comment</label>
              <input v-model="teForm.comment" type="text" placeholder="What did you work on?" />
            </div>
          </div>
          <!-- PAI-335: super-admin user picker. Hidden for everyone
               else so the picker can never silently submit a foreign
               user_id from a stale form state. -->
          <div v-if="authStore.isSuperAdmin" class="te-form-row te-form-row--super">
            <div class="field" style="flex:1">
              <label>Log on behalf of</label>
              <select v-model.number="teForm.userId">
                <option v-if="!assignableUsers.length" :value="authStore.user?.id ?? 0">
                  {{ authStore.user?.username ?? 'me' }}
                </option>
                <option v-for="u in assignableUsers" :key="u.id" :value="u.id">
                  {{ u.username }}{{ u.id === authStore.user?.id ? ' (me)' : '' }}
                </option>
              </select>
            </div>
            <div v-if="isActingAsOther" class="te-acting-badge" :title="`Time entry will be saved as ${pickedUser?.username}, not as you.`">
              <AppIcon name="user" :size="12" />
              Acting as <strong>{{ pickedUser?.username }}</strong>
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
              <td
                class="te-date"
                :class="{ 'te-struck': e.override != null, 'te-date--editable': canEditEntry(e) && e.stopped_at }"
                :title="e.override != null ? 'Manual override active — the start/stop times do not determine the hours; hours are set manually below. Click to change the date.' : (canEditEntry(e) && e.stopped_at ? 'Click to change the date' : undefined)"
                @click="startEditDate(e)"
              >
                <input
                  v-if="editingTeDateId === e.id"
                  v-model="editingTeDate"
                  type="date"
                  class="te-date-input te-date-input--inline"
                  @blur="saveDate(e)"
                  @keydown.enter="saveDate(e)"
                  @keydown.escape="editingTeDateId = null"
                  @click.stop
                  autofocus
                />
                <template v-else>{{ fmtShortDateTime(e.started_at) }}</template>
              </td>
              <td
                class="te-date"
                :class="{ 'te-struck': e.override != null }"
                :title="e.override != null ? 'Manual override active — the start/stop times do not determine the hours; hours are set manually below.' : undefined"
              >{{ e.stopped_at ? fmtShortDateTime(e.stopped_at) : '—' }}</td>
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
/* PAI-335 — super-admin user picker row. Anchors the "Acting as" badge
   to the right so a moment of inattention never lets you misattribute
   hours. */
.te-form-row--super {
  align-items: flex-end;
}
.te-acting-badge {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  padding: .35rem .65rem;
  border-radius: 4px;
  background: #fff7e6;
  color: #92400e;
  border: 1px solid #fde68a;
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
  align-self: flex-end;
}
.te-acting-badge strong {
  font-weight: 700;
}
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
.te-date--editable { cursor: pointer; }
.te-date--editable:hover { color: var(--bp-blue); }
.te-date-input {
  font-size: 12px; border: 1px solid var(--border); border-radius: 3px;
  padding: .1rem .3rem; outline: none; font-family: inherit;
  background: var(--bg-card); color: var(--text);
}
.te-date-input--inline {
  width: 130px; border-color: var(--bp-blue);
}
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
