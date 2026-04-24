<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-145. Two-pane "What's new" modal.

 Layout: narrow rail of versions (left) + content (right). The shell
 owns a bounded height + `overflow: hidden` so the rail and the body
 each scroll independently inside the modal frame. AppModal itself
 doesn't constrain height — the previous attempt let the rail
 overflow the modal entirely.

 Keyboard nav (focus inside the rail): ↑/↓ step one version,
 PgUp/PgDn step five, Home/End jump to newest / oldest. Selection
 auto-scrolls into view via `scrollIntoView({ block: 'nearest' })`.
-->
<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import AppModal from '@/components/AppModal.vue'
import changelogRaw from '@docs/CHANGELOG.md?raw'

const props = defineProps<{ open: boolean }>()
defineEmits<{ close: [] }>()

interface VersionEntry {
  version: string
  date: string
  bodyMd: string
  bumpKind: 'major' | 'minor' | 'patch' | 'unknown'
}

// Matches `## [X.Y.Z] — YYYY-MM-DD` (canonical em-dash). Anything else
// is silently ignored — better to drop a malformed heading than to
// corrupt the parse with a noisy fallback.
const VERSION_HEADING_RE = /^## \[(\d+\.\d+\.\d+)\] — (\d{4}-\d{2}-\d{2})/m

const entries = computed<VersionEntry[]>(() => {
  const sections = changelogRaw
    .split(/(?=^## \[\d+\.\d+\.\d+\])/m)
    .map(s => s.trim())
    .filter(s => VERSION_HEADING_RE.test(s))

  const list: VersionEntry[] = sections.map(s => {
    const m = s.match(VERSION_HEADING_RE)!
    return {
      version: m[1],
      date: m[2],
      bodyMd: s.replace(VERSION_HEADING_RE, '').trim(),
      bumpKind: 'unknown',
    }
  })

  for (let i = 0; i < list.length - 1; i++) {
    const cur = list[i].version.split('.').map(Number)
    const prev = list[i + 1].version.split('.').map(Number)
    if (cur[0] > prev[0])      list[i].bumpKind = 'major'
    else if (cur[1] > prev[1]) list[i].bumpKind = 'minor'
    else if (cur[2] > prev[2]) list[i].bumpKind = 'patch'
  }
  return list
})

const selectedIndex = ref(0)
const selected = computed<VersionEntry | null>(() =>
  entries.value[selectedIndex.value] ?? null,
)

// Re-anchor to newest on every open. Most users open this modal to
// answer "what just shipped?", not to resume browsing where they left
// off — picking up the previous selection would be the wrong default.
watch(
  () => props.open,
  (open) => {
    if (open) selectedIndex.value = 0
  },
  { immediate: true },
)

const renderedHtml = computed(() => {
  if (!selected.value) return ''
  return DOMPurify.sanitize(marked.parse(selected.value.bodyMd) as string)
})

// Compact sidebar date — "24 Apr" / "24 Apr 25" if the year differs
// from the current one. Saves horizontal pixels while staying readable.
function fmtSidebarDate(iso: string): string {
  const d = new Date(iso + 'T00:00:00Z')
  const sameYear = d.getUTCFullYear() === new Date().getUTCFullYear()
  return d.toLocaleDateString(undefined, sameYear
    ? { day: '2-digit', month: 'short' }
    : { day: '2-digit', month: 'short', year: '2-digit' })
}
function fmtFullDate(iso: string): string {
  const d = new Date(iso + 'T00:00:00Z')
  return d.toLocaleDateString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric',
  })
}

// ── Keyboard nav ────────────────────────────────────────────────────
// Wired on the rail itself (tabindex 0). Doesn't conflict with the
// modal's Escape handler — different target, different keys.
const railRef = ref<HTMLElement | null>(null)

function onRailKey(e: KeyboardEvent) {
  const max = entries.value.length - 1
  if (max < 0) return
  let next = selectedIndex.value
  switch (e.key) {
    case 'ArrowDown': next = Math.min(max, selectedIndex.value + 1); break
    case 'ArrowUp':   next = Math.max(0,   selectedIndex.value - 1); break
    case 'PageDown':  next = Math.min(max, selectedIndex.value + 5); break
    case 'PageUp':    next = Math.max(0,   selectedIndex.value - 5); break
    case 'Home':      next = 0; break
    case 'End':       next = max; break
    default: return
  }
  e.preventDefault()
  if (next !== selectedIndex.value) {
    selectedIndex.value = next
    nextTick(() => {
      // Scroll the freshly-selected button into view if it slipped past
      // the viewport bounds — `block: 'nearest'` keeps the rail still
      // when the active item is already visible.
      const el = railRef.value?.querySelector<HTMLElement>(
        `[data-version="${entries.value[next].version}"]`,
      )
      el?.scrollIntoView({ block: 'nearest' })
    })
  }
}

// Auto-focus the rail on open so arrow keys work without a click first.
watch(
  () => props.open,
  (open) => {
    if (!open) return
    nextTick(() => railRef.value?.focus())
  },
)
</script>

<template>
  <AppModal
    title="What's new"
    :open="open"
    max-width="980px"
    @close="$emit('close')"
  >
    <div class="cl-shell">
      <!-- ── Rail: dense version list ────────────────────────── -->
      <div
        ref="railRef"
        class="cl-rail"
        tabindex="0"
        role="listbox"
        aria-label="Version history (use arrow keys to navigate)"
        @keydown="onRailKey"
      >
        <ol class="cl-rail-list">
          <li v-for="(e, i) in entries" :key="e.version" role="presentation">
            <button
              type="button"
              :class="[
                'cl-row',
                `cl-row--${e.bumpKind}`,
                { 'cl-row--active': i === selectedIndex },
              ]"
              role="option"
              :aria-selected="i === selectedIndex"
              :data-version="e.version"
              @click="selectedIndex = i"
            >
              <span class="cl-row-dot" />
              <span class="cl-row-ver">{{ e.version }}</span>
              <span class="cl-row-date">{{ fmtSidebarDate(e.date) }}</span>
            </button>
          </li>
        </ol>
        <p class="cl-rail-hint">↑ ↓ to navigate</p>
      </div>

      <!-- ── Content pane ────────────────────────────────────── -->
      <section class="cl-content" aria-live="polite">
        <header v-if="selected" class="cl-content-head">
          <div class="cl-content-head-id">
            <span :class="['cl-row-dot', 'cl-row-dot--lg', `cl-row--${selected.bumpKind}`]" />
            <h2>v{{ selected.version }}</h2>
            <span v-if="selected.bumpKind !== 'unknown'" class="cl-bump-label">{{ selected.bumpKind }}</span>
          </div>
          <time class="cl-content-head-date" :datetime="selected.date">
            {{ fmtFullDate(selected.date) }}
          </time>
        </header>

        <Transition name="cl-fade" mode="out-in">
          <article
            :key="selected?.version ?? 'empty'"
            class="cl-body"
            v-html="renderedHtml"
          />
        </Transition>
      </section>
    </div>
  </AppModal>
</template>

<style scoped>
/* ── Shell ─────────────────────────────────────────────────────────
   Bleeds to the modal frame on all four sides (cancels modal-body's
   1.5rem padding) so the rail can have its own background flush with
   the modal edges. The fixed `height` is the critical bit: it caps
   the layout so the children's `overflow: auto` actually engages.
*/
.cl-shell {
  display: grid;
  grid-template-columns: 168px 1fr;
  margin: -1.5rem;
  height: min(560px, 70vh);
  overflow: hidden;
  border-radius: 0 0 8px 8px; /* match the modal's bottom corners */
}
@media (max-width: 720px) {
  .cl-shell {
    grid-template-columns: 1fr;
    grid-template-rows: 152px 1fr;
    height: min(640px, 80vh);
  }
}

/* ── Rail (sidebar) ────────────────────────────────────────────── */
.cl-rail {
  background: #fafbfc;
  border-right: 1px solid var(--border);
  overflow-y: auto;
  overflow-x: hidden;
  padding: .5rem .25rem .25rem;
  outline: none;
  display: flex; flex-direction: column;
  scrollbar-width: thin;
}
.cl-rail:focus-visible {
  /* Inset focus ring — the rail is the keyboard handler, so it should
     be visibly focusable without breaking the layout. */
  box-shadow: inset 0 0 0 2px rgba(46, 109, 164, .35);
}
@media (max-width: 720px) {
  .cl-rail {
    border-right: none;
    border-bottom: 1px solid var(--border);
  }
}

.cl-rail-list {
  list-style: none; padding: 0; margin: 0;
  display: flex; flex-direction: column;
  gap: 1px;
  flex: 1 1 auto;
}

.cl-row {
  width: 100%;
  display: grid;
  grid-template-columns: 7px 1fr auto;
  align-items: center;
  gap: .45rem;
  padding: .3rem .55rem;
  background: transparent;
  border: none;
  border-left: 2px solid transparent;
  border-radius: 0 4px 4px 0;
  text-align: left;
  cursor: pointer;
  font-family: inherit;
  color: var(--text);
  /* No height: row is content-tall (~26 px); dense by design. */
  transition: background .1s, border-color .1s;
}
.cl-row:hover { background: rgba(46, 109, 164, .06); }
.cl-row--active {
  background: var(--bp-blue-pale);
  border-left-color: var(--bp-blue);
}
.cl-row--active .cl-row-ver {
  color: var(--bp-blue-dark);
}

.cl-row-dot {
  width: 7px; height: 7px;
  border-radius: 50%;
  background: #cbd5e1;
  /* Hairline ring for crispness on white. */
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, .08);
}
.cl-row-dot--lg { width: 9px; height: 9px; align-self: center; }

/* Bump-kind colors — applied to the row OR to the standalone dot. */
.cl-row--patch  .cl-row-dot,
.cl-row-dot.cl-row--patch  { background: #16a34a; }
.cl-row--minor  .cl-row-dot,
.cl-row-dot.cl-row--minor  { background: var(--bp-blue); }
.cl-row--major  .cl-row-dot,
.cl-row-dot.cl-row--major  { background: #f59e0b; }

.cl-row-ver {
  font-family: 'DM Mono', 'Fira Code', monospace;
  font-size: 12px; font-weight: 700;
  font-variant-numeric: tabular-nums;
  letter-spacing: -.01em;
  /* No "v" prefix in the rail — saves 6 pixels per row and the column
     header / content header carry the format already. */
}
.cl-row-date {
  font-size: 10.5px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.cl-row--active .cl-row-date { color: var(--bp-blue-dark); opacity: .75; }

.cl-rail-hint {
  flex-shrink: 0;
  margin: .5rem .25rem 0;
  padding: .35rem .55rem;
  font-size: 10px;
  color: var(--text-muted);
  letter-spacing: .03em;
  text-align: center;
  border-top: 1px dashed var(--border);
}

/* ── Content pane ──────────────────────────────────────────────── */
.cl-content {
  overflow-y: auto;
  padding: 1.1rem 1.5rem 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  min-width: 0; /* allow grid child to shrink rather than overflow */
}

.cl-content-head {
  display: flex; align-items: baseline; justify-content: space-between;
  gap: 1rem;
  padding-bottom: .85rem;
  border-bottom: 1px solid var(--border);
  position: sticky; top: -1.1rem;
  background: var(--bg-card);
  z-index: 1;
  margin-top: -1.1rem;
  padding-top: 1.1rem;
}

.cl-content-head-id {
  display: flex; align-items: baseline; gap: .55rem;
  min-width: 0;
}
.cl-content-head-id h2 {
  font-family: 'DM Mono', monospace;
  font-size: 22px; font-weight: 800;
  letter-spacing: -.02em;
  margin: 0;
  color: var(--text);
  font-variant-numeric: tabular-nums;
}

.cl-bump-label {
  font-size: 10px; font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .08em;
  color: var(--text-muted);
}

.cl-content-head-date {
  font-size: 12px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

/* ── Markdown body ─────────────────────────────────────────────── */
.cl-body { font-size: 13px; color: var(--text); line-height: 1.6; }

.cl-body :deep(h2) {
  font-size: 14px; font-weight: 700; color: var(--text);
  margin: 1.5rem 0 .55rem;
  letter-spacing: -.005em;
  padding-bottom: .25rem;
  border-bottom: 1px solid var(--border);
}
.cl-body :deep(h2:first-child) { margin-top: .25rem; }
.cl-body :deep(h3) {
  font-size: 11px; font-weight: 700; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .07em;
  margin: 1.1rem 0 .4rem;
}
.cl-body :deep(p) { margin: 0 0 .65rem; }
.cl-body :deep(ul) { margin: .25rem 0 .8rem 1.25rem; padding: 0; list-style: disc; }
.cl-body :deep(li) { margin-bottom: .3rem; }
.cl-body :deep(li > ul) { margin-top: .25rem; }
.cl-body :deep(strong) { font-weight: 700; color: var(--text); }

.cl-body :deep(code) {
  font-family: 'DM Mono', 'Fira Code', monospace;
  font-size: 12px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 3px;
  padding: .05rem .35rem;
  color: var(--text);
}
.cl-body :deep(pre) {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: .65rem .85rem;
  overflow-x: auto;
  margin: .4rem 0 .85rem;
}
.cl-body :deep(pre code) {
  background: none; border: none; padding: 0;
  font-size: 12px;
}
.cl-body :deep(a) { color: var(--bp-blue); text-decoration: underline; text-underline-offset: 2px; }
.cl-body :deep(a:hover) { color: var(--bp-blue-dark); }
.cl-body :deep(hr) { display: none; }

/* ── Cross-version transition ──────────────────────────────────── */
.cl-fade-enter-active, .cl-fade-leave-active {
  transition: opacity .12s ease, transform .12s ease;
}
.cl-fade-enter-from { opacity: 0; transform: translateY(4px); }
.cl-fade-leave-to   { opacity: 0; transform: translateY(-4px); }
</style>
