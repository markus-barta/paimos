<script setup lang="ts">
import { ref, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import CreateIssueModal from '@/components/CreateIssueModal.vue'
import { useNewIssueStore } from '@/stores/newIssue'
import { useRecentProjects } from '@/composables/useRecentProjects'
import { provideIssueContext } from '@/composables/useIssueContext'
import { api } from '@/api/client'
import type { User, Tag, Project, Sprint, Issue } from '@/types'

// Global fallback "New Issue" modal — picks up newIssueStore.trigger on routes
// that don't mount an IssueList (Dashboard, Sprint Board, Settings, …).
// Routes with their own listener handle creation in-place.
const ROUTES_WITH_LOCAL_LISTENER = [
  /^\/projects\/\d+$/,
  /^\/projects\/\d+\/issues\/\d+$/,
  /^\/issues$/,
]

const route = useRoute()
const router = useRouter()
const newIssueStore = useNewIssueStore()
const { recentProjects, loadRecentProjects } = useRecentProjects()

const users     = ref<User[]>([])
const allTags   = ref<Tag[]>([])
const projects  = ref<Project[]>([])
const sprints   = ref<Sprint[]>([])
const costUnits = ref<string[]>([])
const releases  = ref<string[]>([])

provideIssueContext({ users, allTags, costUnits, releases, projects, sprints })

const metaLoaded = ref(false)
async function ensureMeta() {
  if (metaLoaded.value) return
  const [u, p, t, cu, rel] = await Promise.all([
    api.get<User[]>('/users').catch(() => [] as User[]),
    api.get<Project[]>('/projects').catch(() => [] as Project[]),
    api.get<Tag[]>('/tags').catch(() => [] as Tag[]),
    api.get<string[]>('/cost-units').catch(() => [] as string[]),
    api.get<string[]>('/releases').catch(() => [] as string[]),
  ])
  users.value     = u
  projects.value  = p
  allTags.value   = t
  costUnits.value = cu
  releases.value  = rel
  metaLoaded.value = true
}

const show = ref(false)
const issuesForProject = ref<Issue[]>([])
const initialProjectId = ref<number | null>(null)
const modalRef = ref<InstanceType<typeof CreateIssueModal> | null>(null)

async function loadIssuesForProject(pid: number | null) {
  if (!pid) { issuesForProject.value = []; return }
  try {
    issuesForProject.value = await api.get<Issue[]>(`/projects/${pid}/issues?fields=list`)
  } catch {
    issuesForProject.value = []
  }
}

watch(() => newIssueStore.trigger, async (v) => {
  if (!v) return
  if (ROUTES_WITH_LOCAL_LISTENER.some(re => re.test(route.path))) return

  await ensureMeta()
  if (!recentProjects.value.length) await loadRecentProjects()

  const ctx = newIssueStore.context
  const preselect = ctx.projectId ?? recentProjects.value[0]?.id ?? null
  initialProjectId.value = preselect
  await loadIssuesForProject(preselect)

  show.value = true
  await nextTick()
  modalRef.value?.openCreate(undefined, ctx.type, ctx.parentId)
})

function onProjectChanged(pid: number | null) {
  loadIssuesForProject(pid)
}

function onClose() { show.value = false }

function onCreated(issue: Issue) {
  show.value = false
  const target = `/projects/${issue.project_id}/issues/${issue.id}`
  if (route.path !== target) router.push(target)
}

function onCostUnitAdded(v: string) {
  if (!costUnits.value.includes(v)) costUnits.value = [...costUnits.value, v]
}
function onReleaseAdded(v: string) {
  if (!releases.value.includes(v)) releases.value = [...releases.value, v]
}
</script>

<template>
  <CreateIssueModal
    ref="modalRef"
    :open="show"
    :issues="issuesForProject"
    :project-all-issues="issuesForProject"
    :initial-project-id="initialProjectId"
    :derived-create-type="null"
    @close="onClose"
    @created="onCreated"
    @project-changed="onProjectChanged"
    @cost-unit-added="onCostUnitAdded"
    @release-added="onReleaseAdded"
  />
</template>
