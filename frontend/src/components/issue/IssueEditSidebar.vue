<script setup lang="ts">
import { computed } from 'vue'
import AutocompleteInput from '@/components/AutocompleteInput.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import AppIcon from '@/components/AppIcon.vue'
import NumericInput from '@/components/NumericInput.vue'
import TagSelector from '@/components/TagSelector.vue'
import SprintChips from '@/components/issue/SprintChips.vue'
import { EPIC_COLOR_PALETTE } from '@/config/epicColors'
import type { Issue, Tag, Sprint, User } from '@/types'
import { assignableIssueUsers } from '@/utils/users'
import {
  TYPE_SVGS,
  STATUS_DOT_STYLE, STATUS_LABEL,
  PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL,
} from '@/composables/useIssueDisplay'

interface IssueForm {
  title: string; description: string; acceptance_criteria: string; notes: string
  type: string; status: string; priority: string
  cost_unit: string; release: string
  parent_id: number | null; assignee_id: number | null
  billing_type: string | null; total_budget: number | null
  rate_hourly: number | null; rate_lp: number | null
  estimate_hours: number | null; estimate_lp: number | null
  ar_hours: number | null; ar_lp: number | null
  time_override: number | null
  start_date: string | null; end_date: string | null
  group_state: string | null; sprint_state: string | null
  jira_id: string | null; jira_version: string | null; jira_text: string | null
  color: string | null
}

const props = defineProps<{
  form: IssueForm
  issueType: string
  costUnits: string[]
  releases: string[]
  allTags: Tag[]
  issueTagIds: number[]
  validParents: Issue[]
  users: Pick<User, 'id' | 'username' | 'role' | 'status'>[]
  assignedSprints: Sprint[]
  typeChangeWarning: string
  linkedBillingType: string | null
  timeUnit: string
  timeLabel: () => string
  toggleTimeUnit: () => void
  saving: boolean
  saveError: string
}>()

const emit = defineEmits<{
  (e: 'save'): void
  (e: 'cancel'): void
  (e: 'add-tag', tagId: number): void
  (e: 'remove-tag', tagId: number): void
  (e: 'remove-sprint', sprintId: number): void
  (e: 'toggle-sprint-dropdown'): void
}>()

const TYPE_OPTIONS: MetaOption[] = [
  { value: 'epic',      label: 'Epic',      icon: TYPE_SVGS.epic      },
  { value: 'ticket',    label: 'Ticket',    icon: TYPE_SVGS.ticket    },
  { value: 'task',      label: 'Task',      icon: TYPE_SVGS.task      },
  { value: 'cost_unit', label: 'Cost Unit', icon: TYPE_SVGS.cost_unit },
  { value: 'release',   label: 'Release',   icon: TYPE_SVGS.release   },
  { value: 'sprint',    label: 'Sprint',    icon: TYPE_SVGS.sprint    },
]
const STATUS_OPTIONS: MetaOption[] = [
  { value: 'new',          label: STATUS_LABEL.new,             dotColor: STATUS_DOT_STYLE.new.color,             dotOutline: STATUS_DOT_STYLE.new.outline },
  { value: 'backlog',      label: STATUS_LABEL.backlog,         dotColor: STATUS_DOT_STYLE.backlog.color,         dotOutline: STATUS_DOT_STYLE.backlog.outline },
  { value: 'in-progress',  label: STATUS_LABEL['in-progress'],  dotColor: STATUS_DOT_STYLE['in-progress'].color,  dotOutline: STATUS_DOT_STYLE['in-progress'].outline },
  { value: 'qa',           label: STATUS_LABEL.qa,              dotColor: STATUS_DOT_STYLE.qa.color,              dotOutline: STATUS_DOT_STYLE.qa.outline },
  { value: 'done',         label: STATUS_LABEL.done,            dotColor: STATUS_DOT_STYLE.done.color,            dotOutline: STATUS_DOT_STYLE.done.outline },
  { value: 'delivered',    label: STATUS_LABEL.delivered,       dotColor: STATUS_DOT_STYLE.delivered.color,       dotOutline: STATUS_DOT_STYLE.delivered.outline },
  { value: 'accepted',     label: STATUS_LABEL.accepted,        dotColor: STATUS_DOT_STYLE.accepted.color,        dotOutline: STATUS_DOT_STYLE.accepted.outline },
  { value: 'invoiced',     label: STATUS_LABEL.invoiced,        dotColor: STATUS_DOT_STYLE.invoiced.color,        dotOutline: STATUS_DOT_STYLE.invoiced.outline },
  { value: 'cancelled',    label: STATUS_LABEL.cancelled,       dotColor: STATUS_DOT_STYLE.cancelled.color,       dotOutline: STATUS_DOT_STYLE.cancelled.outline },
]
const PRIORITY_OPTIONS: MetaOption[] = [
  { value: 'high',   label: PRIORITY_LABEL.high,   arrow: PRIORITY_ICON.high,   arrowColor: PRIORITY_COLOR.high   },
  { value: 'medium', label: PRIORITY_LABEL.medium, arrow: PRIORITY_ICON.medium, arrowColor: PRIORITY_COLOR.medium },
  { value: 'low',    label: PRIORITY_LABEL.low,    arrow: PRIORITY_ICON.low,    arrowColor: PRIORITY_COLOR.low    },
]

const parentOptions = computed<MetaOption[]>(() => {
  const terminal = new Set(['done', 'accepted', 'invoiced', 'cancelled'])
  const active = props.validParents.filter(p => !terminal.has(p.status))
  const closed = props.validParents.filter(p =>  terminal.has(p.status))
  return [
    ...active.map(p => ({ value: String(p.id), label: `${p.issue_key} ${p.title}` })),
    ...closed.map(p => ({ value: String(p.id), label: `${p.issue_key} ${p.title} (done)` })),
  ]
})

const assigneeOptions = computed<MetaOption[]>(() => [
  { value: '', label: 'Unassigned' },
  ...assignableIssueUsers(props.users).map(u => ({ value: String(u.id), label: u.username })),
])

const formParentStr = computed({
  get: () => props.form.parent_id !== null ? String(props.form.parent_id) : '',
  set: (v: string) => { props.form.parent_id = v ? Number(v) : null },
})
const formAssigneeStr = computed({
  get: () => props.form.assignee_id !== null ? String(props.form.assignee_id) : '',
  set: (v: string) => { props.form.assignee_id = v ? Number(v) : null },
})

const showEstimateHours = computed(() => { const bt = props.linkedBillingType; return !bt || bt === 'time_and_material' || bt === 'mixed' })
const showEstimateLp    = computed(() => { const bt = props.linkedBillingType; return !bt || bt === 'fixed_price' || bt === 'mixed' })
const showArHours       = computed(() => showEstimateHours.value)
const showArLp          = computed(() => showEstimateLp.value)
</script>

<template>
  <div class="edit-sidebar">
    <div class="field">
      <label>Type</label>
      <MetaSelect v-model="form.type" :options="TYPE_OPTIONS" />
      <p v-if="typeChangeWarning" class="field-warning">{{ typeChangeWarning }}</p>
    </div>
    <div class="field" v-if="form.type !== 'epic'">
      <label>Parent</label>
      <MetaSelect v-model="formParentStr" :options="parentOptions" placeholder="None (top-level)" />
    </div>
    <div class="field">
      <label>Status</label>
      <MetaSelect v-model="form.status" :options="STATUS_OPTIONS" />
    </div>
    <template v-if="issueType !== 'sprint'">
      <div class="field">
        <label>Priority</label>
        <MetaSelect v-model="form.priority" :options="PRIORITY_OPTIONS" />
      </div>
      <div class="field">
        <label>Assignee</label>
        <MetaSelect v-model="formAssigneeStr" :options="assigneeOptions" placeholder="Unassigned" />
      </div>
    </template>
    <div class="field" v-if="['epic','ticket','task'].includes(form.type)">
      <label>Cost Unit</label>
      <AutocompleteInput v-model="form.cost_unit" :suggestions="costUnits" placeholder="e.g. PROJ-2024" />
    </div>
    <div class="field" v-if="['epic','ticket','task'].includes(form.type)">
      <label>Release</label>
      <select v-model="form.release" class="v2-select">
        <option value="">— None —</option>
        <option v-for="r in releases" :key="r" :value="r">{{ r }}</option>
      </select>
    </div>
    <div class="field" v-if="allTags.length">
      <label>Tags</label>
      <TagSelector
        :all-tags="allTags"
        :selected-ids="issueTagIds"
        @add="$emit('add-tag', $event)"
        @remove="$emit('remove-tag', $event)"
      />
    </div>

    <!-- Epic / Cost Unit fields -->
    <template v-if="form.type === 'epic' || form.type === 'cost_unit'">
      <div class="field">
        <label>Billing Type</label>
        <select v-model="form.billing_type" class="v2-select">
          <option :value="null">— None —</option>
          <option value="time_and_material">Time &amp; Material</option>
          <option value="fixed_price">Fixed Price</option>
          <option value="mixed">Mixed</option>
        </select>
      </div>
      <div class="field">
        <label>Total Budget</label>
        <NumericInput v-model="form.total_budget" placeholder="e.g. 10000" />
      </div>
      <div class="field">
        <label>Hourly Rate</label>
        <NumericInput v-model="form.rate_hourly" placeholder="e.g. 150" />
      </div>
      <div class="field">
        <label>Rate LP</label>
        <NumericInput v-model="form.rate_lp" placeholder="e.g. 5000" />
      </div>
      <div class="field">
        <label>Jira ID</label>
        <input v-model="form.jira_id" type="text" placeholder="e.g. PROJ-123" />
      </div>
      <div class="field" v-if="form.type === 'epic'">
        <label>Color</label>
        <div class="epic-color-picker">
          <button
            v-for="c in EPIC_COLOR_PALETTE" :key="c.key"
            type="button"
            class="epic-color-swatch"
            :class="{ 'epic-color-swatch--active': form.color === c.key }"
            :style="{ background: c.bg, color: c.fg }"
            :title="c.key"
            @click="form.color = form.color === c.key ? null : c.key"
          >{{ form.color === c.key ? '✓' : '' }}</button>
        </div>
      </div>
    </template>

    <!-- Ticket / Task fields -->
    <template v-if="form.type === 'ticket' || form.type === 'task'">
      <div class="field" v-if="showEstimateHours">
        <label class="meta-label--toggle" @click="toggleTimeUnit">Est. <span class="unit-toggle">{{ timeLabel() }}</span></label>
        <NumericInput v-model="form.estimate_hours" :placeholder="timeUnit === 'pt' ? 'e.g. 5 PT' : 'e.g. 40h'" />
      </div>
      <div class="field" v-if="showEstimateLp">
        <label>Est. LP</label>
        <NumericInput v-model="form.estimate_lp" placeholder="e.g. 3" />
      </div>
      <div class="field" v-if="showArHours">
        <label class="meta-label--toggle" @click="toggleTimeUnit">AR <span class="unit-toggle">{{ timeLabel() }}</span></label>
        <NumericInput v-model="form.ar_hours" :placeholder="timeUnit === 'pt' ? 'e.g. 5 PT' : 'e.g. 40h'" />
      </div>
      <div class="field" v-if="showArLp">
        <label>AR LP</label>
        <NumericInput v-model="form.ar_lp" placeholder="e.g. 3" />
      </div>
      <div class="field">
        <label>Time Override</label>
        <NumericInput v-model="form.time_override" placeholder="e.g. 8 (hours)" />
      </div>
    </template>

    <!-- Release fields -->
    <template v-if="form.type === 'release'">
      <div class="field">
        <label>Jira Version</label>
        <input v-model="form.jira_version" type="text" placeholder="e.g. v1.2.0" />
      </div>
      <div class="field">
        <label>Group State</label>
        <select v-model="form.group_state" class="v2-select">
          <option :value="null">— None —</option>
          <option value="unreleased">Unreleased</option>
          <option value="released">Released</option>
        </select>
      </div>
    </template>

    <!-- Sprint fields -->
    <template v-if="form.type === 'sprint'">
      <div class="field">
        <label>Sprint State</label>
        <select v-model="form.sprint_state" class="v2-select">
          <option :value="null">— None —</option>
          <option value="planned">Planned</option>
          <option value="active">Active</option>
          <option value="complete">Complete</option>
        </select>
      </div>
      <div class="field">
        <label>Jira ID</label>
        <input v-model="form.jira_id" type="text" placeholder="e.g. SPRINT-5" />
      </div>
      <div class="field">
        <label>Jira Text</label>
        <input v-model="form.jira_text" type="text" placeholder="Sprint label from Jira" />
      </div>
    </template>

    <!-- Date fields (release + sprint) -->
    <template v-if="form.type === 'release' || form.type === 'sprint'">
      <div class="field">
        <label>Start Date</label>
        <input v-model="form.start_date" type="date" />
      </div>
      <div class="field">
        <label>End Date</label>
        <input v-model="form.end_date" type="date" />
      </div>
    </template>

    <!-- Ticket fields -->
    <template v-if="form.type === 'ticket'">
      <div class="field">
        <label>Jira ID</label>
        <input v-model="form.jira_id" type="text" placeholder="e.g. PROJ-123" />
      </div>
    </template>

    <!-- Sprint assignment in edit mode -->
    <template v-if="['epic','ticket','task'].includes(form.type)">
      <div class="field">
        <label>Sprints</label>
        <div class="sprint-assign">
          <SprintChips
            v-if="assignedSprints.length"
            :sprint-ids="assignedSprints.map(s => s.id)"
            :sprints="assignedSprints"
            removable
            @remove="(sid: number) => $emit('remove-sprint', sid)"
          />
          <div class="sprint-add-wrap">
            <button class="sprint-add-btn" type="button" @click="$emit('toggle-sprint-dropdown')">
              <AppIcon name="plus" :size="12" /> Add sprint
            </button>
            <slot name="sprint-dropdown" />
          </div>
        </div>
      </div>
    </template>

    <div v-if="saveError" class="form-error">{{ saveError }}</div>

    <div class="sidebar-footer">
      <button class="btn btn-ghost btn-sm" @click="$emit('cancel')">Cancel</button>
      <button class="btn btn-primary btn-sm" @click="$emit('save')" :disabled="saving">
        {{ saving ? 'Saving…' : 'Save changes' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.edit-sidebar {
  padding: 1.25rem 1.25rem 0;
  display: flex; flex-direction: column; gap: .85rem;
  background: var(--bg);
  overflow-y: auto;
}
.sidebar-footer {
  position: sticky; bottom: 0;
  display: flex; justify-content: flex-end; gap: .5rem;
  padding: .85rem 0 1.25rem;
  background: var(--bg);
  border-top: 1px solid var(--border);
  margin-top: auto;
}
.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 11px; font-weight: 700; color: var(--text-muted); text-transform: uppercase; letter-spacing: .06em; }
.field-warning {
  font-size: 11px; color: #b94040;
  background: #fde8e8; border-radius: var(--radius);
  padding: .3rem .5rem; margin-top: .15rem;
}
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
.meta-label--toggle { cursor: pointer; }
.meta-label--toggle:hover .unit-toggle { color: var(--bp-blue); }
.unit-toggle { color: var(--bp-blue); text-decoration: underline; text-decoration-style: dotted; }

.v2-select {
  border: 1px solid var(--border); border-radius: var(--radius);
  padding: .35rem .55rem; font-size: 13px; font-family: inherit;
  background: var(--bg); color: var(--text);
  outline: none; width: 100%;
}
.v2-select:focus { border-color: var(--bp-blue); }

.epic-color-picker { display: flex; flex-wrap: wrap; gap: .35rem; }
.epic-color-swatch {
  width: 26px; height: 26px; border-radius: 50%; border: 2px solid transparent;
  cursor: pointer; font-size: 11px; font-weight: 700;
  display: inline-flex; align-items: center; justify-content: center;
  transition: border-color .1s, transform .1s;
}
.epic-color-swatch:hover { transform: scale(1.15); }
.epic-color-swatch--active { border-color: var(--text); transform: scale(1.15); }

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
