<script setup lang="ts">
import { ref, computed } from 'vue'
import { api } from '@/api/client'

interface DevReportMeta { filename: string; version: string; generated_at: string; size_bytes: number; passed?: number; failed?: number; total?: number }
interface DevSummary { version: string; failures?: number; quick_failures?: number; complete_failures?: number; generated_at: string; passed?: number; failed?: number; total?: number }

const devReports     = ref<DevReportMeta[]>([])
const devSummary     = ref<DevSummary | null>(null)
const devLoading     = ref(true)
const devLoaded      = ref(false)
const devSelectedReport = ref<string | null>(null)
const devReportHTML  = ref('')
const devReportLoading = ref(false)
const devShowAll     = ref(false)
const DEV_LIMIT      = 20

const devVisibleReports = computed(() =>
  devShowAll.value ? devReports.value : devReports.value.slice(0, DEV_LIMIT)
)
const devHasMore = computed(() => devReports.value.length > DEV_LIMIT)

function devReportCounts(r: DevReportMeta) {
  if (r.passed != null && r.failed != null) return { passed: r.passed, failed: r.failed, total: r.total ?? (r.passed + r.failed) }
  const s = devSummary.value
  if (!s || s.version !== r.version) return null
  const failed = s.failures ?? ((s.quick_failures ?? 0) + (s.complete_failures ?? 0))
  const total = s.total ?? 0
  const passed = s.passed ?? (total - failed)
  return { passed, failed, total }
}

async function loadDev() {
  if (devLoaded.value) return
  devLoading.value = true
  try {
    const [r, s] = await Promise.all([
      api.get<DevReportMeta[]>('/dev/test-reports'),
      api.get<DevSummary>('/dev/test-reports/summary'),
    ])
    devReports.value = r
    devSummary.value = s
    if (r.length > 0) openDevReport(r[0].filename)
  } catch {}
  finally { devLoading.value = false; devLoaded.value = true }
}

async function openDevReport(filename: string) {
  if (devSelectedReport.value === filename) return
  devSelectedReport.value = filename
  devReportLoading.value = true
  try {
    const res = await fetch(`/api/dev/test-reports/${filename}`, { credentials: 'same-origin' })
    devReportHTML.value = await res.text()
  } catch { devReportHTML.value = '<p>Failed to load report.</p>' }
  finally { devReportLoading.value = false }
}

// Init
loadDev()
</script>

<template>
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Test Reports</h2>
      <p class="section-desc">CI test results from the latest and previous deployments.</p>
    </div>

    <div v-if="devLoading" class="loading">Loading…</div>
    <template v-else>
      <!-- Summary banner -->
      <div v-if="devSummary" :class="['dev-summary', (devSummary.failures ?? ((devSummary.quick_failures ?? 0) + (devSummary.complete_failures ?? 0))) > 0 ? 'dev-summary--fail' : 'dev-summary--pass']">
        <template v-if="(devSummary.failures ?? ((devSummary.quick_failures ?? 0) + (devSummary.complete_failures ?? 0))) === 0">
          All tests passed (v{{ devSummary.version }})
        </template>
        <template v-else>
          {{ devSummary.failures ?? ((devSummary.quick_failures ?? 0) + (devSummary.complete_failures ?? 0)) }} failure(s) in v{{ devSummary.version }}
        </template>
        <span class="dev-summary-date">{{ devSummary.generated_at?.slice(0, 10) }}</span>
      </div>

      <div class="dev-master-detail">
        <!-- Master: scrollable list -->
        <div class="dev-master">
          <div class="card" style="padding:0;overflow:hidden">
            <table class="settings-table">
              <thead><tr><th>Version</th><th>Date</th><th>Pass</th><th>Fail</th></tr></thead>
              <tbody>
                <tr
                  v-for="r in devVisibleReports" :key="r.filename"
                  :class="{ 'row-active': devSelectedReport === r.filename }"
                  style="cursor:pointer" @click="openDevReport(r.filename)"
                >
                  <td class="fw500">{{ r.version }}</td>
                  <td class="muted">{{ r.generated_at?.slice(0, 10) }}</td>
                  <td>
                    <span v-if="devReportCounts(r)" class="dev-count dev-count--pass">{{ devReportCounts(r)!.passed }}</span>
                    <span v-else class="muted">—</span>
                  </td>
                  <td>
                    <span v-if="devReportCounts(r)" :class="['dev-count', devReportCounts(r)!.failed > 0 ? 'dev-count--fail' : 'dev-count--pass']">{{ devReportCounts(r)!.failed }}</span>
                    <span v-else class="muted">—</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="!devShowAll && devHasMore" class="dev-show-more">
            <button class="btn btn-ghost btn-sm" @click="devShowAll = true">
              Show all {{ devReports.length }} reports
            </button>
          </div>
        </div>

        <!-- Detail: report viewer -->
        <div class="dev-detail">
          <div v-if="!devSelectedReport" class="dev-detail-empty">Select a report to view</div>
          <div v-else-if="devReportLoading" class="dev-detail-empty">Loading…</div>
          <iframe v-else :srcdoc="devReportHTML" class="dev-report-iframe" />
        </div>
      </div>
    </template>
  </div>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.dev-summary { padding: .5rem .75rem; border-radius: var(--radius); font-size: 13px; font-weight: 600; display: flex; align-items: center; justify-content: space-between; }
.dev-summary--pass { background: #dcfce7; color: #166534; }
.dev-summary--fail { background: #fee2e2; color: #991b1b; }
.dev-summary-date { font-weight: 400; color: inherit; opacity: .7; }
.dev-count { font-weight: 600; font-variant-numeric: tabular-nums; }
.dev-count--pass { color: #166534; }
.dev-count--fail { color: #991b1b; }
.dev-show-more { margin-top: .5rem; text-align: center; }
.row-active { background: var(--bp-blue-pale); }
.dev-master-detail {
  display: grid; grid-template-columns: 300px 1fr; gap: .75rem;
  height: max(500px, calc(100vh - 300px)); margin-top: .75rem;
}
.dev-master { overflow-y: auto; display: flex; flex-direction: column; gap: .5rem; min-height: 0; }
.dev-detail {
  border: 1px solid var(--border); border-radius: 8px;
  overflow: hidden; display: flex; flex-direction: column; background: #fff;
}
.dev-detail-empty {
  display: flex; align-items: center; justify-content: center;
  height: 100%; color: var(--text-muted); font-size: 13px; font-style: italic;
}
.dev-report-iframe { width: 100%; height: 100%; border: none; flex: 1; }
</style>
