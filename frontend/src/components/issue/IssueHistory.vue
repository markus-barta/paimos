<script setup lang="ts">
import { ref, computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { fmtShortDateTime } from '@/utils/formatTime'
import { loadIssueHistory, type IssueHistoryEntry as HistoryEntry } from '@/services/issueHistory'
import { loadIssueAICalls, type AICallRow } from '@/services/aiPaperTrail'

const props = defineProps<{
  issueId: number
  open: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const historyLoading = ref(false)
const historyEntries = ref<HistoryEntry[]>([])
const historyIndex   = ref(0)
const aiCalls = ref<AICallRow[]>([])

async function load() {
  historyLoading.value = true
  try {
    historyEntries.value = await loadIssueHistory(props.issueId)
    historyIndex.value = Math.max(0, historyEntries.value.length - 1)
    aiCalls.value = (await loadIssueAICalls(props.issueId, { limit: 25 })).rows
  } finally {
    historyLoading.value = false
  }
}

defineExpose({ load })

function historyPrev() { if (historyIndex.value > 0) historyIndex.value-- }
function historyNext() { if (historyIndex.value < historyEntries.value.length - 1) historyIndex.value++ }

const currentSnapshot  = computed(() => historyEntries.value[historyIndex.value]?.snapshot ?? null)
const previousSnapshot = computed(() => historyIndex.value > 0 ? historyEntries.value[historyIndex.value - 1]?.snapshot ?? null : null)
const currentEntry     = computed(() => historyEntries.value[historyIndex.value] ?? null)

const SHORT_FIELDS = [
  'title','type','status','priority','cost_unit','release','assignee_id','parent_id',
  'billing_type','total_budget','rate_hourly','rate_lp',
  'estimate_hours','estimate_lp','ar_hours','ar_lp',
  'start_date','end_date','group_state','sprint_state',
  'jira_id','jira_version','jira_text','color',
] as const
const LONG_FIELDS = ['description','acceptance_criteria','notes'] as const

function isChanged(field: string): boolean {
  if (!previousSnapshot.value) return false
  const cur = currentSnapshot.value
  const prv = previousSnapshot.value
  if (!cur) return false
  if (field === 'tags') {
    const cids = (cur.tags ?? []).map((t: any) => t.id).sort().join(',')
    const pids = (prv.tags ?? []).map((t: any) => t.id).sort().join(',')
    return cids !== pids
  }
  return JSON.stringify(cur[field]) !== JSON.stringify(prv[field])
}

function displayVal(snap: Record<string, any> | null, field: string): string {
  if (!snap) return '—'
  const v = snap[field]
  if (v === null || v === undefined || v === '') return '—'
  if (field === 'assignee_id') return snap.assignee?.username ?? String(v)
  if (field === 'parent_id')   return String(v)
  return String(v)
}
</script>

<template>
  <Teleport to="body">
    <Transition name="history-fade">
      <div v-if="open" class="history-overlay" @click.self="emit('close')">
        <div class="history-panel">
          <div class="history-header">
            <div class="history-nav">
              <button class="hist-arrow" :disabled="historyIndex === 0" @click="historyPrev"><AppIcon name="chevron-left" :size="16" /></button>
              <span class="history-pos" v-if="historyEntries.length">
                Version {{ historyIndex + 1 }} of {{ historyEntries.length }}
              </span>
              <button class="hist-arrow" :disabled="historyIndex === historyEntries.length - 1" @click="historyNext"><AppIcon name="chevron-right" :size="16" /></button>
            </div>
            <div class="history-meta" v-if="currentEntry">
              <span class="history-by">{{ historyIndex === 0 ? 'Created' : 'Changed' }} by <strong>{{ currentEntry.changed_by_name || 'unknown' }}</strong></span>
              <span class="history-at">{{ fmtShortDateTime(currentEntry.changed_at) }}</span>
            </div>
            <button class="hist-close" @click="emit('close')">
              <AppIcon name="x" :size="16" />
            </button>
          </div>

          <div class="history-body" v-if="historyLoading">
            <span class="history-loading">Loading history…</span>
          </div>
          <div class="history-body" v-else-if="currentSnapshot">
            <div class="hist-row" :class="{ changed: isChanged('title') }">
              <span class="hist-label">Title</span>
              <span class="hist-val">{{ currentSnapshot.title }}</span>
              <span v-if="isChanged('title') && previousSnapshot" class="hist-old">was: {{ displayVal(previousSnapshot, 'title') }}</span>
            </div>

            <div class="hist-meta-grid">
              <div v-for="f in SHORT_FIELDS.filter(f => f !== 'title')" :key="f" class="hist-meta-item" :class="{ changed: isChanged(f) }">
                <span class="hist-label">{{ f.replace('_',' ') }}</span>
                <span class="hist-val">{{ displayVal(currentSnapshot, f) }}</span>
                <span v-if="isChanged(f) && previousSnapshot" class="hist-old">{{ displayVal(previousSnapshot, f) }}</span>
              </div>
              <div class="hist-meta-item" :class="{ changed: isChanged('tags') }">
                <span class="hist-label">Tags</span>
                <span class="hist-val">{{ (currentSnapshot.tags ?? []).map((t: any) => t.name).join(', ') || '—' }}</span>
                <span v-if="isChanged('tags') && previousSnapshot" class="hist-old">{{ (previousSnapshot.tags ?? []).map((t: any) => t.name).join(', ') || '—' }}</span>
              </div>
            </div>

            <div v-for="f in LONG_FIELDS" :key="f" class="hist-row" :class="{ changed: isChanged(f) }">
              <span class="hist-label">{{ f.replace('_',' ') }}</span>
              <template v-if="isChanged(f) && previousSnapshot">
                <div class="hist-text-old">{{ displayVal(previousSnapshot, f) }}</div>
                <div class="hist-text-new">{{ currentSnapshot[f] || '—' }}</div>
              </template>
              <span v-else class="hist-text">{{ currentSnapshot[f] || '—' }}</span>
            </div>

            <div class="hist-ai">
              <div class="hist-ai-head">
                <span class="hist-label">AI activity</span>
              </div>
              <div v-if="aiCalls.length" class="hist-ai-list">
                <div v-for="call in aiCalls" :key="call.id" class="hist-ai-item">
                  <div class="hist-ai-main">
                    <strong>{{ call.action_key }}</strong>
                    <span v-if="call.sub_action" class="hist-ai-sub">{{ call.sub_action }}</span>
                    <span class="hist-ai-outcome">{{ call.outcome }}</span>
                  </div>
                  <div class="hist-ai-meta">
                    <span>{{ call.model || '—' }}</span>
                    <span>{{ call.total_tokens }} tokens</span>
                    <span>${{ (call.cost_micro_usd / 1_000_000).toFixed(4) }}</span>
                    <span>{{ fmtShortDateTime(call.created_at) }}</span>
                  </div>
                </div>
              </div>
              <p v-else class="hist-old">No AI calls recorded for this issue yet.</p>
            </div>
          </div>
          <div class="history-body" v-else>
            <span class="history-loading">No history yet.</span>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.history-overlay {
  position: fixed; inset: 0; z-index: 300;
  background: rgba(10,20,35,.45);
  display: flex; align-items: flex-start; justify-content: center;
  padding: 2rem 1rem;
  overflow-y: auto;
}
.history-panel {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow-md);
  width: 100%; max-width: 680px;
  margin: auto;
  display: flex; flex-direction: column;
  overflow: hidden;
}
.history-header {
  display: flex; align-items: center; gap: 1rem;
  padding: 1rem 1.25rem;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
  position: sticky; top: 0; z-index: 1;
  flex-wrap: wrap;
}
.history-nav {
  display: flex; align-items: center; gap: .5rem;
}
.hist-arrow {
  background: none; border: 1px solid var(--border); color: var(--text);
  width: 28px; height: 28px; border-radius: var(--radius);
  font-size: 16px; line-height: 1; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: background .1s, border-color .1s;
}
.hist-arrow:hover:not(:disabled) { background: var(--bg); border-color: var(--bp-blue); color: var(--bp-blue); }
.hist-arrow:disabled { opacity: .35; cursor: default; }
.history-pos { font-size: 13px; font-weight: 600; color: var(--text); white-space: nowrap; }
.history-meta { display: flex; flex-direction: column; gap: .1rem; margin-left: auto; text-align: right; }
.history-by  { font-size: 12px; color: var(--text-muted); }
.history-by strong { color: var(--text); }
.history-at  { font-size: 11px; color: var(--text-muted); font-family: monospace; }
.hist-close {
  background: none; border: none; color: var(--text-muted);
  cursor: pointer; padding: .25rem; border-radius: var(--radius);
  display: flex; align-items: center;
}
.hist-close:hover { background: var(--bg); color: var(--text); }

.history-body { padding: 1.25rem; display: flex; flex-direction: column; gap: 1rem; }
.history-loading { font-size: 13px; color: var(--text-muted); }

.hist-row {
  display: flex; flex-direction: column; gap: .25rem;
  padding: .6rem .75rem; border-radius: var(--radius);
  border: 1px solid transparent;
}
.hist-row.changed {
  background: #fffbeb;
  border-color: #f5d66a;
}
.hist-label {
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--text-muted);
}
.hist-val  { font-size: 13px; color: var(--text); font-weight: 500; }
.hist-old  { font-size: 11px; color: var(--text-muted); text-decoration: line-through; }

.hist-meta-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: .5rem;
}
.hist-meta-item {
  display: flex; flex-direction: column; gap: .2rem;
  padding: .5rem .65rem; border-radius: var(--radius);
  border: 1px solid transparent;
  background: var(--bg);
}
.hist-meta-item.changed {
  background: #fffbeb;
  border-color: #f5d66a;
}

.hist-text     { font-size: 13px; color: var(--text); line-height: 1.6; white-space: pre-wrap; }
.hist-text-old { font-size: 12px; color: var(--text-muted); background: #fde8e8; padding: .4rem .5rem; border-radius: 4px; white-space: pre-wrap; line-height: 1.5; text-decoration: line-through; }
.hist-text-new { font-size: 13px; color: var(--text); background: #fffbeb; padding: .4rem .5rem; border-radius: 4px; white-space: pre-wrap; line-height: 1.6; border-left: 3px solid #f5d66a; }

.hist-ai {
  display: flex;
  flex-direction: column;
  gap: .5rem;
  padding-top: .75rem;
  border-top: 1px solid var(--border);
}
.hist-ai-list {
  display: flex;
  flex-direction: column;
  gap: .45rem;
}
.hist-ai-item {
  padding: .55rem .65rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--bg);
}
.hist-ai-main,
.hist-ai-meta {
  display: flex;
  gap: .5rem;
  flex-wrap: wrap;
}
.hist-ai-main {
  font-size: 12px;
  color: var(--text);
}
.hist-ai-sub,
.hist-ai-outcome,
.hist-ai-meta {
  font-family: monospace;
  font-size: 11px;
  color: var(--text-muted);
}

.history-fade-enter-active, .history-fade-leave-active { transition: opacity .18s; }
.history-fade-enter-from, .history-fade-leave-to { opacity: 0; }
</style>
