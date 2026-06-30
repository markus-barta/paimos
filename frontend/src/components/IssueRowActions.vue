<template>
  <div class="row-actions" :class="{ 'row-actions--collapsed': collapsed }">
    <button
      v-if="aiWorkStatus && implementState === 'idle'"
      type="button"
      class="ai-work-badge"
      :class="`ai-work-badge--${aiWorkStatus.status}`"
      :title="aiWorkTitle"
      :aria-label="`${aiWorkLabel}; open issue run history`"
      @click.stop="$emit('view')"
    >{{ aiWorkLabel }}</button>

    <!-- PAI-610/612: transient "Implement this" feedback. On success it's a
         follow-through to the issue's run panel (PAI-618). -->
    <button
      v-if="implementState === 'done'"
      type="button"
      class="implement-status implement-status--done implement-status--link"
      title="Open the issue to watch the run"
      @click.stop="$emit('view')"
    >{{ implementMsg }} →</button>
    <span
      v-else-if="implementState !== 'idle'"
      class="implement-status"
      :class="`implement-status--${implementState}`"
    >{{ implementMsg }}</span>

    <!-- Timer — always visible, never collapsed -->
    <!-- State 1: Running timer — green pulsing badge -->
    <span v-if="timerStore.isRunning(issueId)" class="timer-badge timer-badge--running" @click.stop="stopTimer" title="Stop timer">
      <span class="timer-badge-dot"></span>
      <span class="timer-badge-time">{{ smartElapsed }}</span>
    </span>
    <!-- State 2: Booked time but no running timer — greenish badge + play icon -->
    <span v-else-if="(bookedHours ?? 0) > 0" class="timer-badge timer-badge--booked" @click.stop="timerStore.start(issueId)" title="Start timer">
      <AppIcon name="play" :size="8" class="badge-play-icon" />
      <span class="timer-badge-time">{{ formatSmart(bookedHours ?? 0) }}</span>
    </span>
    <!-- State 3: No time — ghost play on hover -->
    <button v-else class="row-act row-act--hover row-act--play" @click.stop="timerStore.start(issueId)" title="Start timer">
      <AppIcon name="play" :size="13" />
    </button>

    <!-- Normal mode: show all buttons -->
    <template v-if="!collapsed">
      <button class="row-act row-act--hover" title="Copy issue key to clipboard" @click.stop="$emit('copy')">
        <AppIcon name="clipboard" :size="13" />
      </button>
      <button class="row-act row-act--hover" title="View" @click.stop="$emit('view')">
        <AppIcon name="eye" :size="14" />
      </button>
      <button class="row-act row-act--hover" title="Quick edit" @click.stop="$emit('edit')">
        <AppIcon name="pencil" :size="13" />
      </button>
      <button v-if="canHaveChildren && !compact" class="row-act row-act--hover" title="Add child issue" @click.stop="$emit('add-child')">
        <AppIcon name="git-branch-plus" :size="14" style="transform: rotate(90deg)" />
      </button>
      <button v-if="isImplementable" class="row-act row-act--hover row-act--implement" title="Implement this — hand to your local agent" :disabled="implementState === 'busy'" @click.stop="implement()">
        <AppIcon name="zap" :size="13" />
      </button>
      <button v-if="isAdmin" class="row-act row-act--hover row-act--danger" title="Move to trash (recoverable)" @click.stop="$emit('delete')">
        <AppIcon name="trash-2" :size="13" />
      </button>
    </template>

    <!-- Collapsed mode: ellipsis menu -->
    <template v-else>
      <div class="ellipsis-wrap" ref="ellipsisRef">
        <button class="row-act row-act--hover row-act--ellipsis" @click.stop="toggleMenu" title="Actions">
          <AppIcon name="ellipsis-vertical" :size="14" />
        </button>
        <Teleport to="body">
          <div v-if="menuOpen" class="ellipsis-menu" :style="menuPos" @click.stop>
            <button class="ellipsis-item" @click.stop="$emit('copy'); menuOpen = false">
              <AppIcon name="clipboard" :size="13" /> Copy key
            </button>
            <button class="ellipsis-item" @click.stop="$emit('view'); menuOpen = false">
              <AppIcon name="eye" :size="14" /> View
            </button>
            <button class="ellipsis-item" @click.stop="$emit('edit'); menuOpen = false">
              <AppIcon name="pencil" :size="13" /> Edit
            </button>
            <button v-if="canHaveChildren && !compact" class="ellipsis-item" @click.stop="$emit('add-child'); menuOpen = false">
              <AppIcon name="git-branch-plus" :size="14" style="transform: rotate(90deg)" /> Add child
            </button>
            <button v-if="isImplementable" class="ellipsis-item" @click.stop="implement()">
              <AppIcon name="zap" :size="13" /> Implement this
            </button>
            <button v-if="isAdmin" class="ellipsis-item ellipsis-item--danger" @click.stop="$emit('delete'); menuOpen = false">
              <AppIcon name="trash-2" :size="13" /> Move to trash
            </button>
          </div>
        </Teleport>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { api, errMsg } from '@/api/client'
import { useTimerStore } from '@/stores/timer'
import type { IssueAIWorkStatus } from '@/types'

const props = defineProps<{
  canHaveChildren: boolean
  compact?: boolean
  collapsed?: boolean
  issueId: number
  issueType?: string
  issueKey?: string
  issueTitle?: string
  bookedHours?: number
  isAdmin?: boolean
  aiWorkStatus?: IssueAIWorkStatus | null
}>()

// PAI-610/612: "Implement this" — hand the ticket to a local agent run.
const isImplementable = computed(
  () =>
    props.issueType === 'ticket' ||
    props.issueType === 'task' ||
    props.issueType === 'epic',
)
const implementState = ref<'idle' | 'busy' | 'done' | 'error'>('idle')
const implementMsg = ref('')
let implementTimer: ReturnType<typeof setTimeout> | null = null
let alive = true // guards post-await writes/timer after unmount (M3)

const AI_WORK_LABEL: Record<string, string> = {
  queued: 'AI queued',
  running: 'AI running',
  tests_passed: 'AI tests ok',
  tests_failed: 'AI tests failed',
  deployed: 'AI deployed',
  failed: 'AI failed',
  cancelled: 'AI cancelled',
}
const aiWorkStatus = computed(() => props.aiWorkStatus ?? null)
const aiWorkLabel = computed(() => {
  const status = aiWorkStatus.value?.status ?? ''
  return AI_WORK_LABEL[status] ?? `AI ${status}`
})
const aiWorkTitle = computed(() => {
  const run = aiWorkStatus.value
  if (!run) return ''
  const bits = [`${aiWorkLabel.value} - open run history`]
  if (run.version) bits.push(`v${run.version}`)
  if (run.deploy_target) bits.push(`→ ${run.deploy_target}`)
  if (run.device_id) bits.push(run.device_id)
  if (run.tests_summary) bits.push(run.tests_summary)
  if (run.error) bits.push(run.error)
  return bits.join(' · ')
})

async function implement() {
  if (implementState.value === 'busy') return
  if (implementTimer) clearTimeout(implementTimer) // M3: don't let a prior reset fire mid-action
  implementState.value = 'busy'
  implementMsg.value = ''
  menuOpen.value = false
  try {
    await api.post(`/issues/${props.issueId}/implement`, {})
    if (!alive) return
    implementState.value = 'done'
    implementMsg.value = 'Queued — view'
  } catch (e: unknown) {
    if (!alive) return
    implementState.value = 'error'
    implementMsg.value = errMsg(e, 'Failed')
  }
  if (!alive) return
  implementTimer = setTimeout(() => {
    implementState.value = 'idle'
    implementMsg.value = ''
    implementTimer = null
  }, 4000)
}

defineEmits<{
  (e: 'add-child'): void
  (e: 'edit'): void
  (e: 'view'): void
  (e: 'copy'): void
  (e: 'delete'): void
}>()

const timerStore = useTimerStore()
const runningEntry = computed(() => timerStore.getRunningEntry(props.issueId))

/** Smart format: no seconds, tabular-friendly */
function formatSmart(hours: number): string {
  const totalMin = Math.round(hours * 60)
  if (totalMin < 1) return '<1m'
  if (totalMin < 60) return `${totalMin}m`
  const h = Math.floor(totalMin / 60)
  const m = totalMin % 60
  if (h >= 24) {
    const d = Math.floor(h / 24)
    const rh = h % 24
    return rh > 0 ? `${d}d ${rh}h` : `${d}d`
  }
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

const smartElapsed = computed(() => {
  const secs = timerStore.elapsedMap.get(runningEntry.value?.id ?? 0) ?? 0
  return formatSmart(secs / 3600)
})

function stopTimer() {
  if (runningEntry.value) timerStore.stop(runningEntry.value.id)
}

// ── Ellipsis menu ────────────────────────────────────────────────────────────
const menuOpen = ref(false)
const ellipsisRef = ref<HTMLElement | null>(null)
const menuPos = ref({ position: 'fixed' as const, top: '0px', right: '0px' })

function toggleMenu() {
  menuOpen.value = !menuOpen.value
  if (menuOpen.value && ellipsisRef.value) {
    const rect = ellipsisRef.value.getBoundingClientRect()
    menuPos.value = {
      position: 'fixed',
      top: `${rect.bottom + 4}px`,
      right: `${window.innerWidth - rect.right}px`,
    }
  }
}

function onOutsideClick(e: MouseEvent) {
  const target = e.target as Node
  if (ellipsisRef.value?.contains(target)) return
  const menu = document.querySelector('.ellipsis-menu')
  if (menu?.contains(target)) return
  menuOpen.value = false
}

watch(menuOpen, (open) => {
  if (open) document.addEventListener('mousedown', onOutsideClick)
  else      document.removeEventListener('mousedown', onOutsideClick)
})
onUnmounted(() => {
  alive = false
  document.removeEventListener('mousedown', onOutsideClick)
  if (implementTimer) clearTimeout(implementTimer)
})
</script>

<style scoped>
.row-actions {
  display: flex; align-items: center; gap: 2px; justify-content: flex-end;
}
.row-act {
  background: none; border: none; cursor: pointer; padding: 3px;
  color: var(--text-muted); border-radius: 4px;
  display: inline-flex; align-items: center;
  transition: color .15s, background .15s, opacity .15s;
}
.row-act:hover { color: var(--text); background: var(--bg); }
.row-act--hover { opacity: 0; }
tr:hover .row-act--hover,
.tree-row:hover .row-act--hover { opacity: 1; }

/* Timer badge — shared */
.timer-badge {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: 10px; font-weight: 700; padding: 2px 6px;
  border-radius: 10px; white-space: nowrap;
  min-width: 0; justify-content: center;
  font-variant-numeric: tabular-nums;
}
.timer-badge-time { line-height: 1; }

/* State 1: Running — green pulsing */
.timer-badge--running {
  background: var(--bp-green, #16a34a); color: #fff;
  cursor: pointer;
  animation: badge-pulse 2s ease-in-out infinite;
}
.timer-badge--running:hover { background: #15803d; }
.timer-badge-dot {
  width: 5px; height: 5px; border-radius: 50%; background: #fff;
}
@keyframes badge-pulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(22, 163, 74, .3); }
  50% { box-shadow: 0 0 0 3px rgba(22, 163, 74, 0); }
}

/* State 2: Booked — subtle green tint */
.timer-badge--booked {
  background: color-mix(in srgb, #10b981 6%, var(--bg));
  color: color-mix(in srgb, #10b981 8%, #637383);
  border: 1px solid color-mix(in srgb, #10b981 12%, var(--border));
  cursor: pointer;
  transition: background .15s, color .15s, border-color .15s;
}
.timer-badge--booked:hover {
  background: color-mix(in srgb, #10b981 14%, var(--bg));
  color: #059669;
  border-color: color-mix(in srgb, #10b981 30%, var(--border));
}
.badge-play-icon { flex-shrink: 0; display: inline-flex; }
.timer-badge--booked:hover .badge-play-icon { color: #059669; }
.row-act--play:hover { color: var(--bp-green, #16a34a); }
.row-act--danger:hover { color: #dc2626; background: #fef2f2; }
.row-act--implement:hover { color: var(--bp-blue, #2563eb); }
.row-act--implement:disabled { opacity: .5; cursor: not-allowed; }

.ai-work-badge {
  border: 0; cursor: pointer; font-family: inherit;
  font-size: 10px; font-weight: 700; padding: 2px 6px; border-radius: 10px;
  white-space: nowrap; line-height: 1;
  color: var(--text-muted);
  background: color-mix(in srgb, var(--text-muted) 12%, transparent);
}
.ai-work-badge:hover { text-decoration: underline; }
.ai-work-badge--queued,
.ai-work-badge--running {
  color: var(--bp-blue, #2563eb);
  background: color-mix(in srgb, var(--bp-blue, #2563eb) 16%, transparent);
}
.ai-work-badge--tests_passed,
.ai-work-badge--deployed {
  color: #1e8449;
  background: color-mix(in srgb, #2ecc71 22%, transparent);
}
.ai-work-badge--tests_failed,
.ai-work-badge--failed {
  color: #c0392b;
  background: #fef2f2;
}

/* "Implement this" transient feedback */
.implement-status {
  font-size: 10px; font-weight: 700; padding: 2px 6px; border-radius: 10px;
  white-space: nowrap; line-height: 1;
}
.implement-status--busy { color: var(--text-muted); }
.implement-status--done {
  background: color-mix(in srgb, #2ecc71 22%, transparent); color: #1e8449;
}
.implement-status--error { background: #fef2f2; color: #c0392b; }
.implement-status--link {
  border: 0; cursor: pointer; font-family: inherit;
}
.implement-status--link:hover { text-decoration: underline; }

/* Ellipsis menu */
.ellipsis-wrap { position: relative; }
.row-act--ellipsis { opacity: 0; }
tr:hover .row-act--ellipsis,
.tree-row:hover .row-act--ellipsis { opacity: 1; }
</style>

<style>
/* Ellipsis dropdown — not scoped, rendered via Teleport to body */
.ellipsis-menu {
  z-index: 9000;
  background: var(--bg-card, #fff);
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 8px;
  box-shadow: 0 4px 16px rgba(0,0,0,.12);
  padding: .25rem 0;
  min-width: 130px;
}
.ellipsis-item {
  display: flex; align-items: center; gap: .5rem;
  width: 100%; padding: .45rem .75rem; font-size: 13px;
  background: none; border: none; cursor: pointer; font-family: inherit;
  color: var(--text, #1f2937); text-align: left;
  transition: background .1s;
}
.ellipsis-item:hover { background: #f0f2f4; }
.ellipsis-item--danger { color: #dc2626; }
.ellipsis-item--danger:hover { background: #fef2f2; }
</style>
