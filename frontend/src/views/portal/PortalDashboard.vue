<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { portalGreeting, type PortalLocale } from '@/composables/portalGreetings'
import { STATUS_DOT_STYLE, STATUS_LABEL } from '@/composables/useIssueDisplay'
import { fmtRelative } from '@/utils/formatTime'
import { formatInteger } from '@/composables/useNumberFormat'
import AppIcon from '@/components/AppIcon.vue'
import LoadingText from '@/components/LoadingText.vue'
import UserAvatar from '@/components/UserAvatar.vue'

interface PortalProject {
  id: number
  key: string
  name: string
  description: string
  status: string
  logo_path: string
  issue_count: number
  done_count: number
  last_activity?: string
  by_status?: Record<string, number>
}

interface AwaitingIssue {
  id: number
  issue_key: string
  title: string
  type: string
  status: string
  priority: string
  project_id: number
  project_key: string
  project_name: string
  updated_at: string
  can_edit: boolean
}

interface ProjektberichtLink {
  code: string
  project_id: number
  project_key: string
  project_name: string
  status: string
  total_issues: number
  created_at: string
  accepted_at: string | null
}

interface OverviewKpis {
  active_projects: number
  open_issues: number
  awaiting_acceptance: number
  accepted_this_month: number
}

interface Overview {
  kpis: OverviewKpis
  projects: PortalProject[]
  awaiting_acceptance: AwaitingIssue[]
  recent_projektberichte: ProjektberichtLink[]
}

const { t, locale } = useI18n()
const router = useRouter()
const auth = useAuthStore()

const loading = ref(true)
const overview = ref<Overview>({
  kpis: { active_projects: 0, open_issues: 0, awaiting_acceptance: 0, accepted_this_month: 0 },
  projects: [],
  awaiting_acceptance: [],
  recent_projektberichte: [],
})

const pendingAction = ref<Record<number, 'accept' | 'reject' | null>>({})
const actionError = ref<string | null>(null)

const portalLocale = computed<PortalLocale>(() => (locale.value === 'de' ? 'de' : 'en'))
const greet = computed(() =>
  portalGreeting(auth.user?.first_name, auth.user?.username ?? '', portalLocale.value),
)

const awaitingHeadingRef = ref<HTMLElement | null>(null)
function scrollToAwaiting() {
  awaitingHeadingRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

onMounted(load)

async function load() {
  loading.value = true
  try {
    overview.value = await api.get<Overview>('/portal/overview')
  } catch {
    // soft-fail: show the empty shell
  } finally {
    loading.value = false
  }
}

function projectProgress(p: PortalProject): number {
  if (!p.issue_count) return 0
  return Math.round((p.done_count / p.issue_count) * 100)
}

const STATUS_BAR_ORDER = [
  'new',
  'backlog',
  'in-progress',
  'qa',
  'delivered',
  'done',
  'accepted',
  'invoiced',
  'cancelled',
] as const

interface StatusSegment { status: string; count: number; pct: number; color: string }

function statusSegments(p: PortalProject): StatusSegment[] {
  const by = p.by_status ?? {}
  const total = Object.values(by).reduce((sum, n) => sum + n, 0)
  if (!total) return []
  const out: StatusSegment[] = []
  for (const st of STATUS_BAR_ORDER) {
    const count = by[st] ?? 0
    if (!count) continue
    out.push({
      status: st,
      count,
      pct: (count / total) * 100,
      color: STATUS_DOT_STYLE[st]?.color ?? '#9ca3af',
    })
  }
  return out
}

function statusTooltip(p: PortalProject): string {
  const by = p.by_status ?? {}
  return STATUS_BAR_ORDER
    .filter((st) => (by[st] ?? 0) > 0)
    .map((st) => `${STATUS_LABEL[st] ?? st}: ${by[st]}`)
    .join(' · ')
}

async function actOn(item: AwaitingIssue, kind: 'accept' | 'reject') {
  if (!item.can_edit) return
  pendingAction.value[item.id] = kind
  actionError.value = null
  try {
    await api.post(`/portal/issues/${item.id}/${kind}`, {})
    // Optimistic removal — keep counters in sync.
    overview.value.awaiting_acceptance = overview.value.awaiting_acceptance.filter(
      (i) => i.id !== item.id,
    )
    overview.value.kpis.awaiting_acceptance = Math.max(
      0,
      overview.value.kpis.awaiting_acceptance - 1,
    )
    if (kind === 'accept') {
      overview.value.kpis.accepted_this_month += 1
    }
  } catch {
    actionError.value = t('portal.welcome.awaiting.actionFailed')
  } finally {
    pendingAction.value[item.id] = null
  }
}

function openIssue(item: AwaitingIssue) {
  router.push(`/portal/projects/${item.project_id}/issues/${item.id}`)
}

function lastActivityText(p: PortalProject): string {
  if (!p.last_activity) return t('portal.welcome.projects.noActivity')
  return t('portal.welcome.projects.lastActivity', { when: fmtRelative(p.last_activity) })
}

function reportItemCount(n: number): string {
  return t('portal.welcome.reports.itemCount', { n }, n)
}
</script>

<template>
  <div class="portal-dashboard">
    <!-- Greeting hero -->
    <header class="welcome-hero">
      <div class="welcome-avatar">
        <UserAvatar v-if="auth.user" :user="auth.user" size="lg" />
      </div>
      <div class="welcome-text">
        <h1 class="welcome-title">{{ greet.prefix }}, {{ greet.name }}</h1>
        <p class="welcome-message">{{ greet.message }}</p>
      </div>
    </header>

    <LoadingText
      v-if="loading"
      class="welcome-loading"
      :label="$t('portal.welcome.loading')"
    />

    <template v-else>
      <!-- KPI strip -->
      <section class="kpi-strip" :aria-label="$t('portal.welcome.kpi.activeProjects')">
        <article class="kpi-card">
          <div class="kpi-value">{{ formatInteger(overview.kpis.active_projects) }}</div>
          <div class="kpi-label">{{ $t('portal.welcome.kpi.activeProjects') }}</div>
        </article>
        <article class="kpi-card">
          <div class="kpi-value">{{ formatInteger(overview.kpis.open_issues) }}</div>
          <div class="kpi-label">{{ $t('portal.welcome.kpi.openIssues') }}</div>
        </article>
        <button
          type="button"
          class="kpi-card kpi-card--clickable"
          :class="{ 'kpi-card--accent': overview.kpis.awaiting_acceptance > 0 }"
          :disabled="overview.kpis.awaiting_acceptance === 0"
          @click="scrollToAwaiting"
        >
          <div class="kpi-value">{{ formatInteger(overview.kpis.awaiting_acceptance) }}</div>
          <div class="kpi-label">{{ $t('portal.welcome.kpi.awaiting') }}</div>
        </button>
        <article class="kpi-card">
          <div class="kpi-value">{{ formatInteger(overview.kpis.accepted_this_month) }}</div>
          <div class="kpi-label">{{ $t('portal.welcome.kpi.acceptedThisMonth') }}</div>
        </article>
      </section>

      <!-- Awaiting your acceptance -->
      <section ref="awaitingHeadingRef" class="welcome-section">
        <header class="section-header">
          <h2 class="section-title">{{ $t('portal.welcome.awaiting.title') }}</h2>
          <p class="section-subtitle">{{ $t('portal.welcome.awaiting.subtitle') }}</p>
        </header>
        <div v-if="actionError" class="section-error" role="alert">{{ actionError }}</div>
        <div v-if="overview.awaiting_acceptance.length === 0" class="section-empty">
          {{ $t('portal.welcome.awaiting.empty') }}
        </div>
        <ul v-else class="awaiting-list">
          <li v-for="item in overview.awaiting_acceptance" :key="item.id" class="awaiting-row">
            <button type="button" class="awaiting-main" @click="openIssue(item)">
              <span class="awaiting-key">{{ item.issue_key }}</span>
              <span class="awaiting-title">{{ item.title }}</span>
              <span class="awaiting-project">{{ item.project_name }}</span>
              <span class="awaiting-when">
                {{
                  item.status === 'delivered'
                    ? $t('portal.welcome.awaiting.deliveredAt', { when: fmtRelative(item.updated_at) })
                    : $t('portal.welcome.awaiting.doneAt', { when: fmtRelative(item.updated_at) })
                }}
              </span>
            </button>
            <div v-if="item.can_edit" class="awaiting-actions">
              <button
                type="button"
                class="btn btn-sm btn-success"
                :disabled="pendingAction[item.id] !== null && pendingAction[item.id] !== undefined"
                @click="actOn(item, 'accept')"
              >
                <AppIcon name="check" :size="14" />
                {{ $t('portal.welcome.awaiting.accept') }}
              </button>
              <button
                type="button"
                class="btn btn-sm btn-ghost"
                :disabled="pendingAction[item.id] !== null && pendingAction[item.id] !== undefined"
                @click="actOn(item, 'reject')"
              >
                {{ $t('portal.welcome.awaiting.reject') }}
              </button>
            </div>
            <span
              v-else
              class="awaiting-viewer-mark"
              :title="$t('portal.welcome.awaiting.viewerHint')"
              :aria-label="$t('portal.welcome.awaiting.viewerHint')"
            >
              <AppIcon name="eye" :size="14" />
            </span>
          </li>
        </ul>
      </section>

      <!-- Project cards -->
      <section class="welcome-section">
        <header class="section-header">
          <h2 class="section-title">{{ $t('portal.welcome.projects.title') }}</h2>
        </header>
        <div v-if="overview.projects.length === 0" class="section-empty">
          {{ $t('portal.welcome.projects.empty') }}
        </div>
        <div v-else class="project-grid">
          <router-link
            v-for="p in overview.projects"
            :key="p.id"
            :to="`/portal/projects/${p.id}`"
            class="project-card"
          >
            <div class="card-header">
              <img v-if="p.logo_path" :src="p.logo_path" class="card-logo" alt="" />
              <div class="card-key">{{ p.key }}</div>
            </div>
            <h3 class="card-name">{{ p.name }}</h3>
            <p v-if="p.description" class="card-desc">{{ p.description }}</p>

            <div class="card-stats">
              <div class="stat">
                <span class="stat-value">{{ formatInteger(p.issue_count) }}</span>
                <span class="stat-label">{{ $t('portal.issues') }}</span>
              </div>
              <div class="stat">
                <span class="stat-value">{{ formatInteger(p.done_count) }}</span>
                <span class="stat-label">{{ $t('portal.done') }}</span>
              </div>
              <div class="stat stat-progress">
                <span class="stat-value">{{ formatInteger(projectProgress(p)) }}%</span>
                <span class="stat-label">{{ $t('status.done') }}</span>
              </div>
            </div>

            <div
              v-if="statusSegments(p).length > 0"
              class="status-bar"
              :title="statusTooltip(p)"
            >
              <span
                v-for="seg in statusSegments(p)"
                :key="seg.status"
                class="status-seg"
                :style="{ width: seg.pct + '%', background: seg.color }"
              />
            </div>

            <div class="card-footer">{{ lastActivityText(p) }}</div>
          </router-link>
        </div>
      </section>

      <!-- Recent Projektberichte -->
      <section v-if="overview.recent_projektberichte.length > 0" class="welcome-section">
        <header class="section-header">
          <h2 class="section-title">{{ $t('portal.welcome.reports.title') }}</h2>
          <p class="section-subtitle">{{ $t('portal.welcome.reports.subtitle') }}</p>
        </header>
        <ul class="report-list">
          <li v-for="r in overview.recent_projektberichte" :key="r.code">
            <router-link :to="`/accept/${r.code}`" class="report-row">
              <span class="report-project">{{ r.project_name }}</span>
              <span class="report-code">{{ r.code }}</span>
              <span class="report-count">{{ reportItemCount(r.total_issues) }}</span>
              <span class="report-when">{{ fmtRelative(r.created_at) }}</span>
              <span
                class="report-badge"
                :class="r.accepted_at ? 'report-badge--accepted' : 'report-badge--pending'"
              >
                {{
                  r.accepted_at
                    ? $t('portal.welcome.reports.accepted')
                    : $t('portal.welcome.reports.pending')
                }}
              </span>
            </router-link>
          </li>
        </ul>
      </section>
    </template>
  </div>
</template>

<style scoped>
.portal-dashboard {
  display: flex;
  flex-direction: column;
  gap: 1.75rem;
}

/* ── Greeting hero ───────────────────────────────────────────────────────── */
.welcome-hero {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding-bottom: .25rem;
}
.welcome-avatar :deep(.user-avatar) {
  width: 56px !important;
  height: 56px !important;
  font-size: 16px !important;
}
.welcome-text { min-width: 0; }
.welcome-title {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.25;
  color: var(--text);
  margin: 0;
}
.welcome-message {
  font-size: 14px;
  color: var(--text-muted);
  margin: .25rem 0 0;
}
.welcome-loading {
  color: var(--text-muted);
  padding: 1.5rem 0;
}

/* ── KPI strip ───────────────────────────────────────────────────────────── */
.kpi-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: .75rem;
}
@media (max-width: 720px) {
  .kpi-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
.kpi-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1rem 1.1rem;
  text-align: left;
  box-shadow: var(--shadow);
  display: flex;
  flex-direction: column;
  gap: .25rem;
}
.kpi-card--clickable {
  cursor: pointer;
  font: inherit;
  color: inherit;
  transition: border-color .15s, box-shadow .15s, background .15s;
}
.kpi-card--clickable:hover:not(:disabled) {
  border-color: var(--brand-blue);
  box-shadow: var(--shadow-md);
}
.kpi-card--clickable:disabled {
  cursor: default;
  opacity: .7;
}
.kpi-card--accent {
  border-color: var(--brand-blue);
  background: var(--brand-blue-pale);
}
.kpi-value {
  font-size: 24px;
  font-weight: 700;
  color: var(--text);
  font-variant-numeric: tabular-nums;
}
.kpi-card--accent .kpi-value { color: var(--brand-blue-dark); }
.kpi-label {
  font-size: 11px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: .04em;
  font-weight: 600;
}

/* ── Section frame ──────────────────────────────────────────────────────── */
.welcome-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow);
  overflow: hidden;
}
.section-header {
  padding: 1rem 1.25rem .65rem;
  border-bottom: 1px solid var(--border);
}
.section-title {
  font-size: 13px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .06em;
  color: var(--text-muted);
  margin: 0;
}
.section-subtitle {
  font-size: 13px;
  color: var(--text-muted);
  margin: .25rem 0 0;
}
.section-empty {
  padding: 1.25rem;
  font-size: 13px;
  color: var(--text-muted);
}
.section-error {
  padding: .75rem 1.25rem;
  background: rgba(220, 38, 38, .08);
  color: #b91c1c;
  font-size: 13px;
  border-bottom: 1px solid var(--border);
}

/* ── Awaiting list ──────────────────────────────────────────────────────── */
.awaiting-list { list-style: none; margin: 0; padding: 0; }
.awaiting-row {
  display: flex;
  align-items: center;
  gap: .75rem;
  padding: .65rem 1.25rem;
  border-bottom: 1px solid var(--border);
}
.awaiting-row:last-child { border-bottom: none; }
.awaiting-main {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr) 160px 110px;
  align-items: center;
  gap: .5rem;
  background: none;
  border: none;
  padding: 0;
  font: inherit;
  text-align: left;
  flex: 1;
  cursor: pointer;
  color: inherit;
}
.awaiting-main:hover .awaiting-title { color: var(--brand-blue); }
.awaiting-key {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: .04em;
  font-family: monospace;
  color: var(--brand-blue-dark);
  background: var(--brand-blue-pale);
  padding: .15rem .4rem;
  border-radius: 3px;
  text-align: center;
}
.awaiting-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.awaiting-project {
  font-size: 12px;
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.awaiting-when {
  font-size: 11px;
  color: var(--text-muted);
  text-align: right;
}
.awaiting-actions {
  display: flex;
  gap: .35rem;
  flex-shrink: 0;
}
.awaiting-viewer-mark {
  color: var(--text-muted);
  opacity: .55;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  padding: 0 .6rem;
}
.btn-sm {
  padding: .3rem .6rem;
  font-size: 12px;
  display: inline-flex;
  align-items: center;
  gap: .25rem;
}
.btn-success {
  background: var(--brand-green, #16a34a);
  color: white;
  border: 1px solid transparent;
}
.btn-success:hover:not(:disabled) { background: #15803d; }
.btn-success:disabled { opacity: .55; cursor: not-allowed; }

@media (max-width: 720px) {
  .awaiting-row { flex-wrap: wrap; }
  .awaiting-main {
    grid-template-columns: 80px minmax(0, 1fr);
    grid-row-gap: .15rem;
  }
  .awaiting-project, .awaiting-when {
    grid-column: 2;
    text-align: left;
  }
  .awaiting-viewer-mark { padding: 0; }
}

/* ── Project grid ───────────────────────────────────────────────────────── */
.project-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 1rem;
  padding: 1rem 1.25rem 1.25rem;
}
.project-card {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius, 8px);
  padding: 1.1rem;
  text-decoration: none;
  color: inherit;
  display: flex;
  flex-direction: column;
  gap: .65rem;
  transition: box-shadow .15s, border-color .15s, transform .15s;
}
.project-card:hover {
  border-color: var(--brand-blue);
  box-shadow: var(--shadow-md);
  transform: translateY(-1px);
}
.card-header {
  display: flex;
  align-items: center;
  gap: .5rem;
}
.card-logo {
  width: 28px;
  height: 28px;
  border-radius: 4px;
  object-fit: cover;
}
.card-key {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: .03em;
  padding: .15rem .5rem;
  border-radius: 4px;
  background: var(--brand-blue-pale);
  color: var(--brand-blue-dark);
  font-family: monospace;
}
.card-name {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  color: var(--text);
}
.card-desc {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.card-stats {
  display: flex;
  gap: 1.5rem;
}
.stat { display: flex; flex-direction: column; gap: .1rem; }
.stat-value {
  font-size: 17px;
  font-weight: 700;
  color: var(--text);
  font-variant-numeric: tabular-nums;
}
.stat-label {
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: .04em;
}
.stat-progress .stat-value { font-size: 14px; color: var(--text-muted); }

.status-bar {
  display: flex;
  height: 6px;
  background: var(--border);
  border-radius: 3px;
  overflow: hidden;
}
.status-seg { display: block; height: 100%; }

.card-footer {
  font-size: 11px;
  color: var(--text-muted);
  opacity: .85;
}

/* ── Recent Projektberichte ─────────────────────────────────────────────── */
.report-list { list-style: none; margin: 0; padding: 0; }
.report-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 90px 110px 80px 130px;
  align-items: center;
  gap: .65rem;
  padding: .65rem 1.25rem;
  border-bottom: 1px solid var(--border);
  text-decoration: none;
  color: inherit;
  transition: background .12s;
}
.report-row:last-child { border-bottom: none; }
.report-row:hover { background: var(--bg); }
.report-project {
  font-size: 13px;
  font-weight: 500;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.report-code {
  font-family: monospace;
  font-size: 11px;
  font-weight: 700;
  color: var(--brand-blue-dark);
  background: var(--brand-blue-pale);
  padding: .15rem .4rem;
  border-radius: 3px;
  text-align: center;
  letter-spacing: .04em;
}
.report-count, .report-when {
  font-size: 11px;
  color: var(--text-muted);
}
.report-badge {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .04em;
  padding: .2rem .55rem;
  border-radius: 999px;
  text-align: center;
}
.report-badge--pending {
  background: rgba(245, 158, 11, .15);
  color: #b45309;
}
.report-badge--accepted {
  background: rgba(34, 197, 94, .15);
  color: #166534;
}

@media (max-width: 720px) {
  .report-row {
    grid-template-columns: minmax(0, 1fr) auto;
    grid-row-gap: .25rem;
  }
  .report-code, .report-count, .report-when { grid-column: 1; }
  .report-badge { grid-column: 2; grid-row: 1; justify-self: end; }
}
</style>
