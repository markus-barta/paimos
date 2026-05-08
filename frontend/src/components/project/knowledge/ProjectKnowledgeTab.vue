<script setup lang="ts">
// PAI-339 — top-level Knowledge tab. Hosts a sub-nav over the five
// knowledge categories (memory / runbooks / external systems /
// related projects / guidelines) and a single shared search box that
// filters whichever category panel is currently active.
//
// Counts in the sub-nav are populated lazily via the @count event
// each KnowledgeCategoryPanel emits when its data lands. Keeps the
// initial render fast — we don't fan out 5 GETs at mount; only the
// active panel fetches.

import { ref, computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import KnowledgeCategoryPanel from './KnowledgeCategoryPanel.vue'
import type { KnowledgeCategory } from '@/types'

const props = defineProps<{
  projectId: number
  canWrite: boolean
}>()

interface CategoryDef {
  key: KnowledgeCategory
  label: string
  icon: string
  blurb: string
}

const categories: CategoryDef[] = [
  { key: 'memory', label: 'Memory', icon: 'lightbulb', blurb: 'Declarative facts learned from incidents and project state.' },
  { key: 'runbook', label: 'Runbooks', icon: 'list-checks', blurb: 'Procedural step-by-step playbooks tied to agents.' },
  { key: 'external_system', label: 'External systems', icon: 'plug', blurb: 'Pointers to systems outside paimos this project depends on.' },
  { key: 'related_project', label: 'Related projects', icon: 'link', blurb: 'Cross-project references with instance URLs.' },
  { key: 'guideline', label: 'Guidelines', icon: 'shield-check', blurb: 'Lightweight normative rules surfaced in agent prompts.' },
]

const activeCategory = ref<KnowledgeCategory>('memory')
const search = ref('')
// Counts per category, populated by panel @count emits. Showing the
// number in the tab strip helps users find the populated category
// without click-by-click exploration.
const counts = ref<Record<KnowledgeCategory, number | null>>({
  memory: null,
  runbook: null,
  external_system: null,
  related_project: null,
  guideline: null,
})

function setActive(c: KnowledgeCategory) {
  activeCategory.value = c
}

function onCountUpdate(c: KnowledgeCategory, n: number) {
  counts.value = { ...counts.value, [c]: n }
}

const activeDef = computed(() => categories.find((c) => c.key === activeCategory.value)!)
</script>

<template>
  <div class="pkt-root">
    <!-- Sub-nav over the 5 categories. Mirrors the existing
         tab-btn vocabulary so the visual grammar matches the
         outer tab strip. -->
    <nav class="pkt-subnav" role="tablist">
      <button
        v-for="c in categories"
        :key="c.key"
        type="button"
        class="pkt-subtab"
        :class="{ active: activeCategory === c.key }"
        :data-label="c.label"
        role="tab"
        :aria-selected="activeCategory === c.key"
        @click="setActive(c.key)"
      >
        <AppIcon :name="c.icon" :size="13" />
        <span>{{ c.label }}</span>
        <span v-if="counts[c.key] !== null" class="pkt-count">{{ counts[c.key] }}</span>
      </button>
    </nav>

    <div class="pkt-toolbar">
      <div class="pkt-search">
        <AppIcon name="search" :size="13" />
        <input
          v-model="search"
          type="search"
          :placeholder="`Search ${activeDef.label.toLowerCase()} (title, slug, body)`"
          aria-label="Search knowledge entries"
        />
      </div>
      <p class="pkt-blurb">{{ activeDef.blurb }}</p>
    </div>

    <!-- Render every category lazily — only the active one mounts.
         Switching categories tears down the previous panel + its
         loaded data, which keeps memory bounded for big projects.
         The category panels self-load on mount. -->
    <KnowledgeCategoryPanel
      v-for="c in categories"
      v-show="c.key === activeCategory"
      :key="c.key"
      :project-id="projectId"
      :category="c.key"
      :search-query="search"
      :can-write="canWrite"
      @count="(n: number) => onCountUpdate(c.key, n)"
    />
  </div>
</template>

<style scoped>
.pkt-root { display: flex; flex-direction: column; gap: .85rem; padding: .25rem 0; }
.pkt-subnav { display: flex; gap: 0; flex-wrap: wrap; border-bottom: 1px solid var(--border); }
.pkt-subtab {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  padding: .45rem .8rem;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  font: inherit;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: pointer;
  margin-bottom: -1px;
  transition: color .15s, border-color .15s;
}
/* Pre-reserve bold width to prevent layout shift when toggling
   active state. Same trick as the outer .tab-btn. */
.pkt-subtab::after {
  content: attr(data-label);
  font-weight: 600;
  visibility: hidden;
  height: 0;
  display: block;
  overflow: hidden;
}
.pkt-subtab:hover { color: var(--text); }
.pkt-subtab.active {
  color: var(--bp-blue);
  border-bottom-color: var(--bp-blue);
  font-weight: 600;
}
.pkt-count {
  background: var(--surface-2, var(--bg-card));
  color: var(--text-muted);
  border-radius: 10px;
  padding: 0 .45rem;
  font-size: 10px;
  font-weight: 700;
  line-height: 1.55;
}
.pkt-subtab.active .pkt-count {
  background: var(--bp-blue);
  color: #fff;
}

.pkt-toolbar { display: flex; align-items: center; gap: .8rem; flex-wrap: wrap; }
.pkt-search {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  padding: .35rem .55rem;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  flex: 1 1 280px;
  min-width: 220px;
}
.pkt-search input {
  border: none;
  background: transparent;
  outline: none;
  font: inherit;
  font-size: 13px;
  color: var(--text);
  width: 100%;
}
.pkt-blurb {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
  flex: 1 1 200px;
}

@media (max-width: 540px) {
  .pkt-toolbar { flex-direction: column; align-items: stretch; }
  .pkt-blurb { order: -1; }
}
</style>
