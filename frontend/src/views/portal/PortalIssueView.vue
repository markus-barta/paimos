<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/api/client'
import StatusDot from '@/components/StatusDot.vue'
import { useConfirm } from '@/composables/useConfirm'

interface PortalIssue {
  id: number; issue_key: string; title: string; description: string
  acceptance_criteria: string; status: string; priority: string; type: string
  cost_unit: string; release: string
  estimate_hours: number | null; estimate_lp: number | null
  ar_hours: number | null; ar_lp: number | null
  estimate_eur: number | null; ar_eur: number | null
  accepted_at: string | null; created_at: string; updated_at: string
}

const route = useRoute()
const router = useRouter()
const projectId = Number(route.params.id)
const issueId = Number(route.params.issueId)

const issue = ref<PortalIssue | null>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    issue.value = await api.get<PortalIssue>(`/portal/projects/${projectId}/issues/${issueId}`)
  } catch { /* ignore */ }
  loading.value = false
})

async function acceptIssue() {
  if (!issue.value) return
  try {
    await api.post(`/portal/issues/${issue.value.id}/accept`, {})
    issue.value.status = 'accepted'
  } catch { /* ignore */ }
}

const showRejectForm = ref(false)
const rejectTitle = ref('')
const rejectDesc = ref('')
const rejectLoading = ref(false)

async function rejectIssue() {
  if (!issue.value || !rejectTitle.value.trim()) return
  rejectLoading.value = true
  try {
    await api.post(`/portal/issues/${issue.value.id}/reject`, {
      title: rejectTitle.value.trim(),
      description: rejectDesc.value.trim(),
    })
    issue.value.status = 'in-progress'
    showRejectForm.value = false
    rejectTitle.value = ''
    rejectDesc.value = ''
  } catch { /* ignore */ }
  rejectLoading.value = false
}

function fmtEur(v: number | null | undefined): string {
  if (v == null) return '-'
  return new Intl.NumberFormat('de-DE', { style: 'currency', currency: 'EUR' }).format(v)
}

function fmtNum(v: number | null | undefined): string {
  return v != null ? String(v) : '-'
}

function fmtDate(v: string): string {
  if (!v) return '-'
  return new Date(v).toLocaleDateString('de-DE', { day: '2-digit', month: '2-digit', year: 'numeric' })
}

const STATUS_COLORS: Record<string, string> = {
  backlog: '#6b7280', 'in-progress': '#2563eb', done: '#16a34a', cancelled: '#9ca3af',
}

const isAccepted = computed(() => issue.value?.status === 'accepted' || issue.value?.status === 'invoiced')
const canAccept = computed(() => issue.value?.status === 'done' || issue.value?.status === 'delivered')
const today = new Date().toISOString().slice(0, 10)
const canUndoAccept = computed(() =>
  issue.value?.status === 'accepted' && issue.value?.accepted_at?.startsWith(today)
)

const { confirm } = useConfirm()

async function undoAccept() {
  if (!issue.value || !await confirm({ message: 'Undo acceptance? The issue will return to "done".', confirmLabel: 'Undo' })) return
  try {
    await api.post(`/portal/issues/${issue.value.id}/undo-accept`, {})
    issue.value.status = 'done'
    issue.value.accepted_at = null
  } catch { /* ignore */ }
}
</script>

<template>
  <div class="portal-issue" v-if="!loading && issue">
    <router-link :to="`/portal/projects/${projectId}`" class="back-link">
      &larr; Back to Issues
    </router-link>

    <div class="issue-header">
      <div class="header-top">
        <span class="key-badge">{{ issue.issue_key }}</span>
        <span class="type-badge">{{ issue.type }}</span>
        <span class="status-chip">
          <StatusDot :status="issue.status" />
          {{ issue.status }}
        </span>
        <span class="priority-chip">{{ issue.priority }}</span>
      </div>
      <h1 class="issue-title">{{ issue.title }}</h1>
    </div>

    <!-- Accept bar -->
    <div v-if="isAccepted" class="accepted-bar">
      <span>{{ issue.status === 'invoiced' ? 'Invoiced' : 'Accepted' }}</span>
      <button v-if="canUndoAccept" class="btn btn-ghost btn-sm" @click="undoAccept">Undo</button>
    </div>
    <div v-else-if="canAccept" class="accept-bar">
      <span>This issue is done and ready for review.</span>
      <div class="accept-actions">
        <button class="btn btn-primary" @click="acceptIssue">Accept</button>
        <button class="btn btn-ghost" style="border:1px solid #c0392b; color:#c0392b" @click="showRejectForm = !showRejectForm">Reject</button>
      </div>
    </div>
    <div v-if="showRejectForm" class="reject-form">
      <input v-model="rejectTitle" type="text" placeholder="Short description of the problem (required)" />
      <textarea v-model="rejectDesc" rows="3" placeholder="Detailed explanation (optional)"></textarea>
      <div class="reject-actions">
        <button class="btn btn-ghost btn-sm" @click="showRejectForm = false; rejectTitle = ''; rejectDesc = ''">Cancel</button>
        <button class="btn btn-sm" style="background:#c0392b; color:#fff; border-color:#a93226" @click="rejectIssue" :disabled="rejectLoading || !rejectTitle.trim()">
          {{ rejectLoading ? 'Submitting…' : 'Report Problem' }}
        </button>
      </div>
    </div>

    <!-- Fields -->
    <div class="fields-grid">
      <div class="field-group" v-if="issue.cost_unit">
        <div class="fg-label">Cost Unit</div>
        <div class="fg-value">{{ issue.cost_unit }}</div>
      </div>
      <div class="field-group" v-if="issue.release">
        <div class="fg-label">Release</div>
        <div class="fg-value">{{ issue.release }}</div>
      </div>
      <div class="field-group">
        <div class="fg-label">Estimate (h / LP / EUR)</div>
        <div class="fg-value">{{ fmtNum(issue.estimate_hours) }} h / {{ fmtNum(issue.estimate_lp) }} LP / {{ fmtEur(issue.estimate_eur) }}</div>
      </div>
      <div class="field-group">
        <div class="fg-label">AR (h / LP / EUR)</div>
        <div class="fg-value">{{ fmtNum(issue.ar_hours) }} h / {{ fmtNum(issue.ar_lp) }} LP / {{ fmtEur(issue.ar_eur) }}</div>
      </div>
      <div class="field-group">
        <div class="fg-label">Created</div>
        <div class="fg-value">{{ fmtDate(issue.created_at) }}</div>
      </div>
      <div class="field-group">
        <div class="fg-label">Updated</div>
        <div class="fg-value">{{ fmtDate(issue.updated_at) }}</div>
      </div>
    </div>

    <!-- Description -->
    <section v-if="issue.description" class="content-section">
      <h2 class="section-title">Description</h2>
      <div class="section-body prose">{{ issue.description }}</div>
    </section>

    <!-- Acceptance Criteria -->
    <section v-if="issue.acceptance_criteria" class="content-section">
      <h2 class="section-title">Acceptance Criteria</h2>
      <div class="section-body prose">{{ issue.acceptance_criteria }}</div>
    </section>
  </div>
  <div v-else-if="loading" class="loading">Loading...</div>
  <div v-else class="loading">Issue not found.</div>
</template>

<style scoped>
.back-link {
  font-size: 13px;
  color: var(--text-muted);
  display: inline-block;
  margin-bottom: .75rem;
}
.back-link:hover { color: var(--bp-blue); }

.issue-header { margin-bottom: 1rem; }
.header-top {
  display: flex;
  align-items: center;
  gap: .5rem;
  margin-bottom: .5rem;
  flex-wrap: wrap;
}
.key-badge {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: .03em;
  padding: .15rem .5rem;
  border-radius: 4px;
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
}
.type-badge {
  font-size: 11px;
  font-weight: 600;
  text-transform: capitalize;
  color: var(--text-muted);
}
.status-chip {
  display: inline-flex;
  align-items: center;
  gap: .3rem;
  font-size: 12px;
  font-weight: 500;
}
.status-chip .status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--dot);
}
.priority-chip {
  font-size: 11px;
  font-weight: 600;
  text-transform: capitalize;
  color: var(--text-muted);
}
.issue-title {
  font-size: 20px;
  font-weight: 700;
}

.accepted-bar {
  background: #f0fdf4;
  border: 1px solid #bbf7d0;
  border-radius: var(--radius);
  padding: .75rem 1rem;
  font-size: 13px;
  font-weight: 600;
  color: #16a34a;
  margin-bottom: 1rem;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.accept-bar {
  background: #fffbeb;
  border: 1px solid #fde68a;
  border-radius: var(--radius);
  padding: .75rem 1rem;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
  font-size: 13px;
}
.accept-actions { display: flex; gap: .5rem; }
.reject-form {
  background: #fef2f2;
  border: 1px solid #fecaca;
  border-radius: var(--radius);
  padding: .75rem 1rem;
  margin-bottom: 1rem;
  display: flex;
  flex-direction: column;
  gap: .5rem;
}
.reject-form textarea { font-size: 13px; resize: vertical; }
.reject-actions { display: flex; gap: .5rem; justify-content: flex-end; }

.fields-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: .75rem;
  margin-bottom: 1.5rem;
}
.field-group {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: .75rem 1rem;
}
.fg-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: .04em;
  color: var(--text-muted);
  margin-bottom: .2rem;
}
.fg-value {
  font-size: 14px;
  font-weight: 500;
}

.content-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1rem 1.25rem;
  margin-bottom: 1rem;
}
.section-title {
  font-size: 13px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .04em;
  color: var(--text-muted);
  margin-bottom: .5rem;
}
.section-body {
  font-size: 14px;
  line-height: 1.6;
  white-space: pre-wrap;
}

.loading { color: var(--text-muted); padding: 3rem; text-align: center; }
</style>
