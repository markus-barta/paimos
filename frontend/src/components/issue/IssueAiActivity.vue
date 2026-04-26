<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { fmtShortDateTime } from '@/utils/formatTime'
import { loadIssueAIActivity, type IssueAIActivityResponse, undoMutation } from '@/services/aiPaperTrail'

const props = defineProps<{
  issueId: number
}>()

const loading = ref(false)
const error = ref('')
const undoingLogId = ref<number | null>(null)
const payload = ref<IssueAIActivityResponse | null>(null)
async function load() {
  loading.value = true
  error.value = ''
  try {
    payload.value = await loadIssueAIActivity(props.issueId)
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to load AI activity.'
  } finally {
    loading.value = false
  }
}

async function undoRow(logId: number) {
  undoingLogId.value = logId
  error.value = ''
  try {
    await undoMutation(logId)
    await load()
  } catch (e: any) {
    error.value = e?.message ?? 'Undo failed.'
  } finally {
    undoingLogId.value = null
  }
}

watch(() => props.issueId, () => { void load() }, { immediate: true })
onMounted(() => { if (!payload.value) void load() })
</script>

<template>
  <details class="issue-ai">
    <summary class="issue-ai__summary">
      <span class="issue-ai__title">
        <AppIcon name="sparkles" :size="14" />
        AI activity
      </span>
      <span class="issue-ai__badges">
        <span class="issue-ai__badge">{{ payload?.count ?? 0 }}</span>
        <span class="issue-ai__hint">{{ payload?.last_week_count ?? 0 }} in the last week</span>
      </span>
    </summary>

    <div class="issue-ai__body">
      <div v-if="loading" class="issue-ai__empty">Loading AI activity…</div>
      <div v-else-if="error" class="issue-ai__empty">{{ error }}</div>
      <div v-else-if="!(payload?.rows?.length)" class="issue-ai__empty">No AI activity recorded for this issue yet.</div>
      <div v-else class="issue-ai__list">
        <div v-for="row in payload?.rows" :key="`${row.request_id}-${row.created_at}`" class="issue-ai__item">
          <div class="issue-ai__head">
            <div class="issue-ai__head-main">
              <strong>{{ row.action_key }}</strong>
              <span v-if="row.sub_action" class="issue-ai__mono">{{ row.sub_action }}</span>
              <span class="issue-ai__outcome">{{ row.outcome }}</span>
            </div>
            <button
              v-if="row.on_user_stack"
              type="button"
              class="issue-ai__undo"
              :disabled="undoingLogId === row.log_id"
              @click="undoRow(row.log_id)"
            >{{ undoingLogId === row.log_id ? 'Undoing…' : 'Undo' }}</button>
          </div>
          <div class="issue-ai__meta">
            <span>{{ row.user_name }}</span>
            <span class="issue-ai__mono">{{ row.model || '—' }}</span>
            <span class="issue-ai__mono">{{ row.prompt_tokens + row.completion_tokens }} tokens</span>
            <span class="issue-ai__mono">${{ (row.cost_micro_usd / 1_000_000).toFixed(4) }}</span>
            <span>{{ fmtShortDateTime(row.created_at) }}</span>
          </div>
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
  box-shadow: 0 8px 20px rgba(30, 50, 80, .06);
}
.issue-ai__summary {
  list-style: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .75rem;
  padding: .8rem .95rem;
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
  gap: .45rem;
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
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
}
.issue-ai__badge {
  padding: .12rem .38rem;
  border-radius: 999px;
  background: rgba(46, 109, 164, .1);
  color: var(--bp-blue-dark);
}
.issue-ai__hint,
.issue-ai__meta,
.issue-ai__outcome {
  color: var(--text-muted);
}
.issue-ai__body {
  padding: 0 .95rem .95rem;
}
.issue-ai__list {
  display: flex;
  flex-direction: column;
  gap: .5rem;
}
.issue-ai__item {
  padding: .65rem .75rem;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--bg);
}
.issue-ai__undo {
  border: 1px solid var(--border);
  background: var(--bg-card);
  border-radius: 999px;
  padding: .2rem .55rem;
  font-size: 11px;
  font-family: "DM Mono", "JetBrains Mono", monospace;
  cursor: pointer;
}
.issue-ai__undo:disabled {
  opacity: .65;
  cursor: wait;
}
.issue-ai__empty {
  font-size: 13px;
  color: var(--text-muted);
}
</style>
