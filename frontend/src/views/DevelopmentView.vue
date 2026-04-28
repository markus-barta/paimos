<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'

import AppIcon from '@/components/AppIcon.vue'

const auth   = useAuthStore()
const router = useRouter()

// Guard: admin only
if (auth.user && auth.user.role !== 'admin') router.replace('/')

// ── Test reports ──────────────────────────────────────────────────────────────

interface ReportMeta {
  filename:     string
  version:      string
  generated_at: string
  size_bytes:   number
}

interface ReportSummary {
  version:           string
  failures?:         number
  quick_failures?:   number
  complete_failures?: number
  generated_at:      string
  available?:        boolean
  status?:           string
  report_count?:     number
}

const reports     = ref<ReportMeta[]>([])
const summary     = ref<ReportSummary | null>(null)
const loading     = ref(true)
const selectedReport = ref<string | null>(null)
const reportHTML  = ref('')
const reportLoading = ref(false)

onMounted(async () => {
  try {
    const [r, s] = await Promise.all([
      api.get<ReportMeta[]>('/dev/test-reports'),
      api.get<ReportSummary>('/dev/test-reports/summary'),
    ])
    reports.value = r
    summary.value = s
  } catch { /* ignore */ }
  finally { loading.value = false }
})

async function openReport(filename: string) {
  if (selectedReport.value === filename) {
    selectedReport.value = null
    reportHTML.value = ''
    return
  }
  selectedReport.value = filename
  reportLoading.value = true
  reportHTML.value = ''
  try {
    // Fetch raw HTML — use fetch directly since api client parses JSON.
    const resp = await fetch(`/api/dev/test-reports/${filename}`, {
      credentials: 'same-origin',
    })
    reportHTML.value = await resp.text()
  } catch { reportHTML.value = '<p>Failed to load report.</p>' }
  finally { reportLoading.value = false }
}

function formatDate(iso: string): string {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString('en-GB', {
      year: 'numeric', month: 'short', day: '2-digit',
      hour: '2-digit', minute: '2-digit',
    })
  } catch { return iso }
}

function totalFailures(s: ReportSummary): number {
  return s.failures ?? ((s.quick_failures ?? 0) + (s.complete_failures ?? 0))
}
const hasFailures = computed(() =>
  summary.value != null && totalFailures(summary.value) > 0
)
const missingReports = computed(() =>
  !loading.value && reports.value.length === 0 && (summary.value?.status === 'missing_reports' || !summary.value?.available)
)
</script>

<template>
  <div class="dev-view">
    <Teleport defer to="#app-header-left">
      <span class="ah-title">Development</span>
      <span class="ah-subtitle">Admin tools and diagnostics</span>
    </Teleport>

    <!-- Summary banner -->
    <div v-if="summary?.available && !loading" :class="['summary-banner', hasFailures ? 'banner-warn' : 'banner-ok']">
      <AppIcon :name="hasFailures ? 'triangle-alert' : 'circle-check'" :size="15" />
      <template v-if="hasFailures">
        Last run v{{ summary.version }}:
        <strong>{{ totalFailures(summary!) }} failure{{ totalFailures(summary!) > 1 ? 's' : '' }}</strong>
      </template>
      <template v-else>
        All tests passed — v{{ summary.version }}
      </template>
      <span class="banner-date">{{ formatDate(summary.generated_at) }}</span>
    </div>

    <!-- Reports table -->
    <div class="section">
      <h2 class="section-title">Test Reports</h2>

      <div v-if="loading" class="empty">Loading…</div>
      <div v-else-if="missingReports" class="empty">
        This deployment has no ingested test reports yet. GitHub CI may still be green, but this screen only shows reports uploaded into the running instance. Use Settings → Development to upload a report bundle for this environment.
      </div>
      <div v-else-if="reports.length === 0" class="empty">
        No reports are currently available.
      </div>

      <div v-else class="reports-list">
        <div
          v-for="r in reports"
          :key="r.filename"
          :class="['report-row', { 'report-row--open': selectedReport === r.filename }]"
        >
          <button class="report-header" @click="openReport(r.filename)">
            <span class="report-version">v{{ r.version }}</span>
            <span class="report-date">{{ formatDate(r.generated_at) }}</span>
            <span class="report-size">{{ Math.round(r.size_bytes / 1024) }} KB</span>
            <AppIcon
              :name="selectedReport === r.filename ? 'chevron-up' : 'chevron-down'"
              :size="14"
              class="report-chevron"
            />
          </button>

          <!-- Embedded report HTML -->
          <div v-if="selectedReport === r.filename" class="report-embed">
            <div v-if="reportLoading" class="embed-loading">Loading report…</div>
            <iframe
              v-else-if="reportHTML"
              :srcdoc="reportHTML"
              class="report-iframe"
              sandbox="allow-same-origin"
              title="Test report"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dev-view { padding: 2rem; max-width: 960px; }

/* Summary banner */
.summary-banner {
  display: flex; align-items: center; gap: .6rem;
  padding: .7rem 1rem; border-radius: var(--radius);
  font-size: 13px; margin-bottom: 1.5rem;
}
.banner-ok   { background: #d1fae5; color: #065f46; border: 1px solid #6ee7b7; }
.banner-warn { background: #fef3c7; color: #92400e; border: 1px solid #fcd34d; }
.banner-date { margin-left: auto; font-size: 11px; opacity: .75; }

/* Section */
.section { margin-bottom: 2rem; }
.section-title {
  font-size: 15px; font-weight: 700; color: var(--text);
  margin-bottom: 1rem; padding-bottom: .5rem;
  border-bottom: 1px solid var(--border);
}

.empty {
  padding: 2rem; text-align: center; color: var(--text-muted);
  font-size: 13px; background: var(--bg-card);
  border: 1px solid var(--border); border-radius: var(--radius);
}
.empty code { font-family: 'DM Mono', monospace; font-size: 12px; background: var(--bg); padding: .1rem .3rem; border-radius: 3px; }

/* Reports list */
.reports-list {
  border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden;
}
.report-row { border-bottom: 1px solid var(--border); }
.report-row:last-child { border-bottom: none; }

.report-header {
  display: flex; align-items: center; gap: 1rem;
  width: 100%; padding: .75rem 1rem;
  background: var(--bg-card); border: none; cursor: pointer;
  font-family: inherit; font-size: 13px; color: var(--text);
  text-align: left; transition: background .1s;
}
.report-header:hover { background: var(--bg); }
.report-row--open .report-header { background: var(--bp-blue-pale); }

.report-version { font-weight: 700; color: var(--bp-blue-dark); font-family: 'DM Mono', monospace; font-size: 12px; }
.report-date    { color: var(--text-muted); font-size: 12px; }
.report-size    { color: var(--text-muted); font-size: 11px; margin-left: auto; }
.report-chevron { color: var(--text-muted); flex-shrink: 0; }

.report-embed { border-top: 1px solid var(--border); }
.embed-loading { padding: 1.5rem; text-align: center; color: var(--text-muted); font-size: 13px; }
.report-iframe {
  width: 100%; height: 600px; border: none;
  display: block; background: #fff;
}
</style>
