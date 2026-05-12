<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import LoadingText from '@/components/LoadingText.vue'
import type { User } from '@/types'

interface SuperAdminAuditRow {
  id: number
  actor_user_id: number | null
  actor_username: string | null
  target_user_id: number | null
  target_username: string | null
  capability: string
  endpoint: string
  request_id: string
  details: Record<string, unknown>
  created_at: string
}

interface AuditResponse {
  items: SuperAdminAuditRow[]
}

const rows = ref<SuperAdminAuditRow[]>([])
const users = ref<User[]>([])
const loading = ref(true)
const error = ref('')
const selectedCapability = ref('')
const selectedActor = ref<number | null>(null)
const selectedTarget = ref<number | null>(null)

const capabilityOptions = [
  { value: '', label: 'All capabilities' },
  { value: 'time_entries.write_any_user', label: 'Time entries: any user' },
  { value: 'users.grant_super_admin', label: 'Super-admin grants' },
  { value: 'auth.impersonation.start', label: 'Impersonation starts' },
  { value: 'auth.impersonation.end', label: 'Impersonation ends' },
  { value: 'auth.impersonation.action', label: 'Impersonated actions' },
  { value: 'security.super_admin_audit.read', label: 'Audit feed reads' },
]

const activeUsers = computed(() =>
  users.value
    .filter(u => u.status !== 'deleted')
    .slice()
    .sort((a, b) => a.username.localeCompare(b.username)),
)

function rowAction(row: SuperAdminAuditRow): string {
  const action = row.details?.action
  if (typeof action === 'string' && action.trim()) return action.replace(/_/g, ' ')
  return row.capability
}

function formatDate(value: string): string {
  if (!value) return ''
  try {
    return new Date(value).toLocaleString('en-GB', {
      year: 'numeric',
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
  } catch {
    return value
  }
}

function detailsSummary(row: SuperAdminAuditRow): string {
  const details = row.details ?? {}
  const parts: string[] = []
  for (const key of ['time_entry_id', 'issue_id', 'old_role', 'new_role', 'target_role', 'status_code']) {
    const value = details[key]
    if (value !== undefined && value !== null && value !== '') parts.push(`${key}: ${value}`)
  }
  return parts.join(' · ')
}

async function load() {
  loading.value = true
  error.value = ''
  const params = new URLSearchParams({ days: '30', limit: '100' })
  if (selectedCapability.value) params.set('capability', selectedCapability.value)
  if (selectedActor.value) params.set('actor_id', String(selectedActor.value))
  if (selectedTarget.value) params.set('target_user_id', String(selectedTarget.value))
  try {
    const [audit, userRows] = await Promise.all([
      api.get<AuditResponse>(`/super-admin-activity?${params.toString()}`),
      users.value.length ? Promise.resolve(users.value) : api.get<User[]>('/users'),
    ])
    rows.value = audit.items ?? []
    users.value = userRows
  } catch (e: unknown) {
    error.value = errMsg(e, 'Failed to load security activity.')
  } finally {
    loading.value = false
  }
}

function clearFilters() {
  selectedCapability.value = ''
  selectedActor.value = null
  selectedTarget.value = null
  void load()
}

onMounted(load)
</script>

<template>
  <div class="section">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">Security</h2>
        <p class="section-desc">Privileged role and cross-user activity from the last 30 days.</p>
      </div>
      <button class="btn btn-ghost btn-sm" @click="load" :disabled="loading" title="Refresh">
        <AppIcon name="refresh-cw" :size="14" />
        Refresh
      </button>
    </div>

    <div class="security-toolbar">
      <label>
        <span>Capability</span>
        <select v-model="selectedCapability" @change="load">
          <option v-for="option in capabilityOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
        </select>
      </label>
      <label>
        <span>Actor</span>
        <select v-model="selectedActor" @change="load">
          <option :value="null">All actors</option>
          <option v-for="u in activeUsers" :key="u.id" :value="u.id">{{ u.username }}</option>
        </select>
      </label>
      <label>
        <span>Target</span>
        <select v-model="selectedTarget" @change="load">
          <option :value="null">All targets</option>
          <option v-for="u in activeUsers" :key="u.id" :value="u.id">{{ u.username }}</option>
        </select>
      </label>
      <button class="btn btn-ghost btn-sm security-clear" @click="clearFilters">Clear</button>
    </div>

    <div v-if="error" class="form-error">{{ error }}</div>

    <div class="card security-table-card">
      <LoadingText v-if="loading" class="security-loading" label="Loading…" />
      <table v-else-if="rows.length" class="settings-table security-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Actor</th>
            <th>Action</th>
            <th>Target</th>
            <th>Endpoint</th>
            <th>Details</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id">
            <td class="security-time" :title="row.created_at">{{ formatDate(row.created_at) }}</td>
            <td>{{ row.actor_username || 'System' }}</td>
            <td>
              <span class="capability-pill">{{ rowAction(row) }}</span>
            </td>
            <td>{{ row.target_username || '—' }}</td>
            <td class="security-endpoint" :title="row.request_id">{{ row.endpoint || '—' }}</td>
            <td class="security-details">{{ detailsSummary(row) || '—' }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty-state">
        <AppIcon name="shield-check" :size="18" />
        No privileged activity found.
      </div>
    </div>
  </div>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.security-toolbar {
  display: flex;
  align-items: flex-end;
  gap: .75rem;
  flex-wrap: wrap;
  margin-bottom: .9rem;
}
.security-toolbar label {
  display: flex;
  flex-direction: column;
  gap: .3rem;
  min-width: 170px;
}
.security-toolbar span {
  font-size: 11px;
  font-weight: 700;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: .06em;
}
.security-toolbar select {
  height: 34px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--bg-card);
  color: var(--text);
  padding: 0 .6rem;
  font: inherit;
  font-size: 13px;
}
.security-clear { height: 34px; }
.security-table-card {
  padding: 0;
  overflow: hidden;
}
.security-loading {
  padding: 1rem;
  color: var(--text-muted);
  font-size: 13px;
}
.security-table td {
  white-space: nowrap;
}
.security-time {
  color: var(--text-muted);
  font-size: 12px;
}
.capability-pill {
  display: inline-flex;
  align-items: center;
  max-width: 220px;
  padding: .15rem .45rem;
  border-radius: 4px;
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
  font-size: 11px;
  font-weight: 700;
  text-transform: capitalize;
}
.security-endpoint {
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--text-muted);
  font-family: 'DM Mono', 'Fira Code', monospace;
  font-size: 12px;
}
.security-details {
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--text-muted);
  font-size: 12px;
}
.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: .45rem;
  min-height: 130px;
  color: var(--text-muted);
  font-size: 13px;
}
</style>
