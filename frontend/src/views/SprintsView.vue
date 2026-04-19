<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import IssueList from '@/components/IssueList.vue'
import AppFooter from '@/components/AppFooter.vue'
import { api, errMsg } from '@/api/client'
import { provideIssueContext } from '@/composables/useIssueContext'
import type { Issue, Project, Tag, Sprint, User } from '@/types'

interface IssueEnvelope {
  issues: Issue[]
  total: number
  offset: number
  limit: number
}

const PAGE = 200

const issues      = ref<Issue[]>([])
const total       = ref(0)
const users       = ref<User[]>([])
const projects    = ref<Project[]>([])
const allTags     = ref<Tag[]>([])
const sprints     = ref<Sprint[]>([])
const costUnits   = ref<string[]>([])
const releases    = ref<string[]>([])

provideIssueContext({ users, allTags, costUnits, releases, projects, sprints })

const loading     = ref(true)
const loadingMore = ref(false)
const error       = ref('')

const issueListRef = ref<InstanceType<typeof IssueList> | null>(null)

const remaining = computed(() => Math.max(0, total.value - issues.value.length))
const hasMore   = computed(() => remaining.value > 0)

async function fetchSprints(limit: number, replace = false) {
  const offset = replace ? 0 : issues.value.length
  try {
    const data = await api.get<IssueEnvelope>(`/issues?fields=list&type=sprint&limit=${limit}&offset=${offset}`)
    if (replace) {
      issues.value = data.issues
    } else {
      issues.value = [...issues.value, ...data.issues]
    }
    total.value = data.total
  } catch (e: unknown) {
    error.value = errMsg(e, 'Failed to load sprints.')
  }
}

async function load() {
  loading.value = true
  error.value   = ''
  const [, u, p, tags, spr] = await Promise.all([
    fetchSprints(PAGE, true),
    api.get<User[]>('/users'),
    api.get<Project[]>('/projects'),
    api.get<Tag[]>('/tags').catch(() => []),
    api.get<Sprint[]>('/sprints').catch(() => []),
  ])
  users.value    = u
  projects.value = p
  allTags.value  = tags
  sprints.value  = spr
  loading.value  = false
}

async function loadMore() {
  loadingMore.value = true
  await fetchSprints(PAGE)
  loadingMore.value = false
}

onMounted(load)

function onCreated(issue: Issue) { issues.value.push(issue); total.value++ }
function onUpdated(issue: Issue) {
  const idx = issues.value.findIndex(i => i.id === issue.id)
  if (idx >= 0) issues.value[idx] = issue
}
function onDeleted(id: number) {
  issues.value = issues.value.filter(i => i.id !== id)
  total.value = Math.max(0, total.value - 1)
}
</script>

<template>
  <div>
     <Teleport defer to="#app-header-left">
       <span class="ah-title">Sprints</span>
       <span v-if="!loading" class="ah-subtitle">
         {{ total.toLocaleString() }} sprint{{ total !== 1 ? 's' : '' }} across all projects
       </span>
     </Teleport>

    <div v-if="loading" class="loading">Loading…</div>
    <div v-else-if="error" class="load-error">{{ error }}</div>

    <template v-else>
      <IssueList
        ref="issueListRef"
        :issues="issues"
        @created="onCreated"
        @updated="onUpdated"
        @deleted="onDeleted"
      />

      <div v-if="hasMore" class="load-more">
        <span class="load-more-label">{{ remaining.toLocaleString() }} more sprint{{ remaining !== 1 ? 's' : '' }}</span>
        <button class="btn btn-ghost btn-sm" :disabled="loadingMore" @click="loadMore">
          {{ loadingMore ? 'Loading…' : 'Load more' }}
        </button>
      </div>
    </template>

    <AppFooter />
  </div>
</template>

<style scoped>
.sprints-header {
  display: flex; align-items: baseline; gap: .75rem;
  margin-bottom: 1.5rem;
}
.sprints-title {
  font-size: 22px; font-weight: 800; color: var(--text);
  letter-spacing: -.02em; line-height: 1; margin: 0;
}
.sprints-subtitle {
  font-size: 13px; color: var(--text-muted);
}
.loading, .load-error {
  color: var(--text-muted); padding: 2rem 0; font-size: 13px;
}
.load-error { color: #c0392b; }
.load-more {
  display: flex; align-items: center; gap: 1rem; flex-wrap: wrap;
  margin-top: 1.25rem; padding: .75rem 1rem;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px;
}
.load-more-label { font-size: 13px; color: var(--text-muted); }
</style>
