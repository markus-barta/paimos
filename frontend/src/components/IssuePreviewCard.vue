<script setup lang="ts">
import { useIssuePreview } from '@/composables/useIssuePreview'
import StatusDot from '@/components/StatusDot.vue'
import AppIcon from '@/components/AppIcon.vue'
import { formatDuration } from '@/composables/useDurationInput'

const { activeIssue: issue, visible, position, skipAnimation, hidePreview, keepPreview } = useIssuePreview()

const STATUS_LABEL: Record<string, string> = {
  new: 'New', backlog: 'Backlog', 'in-progress': 'In Progress',
  done: 'Done', accepted: 'Accepted', invoiced: 'Invoiced', cancelled: 'Cancelled',
}
const PRIORITY_LABEL: Record<string, string> = { high: 'High', medium: 'Medium', low: 'Low' }
</script>

<template>
  <Teleport to="body">
    <Transition :name="skipAnimation ? '' : 'preview'">
      <div
        v-if="visible && issue"
        class="preview-card"
        :style="{ left: position.x + 'px', top: position.y + 'px' }"
        @mouseenter="keepPreview"
        @mouseleave="hidePreview"
      >
        <div class="pc-header">
          <span class="pc-key">{{ issue.issue_key }}</span>
          <span :class="`pc-type pc-type--${issue.type}`">{{ issue.type }}</span>
          <span class="pc-status"><StatusDot :status="issue.status" /> {{ STATUS_LABEL[issue.status] ?? issue.status }}</span>
          <span class="pc-priority">{{ PRIORITY_LABEL[issue.priority] ?? issue.priority }}</span>
        </div>
        <h3 class="pc-title">{{ issue.title }}</h3>
        <div v-if="issue.assignee" class="pc-assignee">
          <AppIcon name="user" :size="11" /> {{ issue.assignee.username }}
        </div>
        <div v-if="issue.description" class="pc-desc">{{ issue.description.slice(0, 200) }}{{ issue.description.length > 200 ? '...' : '' }}</div>
        <div v-if="issue.acceptance_criteria" class="pc-ac">
          <span class="pc-ac-label">AC:</span> {{ issue.acceptance_criteria.slice(0, 150) }}{{ issue.acceptance_criteria.length > 150 ? '...' : '' }}
        </div>
        <div class="pc-footer">
          <span v-if="issue.booked_hours > 0" class="pc-time">
            <AppIcon name="clock" :size="11" /> {{ formatDuration(issue.booked_hours) }}
          </span>
          <span v-if="issue.parent_id && issue.issue_key" class="pc-parent">
            Parent: {{ issue.issue_key.split('-')[0] }}
          </span>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.preview-card {
  position: fixed;
  z-index: 10000;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 12px 40px rgba(0,0,0,.15), 0 4px 12px rgba(0,0,0,.08);
  padding: 1rem 1.25rem;
  width: 360px;
  max-width: 90vw;
  pointer-events: auto;
}
.pc-header {
  display: flex; align-items: center; gap: .4rem; flex-wrap: wrap;
  margin-bottom: .4rem;
}
.pc-key {
  font-size: 11px; font-weight: 700; letter-spacing: .03em;
  padding: .1rem .4rem; border-radius: 4px;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
}
.pc-type {
  font-size: 10px; font-weight: 600; text-transform: capitalize;
}
.pc-type--epic   { color: var(--type-epic, #5e35b1); }
.pc-type--ticket { color: var(--type-ticket, var(--bp-blue-dark)); }
.pc-type--task   { color: var(--type-task, #2e7d32); }
.pc-status {
  display: inline-flex; align-items: center; gap: .25rem;
  font-size: 11px; font-weight: 500; color: var(--text-muted);
}
.pc-priority {
  font-size: 10px; font-weight: 600; color: var(--text-muted); margin-left: auto;
}
.pc-title {
  font-size: 14px; font-weight: 600; line-height: 1.3;
  margin-bottom: .5rem; color: var(--text);
}
.pc-assignee {
  display: flex; align-items: center; gap: .3rem;
  font-size: 11px; color: var(--text-muted); margin-bottom: .5rem;
}
.pc-desc {
  font-size: 12px; line-height: 1.5; color: var(--text-muted);
  margin-bottom: .4rem; white-space: pre-wrap;
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}
.pc-ac {
  font-size: 11px; line-height: 1.4; color: var(--text-muted);
  margin-bottom: .4rem;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.pc-ac-label { font-weight: 600; color: var(--text-muted); }
.pc-footer {
  display: flex; align-items: center; gap: .75rem;
  padding-top: .4rem; border-top: 1px solid var(--border);
  font-size: 11px; color: var(--text-muted);
}
.pc-time {
  display: inline-flex; align-items: center; gap: .25rem;
  font-weight: 600;
}

/* Scale-in animation */
.preview-enter-active { transition: opacity .15s ease-out, transform .15s ease-out; }
.preview-leave-active { transition: opacity .1s ease-in; }
.preview-enter-from { opacity: 0; transform: scale(0.97); }
.preview-leave-to { opacity: 0; }
</style>
