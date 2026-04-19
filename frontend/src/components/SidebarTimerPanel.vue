<script setup lang="ts">
import { useTimerPanel } from '@/composables/useTimerPanel'
import { useIssuePreview } from '@/composables/useIssuePreview'
import { formatDuration } from '@/composables/useDurationInput'
import AppIcon from '@/components/AppIcon.vue'

defineProps<{
  isExpanded: boolean
}>()

const {
  timer, runningEntries, timerPanelOpen, timerPanelEl,
  openTimerIssue, toggleTimerPanel,
} = useTimerPanel()

const preview = useIssuePreview()
</script>

<template>
  <!-- Collapsed indicator -->
  <div v-if="!isExpanded && timer.hasRunning" class="timer-collapsed-indicator" :title="`${runningEntries.length} timer${runningEntries.length > 1 ? 's' : ''} running`">
    <span class="timer-dot"></span>
  </div>
  <!-- Expanded panel -->
  <div v-if="isExpanded" ref="timerPanelEl" class="timer-panel" :class="{ 'timer-panel--open': timerPanelOpen, 'timer-panel--active': timer.hasRunning }">
    <div class="timer-header" @click="toggleTimerPanel">
      <template v-if="timer.hasRunning">
        <span class="timer-dot"></span>
        <span class="sl timer-label" v-if="runningEntries.length === 1">{{ runningEntries[0].issue_key }} · {{ timer.formattedElapsed(runningEntries[0].id) }}</span>
        <span class="sl timer-label" v-else>{{ runningEntries.length }} timers</span>
      </template>
      <template v-else>
        <AppIcon name="clock" :size="13" class="timer-clock-icon" />
        <span class="sl timer-label timer-label--idle">No active timer</span>
      </template>
      <AppIcon :name="timerPanelOpen ? 'chevron-down' : 'chevron-up'" :size="11" class="sl timer-chevron" />
    </div>
    <div v-if="timerPanelOpen" class="timer-body">
      <div class="tp-section">
        <div class="tp-heading">Running<button v-if="runningEntries.length >= 2" class="timer-stop-all" @click.stop="timer.stopAll()" title="Stop all timers">Stop all</button></div>
        <template v-if="runningEntries.length">
          <div v-for="e in runningEntries" :key="e.id" class="tp-row" @mouseenter="preview.showPreview(e.issue_id, $event, 100)" @mouseleave="preview.hidePreview()">
            <a class="tp-key" @click.prevent="openTimerIssue(e)">{{ e.issue_key }}</a>
            <span class="tp-title">{{ e.issue_title }}</span>
            <span class="tp-elapsed">{{ timer.formattedElapsed(e.id) }}</span>
            <button class="tp-btn tp-btn--stop" @click.stop="timer.stop(e.id)" title="Stop"><AppIcon name="square" :size="10" /></button>
          </div>
        </template>
        <div v-else class="tp-empty">No active timer — pick a recent item to restart</div>
      </div>
      <div v-if="timer.recentEntries.length" class="tp-section">
        <div class="tp-heading">Recent</div>
        <div v-for="e in timer.recentEntries" :key="e.id" :class="['tp-row', { 'tp-row--running': timer.isRunning(e.issue_id) }]" @mouseenter="preview.showPreview(e.issue_id, $event, 100)" @mouseleave="preview.hidePreview()">
          <a class="tp-key" @click.prevent="openTimerIssue(e)">{{ e.issue_key }}</a>
          <span class="tp-title">{{ e.issue_title }}</span>
          <span class="tp-elapsed tp-elapsed--dim">{{ formatDuration(e.hours) }}</span>
          <span v-if="timer.isRunning(e.issue_id)" class="tp-running-dot" title="Running"></span>
          <button v-else class="tp-btn tp-btn--play" @click.stop="timer.start(e.issue_id)" title="Restart"><AppIcon name="play" :size="10" /></button>
        </div>
      </div>
    </div>
  </div>

  <!-- Timer start confirmation dialog -->
  <Teleport to="body">
    <div v-if="timer.showStartDialog" class="timer-dialog-backdrop" @click="timer.confirmCancel()">
      <div class="timer-dialog" @click.stop>
        <p class="timer-dialog-msg" v-if="runningEntries.length === 1">
          <strong>{{ runningEntries[0].issue_key }}</strong> is currently running
          <span class="timer-dialog-elapsed">({{ timer.formattedElapsed(runningEntries[0].id) }})</span>
        </p>
        <p class="timer-dialog-msg" v-else>{{ runningEntries.length }} timers are currently running</p>
        <div class="timer-dialog-actions">
          <button class="btn btn-primary btn-sm" @click="timer.confirmSwitch()"><u>S</u>witch</button>
          <button class="btn btn-ghost btn-sm" style="border:1px solid var(--border)" @click="timer.confirmBoth()"><template v-if="runningEntries.length === 1"><u>B</u>oth</template><template v-else><u>A</u>ll ({{ runningEntries.length + 1 }})</template></button>
          <button class="btn btn-ghost btn-sm" @click="timer.confirmCancel()"><u>C</u>ancel</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* Collapsed sidebar timer indicator */
.timer-collapsed-indicator {
  display: flex; align-items: center; justify-content: center;
  padding: .4rem 0;
}

/* ── Timer panel (inline expanding) ──────────────────────────────────────── */
.timer-panel {
  margin: 0 .5rem .25rem;
  background: rgba(255, 255, 255, .04);
  border: 1px solid rgba(255, 255, 255, .08);
  border-radius: var(--radius);
  transition: background .2s, border-color .2s;
  overflow: hidden;
}
.timer-panel--active {
  background: rgba(34, 197, 94, .08);
  border-color: rgba(34, 197, 94, .2);
}
.timer-panel--open.timer-panel--active {
  background: rgba(34, 197, 94, .12);
  border-color: rgba(34, 197, 94, .35);
}
.timer-panel--open:not(.timer-panel--active) {
  background: rgba(255, 255, 255, .07);
  border-color: rgba(255, 255, 255, .12);
}
.timer-clock-icon { color: rgba(255,255,255,.3); flex-shrink: 0; }
.timer-label--idle { color: rgba(255,255,255,.3); }
.timer-header {
  display: flex; align-items: center; gap: .5rem;
  padding: .4rem .75rem; cursor: pointer;
  transition: background .15s;
}
.timer-header:hover { background: rgba(34, 197, 94, .1); }
.timer-stop-all {
  background: none; border: none; cursor: pointer; padding: .1rem .35rem;
  font-size: 9px; font-weight: 700; color: #fbbf24;
  border-radius: 3px; margin-left: auto; opacity: 0; transition: opacity .15s, color .1s, background .1s;
}
.timer-body:hover .timer-stop-all { opacity: 1; }
.timer-stop-all:hover { color: #fde68a; background: rgba(251, 191, 36, .15); }
.timer-dot {
  width: 8px; height: 8px; border-radius: 50%;
  background: #22c55e; flex-shrink: 0;
  animation: timer-pulse 2s ease-in-out infinite;
}
@keyframes timer-pulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(34, 197, 94, .4); }
  50% { opacity: .7; box-shadow: 0 0 0 4px rgba(34, 197, 94, 0); }
}
.timer-label {
  font-size: 11px; font-weight: 600; color: #fff;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  flex: 1; min-width: 0;
}
.timer-chevron { color: rgba(255,255,255,.35); flex-shrink: 0; }
/* Expanded body */
.timer-body {
  border-top: 1px solid rgba(34, 197, 94, .15);
  padding: .35rem .5rem .5rem;
  animation: tp-slide-in .15s ease-out;
}
@keyframes tp-slide-in {
  from { opacity: 0; max-height: 0; }
  to   { opacity: 1; max-height: 400px; }
}
.tp-section { margin-bottom: .35rem; }
.tp-section:last-child { margin-bottom: 0; }
.tp-heading {
  display: flex; align-items: center;
  font-size: 9px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .05em; color: rgba(255,255,255,.35); padding: .15rem .3rem .1rem;
}
.tp-row {
  display: flex; align-items: center; gap: .3rem;
  padding: .25rem .3rem; border-radius: 4px; font-size: 11px;
}
.tp-row:hover { background: rgba(255,255,255,.05); }
.tp-key {
  font-weight: 700; color: #7cc4f0; text-decoration: none;
  white-space: nowrap; flex-shrink: 0;
}
.tp-key:hover { text-decoration: underline; color: #a8d8f8; }
.tp-title {
  flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis;
  white-space: nowrap; color: rgba(255,255,255,.4);
}
.tp-elapsed { font-weight: 600; color: #4ade80; white-space: nowrap; flex-shrink: 0; font-size: 10px; }
.tp-elapsed--dim { color: rgba(255,255,255,.4); }
.tp-btn {
  background: none; border: none; padding: 3px 4px; cursor: pointer;
  border-radius: 4px; display: flex; align-items: center; flex-shrink: 0;
  transition: background .1s, color .1s; color: rgba(255,255,255,.35);
}
.tp-btn--stop { color: #4ade80; }
.tp-btn--stop:hover { background: rgba(34, 197, 94, .2); }
.tp-btn--play:hover { background: rgba(255,255,255,.08); color: #4ade80; }
.tp-row--running { opacity: .5; }
.tp-running-dot {
  width: 6px; height: 6px; border-radius: 50%; background: #4ade80;
  flex-shrink: 0; animation: tp-dot-pulse 2s ease-in-out infinite;
}
@keyframes tp-dot-pulse { 0%,100% { opacity: 1; } 50% { opacity: .3; } }
.tp-empty { font-size: 10px; color: rgba(255,255,255,.3); padding: .3rem .4rem; font-style: italic; }

/* ── Timer start dialog ──────────────────────────────────────────────────── */
.timer-dialog-backdrop {
  position: fixed; inset: 0; z-index: 99999;
  background: rgba(0,0,0,.35); display: flex; align-items: center; justify-content: center;
}
.timer-dialog {
  background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px;
  padding: 1.25rem 1.5rem; min-width: 320px; max-width: 420px;
  box-shadow: 0 8px 32px rgba(0,0,0,.2);
}
.timer-dialog-msg { font-size: 14px; margin: 0 0 1rem; line-height: 1.5; }
.timer-dialog-elapsed { color: var(--bp-green, #16a34a); font-weight: 600; }
.timer-dialog-actions { display: flex; gap: .5rem; justify-content: flex-end; }
</style>
