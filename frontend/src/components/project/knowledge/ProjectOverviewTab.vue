<script setup lang="ts">
// PAI-339 — Overview tab. README-like project description, related
// projects list (sourced from the same /related-projects convenience
// endpoint as the Knowledge tab), and current-state callouts derived
// from the issues already loaded by ProjectDetailView (latest
// release-type issue, active epics, recent terminal-state tickets).
//
// Lightweight by design: no extra API calls beyond /related-projects;
// everything else is computed from props the parent already has. The
// Overview is "the at-a-glance card" — drilldowns happen in Issues /
// Knowledge tabs.

import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import LoadingText from '@/components/LoadingText.vue'
import { useMarkdown } from '@/composables/useMarkdown'
import { listKnowledgeEntries } from '@/services/projectKnowledge'
import type { Issue, KnowledgeEntry, Project } from '@/types'

const props = defineProps<{
  project: Project
  issues: Issue[]
}>()

const description = computed(() => props.project.description ?? '')
const descriptionEnabled = ref(true)
const { html: descriptionHtml } = useMarkdown(description, descriptionEnabled)

// ── related projects (load on mount) ───────────────────────────

const relatedProjects = ref<KnowledgeEntry[]>([])
const relatedLoading = ref(true)
const relatedError = ref('')

async function loadRelated() {
  relatedLoading.value = true
  relatedError.value = ''
  try {
    relatedProjects.value = await listKnowledgeEntries(props.project.id, 'related_project')
  } catch (e) {
    relatedError.value = errMsg(e, 'Failed to load related projects.')
  } finally {
    relatedLoading.value = false
  }
}

watch(
  () => props.project.id,
  () => {
    void loadRelated()
  },
)

// ── current-state callouts (derived from props.issues) ─────────

// Latest release: pick the most-recently updated issue with type
// 'release' that's reached a terminal state. Falls back to "no
// recent release" when nothing matches — preferable to picking a
// half-shipped release and over-claiming.
const latestRelease = computed<Issue | null>(() => {
  const releases = props.issues
    .filter((i) => i.type === 'release')
    .filter((i) => i.status === 'done' || i.status === 'delivered' || i.status === 'accepted')
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
  return releases[0] ?? null
})

// Active epics: type=epic, not in a terminal state. Capped at 5
// for the overview — the full list lives in the Issues tab's
// Epics view.
const activeEpics = computed<Issue[]>(() => {
  return props.issues
    .filter((i) => i.type === 'epic')
    .filter((i) => !TERMINAL_STATUSES.has(i.status))
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
    .slice(0, 5)
})

// Recent terminal-state tickets: most-recently updated tickets that
// landed in done/delivered/accepted/invoiced. Capped at 5; this is
// the "what shipped lately" callout.
const recentlyShipped = computed<Issue[]>(() => {
  return props.issues
    .filter((i) => i.type === 'ticket' || i.type === 'task')
    .filter((i) => TERMINAL_STATUSES.has(i.status))
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
    .slice(0, 5)
})

const TERMINAL_STATUSES = new Set(['done', 'delivered', 'accepted', 'invoiced'])

function formatDate(s: string): string {
  // Backend returns "YYYY-MM-DD HH:MM:SS" UTC. Convert to local
  // YYYY-MM-DD which matches the rest of the app's display style.
  if (!s) return ''
  return s.slice(0, 10)
}

onMounted(() => {
  void loadRelated()
})
</script>

<template>
  <div class="pot-root">
    <section class="pot-card pot-description">
      <header class="pot-card-head">
        <h3>About this project</h3>
      </header>
      <div v-if="description" class="pot-md" v-html="descriptionHtml" />
      <div v-else class="pot-empty">
        No description yet — add one in <strong>Settings</strong> to give agents and
        teammates a quick sense of what this project is for.
      </div>
    </section>

    <div class="pot-grid">
      <!-- Current state callouts -->
      <section class="pot-card">
        <header class="pot-card-head">
          <AppIcon name="package-check" :size="14" />
          <h3>Latest release</h3>
        </header>
        <RouterLink
          v-if="latestRelease"
          :to="`/projects/${project.id}/issues/${latestRelease.id}`"
          class="pot-callout"
        >
          <span class="pot-key">{{ latestRelease.issue_key }}</span>
          <span class="pot-callout-title">{{ latestRelease.title }}</span>
          <span class="pot-meta">{{ formatDate(latestRelease.updated_at) }}</span>
        </RouterLink>
        <div v-else class="pot-empty">No release issues completed yet.</div>
      </section>

      <section class="pot-card">
        <header class="pot-card-head">
          <AppIcon name="layers" :size="14" />
          <h3>Active epics</h3>
        </header>
        <ul v-if="activeEpics.length" class="pot-list">
          <li v-for="e in activeEpics" :key="e.id">
            <RouterLink :to="`/projects/${project.id}/issues/${e.id}`" class="pot-row">
              <span class="pot-key">{{ e.issue_key }}</span>
              <span class="pot-row-title">{{ e.title }}</span>
              <span class="pot-pill">{{ e.status }}</span>
            </RouterLink>
          </li>
        </ul>
        <div v-else class="pot-empty">No active epics.</div>
      </section>

      <section class="pot-card">
        <header class="pot-card-head">
          <AppIcon name="circle-check" :size="14" />
          <h3>Recently shipped</h3>
        </header>
        <ul v-if="recentlyShipped.length" class="pot-list">
          <li v-for="i in recentlyShipped" :key="i.id">
            <RouterLink :to="`/projects/${project.id}/issues/${i.id}`" class="pot-row">
              <span class="pot-key">{{ i.issue_key }}</span>
              <span class="pot-row-title">{{ i.title }}</span>
              <span class="pot-meta">{{ formatDate(i.updated_at) }}</span>
            </RouterLink>
          </li>
        </ul>
        <div v-else class="pot-empty">No tickets in a terminal state yet.</div>
      </section>

      <section class="pot-card">
        <header class="pot-card-head">
          <AppIcon name="link" :size="14" />
          <h3>Related projects</h3>
        </header>
        <LoadingText v-if="relatedLoading" class="pot-empty" label="Loading…" />
        <div v-else-if="relatedError" class="pot-empty">{{ relatedError }}</div>
        <ul v-else-if="relatedProjects.length" class="pot-list">
          <li v-for="rp in relatedProjects" :key="rp.slug">
            <a
              v-if="rp.metadata?.['instance_url']"
              :href="String(rp.metadata.instance_url)"
              target="_blank"
              rel="noopener noreferrer"
              class="pot-row"
            >
              <span class="pot-slug">{{ rp.slug }}</span>
              <span class="pot-row-title">{{ rp.title }}</span>
              <AppIcon name="external-link" :size="11" />
            </a>
            <div v-else class="pot-row">
              <span class="pot-slug">{{ rp.slug }}</span>
              <span class="pot-row-title">{{ rp.title }}</span>
            </div>
          </li>
        </ul>
        <div v-else class="pot-empty">
          None linked yet — add some from the <strong>Knowledge → Related projects</strong> tab.
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.pot-root { display: flex; flex-direction: column; gap: 1rem; }
.pot-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: .9rem 1rem;
  display: flex;
  flex-direction: column;
  gap: .55rem;
}
.pot-card-head { display: flex; align-items: center; gap: .35rem; }
.pot-card-head h3 { margin: 0; font-size: 13px; font-weight: 700; color: var(--text); text-transform: uppercase; letter-spacing: .04em; }
.pot-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1rem; }
.pot-md { font-size: 13px; line-height: 1.55; color: var(--text); }
.pot-md :deep(h1), .pot-md :deep(h2), .pot-md :deep(h3) { margin: .55rem 0 .3rem; font-weight: 700; }
.pot-md :deep(p) { margin: .35rem 0; }
.pot-md :deep(code) { background: var(--bg); padding: 0 .25rem; border-radius: 3px; font-size: 12px; }
.pot-md :deep(pre) { background: var(--bg); padding: .5rem .65rem; border-radius: 6px; overflow: auto; }
.pot-md :deep(ul), .pot-md :deep(ol) { padding-left: 1.4rem; margin: .35rem 0; }
.pot-empty { color: var(--text-muted); font-size: 12px; padding: .25rem 0; }

.pot-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: .25rem; }
.pot-row {
  display: flex;
  align-items: baseline;
  gap: .55rem;
  padding: .35rem .55rem;
  border-radius: 6px;
  text-decoration: none;
  color: var(--text);
  font-size: 12.5px;
}
.pot-row:hover { background: var(--bg); }
.pot-callout {
  display: flex;
  align-items: baseline;
  gap: .55rem;
  padding: .55rem .65rem;
  border-radius: 6px;
  text-decoration: none;
  color: var(--text);
  font-size: 13px;
  background: var(--bg);
  border: 1px solid var(--border);
}
.pot-callout-title { font-weight: 600; flex: 1; }
.pot-key, .pot-slug { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-weight: 700; font-size: 11.5px; color: var(--brand-blue); }
.pot-row-title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pot-meta { font-size: 11px; color: var(--text-muted); }
.pot-pill { background: var(--bg); border: 1px solid var(--border); border-radius: 999px; padding: 0 .5rem; font-size: 10px; color: var(--text-muted); line-height: 1.55; }

@media (max-width: 540px) {
  .pot-grid { grid-template-columns: 1fr; }
}
</style>
