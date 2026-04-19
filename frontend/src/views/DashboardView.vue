<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { RouterLink } from 'vue-router'

import AppFooter from '@/components/AppFooter.vue'
import AppIcon from '@/components/AppIcon.vue'
import StatusDot from '@/components/StatusDot.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { greeting } from '@/composables/greetings'
import { fmtRelative } from '@/utils/formatTime'
import { TYPE_SVGS, STATUS_DOT_STYLE, STATUS_LABEL, PRIORITY_ICON, PRIORITY_COLOR, PRIORITY_LABEL } from '@/composables/useIssueDisplay'
import type { Project } from './ProjectsView.vue'
import type { Issue } from '@/types'

const auth = useAuthStore()
const projects = ref<Project[]>([])
const recent = ref<Issue[]>([])
const loading = ref(true)

const greet = computed(() => greeting(auth.user?.first_name, auth.user?.username ?? ''))

onMounted(async () => {
  const [p, r] = await Promise.all([
    api.get<Project[]>('/projects?status=active'),
    api.get<Issue[]>('/issues/recent').catch(() => [] as Issue[]),
  ])
  projects.value = p
  recent.value = r
  loading.value = false
})

function relativeTime(ts: string): string {
  if (!ts) return ''
  const diff = Date.now() - new Date(ts.replace(' ', 'T') + 'Z').getTime()
  const mins = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(diff / 86400000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  if (hours < 24) return `${hours}h ago`
  if (days < 30) return `${days}d ago`
  return new Date(ts).toLocaleDateString()
}

function statusDot(status: string) { return STATUS_DOT_STYLE[status] ?? STATUS_DOT_STYLE.backlog }
function statusLabel(status: string) { return STATUS_LABEL[status] ?? status }
function priorityIcon(p: string) { return PRIORITY_ICON[p] ?? 'minus' }
function priorityColor(p: string) { return PRIORITY_COLOR[p] ?? '#637383' }
function priorityLabel(p: string) { return PRIORITY_LABEL[p] ?? p }
</script>

<template>
    <Teleport defer to="#app-header-left">
      <span class="ah-title">Dashboard</span>
      <span v-if="!loading" class="ah-subtitle">{{ projects.length }} active project{{ projects.length !== 1 ? 's' : '' }}</span>
    </Teleport>

    <div v-if="loading" class="loading">Loading…</div>

    <template v-else>
      <!-- Greeting -->
      <div class="greeting">
        <RouterLink to="/settings?tab=account" class="dashboard-avatar-link">
          <UserAvatar :user="auth.user" size="lg" class="dashboard-avatar" />
          <span class="dashboard-avatar-overlay"><AppIcon name="pencil" :size="18" /></span>
        </RouterLink>
        <div class="greeting-text-block">
          <h1 class="greeting-text">{{ greet.prefix }}, {{ greet.name }}</h1>
          <p class="greeting-msg">{{ greet.message }}</p>
        </div>
      </div>

      <div class="dash-grid">
        <!-- Active Projects -->
        <section class="card">
          <div class="card-header">
            <h2 class="card-title">Active Projects</h2>
            <RouterLink to="/projects" class="card-link">View all</RouterLink>
          </div>
          <div v-if="projects.length === 0" class="empty">No active projects.</div>
          <ul v-else class="project-list">
            <li v-for="p in projects" :key="p.id" class="project-row">
              <RouterLink :to="`/projects/${p.id}`" class="project-row-link">
                <div class="project-thumb">
                  <img v-if="p.logo_path" :src="p.logo_path" class="project-logo" :alt="p.name" />
                  <span v-else class="project-key-box">{{ p.key }}</span>
                </div>
                <div class="project-info">
                  <span class="project-name">{{ p.name }}</span>
                  <span class="project-meta">{{ p.open_issue_count }} open · {{ p.issue_count }} total</span>
                </div>
              </RouterLink>
              <div class="project-right">
                <div v-if="p.active_issue_count > 0" class="dash-progress">
                  <div class="dash-progress-bar"><div class="dash-progress-fill" :style="{ width: `${Math.round((p.done_issue_count / p.active_issue_count) * 100)}%` }"></div></div>
                  <span class="dash-progress-label">{{ p.done_issue_count }}/{{ p.active_issue_count }}</span>
                </div>
                <span v-if="p.last_activity" class="project-activity">{{ relativeTime(p.last_activity) }}</span>
              </div>
            </li>
          </ul>
        </section>

        <!-- Recent Activity -->
        <section class="card">
          <div class="card-header">
            <h2 class="card-title">Recent Issues</h2>
          </div>
          <div v-if="recent.length === 0" class="empty">No recent activity.</div>
          <ul v-else class="issue-list">
            <li v-for="i in recent" :key="i.id">
              <RouterLink :to="`/projects/${i.project_id}/issues/${i.id}`" class="issue-row">
                <span class="issue-key">{{ i.issue_key }}</span>
                <span class="issue-type-icon" v-html="TYPE_SVGS[i.type] ?? ''"></span>
                <span class="issue-title">{{ i.title }}</span>
                <span class="issue-status">
                  <StatusDot :status="i.status" />
                  {{ statusLabel(i.status) }}
                </span>
                <span class="issue-priority" :style="{ color: priorityColor(i.priority) }">
                  <AppIcon :name="priorityIcon(i.priority)" :size="12" />
                  {{ priorityLabel(i.priority) }}
                </span>
                <span class="issue-editor">{{ i.last_changed_by_name || '—' }}</span>
                <span class="issue-time">{{ fmtRelative(i.updated_at) }}</span>
              </RouterLink>
            </li>
          </ul>
        </section>
      </div>

      <AppFooter />
    </template>
</template>

<style scoped>
.loading { color: var(--text-muted); padding: 2rem 0; }

/* ── Greeting ─────────────────────────────────────────────────────────────── */
.greeting {
  display: flex; align-items: center; gap: 1rem;
  margin-bottom: 1.25rem;
}
.dashboard-avatar-link {
  position: relative; display: inline-flex; flex-shrink: 0;
  border-radius: 50%; cursor: pointer; text-decoration: none;
}
.dashboard-avatar {
  width: 50px !important; height: 50px !important; font-size: 14px !important;
  outline: 1.5px solid rgba(0, 0, 0, 0.18);
  outline-offset: 0;
  box-shadow:
    inset 0 3px 6px rgba(0, 0, 0, 0.22),
    inset 0 1px 2px rgba(0, 0, 0, 0.12);
}
.dashboard-avatar-overlay {
  position: absolute; inset: 0; border-radius: 50%;
  background: rgba(0, 0, 0, .35); color: #fff;
  display: flex; align-items: center; justify-content: center;
  opacity: 0; transition: opacity .15s; pointer-events: none;
}
.dashboard-avatar-link:hover .dashboard-avatar-overlay { opacity: 1; }
.greeting-text { font-size: 20px; font-weight: 700; color: var(--text); line-height: 1.3; margin: 0; }
.greeting-msg { font-size: 13px; color: var(--text-muted); margin: .25rem 0 0; }

/* ── Grid ─────────────────────────────────────────────────────────────────── */
.dash-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 1.25rem;
}

.card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow);
  overflow: hidden;
}
.card-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 1rem 1.25rem;
  border-bottom: 1px solid var(--border);
}
.card-title { font-size: 13px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.card-link   { font-size: 12px; color: var(--bp-blue); }
.empty       { padding: 1.25rem; font-size: 13px; color: var(--text-muted); }

/* ── Projects ─────────────────────────────────────────────────────────────── */
.project-list { list-style: none; }
.project-row  {
  display: flex; align-items: center; justify-content: space-between;
  padding: .65rem 1.25rem;
  border-bottom: 1px solid var(--border);
  gap: .75rem;
}
.project-row:last-child { border-bottom: none; }
.project-row-link {
  display: flex; align-items: center; gap: .75rem; min-width: 0;
  text-decoration: none; color: inherit; flex: 1;
}
.project-row-link:hover .project-name { color: var(--bp-blue); }
.project-thumb { width: 36px; height: 36px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; }
.project-logo { width: 36px; height: 36px; object-fit: contain; border-radius: 5px; }
.project-key-box {
  font-size: 9px; font-weight: 700; letter-spacing: .04em; font-family: monospace;
  background: var(--bg); color: var(--text-muted); border: 1px solid var(--border);
  border-radius: 5px; padding: .2rem .35rem; white-space: nowrap;
  width: 36px; height: 36px; display: flex; align-items: center; justify-content: center;
}
.project-info { min-width: 0; }
.project-name { font-size: 14px; font-weight: 500; color: var(--text); display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.project-meta { font-size: 11px; color: var(--text-muted); display: block; margin-top: .1rem; }
.project-right { display: flex; flex-direction: column; align-items: flex-end; gap: .2rem; flex-shrink: 0; }
.project-activity { font-size: 10px; color: var(--text-muted); opacity: .7; }
.dash-progress { display: flex; align-items: center; gap: .4rem; }
.dash-progress-bar { width: 48px; height: 3px; background: var(--border); border-radius: 2px; overflow: hidden; }
.dash-progress-fill { height: 100%; background: var(--bp-blue); border-radius: 2px; }
.dash-progress-label { font-size: 10px; color: var(--text-muted); font-weight: 600; font-variant-numeric: tabular-nums; }

/* ── Issues ───────────────────────────────────────────────────────────────── */
.issue-list { list-style: none; }
.issue-row {
  display: grid;
  grid-template-columns: 82px 18px 1fr 80px 70px 50px 55px;
  align-items: center;
  padding: .5rem 1.25rem;
  border-bottom: 1px solid var(--border);
  gap: .4rem;
  text-decoration: none; color: inherit;
  transition: background .1s;
}
.issue-row:hover { background: var(--bg); }
.issue-list li:last-child .issue-row { border-bottom: none; }
.issue-key {
  font-size: 10px; font-weight: 700; letter-spacing: .04em;
  color: var(--bp-blue-dark); background: var(--bp-blue-pale);
  padding: .1rem .4rem; border-radius: 3px;
  white-space: nowrap; text-align: center;
  font-variant-numeric: tabular-nums;
}
.issue-type-icon { display: flex; align-items: center; justify-content: center; color: var(--text-muted); }
.issue-title {
  font-size: 13px; font-weight: 500; color: var(--text);
  min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.issue-status {
  display: flex; align-items: center; gap: .3rem;
  font-size: 11px; color: var(--text-muted);
}
.issue-priority {
  display: flex; align-items: center; gap: .2rem;
  font-size: 11px;
}
.issue-editor {
  font-size: 10px; color: var(--text-muted); text-align: right;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.issue-time {
  font-size: 10px; color: var(--text-muted); text-align: right;
  white-space: nowrap; font-variant-numeric: tabular-nums;
}
</style>
