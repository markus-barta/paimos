<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { computed, onMounted, ref } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import {
  buildAICallsExportUrl,
  loadAICalls,
  loadMyAICalls,
  type AICallRow,
  type AICallListResponse,
  type AICallQuery,
} from '@/services/aiPaperTrail'

const props = defineProps<{
  mode: 'admin' | 'self'
}>()

const loading = ref(false)
const error = ref('')
const payload = ref<AICallListResponse | null>(null)
const query = ref<AICallQuery>({ limit: 25 })
const selected = ref<AICallRow | null>(null)
const draft = ref<AICallQuery>({ limit: 25 })

const rows = computed(() => payload.value?.rows ?? [])
const totalCost = computed(() => ((payload.value?.total_cost_micro_usd ?? 0) / 1_000_000).toFixed(4))
const exportHref = computed(() => buildAICallsExportUrl(props.mode, query.value))

async function load() {
  loading.value = true
  error.value = ''
  try {
    payload.value = props.mode === 'admin'
      ? await loadAICalls(query.value)
      : await loadMyAICalls(query.value)
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to load AI paper trail.'
  } finally {
    loading.value = false
  }
}

onMounted(() => { void load() })

function applyFilters() {
  query.value = {
    ...draft.value,
    limit: draft.value.limit || 25,
    cursor: '',
  }
  selected.value = null
  void load()
}

function resetFilters() {
  draft.value = { limit: 25 }
  applyFilters()
}

function nextPage() {
  if (!payload.value?.next_cursor) return
  query.value = { ...query.value, cursor: payload.value.next_cursor }
  draft.value = { ...query.value }
  selected.value = null
  void load()
}
</script>

<template>
  <section class="aipt">
    <div class="aipt-head">
      <div>
        <h3>{{ mode === 'admin' ? 'Paper trail' : 'My AI activity' }}</h3>
        <p class="aipt-sub">Per-call metadata only. No prompts or responses are stored in the audit log.</p>
      </div>
      <div class="aipt-actions">
        <a class="btn btn-ghost btn-sm" :href="exportHref">
          <AppIcon name="download" :size="12" /> CSV
        </a>
        <button class="btn btn-ghost btn-sm" @click="load" :disabled="loading">
          <AppIcon name="refresh-cw" :size="12" /> Refresh
        </button>
      </div>
    </div>

    <div class="aipt-totals">
      <span>{{ payload?.total_count ?? 0 }} calls</span>
      <span>${{ totalCost }}</span>
    </div>

    <div class="aipt-filters">
      <input v-model="draft.from" type="date" class="aipt-input" placeholder="From" />
      <input v-model="draft.to" type="date" class="aipt-input" placeholder="To" />
      <input v-model="draft.action_key" type="text" class="aipt-input" placeholder="Action key" />
      <input v-model="draft.model" type="text" class="aipt-input" placeholder="Model" />
      <select v-model="draft.outcome" class="aipt-input">
        <option value="">Any outcome</option>
        <option value="ok">ok</option>
        <option value="bad_request">bad_request</option>
        <option value="denied">denied</option>
        <option value="fail_upstream">fail_upstream</option>
        <option value="unconfigured">unconfigured</option>
      </select>
      <input v-model="draft.surface" type="text" class="aipt-input" placeholder="Surface" />
      <button class="btn btn-primary btn-sm" @click="applyFilters" :disabled="loading">Apply</button>
      <button class="btn btn-ghost btn-sm" @click="resetFilters" :disabled="loading">Reset</button>
    </div>

    <div v-if="error" class="aipt-error">{{ error }}</div>
    <LoadingText v-else-if="loading" class="aipt-empty" label="Loading AI activity…" />
    <div v-else-if="!rows.length" class="aipt-empty">No AI calls recorded yet.</div>
    <div v-else class="aipt-table-wrap">
      <table class="aipt-table">
        <thead>
          <tr>
            <th>Time</th>
            <th v-if="mode === 'admin'">User</th>
            <th>Action</th>
            <th>Subject</th>
            <th>Model</th>
            <th>Tokens</th>
            <th>Cost</th>
            <th>Outcome</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id" class="aipt-row" @click="selected = row">
            <td>{{ row.created_at }}</td>
            <td v-if="mode === 'admin'">{{ row.username }}</td>
            <td>
              <div class="aipt-action">{{ row.action_key }}</div>
              <div v-if="row.sub_action" class="aipt-subkey">{{ row.sub_action }}</div>
            </td>
            <td>{{ row.subject_label || row.surface }}</td>
            <td class="aipt-mono">{{ row.model || '—' }}</td>
            <td class="aipt-mono">{{ row.total_tokens }}</td>
            <td class="aipt-mono">${{ (row.cost_micro_usd / 1_000_000).toFixed(4) }}</td>
            <td>{{ row.outcome }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <aside v-if="selected" class="aipt-detail">
      <div class="aipt-detail__head">
        <div>
          <h4>{{ selected.action_key }}</h4>
          <p>{{ selected.subject_label || selected.surface }}</p>
        </div>
        <button type="button" class="btn btn-ghost btn-sm" @click="selected = null">Close</button>
      </div>
      <dl class="aipt-detail__grid">
        <div><dt>Time</dt><dd>{{ selected.created_at }}</dd></div>
        <div v-if="mode === 'admin'"><dt>User</dt><dd>{{ selected.username }}</dd></div>
        <div><dt>Request</dt><dd class="aipt-mono">{{ selected.request_id }}</dd></div>
        <div><dt>Model</dt><dd class="aipt-mono">{{ selected.model || '—' }}</dd></div>
        <div><dt>Provider</dt><dd class="aipt-mono">{{ selected.provider || '—' }}</dd></div>
        <div><dt>Outcome</dt><dd>{{ selected.outcome }}</dd></div>
        <div><dt>Tokens</dt><dd class="aipt-mono">{{ selected.prompt_tokens }} / {{ selected.completion_tokens }} / {{ selected.total_tokens }}</dd></div>
        <div><dt>Cost</dt><dd class="aipt-mono">${{ (selected.cost_micro_usd / 1_000_000).toFixed(4) }}</dd></div>
        <div><dt>Latency</dt><dd class="aipt-mono">{{ selected.latency_ms }} ms</dd></div>
        <div><dt>Error class</dt><dd class="aipt-mono">{{ selected.error_class || '—' }}</dd></div>
      </dl>
    </aside>

    <div v-if="payload?.next_cursor" class="aipt-pager">
      <button type="button" class="btn btn-ghost btn-sm" @click="nextPage" :disabled="loading">Next page</button>
    </div>
  </section>
</template>

<style scoped>
.aipt {
  display: flex;
  flex-direction: column;
  gap: .75rem;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--bg-card);
}
.aipt-head,
.aipt-totals {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .75rem;
  flex-wrap: wrap;
}
.aipt-sub {
  color: var(--text-muted);
  font-size: 13px;
}
.aipt-actions {
  display: flex;
  gap: .5rem;
}
.aipt-filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
  gap: .5rem;
}
.aipt-input {
  min-height: 34px;
  padding: .45rem .55rem;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  color: var(--text);
  font-size: 12px;
}
.aipt-totals {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 12px;
  color: var(--text-muted);
}
.aipt-error,
.aipt-empty {
  font-size: 13px;
  color: var(--text-muted);
}
.aipt-table-wrap {
  overflow: auto;
}
.aipt-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.aipt-table th,
.aipt-table td {
  padding: .45rem .4rem;
  border-bottom: 1px solid var(--border);
  text-align: left;
  vertical-align: top;
}
.aipt-row {
  cursor: pointer;
}
.aipt-row:hover {
  background: rgba(46, 109, 164, .04);
}
.aipt-action,
.aipt-subkey,
.aipt-mono {
  font-family: "DM Mono", "JetBrains Mono", monospace;
}
.aipt-subkey {
  color: var(--text-muted);
  font-size: 11px;
}
.aipt-detail {
  padding: .9rem 1rem;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: linear-gradient(180deg, rgba(220, 233, 244, .4), rgba(255,255,255,.95));
}
.aipt-detail__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: .75rem;
  margin-bottom: .75rem;
}
.aipt-detail__head h4 {
  margin: 0 0 .15rem;
  font-size: 14px;
}
.aipt-detail__head p {
  margin: 0;
  color: var(--text-muted);
  font-size: 12px;
}
.aipt-detail__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: .75rem;
}
.aipt-detail__grid dt {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: .06em;
  color: var(--text-muted);
  margin-bottom: .15rem;
}
.aipt-detail__grid dd {
  margin: 0;
  font-size: 12px;
  color: var(--text);
}
.aipt-pager {
  display: flex;
  justify-content: flex-end;
}
</style>
