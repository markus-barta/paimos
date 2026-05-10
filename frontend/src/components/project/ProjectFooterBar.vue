<script setup lang="ts">
// PAI-356 / PAI-359 — Project-page footer bar. Six mutually-exclusive
// tabs in one continuous row: the three primary content modes
// (Issues / Overview / Knowledge) plus the three workspace surfaces
// the legacy aux-panel dock used to host (Docs / Coop / Context).
// PAI-359 collapses the dock pattern into peer tabs so there's one
// canonical project-navigation surface, not two stacked rows.
//
// Counters: numbers for tabs that have a meaningful count (Issues,
// Knowledge, Docs, Context-as-repo-count); a tiny dot for boolean
// "populated" state (Coop). null/undefined hides the badge entirely.

import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'

export type ProjectPrimaryTab =
  | 'issues'
  | 'overview'
  | 'knowledge'
  | 'docs'
  | 'coop'
  | 'context'

const props = defineProps<{
  modelValue: ProjectPrimaryTab
  // Numeric counters — null hides the badge entirely.
  openIssues?: number | null
  knowledgeEntries?: number | null
  docsCount?: number | null
  contextRepos?: number | null
  // Boolean "populated" state for tabs whose data is non-numeric
  // (Cooperation = a structured summary, not a list count). When true
  // the tab renders a small filled dot; when false/null no badge.
  coopPopulated?: boolean | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: ProjectPrimaryTab): void
}>()

interface TabSpec {
  key: ProjectPrimaryTab
  label: string
  icon: string
  count?: number | null
  // Render a small filled dot in lieu of a numeric count.
  dot?: boolean
}

const tabs = computed<TabSpec[]>(() => [
  { key: 'issues',    label: 'Issues',    icon: 'layout-list',    count: props.openIssues       ?? null                  },
  { key: 'overview',  label: 'Overview',  icon: 'house',          count: null                                            },
  { key: 'knowledge', label: 'Knowledge', icon: 'book-open',      count: props.knowledgeEntries ?? null                  },
  { key: 'docs',      label: 'Docs',      icon: 'file-text',      count: props.docsCount        ?? null                  },
  { key: 'coop',      label: 'Coop',      icon: 'handshake',      dot: props.coopPopulated === true                       },
  { key: 'context',   label: 'Context',   icon: 'git-branch',     count: props.contextRepos     ?? null                  },
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
      <span v-else-if="t.dot" class="pfb__dot" aria-label="populated"></span>
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
/* PAI-359 / PAI-361 — bottom chrome strip. Renders into the
   `#project-footer-slot` Teleport target which is a peer of
   .app-header under <main>, so the bar naturally spans the full
   main width with no margin escape. Interior padding matches
   .main-content's 1.25rem (20px) edge so the leftmost tab label
   aligns with the page-content gutter. */
.pfb {
  display: flex;
  align-items: stretch;
  gap: 0;
  height: 36px;
  width: 100%;
  padding: 0 1.25rem;
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

/* Boolean "populated" indicator for tabs with non-numeric state
   (currently only Coop). Same colour family as the count badges so
   the chrome stays cohesive; sized to read as a status dot, not a
   pill. Hides automatically when the prop is false. */
.pfb__dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--text-muted);
  flex-shrink: 0;
}
.pfb__tab--active .pfb__dot {
  background: var(--text);
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
