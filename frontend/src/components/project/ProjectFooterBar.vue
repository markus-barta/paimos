<script setup lang="ts">
// PAI-356 — Project-page footer bar. Replaces PAI-339's top tab strip
// with a quiet bottom-anchored switcher. Three tabs (Issues / Overview
// / Knowledge), counters, sticky-inside-content (NOT viewport-fixed)
// so it cohabits with the global SidebarFooter without competing for
// the same chrome slot — see PAI-280.
//
// Active-state cue is a 2px green TOP-border so the cue points UP
// toward the content the tab governs (mirrors PAI-339's bottom-border
// inversion). Counters render as muted badges next to Issues and
// Knowledge; null counts hide the badge entirely so the bar stays
// uncluttered while data loads.

import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'

export type ProjectPrimaryTab = 'issues' | 'overview' | 'knowledge'

const props = defineProps<{
  modelValue: ProjectPrimaryTab
  // Server-supplied counts from GET /api/projects/:id `counts`.
  // null = not loaded yet (hide the badge); 0 = loaded but empty
  // (still show, in muted form, so the user knows the count is zero
  // rather than missing).
  openIssues?: number | null
  knowledgeEntries?: number | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: ProjectPrimaryTab): void
}>()

interface TabSpec {
  key: ProjectPrimaryTab
  label: string
  icon: string
  count?: number | null
}

const tabs = computed<TabSpec[]>(() => [
  { key: 'issues',    label: 'Issues',    icon: 'layout-list', count: props.openIssues       ?? null },
  { key: 'overview',  label: 'Overview',  icon: 'house',       count: null                          },
  { key: 'knowledge', label: 'Knowledge', icon: 'book-open',   count: props.knowledgeEntries ?? null },
])

function select(t: ProjectPrimaryTab) {
  if (t !== props.modelValue) emit('update:modelValue', t)
}
</script>

<template>
  <nav class="pfb" role="tablist" aria-label="Project section">
    <button
      v-for="t in tabs"
      :key="t.key"
      type="button"
      class="pfb__tab"
      :class="{ 'pfb__tab--active': modelValue === t.key }"
      role="tab"
      :aria-selected="modelValue === t.key"
      @click="select(t.key)"
    >
      <AppIcon :name="t.icon" :size="13" class="pfb__icon" />
      <span class="pfb__label">{{ t.label }}</span>
      <span
        v-if="t.count !== null && t.count !== undefined"
        class="pfb__count"
        :class="{ 'pfb__count--zero': t.count === 0 }"
      >{{ t.count }}</span>
    </button>
  </nav>
</template>

<style scoped>
/* PAI-356 — "editor status strip" aesthetic. Quiet, always-there,
   never demanding. The bar anchors to the bottom of the project-page
   flex column (`.pd-page` is `flex:1; min-height:0`) via the parent
   wrapper's `margin-top: auto`, so it sits below the IssueList /
   Overview / Knowledge content without competing for the global
   chrome slot owned by SidebarFooter (PAI-280 invariant). */
.pfb {
  display: flex;
  align-items: stretch;
  gap: 0;
  height: 40px;
  margin-top: auto;
  padding: 0 .25rem;
  background: var(--bg-card, var(--bg, #fff));
  border-top: 1px solid var(--border);
}

.pfb__tab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: .4rem;
  padding: 0 .9rem;
  height: 100%;
  font-family: inherit;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
  background: none;
  border: 0;
  border-top: 2px solid transparent;
  cursor: pointer;
  transition: color .15s, border-color .15s, background-color .15s;
  /* The 2px top-border is the active-state marker. Reserve the same
     2px even when inactive so toggling doesn't shift the label one
     pixel up — the strip should feel rock-steady. */
  margin-top: -1px; /* reach over the bar's top border so active marker sits on the seam */
}

.pfb__tab:hover {
  color: var(--text);
  background: var(--surface-2, transparent);
}

.pfb__tab:focus-visible {
  outline: 2px solid var(--bp-blue);
  outline-offset: -2px;
}

.pfb__tab--active {
  color: var(--bp-green, var(--bp-blue));
  border-top-color: var(--bp-green, var(--bp-blue));
  font-weight: 600;
}

.pfb__icon {
  flex-shrink: 0;
  opacity: .85;
}

.pfb__tab--active .pfb__icon {
  opacity: 1;
}

.pfb__label {
  white-space: nowrap;
}

.pfb__count {
  display: inline-block;
  min-width: 1.25rem;
  padding: 0 .4rem;
  font-size: 11px;
  font-weight: 700;
  line-height: 1.5;
  color: var(--text-muted);
  background: var(--surface-2);
  border-radius: 10px;
  text-align: center;
}

.pfb__count--zero {
  opacity: .5;
}

.pfb__tab--active .pfb__count {
  color: #fff;
  background: var(--bp-green, var(--bp-blue));
  opacity: 1;
}

/* Mobile — three labelled tabs at 375px fit at ~125px each.
   Tighten padding before stripping labels; only drop the labels
   below the threshold where a tap target would otherwise crowd. */
@media (max-width: 480px) {
  .pfb__tab {
    padding: 0 .55rem;
    font-size: 12px;
  }
  .pfb__count {
    font-size: 10px;
  }
}

@media (max-width: 360px) {
  .pfb__label {
    display: none;
  }
}
</style>
