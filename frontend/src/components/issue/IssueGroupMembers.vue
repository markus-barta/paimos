<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, computed, watch } from 'vue'
import { RouterLink } from 'vue-router'
import StatusDot from '@/components/StatusDot.vue'
import { STATUS_LABEL } from '@/composables/useIssueDisplay'
import { loadIssueGroupMembers } from '@/services/issueGroupMembers'
import type { Issue } from '@/types'

const props = defineProps<{
  issueId: number
  issueType: string
  projectId: number | null
}>()

const isGroup  = computed(() => props.issueType === 'epic' || props.issueType === 'cost_unit' || props.issueType === 'release')
const isSprint = computed(() => props.issueType === 'sprint')
const show     = computed(() => isGroup.value || isSprint.value)

const groupMembers    = ref<Issue[]>([])
const groupMemLoading = ref(false)

async function load() {
  if (!props.issueId) return
  if (!isGroup.value && !isSprint.value) return
  groupMemLoading.value = true
  const relType = isSprint.value ? 'sprint' : 'groups'
  try {
    groupMembers.value = await loadIssueGroupMembers(props.issueId, relType)
  } catch { groupMembers.value = [] }
  finally { groupMemLoading.value = false }
}

defineExpose({ load })

watch(() => props.issueId, () => load())

function issueRoute(issueId: number): string {
  return props.projectId ? `/projects/${props.projectId}/issues/${issueId}` : `/issues/${issueId}`
}
</script>

<template>
  <div class="group-panel" v-if="show">
    <h3 class="section-title">{{ isSprint ? 'Sprint Tickets' : 'Linked Tickets' }}</h3>

    <div class="group-members">
      <LoadingText v-if="groupMemLoading" class="rel-empty" label="Loading…" />
      <div v-else-if="!groupMembers.length" class="rel-empty">No tickets linked yet.</div>
      <table v-else class="gm-table">
        <thead>
          <tr>
            <th>Key</th>
            <th>Title</th>
            <th>Status</th>
            <th>Assignee</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in groupMembers" :key="m.id" class="gm-row">
            <td class="gm-key">
              <RouterLink :to="issueRoute(m.id)" class="gm-link">
                {{ m.issue_key }}
              </RouterLink>
            </td>
            <td class="gm-title">
              <RouterLink :to="issueRoute(m.id)" class="gm-link">
                {{ m.title }}
              </RouterLink>
            </td>
            <td>
              <span class="issue-status">
                <StatusDot :status="m.status" />
                {{ STATUS_LABEL[m.status] }}
              </span>
            </td>
            <td class="gm-assignee">{{ m.assignee?.username ?? '—' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.group-panel {
  margin-top: 1.75rem;
  padding-top: 1.5rem;
  border-top: 1px solid var(--border);
}
.section-title {
  font-size: 13px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--text-muted);
  margin-bottom: 1rem;
}
.rel-empty { font-size: 13px; color: var(--text-muted); padding: .5rem 0; }
.gm-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.gm-table th {
  text-align: left; font-size: 11px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .05em;
  padding: .4rem .75rem; border-bottom: 1px solid var(--border);
}
.gm-row { border-bottom: 1px solid var(--border-subtle, var(--border)); }
.gm-row:hover { background: var(--surface-2); }
.gm-row td { padding: .5rem .75rem; vertical-align: middle; }
.gm-key { font-family: monospace; font-size: 12px; white-space: nowrap; }
.gm-title { max-width: 340px; }
.gm-assignee { color: var(--text-muted); white-space: nowrap; }
.gm-link { color: var(--text); text-decoration: none; }
.gm-link:hover { color: var(--bp-blue); text-decoration: underline; }
</style>
