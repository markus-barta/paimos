<script setup lang="ts">
import { ref, watch, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import { useSearchStore } from '@/stores/search'
import AppIcon from '@/components/AppIcon.vue'
import StatusDot from '@/components/StatusDot.vue'

interface SearchIssue {
  id: number
  issue_key: string
  title: string
  type: string
  status: string
  priority: string
  project_id: number | null
  project_key: string
  assignee_username: string | null
}

interface SearchResults {
  issues: SearchIssue[]
  projects: { id: number; name: string; key: string }[]
  has_more: boolean
}

const props = defineProps<{
  visible: boolean
}>()

const emit = defineEmits<{
  close: []
  navigate: [path: string]
}>()

const router = useRouter()
const search = useSearchStore()
const results = ref<SearchResults | null>(null)
const loading = ref(false)
const activeIndex = ref(-1)
const paletteRef = ref<HTMLElement | null>(null)

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let lastFetchedQuery = ''
const cache = new Map<string, SearchResults>()

watch(() => search.query, (q) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  activeIndex.value = -1
  const trimmed = q.trim()
  if (trimmed.length < 2) {
    results.value = null
    return
  }
  // Show cached result immediately
  if (cache.has(trimmed)) {
    results.value = cache.get(trimmed)!
  }
  debounceTimer = setTimeout(() => fetchResults(trimmed), 150)
})

async function fetchResults(q: string) {
  if (q === lastFetchedQuery && results.value) return
  loading.value = true
  try {
    const data = await api.get<SearchResults>(`/search?q=${encodeURIComponent(q)}&limit=10`)
    results.value = data
    cache.set(q, data)
    lastFetchedQuery = q
  } catch { /* silent */ }
  finally { loading.value = false }
}

const items = computed<SearchIssue[]>(() => results.value?.issues ?? [])

const directMatch = computed<SearchIssue | null>(() => {
  const q = search.query.trim().toUpperCase()
  if (!q || !items.value.length) return null
  return items.value.find(i => i.issue_key?.toUpperCase() === q) ?? null
})

const otherItems = computed(() => {
  const dm = directMatch.value
  if (!dm) return items.value
  return items.value.filter(i => i.id !== dm.id)
})

function navigateToIssue(issue: SearchIssue) {
  if (issue.project_id) {
    emit('navigate', `/projects/${issue.project_id}/issues/${issue.id}`)
  }
  emit('close')
}

function onKeydown(e: KeyboardEvent) {
  if (!props.visible || !results.value) return

  const total = items.value.length
  if (!total) {
    if (e.key === 'Enter') {
      // Navigate to search results page
      emit('navigate', `/issues`)
      emit('close')
      e.preventDefault()
    }
    return
  }

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    activeIndex.value = Math.min(activeIndex.value + 1, total - 1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    activeIndex.value = Math.max(activeIndex.value - 1, -1)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    if (activeIndex.value >= 0 && activeIndex.value < total) {
      navigateToIssue(items.value[activeIndex.value])
    } else if (directMatch.value) {
      navigateToIssue(directMatch.value)
    } else {
      // Navigate to full results
      emit('navigate', `/issues`)
      emit('close')
    }
  } else if (e.key === 'Escape') {
    emit('close')
  }
}

// Exposed so AppHeader can call directly from its keydown handler
defineExpose({ handleKeydown: onKeydown })

// Scroll active item into view
watch(activeIndex, () => {
  nextTick(() => {
    const el = paletteRef.value?.querySelector('.sp-item--active')
    el?.scrollIntoView({ block: 'nearest' })
  })
})
</script>

<template>
  <div v-if="visible && (loading || (results && items.length > 0))" ref="paletteRef" class="search-palette">
    <div v-if="loading && !results" class="sp-loading">Searching…</div>
    <template v-else-if="results">
      <!-- Direct match — rich row -->
      <div v-if="directMatch" class="sp-item sp-item--direct" :class="{ 'sp-item--active': activeIndex === items.indexOf(directMatch) }"
        @mousedown.prevent="navigateToIssue(directMatch)" @mouseenter="activeIndex = items.indexOf(directMatch)">
        <div class="sp-item-top">
          <span class="sp-key">{{ directMatch.issue_key }}</span>
          <StatusDot :status="directMatch.status" />
          <span class="sp-status">{{ directMatch.status }}</span>
          <span v-if="directMatch.assignee_username" class="sp-assignee">{{ directMatch.assignee_username }}</span>
        </div>
        <div class="sp-item-title">{{ directMatch.title }}</div>
      </div>

      <!-- Separator if both sections -->
      <div v-if="directMatch && otherItems.length" class="sp-separator" />

      <!-- Other results -->
      <div v-for="(item, idx) in otherItems" :key="item.id"
        class="sp-item" :class="{ 'sp-item--active': activeIndex === items.indexOf(item) }"
        @mousedown.prevent="navigateToIssue(item)"
        @mouseenter="activeIndex = items.indexOf(item)">
        <span class="sp-key">{{ item.issue_key }}</span>
        <StatusDot :status="item.status" />
        <span class="sp-title-compact">{{ item.title }}</span>
      </div>

      <div v-if="results.has_more" class="sp-more">
        <span class="sp-more-text">More results — press Enter for full search</span>
      </div>
    </template>
  </div>
</template>

<style scoped>
.search-palette {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  z-index: 9999;
  margin-top: 4px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 32px rgba(0,0,0,.15);
  max-height: 400px;
  overflow-y: auto;
  padding: .25rem 0;
}
.sp-loading {
  padding: .75rem 1rem;
  font-size: 12px;
  color: var(--text-muted);
}
.sp-item {
  display: flex;
  align-items: center;
  gap: .4rem;
  padding: .45rem .75rem;
  cursor: pointer;
  font-size: 13px;
  transition: background .08s;
}
.sp-item:hover, .sp-item--active { background: var(--surface-2); }
.sp-item--direct {
  flex-direction: column;
  align-items: stretch;
  gap: .2rem;
  padding: .6rem .75rem;
}
.sp-item-top {
  display: flex;
  align-items: center;
  gap: .4rem;
}
.sp-item-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--text);
  line-height: 1.35;
}
.sp-key {
  font-family: monospace;
  font-size: 12px;
  font-weight: 700;
  color: var(--bp-blue);
  white-space: nowrap;
  flex-shrink: 0;
}
.sp-status {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}
.sp-assignee {
  font-size: 11px;
  color: var(--text-muted);
  margin-left: auto;
}
.sp-title-compact {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
}
.sp-separator {
  height: 1px;
  background: var(--border);
  margin: .2rem .75rem;
}
.sp-more {
  padding: .4rem .75rem;
  text-align: center;
}
.sp-more-text {
  font-size: 11px;
  color: var(--text-muted);
}
</style>
