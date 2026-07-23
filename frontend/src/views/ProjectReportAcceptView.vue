<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import LoadingText from '@/components/LoadingText.vue'
import { formatInteger } from '@/composables/useNumberFormat'
import type { ProjectReportSnapshot } from '@/types'

const route = useRoute()
const code = computed(() => String(route.params.code || ''))
const report = ref<ProjectReportSnapshot | null>(null)
const loading = ref(true)
const accepting = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    report.value = await api.get<ProjectReportSnapshot>(`/projektberichte/accept/${encodeURIComponent(code.value)}`)
  } catch (e) {
    error.value = errMsg(e, 'Projektbericht konnte nicht geladen werden.')
  } finally {
    loading.value = false
  }
}

async function acceptReport() {
  if (!report.value || accepting.value) return
  accepting.value = true
  error.value = ''
  try {
    report.value = await api.post<ProjectReportSnapshot>(`/projektberichte/accept/${encodeURIComponent(code.value)}`, {})
  } catch (e) {
    error.value = errMsg(e, 'Projektbericht konnte nicht abgenommen werden.')
  } finally {
    accepting.value = false
  }
}

const accepted = computed(() => report.value?.status === 'accepted')
const eligible = computed(() => report.value?.eligible_count ?? 0)
const alreadyFinal = computed(() => report.value?.already_final_count ?? 0)
const skipped = computed(() => report.value?.skipped_count ?? 0)

onMounted(load)
</script>

<template>
  <main class="pra-page">
    <LoadingText v-if="loading" label="Lade Projektbericht…" />
    <section v-else-if="report" class="pra-panel">
      <header class="pra-header">
        <div>
          <p class="pra-kicker">{{ report.project_key }}</p>
          <h1>{{ report.report_key || 'Projektbericht' }}</h1>
          <p class="pra-sub">{{ report.project_name }}</p>
        </div>
        <span :class="['pra-status', accepted ? 'pra-status--accepted' : '']">
          <AppIcon :name="accepted ? 'shield-check' : 'file-check-2'" :size="16" />
          {{ accepted ? 'Abgenommen' : 'Bereit zur Abnahme' }}
        </span>
      </header>

      <div class="pra-stats">
        <div><strong>{{ formatInteger(report.total_issues) }}</strong><span>Tickets im Bericht</span></div>
        <div><strong>{{ formatInteger(eligible) }}</strong><span>können abgenommen werden</span></div>
        <div><strong>{{ formatInteger(alreadyFinal) }}</strong><span>bereits final</span></div>
        <div><strong>{{ formatInteger(skipped) }}</strong><span>übersprungen</span></div>
      </div>

      <p class="pra-copy">
        Dieser Projektbericht ist ein gespeicherter Snapshot. Die Abnahme betrifft nur die enthaltenen Tickets,
        die aktuell im Status <strong>done</strong> oder <strong>delivered</strong> sind.
      </p>

      <p v-if="error" class="pra-error">{{ error }}</p>

      <div class="pra-actions">
        <a class="btn btn-ghost" :href="`/api/projektberichte/${report.code}/pdf`" target="_blank">
          <AppIcon name="download" :size="15" /> PDF öffnen
        </a>
        <button class="btn btn-primary" :disabled="accepted || eligible === 0 || accepting" @click="acceptReport">
          <AppIcon name="shield-check" :size="15" />
          {{ accepting ? 'Übernehme…' : `Jetzt ${eligible} Tickets abnehmen` }}
        </button>
      </div>
    </section>
    <section v-else class="pra-panel">
      <p class="pra-error">{{ error || 'Projektbericht nicht gefunden.' }}</p>
    </section>
  </main>
</template>

<style scoped>
.pra-page {
  max-width: 820px;
  margin: 0 auto;
  padding: 2rem 1rem;
}
.pra-panel {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1.25rem;
}
.pra-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 1rem;
  margin-bottom: 1rem;
}
.pra-kicker {
  margin: 0 0 .2rem;
  font-size: 12px;
  color: var(--text-muted);
  font-weight: 700;
}
.pra-header h1 {
  margin: 0;
  font-size: 22px;
}
.pra-sub {
  margin: .25rem 0 0;
  color: var(--text-muted);
}
.pra-status {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  font-size: 12px;
  font-weight: 700;
  color: var(--brand-blue-dark);
  background: var(--brand-blue-pale);
  border-radius: 999px;
  padding: .35rem .6rem;
  white-space: nowrap;
}
.pra-status--accepted {
  color: #166534;
  background: #dcfce7;
}
.pra-stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  margin-bottom: 1rem;
}
.pra-stats div {
  padding: .85rem;
  border-right: 1px solid var(--border);
}
.pra-stats div:last-child { border-right: 0; }
.pra-stats strong {
  display: block;
  font-size: 22px;
}
.pra-stats span {
  display: block;
  color: var(--text-muted);
  font-size: 12px;
}
.pra-copy {
  color: var(--text);
  line-height: 1.55;
}
.pra-actions {
  display: flex;
  justify-content: flex-end;
  gap: .6rem;
  margin-top: 1rem;
}
.pra-error {
  color: #b42318;
  background: #fef3f2;
  border: 1px solid #fecdca;
  border-radius: 6px;
  padding: .6rem .75rem;
}
@media (max-width: 720px) {
  .pra-header,
  .pra-actions { flex-direction: column; align-items: stretch; }
  .pra-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .pra-stats div:nth-child(2) { border-right: 0; }
}
</style>
