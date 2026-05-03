<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, computed } from 'vue'
import { api } from '@/api/client'

interface DevReportMeta { filename: string; version: string; generated_at: string; size_bytes: number; passed?: number; failed?: number; total?: number }
interface DevSummary { version: string; failures?: number; quick_failures?: number; complete_failures?: number; generated_at: string; passed?: number; failed?: number; total?: number; available?: boolean; status?: string; report_count?: number }

const devReports     = ref<DevReportMeta[]>([])
const devSummary     = ref<DevSummary | null>(null)
const devLoading     = ref(true)
const devLoaded      = ref(false)
const devSelectedReport = ref<string | null>(null)
const devReportHTML  = ref('')
const devReportLoading = ref(false)
const devShowAll     = ref(false)
const reportInput = ref<HTMLInputElement | null>(null)
const summaryInput = ref<HTMLInputElement | null>(null)
const uploadReportFile = ref<File | null>(null)
const uploadSummaryFile = ref<File | null>(null)
const uploadLoading = ref(false)
const uploadError = ref('')
const uploadSuccess = ref('')
const DEV_LIMIT      = 20

const devVisibleReports = computed(() =>
  devShowAll.value ? devReports.value : devReports.value.slice(0, DEV_LIMIT)
)
const devHasMore = computed(() => devReports.value.length > DEV_LIMIT)
const devMissingReports = computed(() =>
  !devLoading.value && devReports.value.length === 0 && (devSummary.value?.status === 'missing_reports' || !devSummary.value?.available)
)
const devPartialReports = computed(() =>
  !devLoading.value && devReports.value.length > 0 && devSummary.value?.status === 'partial'
)

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
  if (devLoaded.value && !uploadLoading.value) return
  devLoading.value = true
  try {
    const [r, s] = await Promise.all([
      api.get<DevReportMeta[]>('/dev/test-reports'),
      api.get<DevSummary>('/dev/test-reports/summary'),
    ])
    devReports.value = r
    devSummary.value = s
    if (r.length > 0) {
      devSelectedReport.value = null
      await openDevReport(r[0].filename)
    } else {
      devSelectedReport.value = null
      devReportHTML.value = ''
    }
  } catch {}
  finally { devLoading.value = false; devLoaded.value = true }
}

function onReportSelected(event: Event) {
  const input = event.target as HTMLInputElement
  uploadReportFile.value = input.files?.[0] ?? null
  uploadError.value = ''
}

function onSummarySelected(event: Event) {
  const input = event.target as HTMLInputElement
  uploadSummaryFile.value = input.files?.[0] ?? null
  uploadError.value = ''
}

function resetUploadInputs() {
  uploadReportFile.value = null
  uploadSummaryFile.value = null
  if (reportInput.value) reportInput.value.value = ''
  if (summaryInput.value) summaryInput.value.value = ''
}

async function uploadBundle() {
  uploadError.value = ''
  uploadSuccess.value = ''
  if (!uploadReportFile.value) {
    uploadError.value = 'Select an HTML report first.'
    return
  }
  const body = new FormData()
  body.append('report', uploadReportFile.value)
  if (uploadSummaryFile.value) body.append('summary', uploadSummaryFile.value)
  uploadLoading.value = true
  try {
    const res = await fetch('/api/dev/test-reports', {
      method: 'POST',
      credentials: 'same-origin',
      body,
    })
    if (!res.ok) {
      const payload = await res.json().catch(() => null)
      throw new Error(payload?.error ?? 'Upload failed.')
    }
    uploadSuccess.value = 'Report bundle uploaded.'
    resetUploadInputs()
    devLoaded.value = false
    await loadDev()
  } catch (err: any) {
    uploadError.value = err?.message ?? 'Upload failed.'
  } finally {
    uploadLoading.value = false
  }
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

    <LoadingText v-if="devLoading" class="loading" label="Loading…" />
    <template v-else>
      <div v-if="devMissingReports" class="dev-state-banner dev-state-banner--warn">
        This deployment has no ingested test reports yet. GitHub CI can still be green; the app only shows reports that were uploaded or deployed into this instance.
      </div>
      <div v-else-if="devPartialReports" class="dev-state-banner dev-state-banner--warn">
        Report files exist, but the latest summary metadata is missing. Counts and sidebar badges may be incomplete.
      </div>

      <!-- Summary banner -->
      <div v-if="devSummary && devSummary.available" :class="['dev-summary', (devSummary.failures ?? ((devSummary.quick_failures ?? 0) + (devSummary.complete_failures ?? 0))) > 0 ? 'dev-summary--fail' : 'dev-summary--pass']">
        <template v-if="(devSummary.failures ?? ((devSummary.quick_failures ?? 0) + (devSummary.complete_failures ?? 0))) === 0">
          All tests passed (v{{ devSummary.version }})
        </template>
        <template v-else>
          {{ devSummary.failures ?? ((devSummary.quick_failures ?? 0) + (devSummary.complete_failures ?? 0)) }} failure(s) in v{{ devSummary.version }}
        </template>
        <span class="dev-summary-date">{{ devSummary.generated_at?.slice(0, 10) }}</span>
      </div>

      <div class="card dev-upload-card">
        <div class="dev-upload-head">
          <strong>Upload Report Bundle</strong>
          <span class="muted">Manual ingest for this deployment</span>
        </div>
        <div class="dev-upload-grid">
          <label class="field">
            <span>HTML report</span>
            <input ref="reportInput" type="file" accept=".html,text/html" @change="onReportSelected" />
          </label>
          <label class="field">
            <span>Summary JSON</span>
            <input ref="summaryInput" type="file" accept=".json,application/json" @change="onSummarySelected" />
          </label>
        </div>
        <p class="muted dev-upload-help">
          Expected names: <code>test-results-&lt;version&gt;.html</code> and optional <code>test-results-&lt;version&gt;-summary.json</code>.
        </p>
        <div v-if="uploadError" class="form-error">{{ uploadError }}</div>
        <div v-else-if="uploadSuccess" class="ok-banner">{{ uploadSuccess }}</div>
        <div class="form-actions">
          <button class="btn btn-primary btn-sm" :disabled="uploadLoading" @click="uploadBundle">
            {{ uploadLoading ? 'Uploading…' : 'Upload bundle' }}
          </button>
        </div>
      </div>

      <div class="dev-master-detail">
        <!-- Master: scrollable list -->
        <div class="dev-master">
          <div class="card" style="padding:0;overflow:hidden">
            <table class="settings-table">
              <thead><tr><th>Version</th><th>Date</th><th>Pass</th><th>Fail</th></tr></thead>
              <tbody>
                <tr v-if="devVisibleReports.length === 0">
                  <td colspan="4" class="muted dev-empty-row">No ingested reports on this instance.</td>
                </tr>
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
          <LoadingText v-else-if="devReportLoading" class="dev-detail-empty" label="Loading…" />
          <iframe v-else :srcdoc="devReportHTML" class="dev-report-iframe" />
        </div>
      </div>
    </template>
  </div>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.dev-state-banner {
  margin-bottom: .75rem;
  padding: .75rem .9rem;
  border: 1px solid #fcd34d;
  border-radius: var(--radius);
  background: #fffbeb;
  color: #92400e;
  font-size: 13px;
}
.dev-summary { padding: .5rem .75rem; border-radius: var(--radius); font-size: 13px; font-weight: 600; display: flex; align-items: center; justify-content: space-between; }
.dev-summary--pass { background: #dcfce7; color: #166534; }
.dev-summary--fail { background: #fee2e2; color: #991b1b; }
.dev-summary-date { font-weight: 400; color: inherit; opacity: .7; }
.dev-count { font-weight: 600; font-variant-numeric: tabular-nums; }
.dev-count--pass { color: #166534; }
.dev-count--fail { color: #991b1b; }
.dev-upload-card { margin-top: .75rem; margin-bottom: .75rem; }
.dev-upload-head { display: flex; align-items: baseline; justify-content: space-between; gap: .75rem; margin-bottom: .75rem; }
.dev-upload-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .75rem; }
.dev-upload-help { margin-top: .5rem; }
.dev-show-more { margin-top: .5rem; text-align: center; }
.row-active { background: var(--bp-blue-pale); }
.dev-empty-row { text-align: center; padding: 1.25rem; }
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
@media (max-width: 900px) {
  .dev-upload-grid { grid-template-columns: 1fr; }
}
</style>
