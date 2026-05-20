<!--
  PAI-467 — admin Customer Portal Visibility report.

  Three sections:
    1. Header — "Visible to customer right now: N issues"
    2. Issue table — pre-filtered to CUSTOMERPORTAL, sortable, the
       cleanest answer to "what does the customer see right now?"
    3. Audit log — paginated chronological feed of every attach /
       detach / migration event from mutation_log

  CSV export buttons hit the dedicated CSV endpoints (admin-gated by
  the backend), opened via a normal anchor so the browser handles the
  download.
-->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'

import { useAuthStore } from '@/stores/auth'
import {
  loadAdminPortalVisibility,
  adminPortalVisibilityCsvUrl,
  type AdminVisibilityReport,
} from '@/services/issueDetail'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const authStore = useAuthStore()

const projectId = computed(() => Number(route.params.id))
const report = ref<AdminVisibilityReport | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)

const auditOffset = ref(0)
const auditLimit = 50

async function refresh() {
  loading.value = true
  error.value = null
  try {
    report.value = await loadAdminPortalVisibility(projectId.value, {
      auditOffset: auditOffset.value,
      auditLimit,
    })
  } catch (e: any) {
    error.value = e?.message ?? 'load failed'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  // Admin-only route; non-admins shouldn't have hit a link here, but
  // bounce them back rather than rendering nothing if they did.
  if (!authStore.isAdmin) {
    router.replace('/')
    return
  }
  void refresh()
})

function prevPage() {
  if (auditOffset.value <= 0) return
  auditOffset.value = Math.max(0, auditOffset.value - auditLimit)
  void refresh()
}

function nextPage() {
  if (!report.value) return
  if (auditOffset.value + auditLimit >= report.value.total_audit) return
  auditOffset.value += auditLimit
  void refresh()
}

function eventLabel(type: string): string {
  // Map the backend's canonical event labels to translated phrases.
  switch (type) {
    case 'auto_tag':
      return t('visibility.auditAuto', { when: '' }).replace(' · ', '').trim()
    case 'migration_backfill':
      return t('visibility.auditMigration', { when: '' })
        .replace(' · ', '')
        .trim()
    case 'toggle_add':
      return t('visibility.bulkMakeVisible')
    case 'toggle_remove':
      return t('visibility.bulkHide')
    default:
      return type
  }
}
</script>

<template>
  <div class="admin-visibility">
    <div class="crumb">
      <RouterLink :to="`/projects/${projectId}`">← {{ $t('portal.allProjects') }}</RouterLink>
    </div>

    <h1 class="admin-visibility__title">
      {{ t('visibility.filterTitle') }} — {{ t('visibility.filterTitle') }}
    </h1>

    <div v-if="loading" class="admin-visibility__loading">{{ $t('portal.loading') }}</div>
    <div v-else-if="error" class="admin-visibility__error">{{ error }}</div>
    <template v-else-if="report">
      <div class="admin-visibility__header">
        <div class="admin-visibility__metric">
          <span class="admin-visibility__metric-value">{{ report.visible_count }}</span>
          <span class="admin-visibility__metric-label">
            {{ t('visibility.label') }}
          </span>
        </div>
        <div class="admin-visibility__exports">
          <a
            class="btn btn-ghost btn-sm"
            :href="adminPortalVisibilityCsvUrl(projectId, 'current')"
          >
            CSV (current)
          </a>
          <a
            class="btn btn-ghost btn-sm"
            :href="adminPortalVisibilityCsvUrl(projectId, 'audit')"
          >
            CSV (audit)
          </a>
        </div>
      </div>

      <section class="admin-visibility__section">
        <h2 class="admin-visibility__section-title">
          Visible issues ({{ report.visible_count }})
        </h2>
        <table v-if="report.issues.length" class="admin-visibility__table">
          <thead>
            <tr>
              <th>Key</th>
              <th>Title</th>
              <th>Status</th>
              <th>Last actor</th>
              <th>Last event</th>
              <th>At</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="iss in report.issues" :key="iss.id">
              <td>
                <RouterLink :to="`/projects/${projectId}/issues/${iss.id}`">
                  {{ iss.issue_key }}
                </RouterLink>
              </td>
              <td>{{ iss.title }}</td>
              <td>{{ iss.status }}</td>
              <td>{{ iss.last_actor ?? '—' }}</td>
              <td>{{ iss.last_event_type ? eventLabel(iss.last_event_type) : '—' }}</td>
              <td>{{ iss.last_at ?? '—' }}</td>
            </tr>
          </tbody>
        </table>
        <p v-else class="admin-visibility__empty">No issues are visible to the customer portal yet.</p>
      </section>

      <section class="admin-visibility__section">
        <h2 class="admin-visibility__section-title">
          Audit feed ({{ report.total_audit }})
        </h2>
        <table v-if="report.audit.length" class="admin-visibility__table">
          <thead>
            <tr>
              <th>At</th>
              <th>Actor</th>
              <th>Event</th>
              <th>Issue</th>
              <th>Title</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(a, idx) in report.audit" :key="idx">
              <td>{{ a.at }}</td>
              <td>{{ a.actor ?? '—' }}</td>
              <td>{{ eventLabel(a.event_type) }}</td>
              <td>
                <RouterLink :to="`/projects/${projectId}/issues/${a.issue_id}`">
                  {{ a.issue_key }}
                </RouterLink>
              </td>
              <td>{{ a.title }}</td>
            </tr>
          </tbody>
        </table>
        <p v-else class="admin-visibility__empty">No audit events yet.</p>

        <div v-if="report.total_audit > auditLimit" class="admin-visibility__paging">
          <button
            class="btn btn-ghost btn-sm"
            :disabled="auditOffset === 0"
            @click="prevPage"
          >
            ← Prev
          </button>
          <span>
            {{ auditOffset + 1 }}–{{ Math.min(auditOffset + auditLimit, report.total_audit) }}
            / {{ report.total_audit }}
          </span>
          <button
            class="btn btn-ghost btn-sm"
            :disabled="auditOffset + auditLimit >= report.total_audit"
            @click="nextPage"
          >
            Next →
          </button>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.admin-visibility {
  padding: 1.5rem;
  max-width: 1100px;
  margin: 0 auto;
}

.crumb {
  margin-bottom: 0.75rem;
  font-size: 0.875rem;
}

.admin-visibility__title {
  font-size: 1.5rem;
  font-weight: 700;
  margin: 0 0 1rem;
}

.admin-visibility__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.5rem;
  padding: 1rem 1.25rem;
  background: var(--bg-subtle, #f9fafb);
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 8px;
}

.admin-visibility__metric-value {
  font-size: 2rem;
  font-weight: 700;
  color: var(--brand, #2563eb);
  margin-right: 0.5rem;
}

.admin-visibility__metric-label {
  color: var(--text-muted, #6b7280);
}

.admin-visibility__exports {
  display: flex;
  gap: 0.5rem;
}

.admin-visibility__section {
  margin-bottom: 2rem;
}

.admin-visibility__section-title {
  font-size: 1.125rem;
  font-weight: 600;
  margin: 0 0 0.75rem;
}

.admin-visibility__table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.875rem;
}

.admin-visibility__table th,
.admin-visibility__table td {
  text-align: left;
  padding: 0.5rem 0.625rem;
  border-bottom: 1px solid var(--border, #e5e7eb);
}

.admin-visibility__table th {
  font-weight: 600;
  background: var(--bg-subtle, #f9fafb);
}

.admin-visibility__empty {
  color: var(--text-muted, #6b7280);
  font-style: italic;
}

.admin-visibility__paging {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-top: 0.75rem;
  font-size: 0.875rem;
  color: var(--text-muted, #6b7280);
}

.admin-visibility__loading,
.admin-visibility__error {
  padding: 2rem 0;
  text-align: center;
  color: var(--text-muted, #6b7280);
}

.admin-visibility__error {
  color: #b91c1c;
}
</style>
