<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import AppModal from '@/components/AppModal.vue'
import AutocompleteInput from '@/components/AutocompleteInput.vue'
import TagSelector from '@/components/TagSelector.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import NumericInput from '@/components/NumericInput.vue'
import SprintChips from '@/components/issue/SprintChips.vue'
import AttachmentSidebar from '@/components/issue/AttachmentSidebar.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import type { Issue, Tag, Sprint, User } from '@/types'
import type { Project } from '@/types'
import { api, errMsg } from '@/api/client'
import { attachmentsEnabled } from '@/api/instance'
import { useIssueDisplay, TYPE_SVGS } from '@/composables/useIssueDisplay'
import { TYPE_OPTIONS, STATUS_OPTIONS, PRIORITY_OPTIONS } from '@/composables/useIssueFilter'
import { useIssueContext } from '@/composables/useIssueContext'
import { useRecentProjects } from '@/composables/useRecentProjects'
import { useTimeUnit } from '@/composables/useTimeUnit'
import { useAttachmentUploads } from '@/composables/useAttachmentUploads'
import { EPIC_COLOR_PALETTE } from '@/config/epicColors'
import { assignableIssueUsers } from '@/utils/users'
// PAI-146: AI text optimization on multiline fields. issue_id is 0
// here (no row exists yet); the backend skips context lookup for that
// sentinel and works on the source text alone.
import AiActionMenu from '@/components/ai/AiActionMenu.vue'
import AiSurfaceFeedback from '@/components/ai/AiSurfaceFeedback.vue'
import { applyIssueTextMutations, type AiApplyInfo } from '@/services/aiActionApply'

function onAiAccept(field: 'description' | 'acceptance_criteria' | 'notes') {
  return (text: string) => { form.value[field] = text }
}

async function applyAiCreateResult(info: AiApplyInfo) {
  const next = applyIssueTextMutations(info, {
    description: form.value.description,
    acceptance_criteria: form.value.acceptance_criteria,
    notes: form.value.notes,
  })
  form.value.description = next.description
  form.value.acceptance_criteria = next.acceptance_criteria
  form.value.notes = next.notes
}

const { users, allTags, costUnits, releases, projects, sprints } = useIssueContext()
const { label: timeLabel, toggle: toggleTimeUnit } = useTimeUnit()
const { recentProjects } = useRecentProjects()

const props = defineProps<{
  open: boolean
  projectId?: number
  issues: Issue[]
  initialType?: string
  defaultParentId?: number
  projectAllIssues?: Issue[]
  derivedCreateType: string | null
  initialProjectId?: number | null
}>()

const emit = defineEmits<{
  close: []
  created: [issue: Issue]
  'cost-unit-added': [value: string]
  'release-added':   [value: string]
  'project-changed': [projectId: number | null]
}>()

const { showTypeIcon, showTypeText } = useIssueDisplay()

const createProjectId = ref<number | null>(null)
const confirmingDiscard = ref(false)

const emptyForm = () => ({
  title: '', description: '', acceptance_criteria: '', notes: '',
  type: 'epic' as string, parent_id: null as number | null,
  status: 'backlog', priority: 'medium',
  cost_unit: '', release: '',
  assignee_id: null as number | null,
  billing_type:  null as string | null,
  total_budget:  null as number | null,
  rate_hourly:   null as number | null,
  rate_lp:  null as number | null,
  estimate_hours: null as number | null,
  estimate_lp:    null as number | null,
  start_date:    null as string | null,
  end_date:      null as string | null,
  group_state:   null as string | null,
  sprint_state:  null as string | null,
  jira_id:       null as string | null,
  jira_version:  null as string | null,
  jira_text:     null as string | null,
  color:         null as string | null,
  sprint_ids:    [] as number[],
})

// Sprint picker dropdown state
const sprintDropOpen = ref(false)
const assignedSprintsForForm = computed(() =>
  sprints.value.filter(s => form.value.sprint_ids.includes(s.id)),
)
const availableSprintsForForm = computed(() =>
  sprints.value.filter(s => !form.value.sprint_ids.includes(s.id)),
)
function addSprintToForm(sprintId: number) {
  if (!form.value.sprint_ids.includes(sprintId)) {
    form.value.sprint_ids = [...form.value.sprint_ids, sprintId]
  }
  sprintDropOpen.value = false
}
function removeSprintFromForm(sprintId: number) {
  form.value.sprint_ids = form.value.sprint_ids.filter(id => id !== sprintId)
}

// Attachments — upload to /api/attachments (pending, no issue_id) while the
// user fills out the form, then promote to the new issue via
// PATCH /api/attachments/link after the create succeeds.
const attachments = useAttachmentUploads({ endpoint: () => '/attachments' })

const form        = ref(emptyForm())
const formTagIds  = ref<number[]>([])
const formError   = ref('')
const saving      = ref(false)

// Session memory: last-used type + parent
const rememberedType     = ref<string | null>(null)
const rememberedParentId = ref<number | null>(null)

const formParentStr = computed({
  get: () => form.value.parent_id !== null ? String(form.value.parent_id) : '',
  set: (v: string) => { form.value.parent_id = v ? Number(v) : null },
})
const formAssigneeStr = computed({
  get: () => form.value.assignee_id !== null ? String(form.value.assignee_id) : '',
  set: (v: string) => { form.value.assignee_id = v ? Number(v) : null },
})

const assignableUsers = computed(() => assignableIssueUsers(users.value))

const assigneeFormOptions = computed<MetaOption[]>(() => [
  { value: '', label: 'Unassigned' },
  ...assignableUsers.value.map(u => ({ value: String(u.id), label: u.username })),
])

const allIssuesForParent = computed(() => props.projectAllIssues ?? props.issues)

const validParents = computed(() => {
  const t = form.value.type
  const pool = allIssuesForParent.value
  if (t === 'epic') return []
  if (['cost_unit','release','sprint'].includes(t)) return pool.filter(i => i.type === 'epic')
  if (t === 'ticket') return pool.filter(i => i.type === 'epic')
  if (t === 'task')   return pool.filter(i => i.type === 'ticket')
  return []
})

const parentOptions = computed<MetaOption[]>(() =>
  validParents.value.map(p => {
    const truncated = p.title.length > 40 ? p.title.slice(0, 40) + '...' : p.title
    return { value: String(p.id), label: `${p.issue_key} — ${truncated}` }
  })
)

const isDirty = computed(() => {
  const f = form.value
  return !!(f.title || f.description || f.acceptance_criteria || f.notes ||
    f.cost_unit || f.release || formTagIds.value.length || attachments.jobs.value.length)
})

function typeLabel(type: string): string {
  return type.charAt(0).toUpperCase() + type.slice(1)
}

function openCreate(parentIssue?: Issue, overrideType?: string, overrideParentId?: number) {
  const f = emptyForm()
  if (parentIssue) {
    f.parent_id = parentIssue.id
    f.type = parentIssue.type === 'epic' ? 'ticket' : 'task'
  } else if (overrideParentId !== undefined) {
    const parent = props.issues.find(i => i.id === overrideParentId)
      ?? props.projectAllIssues?.find(i => i.id === overrideParentId)
    if (parent) {
      f.parent_id = parent.id
      f.type = parent.type === 'epic' ? 'ticket' : 'task'
    } else {
      f.parent_id = overrideParentId
      f.type = overrideType ?? props.initialType ?? 'ticket'
    }
  } else if (overrideType) {
    f.type = overrideType
  } else {
    f.type      = props.derivedCreateType ?? rememberedType.value ?? props.initialType ?? 'ticket'
    f.parent_id = props.defaultParentId   ?? rememberedParentId.value ?? null
  }
  form.value          = f
  formTagIds.value    = []
  formError.value     = ''
  confirmingDiscard.value = false
  attachments.reset()
  if (props.projectId === undefined) {
    const fallback = props.initialProjectId ?? recentProjects.value[0]?.id ?? null
    createProjectId.value = fallback
  }
}

watch(createProjectId, (v) => {
  if (props.projectId === undefined) {
    form.value.parent_id = null
    emit('project-changed', v)
  }
})

function requestClose() {
  if (isDirty.value) {
    confirmingDiscard.value = true
  } else {
    doClose()
  }
}

function doClose() {
  confirmingDiscard.value = false
  emit('close')
}

function onDiscardKey(e: KeyboardEvent) {
  const tag = (e.target as HTMLElement)?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
  if (e.key.toLowerCase() === 'd') { doClose(); e.preventDefault() }
  if (e.key.toLowerCase() === 'k') { confirmingDiscard.value = false; e.preventDefault() }
}
watch(confirmingDiscard, (v) => {
  if (v) window.addEventListener('keydown', onDiscardKey)
  else   window.removeEventListener('keydown', onDiscardKey)
})

function onTagAdd(tagId: number) {
  if (!formTagIds.value.includes(tagId)) formTagIds.value = [...formTagIds.value, tagId]
}
function onTagRemove(tagId: number) {
  formTagIds.value = formTagIds.value.filter(id => id !== tagId)
}

async function syncIssueTags(issueId: number, oldIds: number[], newIds: number[]) {
  const toAdd    = newIds.filter(id => !oldIds.includes(id))
  const toRemove = oldIds.filter(id => !newIds.includes(id))
  await Promise.all([
    ...toAdd.map(id    => api.post(`/issues/${issueId}/tags`, { tag_id: id })),
    ...toRemove.map(id => api.delete(`/issues/${issueId}/tags/${id}`)),
  ])
}

async function saveIssue(andAnother = false) {
  formError.value = ''
  if (!form.value.title.trim()) { formError.value = 'Title required.'; return }
  if (attachments.hasInFlight.value) {
    formError.value = `Please wait — ${attachments.inFlightCount.value} attachment upload${attachments.inFlightCount.value > 1 ? 's' : ''} still in progress.`
    return
  }
  const pid = props.projectId ?? createProjectId.value
  if (!pid) { formError.value = 'Select a project first.'; return }
  saving.value = true
  try {
    const payload = { ...form.value, parent_id: form.value.parent_id || null }
    const created = await api.post<Issue>(`/projects/${pid}/issues`, payload)
    try {
      await syncIssueTags(created.id, [], formTagIds.value)
    } catch (tagErr: unknown) {
      /* non-critical — issue was created, tags may be incomplete */
    }
    try {
      await attachments.linkPending(created.id)
    } catch (attachErr: unknown) {
      /* non-critical — issue was created, attachments stay as pending on the server */
    }
    const fresh = await api.get<Issue>(`/issues/${created.id}`)
    rememberedType.value     = form.value.type
    rememberedParentId.value = form.value.parent_id
    emit('created', fresh)
    const cu = form.value.cost_unit?.trim()
    if (cu && !costUnits.value.includes(cu)) emit('cost-unit-added', cu)
    const rel = fresh.type === 'release' ? fresh.title.trim() : form.value.release?.trim()
    if (rel && !releases.value.includes(rel)) emit('release-added', rel)
    if (andAnother) {
      const savedForm = { ...form.value, title: '' }
      const savedTags = [...formTagIds.value]
      const savedProject = createProjectId.value
      doClose()
      setTimeout(() => {
        createProjectId.value = savedProject
        form.value = savedForm
        formTagIds.value = savedTags
        // Parent re-opens via openCreate being called externally
      }, 80)
    } else {
      doClose()
    }
  } catch (e: unknown) {
    formError.value = errMsg(e)
  } finally {
    saving.value = false
  }
}

defineExpose({ openCreate })
</script>

<template>
  <AppModal title="New Issue" :open="open" @close="requestClose" max-width="1380px">
    <!-- Discard confirm overlay inside modal -->
    <div v-if="confirmingDiscard" class="discard-confirm">
      <p class="discard-msg">Discard unsaved changes?</p>
      <div class="discard-actions">
        <button class="btn btn-ghost" @click="confirmingDiscard = false"><u>K</u>eep editing</button>
        <button class="btn btn-danger" @click="doClose"><u>D</u>iscard</button>
      </div>
    </div>
    <div v-else class="create-layout">
    <form @submit.prevent="saveIssue(false)" class="form">
      <div v-if="!projectId" class="field" style="margin-bottom:.5rem">
        <label>Project</label>
        <select v-model.number="createProjectId" class="v2-select" required>
          <option :value="null" disabled>— Select a project —</option>
          <option v-for="p in (projects ?? []).filter(pp => pp.status === 'active')" :key="p.id" :value="p.id">{{ p.key }} — {{ p.name }}</option>
        </select>
      </div>
      <div class="form-row">
        <div class="field">
          <label>Type</label>
          <div v-if="derivedCreateType" class="type-locked">
            <span :class="`issue-type issue-type--${form.type}`">
              <span v-if="showTypeIcon" v-html="TYPE_SVGS[form.type] ?? ''"></span>
              <span class="type-label-text">{{ typeLabel(form.type) }}</span>
            </span>
            <span class="type-locked-hint">locked by active view</span>
          </div>
          <MetaSelect v-else v-model="form.type" :options="TYPE_OPTIONS" @update:modelValue="form.parent_id = null" />
        </div>
        <div class="field" v-if="form.type !== 'epic'">
          <label>Parent</label>
          <MetaSelect v-model="formParentStr" :options="parentOptions" placeholder="None (top-level)" searchable />
        </div>
      </div>
      <div class="field">
        <label>Title</label>
        <input v-model="form.title" type="text" placeholder="Issue title" required autofocus />
      </div>
      <AiSurfaceFeedback host-key="create-issue:record" :apply="applyAiCreateResult" />
      <div class="field">
        <div class="field-label-row">
          <label>Description</label>
          <AiActionMenu surface="issue"
            host-key="create-issue:description"
            field="description"
            field-label="Description"
            :issue-id="0"
            :text="() => form.description"
            :on-accept="onAiAccept('description')"
          />
        </div>
        <textarea v-auto-grow v-model="form.description" rows="2" placeholder="Optional description"></textarea>
        <AiSurfaceFeedback host-key="create-issue:description" :apply="applyAiCreateResult" />
      </div>
      <div class="field" v-if="['epic','cost_unit','ticket'].includes(form.type)">
        <div class="field-label-row">
          <label>Acceptance Criteria</label>
          <AiActionMenu surface="issue"
            host-key="create-issue:acceptance_criteria"
            field="acceptance_criteria"
            field-label="Acceptance Criteria"
            :issue-id="0"
            :text="() => form.acceptance_criteria"
            :on-accept="onAiAccept('acceptance_criteria')"
          />
        </div>
        <textarea v-auto-grow v-model="form.acceptance_criteria" rows="2" placeholder="When is this done?"></textarea>
        <AiSurfaceFeedback host-key="create-issue:acceptance_criteria" :apply="applyAiCreateResult" />
      </div>
      <div class="field">
        <div class="field-label-row">
          <label>Notes</label>
          <AiActionMenu surface="issue"
            host-key="create-issue:notes"
            field="notes"
            field-label="Notes"
            :issue-id="0"
            :text="() => form.notes"
            :on-accept="onAiAccept('notes')"
          />
        </div>
        <textarea v-auto-grow v-model="form.notes" rows="2" placeholder="Additional notes"></textarea>
        <AiSurfaceFeedback host-key="create-issue:notes" :apply="applyAiCreateResult" />
      </div>
      <div class="form-row">
        <div class="field">
          <label>Status</label>
          <MetaSelect v-model="form.status" :options="STATUS_OPTIONS" />
        </div>
        <div class="field">
          <label>Priority</label>
          <MetaSelect v-model="form.priority" :options="PRIORITY_OPTIONS" />
        </div>
      </div>
      <div class="form-row" v-if="['epic','ticket','task'].includes(form.type)">
        <div class="field">
          <label>Cost Unit</label>
          <AutocompleteInput v-model="form.cost_unit" :suggestions="costUnits" placeholder="e.g. PROJ-2024" />
        </div>
        <div class="field">
          <label>Release</label>
          <select v-model="form.release" class="v2-select">
            <option value="">— None —</option>
            <option v-for="r in releases" :key="r" :value="r">{{ r }}</option>
          </select>
        </div>
      </div>
      <div class="field">
        <label>Assignee</label>
        <MetaSelect v-model="formAssigneeStr" :options="assigneeFormOptions" placeholder="Unassigned" />
      </div>
      <template v-if="['epic','cost_unit'].includes(form.type)">
        <div class="field">
          <label>Billing Type</label>
          <select v-model="form.billing_type" class="v2-select">
            <option :value="null">— None —</option>
            <option value="time_and_material">Time &amp; Material</option>
            <option value="fixed_price">Fixed Price</option>
          </select>
        </div>
        <div class="form-row">
          <div class="field">
            <label>Total Budget</label>
            <NumericInput v-model="form.total_budget" placeholder="e.g. 10000" />
          </div>
          <div class="field">
            <label>Hourly Rate</label>
            <NumericInput v-model="form.rate_hourly" placeholder="e.g. 150" />
          </div>
        </div>
        <div class="field">
          <label>Rate LP</label>
          <NumericInput v-model="form.rate_lp" placeholder="e.g. 5000" />
        </div>
      </template>
      <!-- Epic color picker -->
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
      <div class="form-row" v-if="['ticket','task','epic','cost_unit'].includes(form.type)">
        <div class="field">
          <label class="time-unit-toggle" @click="toggleTimeUnit" title="Toggle h / PT">
            Est. <span class="unit-toggle">{{ timeLabel() }}</span>
          </label>
          <NumericInput v-model="form.estimate_hours" placeholder="e.g. 40" />
        </div>
        <div class="field">
          <label>Est. LP</label>
          <NumericInput v-model="form.estimate_lp" placeholder="e.g. 5" />
        </div>
      </div>
      <div class="field" v-if="['ticket','task','epic'].includes(form.type) && sprints?.length">
        <label>Sprints</label>
        <div class="sprint-picker-inline">
          <SprintChips
            v-if="form.sprint_ids.length"
            :sprint-ids="form.sprint_ids"
            :sprints="assignedSprintsForForm"
            removable
            @remove="removeSprintFromForm"
          />
          <div class="sprint-add-wrap">
            <button type="button" class="sprint-add-btn" @click="sprintDropOpen = !sprintDropOpen">
              + Add sprint
            </button>
            <div v-if="sprintDropOpen" class="sprint-add-dropdown">
              <button
                v-for="s in availableSprintsForForm" :key="s.id"
                type="button"
                class="sprint-add-opt"
                @click="addSprintToForm(s.id)"
              >
                {{ s.title }}
                <span v-if="s.sprint_state" class="sprint-add-state">{{ s.sprint_state }}</span>
              </button>
              <div v-if="!availableSprintsForForm.length" class="sprint-add-empty">All sprints assigned</div>
            </div>
          </div>
        </div>
      </div>
      <div class="form-row" v-if="['release','sprint'].includes(form.type)">
        <div class="field">
          <label>Start Date</label>
          <input v-model="form.start_date" type="date" />
        </div>
        <div class="field">
          <label>End Date</label>
          <input v-model="form.end_date" type="date" />
        </div>
      </div>
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
      <div class="field" v-if="['epic','cost_unit','ticket'].includes(form.type)">
        <label>Jira ID</label>
        <input v-model="form.jira_id" type="text" placeholder="e.g. PROJ-123" />
      </div>
      <div class="field" v-if="allTags?.length">
        <label>Tags</label>
        <TagSelector
          :all-tags="allTags!"
          :selected-ids="formTagIds"
          @add="onTagAdd"
          @remove="onTagRemove"
        />
      </div>
      <div v-if="formError" class="form-error">{{ formError }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="requestClose">Cancel</button>
        <button type="button" class="btn btn-ghost" :disabled="saving || attachments.hasInFlight.value" @click="saveIssue(true)">
          {{ saving ? 'Creating...' : 'Save & add another' }}
        </button>
        <button type="submit" class="btn btn-primary" :disabled="saving || attachments.hasInFlight.value">
          {{ saving
            ? 'Creating...'
            : attachments.hasInFlight.value
              ? `Uploading ${attachments.inFlightCount.value}…`
              : 'Create issue' }}
        </button>
      </div>
    </form>
    <AttachmentSidebar
      v-if="attachmentsEnabled"
      class="create-attach-sidebar"
      title="Attachments"
      :jobs="attachments.jobs.value"
      @add-files="(files) => attachments.addFiles(files)"
      @remove="(job) => attachments.removeJob(job)"
      @retry="(job) => attachments.retryJob(job)"
    />
    </div>
  </AppModal>
</template>

<style scoped>
/* PAI-146: per-field label row holds the label + the AI optimize
   button on the right. Mirrors the IssueDetailView treatment. */
.field-label-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
}
.field-label-row > label { margin-bottom: 0; }

.discard-confirm {
  display: flex; flex-direction: column; gap: 1.25rem;
  padding: .5rem 0;
}
.discard-msg { font-size: 14px; color: var(--text); font-weight: 500; }
.discard-actions { display: flex; justify-content: flex-end; gap: .5rem; }

.create-layout {
  display: flex;
  align-items: stretch;
  gap: 1rem;
  min-height: 0;
}
.form { display: flex; flex-direction: column; gap: .85rem; flex: 1 1 auto; min-width: 0; }
.create-attach-sidebar {
  flex: 0 0 260px;
  align-self: stretch;
  margin: -.5rem -.5rem -.5rem 0;
  border-left: 1px solid var(--border);
}
@media (max-width: 900px) {
  .create-layout { flex-direction: column; }
  .create-attach-sidebar {
    flex: 0 0 auto;
    margin: 0;
    border-left: none;
    border-top: 1px solid var(--border);
  }
}
.field { display: flex; flex-direction: column; gap: .3rem; }
.field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: .75rem; }
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
.form-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }

.type-locked { display: flex; align-items: center; gap: .5rem; padding: .35rem .5rem; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--radius); }
.type-locked-hint { font-size: 11px; color: var(--text-muted); font-style: italic; }
.type-label-text { font-size: 12px; }

.v2-select {
  border: 1px solid var(--border); border-radius: var(--radius);
  padding: .35rem .55rem; font-size: 13px; font-family: inherit;
  background: var(--bg); color: var(--text);
  outline: none; width: 100%;
}
.v2-select:focus { border-color: var(--bp-blue); }

/* Time unit toggle affordance */
.time-unit-toggle { cursor: pointer; }
.time-unit-toggle:hover .unit-toggle { filter: brightness(.9); }
.unit-toggle { color: var(--bp-blue); text-decoration: underline; text-decoration-style: dotted; }

/* Epic color picker */
.epic-color-picker { display: flex; flex-wrap: wrap; gap: .4rem; }
.epic-color-swatch {
  width: 28px; height: 28px; border-radius: 50%;
  border: 2px solid transparent;
  display: inline-flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 700;
  cursor: pointer; padding: 0;
  transition: transform .12s, border-color .12s;
}
.epic-color-swatch:hover { transform: scale(1.15); }
.epic-color-swatch--active { border-color: var(--text); transform: scale(1.15); }

/* Inline sprint picker dropdown */
.sprint-picker-inline { display: flex; flex-wrap: wrap; align-items: center; gap: .4rem; }
.sprint-add-wrap { position: relative; }
.sprint-add-btn {
  display: inline-flex; align-items: center; gap: .3rem;
  background: none; border: 1px dashed var(--border); border-radius: 20px;
  padding: .15rem .65rem; font-size: 12px; color: var(--text-muted);
  cursor: pointer; font-family: inherit;
  transition: border-color .1s, color .1s;
}
.sprint-add-btn:hover { border-color: var(--bp-blue); color: var(--bp-blue); }
.sprint-add-dropdown {
  position: absolute; top: calc(100% + 4px); left: 0; z-index: 300;
  background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0,0,0,.12);
  min-width: 220px; max-height: 240px; overflow-y: auto;
  display: flex; flex-direction: column;
}
.sprint-add-opt {
  display: flex; align-items: center; justify-content: space-between;
  background: none; border: none; text-align: left;
  padding: .45rem .65rem; font-size: 12px; font-family: inherit;
  color: var(--text); cursor: pointer;
}
.sprint-add-opt:hover { background: var(--surface-2, rgba(0,0,0,.04)); }
.sprint-add-state {
  font-size: 9px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .04em; border-radius: 3px; padding: 0 .25rem;
  background: #f3f4f6; color: #6b7280;
}
.sprint-add-empty {
  padding: .5rem .65rem; font-size: 12px; color: var(--text-muted);
}
</style>
