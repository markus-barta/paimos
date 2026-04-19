<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '@/api/client'

interface PortalProject {
  id: number
  key: string
  name: string
  description: string
  status: string
  logo_path: string
  issue_count: number
  done_count: number
}

interface PortalSummary {
  total_issues: number
  by_status: Record<string, number>
  total_estimate_eur: number | null
  total_ar_eur: number | null
}

const projects = ref<PortalProject[]>([])
const summaries = ref<Record<number, PortalSummary>>({})
const loading = ref(true)

onMounted(async () => {
  try {
    projects.value = await api.get<PortalProject[]>('/portal/projects')
    // Fetch summaries for each project
    await Promise.all(projects.value.map(async (p) => {
      try {
        summaries.value[p.id] = await api.get<PortalSummary>(`/portal/projects/${p.id}/summary`)
      } catch { /* ignore */ }
    }))
  } catch { /* ignore */ }
  loading.value = false
})

function fmtEur(v: number | null | undefined): string {
  if (v == null) return '-'
  return new Intl.NumberFormat('de-DE', { style: 'currency', currency: 'EUR' }).format(v)
}
</script>

<template>
  <div class="portal-dashboard">
    <h1 class="page-title">{{ $t('portal.yourProjects') }}</h1>

    <div v-if="loading" class="loading">{{ $t('portal.loading') }}</div>

    <div v-else-if="projects.length === 0" class="empty-state">
      {{ $t('portal.noProjects') }}
    </div>

    <div v-else class="project-grid">
      <router-link
        v-for="p in projects" :key="p.id"
        :to="`/portal/projects/${p.id}`"
        class="project-card"
      >
        <div class="card-header">
          <img v-if="p.logo_path" :src="p.logo_path" class="card-logo" />
          <div class="card-key">{{ p.key }}</div>
        </div>
        <h2 class="card-name">{{ p.name }}</h2>
        <p v-if="p.description" class="card-desc">{{ p.description }}</p>

        <div class="card-stats">
          <div class="stat">
            <span class="stat-value">{{ p.issue_count }}</span>
            <span class="stat-label">{{ $t('portal.issues') }}</span>
          </div>
          <div class="stat">
            <span class="stat-value">{{ p.done_count }}</span>
            <span class="stat-label">{{ $t('portal.done') }}</span>
          </div>
          <div class="stat" v-if="summaries[p.id]?.total_ar_eur != null">
            <span class="stat-value stat-value--money">{{ fmtEur(summaries[p.id]?.total_ar_eur) }}</span>
            <span class="stat-label">AR Cost</span>
          </div>
        </div>

        <div class="card-progress" v-if="p.issue_count > 0">
          <div class="progress-bar">
            <div class="progress-fill" :style="{ width: Math.round(p.done_count / p.issue_count * 100) + '%' }" />
          </div>
          <span class="progress-pct">{{ Math.round(p.done_count / p.issue_count * 100) }}%</span>
        </div>
      </router-link>
    </div>
  </div>
</template>

<style scoped>
.page-title {
  font-size: 20px;
  font-weight: 700;
  margin-bottom: 1.5rem;
}
.loading, .empty-state {
  color: var(--text-muted);
  padding: 3rem;
  text-align: center;
}
.project-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 1rem;
}
.project-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.25rem;
  text-decoration: none;
  color: inherit;
  transition: box-shadow .15s, border-color .15s;
  display: flex;
  flex-direction: column;
  gap: .75rem;
}
.project-card:hover {
  border-color: var(--bp-blue);
  box-shadow: var(--shadow-md);
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
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
}
.card-name {
  font-size: 16px;
  font-weight: 600;
}
.card-desc {
  font-size: 13px;
  color: var(--text-muted);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.card-stats {
  display: flex;
  gap: 1.5rem;
  margin-top: .25rem;
}
.stat {
  display: flex;
  flex-direction: column;
  gap: .1rem;
}
.stat-value {
  font-size: 18px;
  font-weight: 700;
  color: var(--text);
}
.stat-value--money {
  font-size: 14px;
}
.stat-label {
  font-size: 11px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: .04em;
}
.card-progress {
  display: flex;
  align-items: center;
  gap: .5rem;
}
.progress-bar {
  flex: 1;
  height: 6px;
  background: var(--bg);
  border-radius: 3px;
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  background: var(--bp-blue);
  border-radius: 3px;
  transition: width .3s;
}
.progress-pct {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  min-width: 36px;
  text-align: right;
}
</style>
