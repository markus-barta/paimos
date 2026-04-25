<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import type { IssueAnchor } from '@/types'

const props = defineProps<{
  issueId: number
}>()

const loading = ref(true)
const error = ref('')
const anchors = ref<IssueAnchor[]>([])

const grouped = computed(() => {
  const byRepo = new Map<string, { repoLabel: string; repoUrl: string; anchors: IssueAnchor[] }>()
  for (const a of anchors.value) {
    const key = `${a.repo_id}:${a.repo_label}`
    if (!byRepo.has(key)) {
      byRepo.set(key, { repoLabel: a.repo_label || 'Repo', repoUrl: a.repo_url, anchors: [] })
    }
    byRepo.get(key)!.anchors.push(a)
  }
  return [...byRepo.values()]
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    anchors.value = await api.get<IssueAnchor[]>(`/issues/${props.issueId}/anchors`)
  } catch (e) {
    error.value = errMsg(e, 'Failed to load anchors.')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="anchors-section">
    <div class="section-title-row">
      <h3 class="section-title">
        Anchors
        <span v-if="anchors.length" class="section-count">{{ anchors.length }}</span>
      </h3>
      <button class="btn btn-ghost btn-sm" @click="load" :disabled="loading">Refresh</button>
    </div>

    <div v-if="loading" class="anchors-card anchors-empty">Loading anchors…</div>
    <div v-else-if="error" class="anchors-card anchors-error">{{ error }}</div>
    <div v-else-if="!anchors.length" class="anchors-card anchors-empty">
      No anchors yet. They appear here after a repo uploads its `.pmo/anchors.json`.
    </div>

    <div v-else class="anchors-groups">
      <div v-for="group in grouped" :key="group.repoLabel" class="anchors-card">
        <div class="repo-header">
          <div>
            <div class="repo-label">{{ group.repoLabel }}</div>
            <a v-if="group.repoUrl" :href="group.repoUrl" target="_blank" rel="noopener" class="repo-url">{{ group.repoUrl }}</a>
          </div>
          <span class="repo-count">{{ group.anchors.length }} anchor{{ group.anchors.length === 1 ? '' : 's' }}</span>
        </div>

        <div class="anchor-list">
          <div v-for="anchor in group.anchors" :key="anchor.id" :class="['anchor-row', { 'anchor-row--stale': anchor.stale }]">
            <div class="anchor-main">
              <div class="anchor-path-row">
                <a v-if="anchor.deep_link" :href="anchor.deep_link" target="_blank" rel="noopener" class="anchor-path">
                  {{ anchor.file_path }}:{{ anchor.line }}
                </a>
                <span v-else class="anchor-path">{{ anchor.file_path }}:{{ anchor.line }}</span>
                <span class="anchor-confidence">{{ anchor.confidence }}</span>
                <span v-if="anchor.stale" class="anchor-state">stale</span>
              </div>
              <div v-if="anchor.label || anchor.repo_revision" class="anchor-meta">
                <span v-if="anchor.label">{{ anchor.label }}</span>
                <span v-if="anchor.repo_revision" class="anchor-revision">
                  <AppIcon name="git-branch" :size="11" />
                  {{ anchor.repo_revision.slice(0, 12) }}
                </span>
              </div>
            </div>
            <AppIcon name="external-link" :size="14" class="anchor-link-icon" />
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.anchors-section { margin-top: 1.25rem; display: flex; flex-direction: column; gap: .9rem; }
.section-title-row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
.section-title { font-size: 18px; font-weight: 700; color: var(--text); display: flex; align-items: center; gap: .5rem; }
.section-count { font-size: 12px; color: var(--text-muted); background: var(--bg); border: 1px solid var(--border); border-radius: 999px; padding: .1rem .45rem; }
.anchors-groups { display: flex; flex-direction: column; gap: .9rem; }
.anchors-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow);
  padding: 1rem 1.1rem;
}
.anchors-empty { color: var(--text-muted); font-size: 13px; }
.anchors-error { color: #b42318; background: #fef3f2; border-color: #fecdca; }
.repo-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; margin-bottom: .9rem; }
.repo-label { font-size: 14px; font-weight: 700; color: var(--text); }
.repo-url { font-size: 12px; color: var(--text-muted); text-decoration: none; word-break: break-all; }
.repo-url:hover { color: var(--bp-blue-dark); text-decoration: underline; }
.repo-count { font-size: 12px; color: var(--text-muted); }
.anchor-list { display: flex; flex-direction: column; gap: .55rem; }
.anchor-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .8rem;
  padding: .75rem .8rem;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
}
.anchor-row--stale { border-style: dashed; }
.anchor-main { min-width: 0; flex: 1; }
.anchor-path-row { display: flex; align-items: center; gap: .5rem; flex-wrap: wrap; }
.anchor-path { color: var(--text); font-size: 13px; font-weight: 600; text-decoration: none; word-break: break-all; }
.anchor-path:hover { color: var(--bp-blue-dark); text-decoration: underline; }
.anchor-confidence, .anchor-state {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .06em;
  border-radius: 999px;
  padding: .15rem .38rem;
}
.anchor-confidence { background: #eff6ff; color: #1d4ed8; }
.anchor-state { background: #fff7ed; color: #c2410c; }
.anchor-meta { margin-top: .3rem; display: flex; align-items: center; gap: .65rem; flex-wrap: wrap; color: var(--text-muted); font-size: 12px; }
.anchor-revision { display: inline-flex; align-items: center; gap: .25rem; }
.anchor-link-icon { color: var(--text-muted); flex-shrink: 0; }
</style>
