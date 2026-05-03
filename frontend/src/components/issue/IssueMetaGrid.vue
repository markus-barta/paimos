<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import TagChip from '@/components/TagChip.vue'
import StatusDot from '@/components/StatusDot.vue'
import AppIcon from '@/components/AppIcon.vue'
import MarkdownToolbar from '@/components/MarkdownToolbar.vue'
import SprintChips from '@/components/issue/SprintChips.vue'
import {
  STATUS_LABEL,
  PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL,
} from '@/composables/useIssueDisplay'
import { formatDuration } from '@/composables/useDurationInput'
import type { Issue, Sprint } from '@/types'

const props = defineProps<{
  issue: Issue
  parentIssue: Issue | null
  projectId: number | null
  assignedSprints: Sprint[]
  allSprints: Sprint[]
  billingLabel: Record<string, string>
  linkedBillingType: string | null
  // time unit helpers
  timeLabel: () => string
  formatHours: (h: number) => string
  toggleTimeUnit: () => void
}>()

const showEstimateHours = computed(() => { const bt = props.linkedBillingType; return !bt || bt === 'time_and_material' || bt === 'mixed' })
const showEstimateLp    = computed(() => { const bt = props.linkedBillingType; return !bt || bt === 'fixed_price' || bt === 'mixed' })
const showArHours       = computed(() => showEstimateHours.value)
const showArLp          = computed(() => showEstimateLp.value)

const estimateEur = computed(() => {
  const i = props.issue
  if (!i) return null
  const hPart = (i.estimate_hours ?? 0) * (i.rate_hourly ?? 0)
  const lpPart = (i.estimate_lp ?? 0) * (i.rate_lp ?? 0)
  return hPart || lpPart ? hPart + lpPart : null
})
const arEur = computed(() => {
  const i = props.issue
  if (!i) return null
  const hPart = (i.ar_hours ?? 0) * (i.rate_hourly ?? 0)
  const lpPart = (i.ar_lp ?? 0) * (i.rate_lp ?? 0)
  return hPart || lpPart ? hPart + lpPart : null
})

const mdMode = defineModel<boolean>('mdMode', { required: true })

defineEmits<{
  (e: 'remove-sprint', sprintId: number): void
  (e: 'toggle-sprint-dropdown'): void
  (e: 'assign-sprint', sprint: Sprint): void
}>()

function fmtDate(s: string): string {
  if (!s) return '—'
  const d = new Date(s.endsWith('Z') ? s : s + 'Z')
  return isNaN(d.getTime()) ? s.slice(0, 10) : d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}
function fmtDateTime(s: string): string {
  if (!s) return '—'
  const d = new Date(s.endsWith('Z') ? s : s + 'Z')
  return isNaN(d.getTime()) ? s : d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
function issueRoute(issueId: number): string {
  return props.projectId ? `/projects/${props.projectId}/issues/${issueId}` : `/issues/${issueId}`
}
</script>

<template>
  <div class="meta-row">
    <div class="meta-grid">
      <div class="meta-item" v-if="issue.type !== 'sprint'">
        <span class="meta-label">Assignee</span>
        <span class="meta-value">{{ issue.assignee?.username ?? 'Unassigned' }}</span>
      </div>
      <div class="meta-item" v-if="['epic','ticket','task'].includes(issue.type)">
        <span class="meta-label">Cost Unit</span>
        <span class="meta-value">{{ issue.cost_unit || '—' }}</span>
      </div>
      <div class="meta-item" v-if="['epic','ticket','task'].includes(issue.type)">
        <span class="meta-label">Release</span>
        <span class="meta-value">{{ issue.release || '—' }}</span>
      </div>
      <div class="meta-item" v-if="issue.tags?.length">
        <span class="meta-label">Tags</span>
        <div class="meta-tags">
          <TagChip v-for="t in issue.tags" :key="t.id" :tag="t" />
        </div>
      </div>
      <div class="meta-item" v-if="parentIssue">
        <span class="meta-label">Parent</span>
        <RouterLink :to="issueRoute(parentIssue.id)" class="meta-link">
          {{ parentIssue.issue_key }} {{ parentIssue.title }}
        </RouterLink>
      </div>

      <!-- Epic / Cost Unit view fields -->
      <template v-if="issue.type === 'epic' || issue.type === 'cost_unit'">
        <div class="meta-item">
          <span class="meta-label">Billing</span>
          <span class="meta-value">{{ issue.billing_type ? (billingLabel[issue.billing_type] ?? issue.billing_type) : '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">Budget</span>
          <span class="meta-value">{{ issue.total_budget != null ? issue.total_budget.toLocaleString() : '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">Rate / hr</span>
          <span class="meta-value">{{ issue.rate_hourly != null ? issue.rate_hourly : '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">Rate LP</span>
          <span class="meta-value">{{ issue.rate_lp != null ? issue.rate_lp : '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">Jira ID</span>
          <span class="meta-value">{{ issue.jira_id || '—' }}</span>
        </div>
      </template>

      <!-- Estimate + AR fields -->
      <template v-if="['epic','cost_unit','ticket','task'].includes(issue.type)">
        <div class="meta-item" v-if="showEstimateHours">
          <span class="meta-label meta-label--toggle" @click="toggleTimeUnit" title="Toggle h / PT">Est. <span class="unit-toggle">{{ timeLabel() }}</span></span>
          <span class="meta-value">{{ issue.estimate_hours != null ? formatHours(issue.estimate_hours) : '—' }}</span>
        </div>
        <div class="meta-item" v-if="showEstimateLp">
          <span class="meta-label">Est. LP</span>
          <span class="meta-value">{{ issue.estimate_lp != null ? issue.estimate_lp : '—' }}</span>
        </div>
        <div class="meta-item" v-if="estimateEur != null">
          <span class="meta-label">Est. EUR</span>
          <span class="meta-value meta-value--computed">{{ estimateEur.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}</span>
        </div>
        <div class="meta-item" v-if="showArHours">
          <span class="meta-label meta-label--toggle" @click="toggleTimeUnit" title="Toggle h / PT">AR <span class="unit-toggle">{{ timeLabel() }}</span></span>
          <span class="meta-value">{{ issue.ar_hours != null ? formatHours(issue.ar_hours) : '—' }}</span>
        </div>
        <div class="meta-item" v-if="showArLp">
          <span class="meta-label">AR LP</span>
          <span class="meta-value">{{ issue.ar_lp != null ? issue.ar_lp : '—' }}</span>
        </div>
        <div class="meta-item" v-if="arEur != null">
          <span class="meta-label">AR EUR</span>
          <span class="meta-value meta-value--computed">{{ arEur.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}</span>
        </div>
        <div class="meta-item" v-if="issue.time_override != null && (issue.type === 'ticket' || issue.type === 'task')">
          <span class="meta-label">Time Override</span>
          <span class="meta-value">{{ formatDuration(issue.time_override) }}</span>
        </div>
      </template>

      <!-- Release view fields -->
      <template v-if="issue.type === 'release'">
        <div class="meta-item">
          <span class="meta-label">Jira Version</span>
          <span class="meta-value">{{ issue.jira_version || '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">State</span>
          <span v-if="issue.group_state" :class="['v2-state-badge', `v2-state--${issue.group_state}`]">{{ issue.group_state }}</span>
          <span v-else class="meta-value">—</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">Start</span>
          <span class="meta-value">{{ issue.start_date || '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">End</span>
          <span class="meta-value">{{ issue.end_date || '—' }}</span>
        </div>
      </template>

      <!-- Sprint view fields -->
      <template v-if="issue.type === 'sprint'">
        <div class="meta-item">
          <span class="meta-label">Sprint State</span>
          <span v-if="issue.sprint_state" :class="['v2-state-badge', `v2-state--${issue.sprint_state}`]">{{ issue.sprint_state }}</span>
          <span v-else class="meta-value">—</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">Jira ID</span>
          <span class="meta-value">{{ issue.jira_id || '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">Jira Text</span>
          <span class="meta-value">{{ issue.jira_text || '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">Start</span>
          <span class="meta-value">{{ issue.start_date || '—' }}</span>
        </div>
        <div class="meta-item">
          <span class="meta-label">End</span>
          <span class="meta-value">{{ issue.end_date || '—' }}</span>
        </div>
      </template>

      <!-- Ticket view fields -->
      <template v-if="issue.type === 'ticket'">
        <div class="meta-item">
          <span class="meta-label">Jira ID</span>
          <span class="meta-value">{{ issue.jira_id || '—' }}</span>
        </div>
      </template>

      <!-- Sprint assignment (epic, ticket, task) -->
      <template v-if="['epic','ticket','task'].includes(issue.type)">
        <div class="meta-item meta-item--sprint">
          <span class="meta-label">Sprints</span>
          <div class="sprint-assign">
            <SprintChips
              v-if="assignedSprints.length"
              :sprint-ids="assignedSprints.map(s => s.id)"
              :sprints="assignedSprints"
              removable
              @remove="(sid: number) => $emit('remove-sprint', sid)"
            />
            <div class="sprint-add-wrap">
              <button class="sprint-add-btn" @click="$emit('toggle-sprint-dropdown')">
                <AppIcon name="plus" :size="12" /> Add sprint
              </button>
              <slot name="sprint-dropdown" />
            </div>
          </div>
        </div>
      </template>

      <div class="meta-item">
        <span class="meta-label">Updated</span>
        <span class="meta-value" :title="fmtDateTime(issue.updated_at)">{{ fmtDate(issue.updated_at) }}<template v-if="issue.last_changed_by_name"> · {{ issue.last_changed_by_name }}</template></span>
      </div>
      <div class="meta-item">
        <span class="meta-label">Created</span>
        <span class="meta-value" :title="fmtDateTime(issue.created_at)">{{ fmtDate(issue.created_at) }}<template v-if="issue.created_by_name"> · {{ issue.created_by_name }}</template></span>
      </div>
    </div><!-- /meta-grid -->
    <MarkdownToolbar v-model="mdMode" :subtle="true" />
  </div><!-- /meta-row -->
</template>

<style scoped>
.meta-row { display: flex; align-items: baseline; gap: 1.25rem; }
.meta-grid { display: flex; flex-wrap: wrap; gap: 1rem 2rem; flex: 1; }
.meta-item { display: flex; flex-direction: column; gap: .2rem; }
.meta-item--sprint { flex-basis: 100%; }
.meta-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.meta-value { font-size: 13px; color: var(--text); }
.meta-value--computed { color: var(--text-muted); font-style: italic; }
.meta-label--toggle { cursor: pointer; }
.meta-label--toggle:hover .unit-toggle { color: var(--bp-blue); }
.unit-toggle { color: var(--bp-blue); text-decoration: underline; text-decoration-style: dotted; }
.meta-tags  { display: flex; flex-wrap: wrap; gap: .3rem; margin-top: .1rem; }
.meta-link  { font-size: 13px; color: var(--bp-blue); }
.meta-link:hover { color: var(--bp-blue-dark); }

.v2-state-badge {
  display: inline-block;
  font-size: 11px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .05em;
  padding: .15rem .5rem; border-radius: 20px;
}
.v2-state--unreleased { background: #e0eeff; color: #1e4a8a; }
.v2-state--released   { background: #dcfce7; color: #166534; }
.v2-state--planned    { background: #f3f4f6; color: #374151; }
.v2-state--active     { background: #fff3e0; color: #b45309; }
.v2-state--complete   { background: #dcfce7; color: #166534; }
.v2-state--archived   { background: #e5e7eb; color: #6b7280; }

/* Sprint assignment widget — chip styling lives in SprintChips.vue */
.sprint-assign { display: flex; flex-wrap: wrap; align-items: center; gap: .4rem; }
.sprint-add-wrap { position: relative; }
.sprint-add-btn {
  display: inline-flex; align-items: center; gap: .3rem;
  background: none; border: 1px dashed var(--border); border-radius: 20px;
  padding: .15rem .65rem; font-size: 12px; color: var(--text-muted);
  cursor: pointer; font-family: inherit;
  transition: border-color .1s, color .1s;
}
.sprint-add-btn:hover { border-color: var(--bp-blue); color: var(--bp-blue); }
</style>
