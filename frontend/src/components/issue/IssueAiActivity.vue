<script setup lang="ts">
import LoadingText from '@/components/LoadingText.vue'
import { computed, onMounted, ref, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { fmtShortDateTime } from '@/utils/formatTime'
import { loadIssueActivity, type MutationActivityResponse } from '@/services/undoActivity'
import {
  loadIssueAIActivity,
  type IssueAIActivityResponse,
  type IssueAIActivityRow,
} from '@/services/aiPaperTrail'
import { useUndoStore } from '@/stores/undo'

const props = withDefaults(defineProps<{ issueId: number; startOpen?: boolean }>(), {
  startOpen: false,
})

const loading = ref(false)
const error = ref('')
const actingLogId = ref<number | null>(null)
const payload = ref<MutationActivityResponse | null>(null)
const aiPayload = ref<IssueAIActivityResponse | null>(null)
const aiKindFilter = ref('')
const aiProviderFilter = ref('')
const aiStatusFilter = ref('')
const aiAgentFilter = ref('')
const undoStore = useUndoStore()

function actorLine(row: { actor_label: string; origin_label?: string }) {
  return row.origin_label ? `${row.actor_label} - ${row.origin_label}` : row.actor_label
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [activity, aiActivity] = await Promise.all([
      loadIssueActivity(props.issueId),
      loadIssueAIActivity(props.issueId),
    ])
    payload.value = activity
    aiPayload.value = aiActivity
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to load issue activity.'
  } finally {
    loading.value = false
  }
}

const aiRows = computed(() => aiPayload.value?.rows ?? [])

function uniqueSorted(values: Array<string | undefined>) {
  return [...new Set(values.map((v) => (v ?? '').trim()).filter(Boolean))].sort((a, b) =>
    a.localeCompare(b),
  )
}

const aiKindOptions = computed(() => uniqueSorted(aiRows.value.map((row) => row.kind)))
const aiProviderOptions = computed(() =>
  uniqueSorted(aiRows.value.map((row) => row.provider_label || row.provider_id || row.model)),
)
const aiStatusOptions = computed(() =>
  uniqueSorted(aiRows.value.map((row) => row.status || row.outcome)),
)
const aiAgentOptions = computed(() => uniqueSorted(aiRows.value.map((row) => row.agent_name)))

const filteredAIRows = computed(() =>
  aiRows.value.filter((row) => {
    if (aiKindFilter.value && row.kind !== aiKindFilter.value) return false
    const provider = row.provider_label || row.provider_id || row.model
    if (aiProviderFilter.value && provider !== aiProviderFilter.value) return false
    if (aiStatusFilter.value && (row.status || row.outcome) !== aiStatusFilter.value) return false
    if (aiAgentFilter.value && row.agent_name !== aiAgentFilter.value) return false
    return true
  }),
)

function aiRowTitle(row: IssueAIActivityRow) {
  if (row.kind === 'agent_run') {
    return row.provider_label || row.action_key
  }
  return row.sub_action ? `${row.action_key} / ${row.sub_action}` : row.action_key
}

function aiRowOutcome(row: IssueAIActivityRow) {
  return row.status || row.outcome
}

function aiRowMeta(row: IssueAIActivityRow): string[] {
  const parts = [
    row.kind === 'agent_run' ? 'run' : 'action',
    row.provider_label || row.provider_id || row.model,
    row.profile_id && row.effort ? `${row.profile_id}/${row.effort}` : row.profile_id || row.effort,
    row.prompt_preset_ref,
    row.context_pack,
    row.agent_name ? `agent ${row.agent_name}` : '',
    row.device_id ? `runner ${row.device_id}` : '',
    row.source_draft_run_id ? `from draft #${row.source_draft_run_id}` : '',
    row.followup_run_id ? `follow-up #${row.followup_run_id}` : '',
    row.prompt_tokens || row.completion_tokens
      ? `${row.prompt_tokens + row.completion_tokens} tokens`
      : '',
    fmtShortDateTime(row.finished_at || row.created_at),
  ]
  return parts.filter((part): part is string => !!part)
}

async function run(mode: 'undo' | 'redo', logId: number) {
  actingLogId.value = logId
  error.value = ''
  try {
    const row = [...(payload.value?.undo_rows ?? []), ...(payload.value?.redo_rows ?? [])].find(
      (entry) => entry.id === logId,
    )
    if (!row) return
    if (mode === 'undo') await undoStore.undoRow(row)
    else await undoStore.redoRow(row)
    await load()
  } catch (e: any) {
    error.value = e?.message ?? `${mode} failed.`
  } finally {
    actingLogId.value = null
  }
}

watch(
  () => props.issueId,
  () => {
    void load()
  },
  { immediate: true },
)
onMounted(() => {
  if (!payload.value) void load()
})
</script>

<template>
  <details class="issue-ai" :open="startOpen">
    <summary class="issue-ai__summary">
      <span class="issue-ai__title">
        <AppIcon name="rewind" :size="14" />
        Activity
      </span>
      <span class="issue-ai__badges">
        <span class="issue-ai__badge">{{
          filteredAIRows.length +
          (payload?.undo_rows.length ?? 0) +
          (payload?.redo_rows.length ?? 0) +
          (payload?.history_rows.length ?? 0)
        }}</span>
        <span class="issue-ai__hint">AI + undo + history</span>
      </span>
    </summary>

    <div class="issue-ai__body">
      <LoadingText v-if="loading" class="issue-ai__empty" label="Loading activity…" />
      <div v-else-if="error" class="issue-ai__empty">{{ error }}</div>
      <div
        v-else-if="
          !(
            payload?.undo_rows.length ||
            payload?.redo_rows.length ||
            payload?.history_rows.length ||
            filteredAIRows.length
          )
        "
        class="issue-ai__empty"
      >
        No tracked activity for this issue yet.
      </div>
      <div v-else class="issue-ai__list">
        <div v-if="aiRows.length" class="issue-ai__filters" aria-label="AI activity filters">
          <select v-model="aiKindFilter" class="issue-ai__select" aria-label="AI activity kind">
            <option value="">All AI</option>
            <option v-for="kind in aiKindOptions" :key="kind" :value="kind">
              {{ kind === 'agent_run' ? 'Runs' : 'Actions' }}
            </option>
          </select>
          <select v-model="aiProviderFilter" class="issue-ai__select" aria-label="AI provider">
            <option value="">Any provider</option>
            <option v-for="provider in aiProviderOptions" :key="provider" :value="provider">
              {{ provider }}
            </option>
          </select>
          <select v-model="aiStatusFilter" class="issue-ai__select" aria-label="AI status">
            <option value="">Any status</option>
            <option v-for="status in aiStatusOptions" :key="status" :value="status">
              {{ status }}
            </option>
          </select>
          <select
            v-if="aiAgentOptions.length"
            v-model="aiAgentFilter"
            class="issue-ai__select"
            aria-label="AI agent"
          >
            <option value="">Any agent</option>
            <option v-for="agent in aiAgentOptions" :key="agent" :value="agent">
              {{ agent }}
            </option>
          </select>
        </div>

        <div
          v-for="row in filteredAIRows"
          :key="`${row.kind}-${row.run_id ?? row.log_id}-${row.request_id}`"
          class="issue-ai__item issue-ai__item--ai"
        >
          <div class="issue-ai__head">
            <div class="issue-ai__head-main">
              <strong>{{ aiRowTitle(row) }}</strong>
              <span class="issue-ai__outcome">{{ aiRowOutcome(row) }}</span>
            </div>
          </div>
          <div class="issue-ai__meta">
            <span
              v-for="part in aiRowMeta(row)"
              :key="part"
              :class="{ 'issue-ai__mono': part.includes('_') || part.includes('/') }"
            >
              {{ part }}
            </span>
          </div>
          <div v-if="row.tests_summary || row.error" class="issue-ai__detail">
            {{ row.error || row.tests_summary }}
          </div>
        </div>

        <div
          v-for="row in payload?.undo_rows"
          :key="`undo-${row.id}`"
          class="issue-ai__item issue-ai__item--undo"
        >
          <div class="issue-ai__head">
            <div class="issue-ai__head-main">
              <strong>{{ row.summary }}</strong>
              <span class="issue-ai__outcome">undo</span>
            </div>
            <button
              type="button"
              class="issue-ai__undo"
              :disabled="actingLogId === row.id"
              @click="run('undo', row.id)"
            >
              {{ actingLogId === row.id ? 'Undoing…' : 'Undo' }}
            </button>
          </div>
          <div class="issue-ai__meta">
            <span>{{ actorLine(row) }}</span>
            <span class="issue-ai__mono">{{ row.mutation_type }}</span>
            <span>{{ fmtShortDateTime(row.created_at) }}</span>
          </div>
          <div v-if="row.change_detail" class="issue-ai__detail">{{ row.change_detail }}</div>
        </div>

        <div
          v-for="row in payload?.redo_rows"
          :key="`redo-${row.id}`"
          class="issue-ai__item issue-ai__item--redo"
        >
          <div class="issue-ai__head">
            <div class="issue-ai__head-main">
              <strong>{{ row.summary }}</strong>
              <span class="issue-ai__outcome">redo</span>
            </div>
            <button
              type="button"
              class="issue-ai__undo"
              :disabled="actingLogId === row.id"
              @click="run('redo', row.id)"
            >
              {{ actingLogId === row.id ? 'Redoing…' : 'Redo' }}
            </button>
          </div>
          <div class="issue-ai__meta">
            <span>{{ actorLine(row) }}</span>
            <span class="issue-ai__mono">{{ row.mutation_type }}</span>
            <span>{{ fmtShortDateTime(row.created_at) }}</span>
          </div>
          <div v-if="row.change_detail" class="issue-ai__detail">{{ row.change_detail }}</div>
        </div>

        <div
          v-for="row in payload?.history_rows"
          :key="`history-${row.id}`"
          class="issue-ai__item issue-ai__item--history"
        >
          <div class="issue-ai__head">
            <div class="issue-ai__head-main">
              <strong>{{ row.summary }}</strong>
              <span class="issue-ai__outcome">{{ row.undone ? 'undone' : 'history' }}</span>
            </div>
          </div>
          <div class="issue-ai__meta">
            <span>{{ actorLine(row) }}</span>
            <span class="issue-ai__mono">{{ row.mutation_type }}</span>
            <span>{{ fmtShortDateTime(row.created_at) }}</span>
          </div>
          <div v-if="row.change_detail" class="issue-ai__detail">{{ row.change_detail }}</div>
        </div>
      </div>
    </div>
  </details>
</template>

<style scoped>
.issue-ai {
  margin-top: 1rem;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--bg-card);
}
.issue-ai[open] {
  box-shadow: 0 8px 20px rgba(30, 50, 80, 0.06);
}
.issue-ai__summary {
  list-style: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.8rem 0.95rem;
  cursor: pointer;
}
.issue-ai__summary::-webkit-details-marker {
  display: none;
}
.issue-ai__title,
.issue-ai__badges,
.issue-ai__meta,
.issue-ai__head,
.issue-ai__head-main {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  flex-wrap: wrap;
}
.issue-ai__head {
  justify-content: space-between;
}
.issue-ai__title {
  font-weight: 600;
}
.issue-ai__badge,
.issue-ai__mono,
.issue-ai__hint,
.issue-ai__outcome {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 11px;
}
.issue-ai__badge {
  padding: 0.12rem 0.38rem;
  border-radius: 999px;
  background: rgba(46, 109, 164, 0.1);
  color: var(--bp-blue-dark);
}
.issue-ai__hint,
.issue-ai__meta,
.issue-ai__outcome {
  color: var(--text-muted);
}
.issue-ai__detail {
  margin-top: 0.35rem;
  font-size: 12px;
  line-height: 1.35;
  color: var(--text);
  overflow-wrap: anywhere;
}
.issue-ai__body {
  padding: 0 0.95rem 0.95rem;
}
.issue-ai__list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.issue-ai__filters {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  flex-wrap: wrap;
}
.issue-ai__select {
  font: inherit;
  font-size: 12px;
  color: var(--text);
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.25rem 0.4rem;
  max-width: 11rem;
}
.issue-ai__item {
  padding: 0.65rem 0.75rem;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--bg);
}
.issue-ai__item--undo {
  border-color: rgba(46, 109, 164, 0.18);
}
.issue-ai__item--redo {
  border-color: rgba(22, 163, 74, 0.18);
}
.issue-ai__item--ai {
  border-color: rgba(46, 109, 164, 0.2);
  background: color-mix(in srgb, var(--bp-blue, #2563eb) 4%, var(--bg));
}
.issue-ai__undo {
  border: 1px solid var(--border);
  background: var(--bg-card);
  border-radius: 999px;
  padding: 0.2rem 0.55rem;
  font-size: 11px;
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  cursor: pointer;
}
.issue-ai__undo:disabled {
  opacity: 0.65;
  cursor: wait;
}
.issue-ai__empty {
  font-size: 13px;
  color: var(--text-muted);
}
</style>
