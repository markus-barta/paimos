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
/* PAI-356 / PAI-358 — "editor status strip" aesthetic, refined per
   v3.0 feedback. Spans the full project-page width by negating the
   page padding (parent .pd-page applies horizontal padding for the
   content; the bar escapes it via negative inline margin so it
   matches the app header / subheader rule). Neutral active state —
   no green/blue tint; just a soft surface bg + bold weight, mirroring
   the sidebar nav-item treatment so the chrome reads as one family. */
.pfb {
  display: flex;
  align-items: stretch;
  gap: 0;
  height: 36px;
  margin-top: auto;
  /* AppLayout's .main-content has padding: 2rem 2.5rem; self-scroll
     views trim bottom to .5rem (AppLayout.vue:473). Escape both with
     negative margins so the bar spans the full project page width
     and pins to the viewport bottom — no white gutters at the edges. */
  margin-left: -2.5rem;
  margin-right: -2.5rem;
  margin-bottom: -.5rem;
  padding: 0 2.5rem;
  background: var(--bg-card, var(--bg, #fff));
  border-top: 1px solid var(--border);
}

.pfb__tab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: .4rem;
  padding: 0 .85rem;
  height: 100%;
  font-family: inherit;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
  background: none;
  border: 0;
  cursor: pointer;
  transition: color .12s, background-color .12s;
}

.pfb__tab:hover {
  color: var(--text);
  background: var(--surface-2, var(--bg));
}

.pfb__tab:focus-visible {
  outline: 2px solid var(--bp-blue);
  outline-offset: -2px;
}

.pfb__tab--active {
  color: var(--text);
  font-weight: 600;
  /* Soft tint matching the sidebar's nav-item.active. Same family of
     highlights as the rest of the chrome; no fresh accent color. */
  background: color-mix(in srgb, var(--bp-blue) 12%, transparent);
}

.pfb__icon {
  flex-shrink: 0;
  opacity: .8;
}

.pfb__tab--active .pfb__icon {
  opacity: 1;
}

.pfb__label {
  white-space: nowrap;
}

/* Count is informational, not decorative. Same muted treatment for
   active and inactive — the active state is the bg tint, not the
   badge color. Avoids the "why is this blue?" reaction. */
.pfb__count {
  display: inline-block;
  min-width: 1.25rem;
  padding: 0 .4rem;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--text-muted);
  background: var(--surface-2, var(--bg));
  border-radius: 10px;
  text-align: center;
  font-variant-numeric: tabular-nums;
}

.pfb__count--zero {
  opacity: .45;
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
