<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-145. Two-pane "What's new" modal.

 Left sidebar lists every release parsed from CHANGELOG.md (no
 truncation, no expand button — the sidebar IS the all-releases
 view); right pane renders only the selected version. Bump kind
 (patch / minor / major) is inferred from each version's diff to the
 next-older entry and surfaced as a small dot in the sidebar so the
 timeline reads at a glance.
-->
<script setup lang="ts">
import { ref, computed, watch } from 'vue'
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

// Matches `## [X.Y.Z] — YYYY-MM-DD` (the format we write today). The
// em-dash is canonical in the file; if a future entry sneaks in a hyphen
// it will fall through silently rather than corrupting the parse.
const VERSION_HEADING_RE = /^## \[(\d+\.\d+\.\d+)\] — (\d{4}-\d{2}-\d{2})/m

const entries = computed<VersionEntry[]>(() => {
  // Split on each version heading; keep the heading with its section.
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

  // Bump kind is each entry's diff vs the next-older entry. Newest gets
  // 'unknown' if it has no predecessor (single-release case); otherwise
  // patch / minor / major from the version triple.
  for (let i = 0; i < list.length - 1; i++) {
    const cur = list[i].version.split('.').map(Number)
    const prev = list[i + 1].version.split('.').map(Number)
    if (cur[0] > prev[0])      list[i].bumpKind = 'major'
    else if (cur[1] > prev[1]) list[i].bumpKind = 'minor'
    else if (cur[2] > prev[2]) list[i].bumpKind = 'patch'
  }
  return list
})

const selectedVersion = ref<string>('')

// Default selection = newest. Watch on `open` so re-opening always lands
// on the latest entry rather than wherever the user left off (matches
// the user's most likely intent: "what's the latest?").
watch(
  () => props.open,
  (open) => {
    if (open && entries.value.length) {
      selectedVersion.value = entries.value[0].version
    }
  },
  { immediate: true },
)

const selected = computed<VersionEntry | null>(() =>
  entries.value.find(e => e.version === selectedVersion.value) ?? entries.value[0] ?? null,
)

const renderedHtml = computed(() => {
  if (!selected.value) return ''
  return DOMPurify.sanitize(marked.parse(selected.value.bodyMd) as string)
})

// Pretty date for both sidebar + content header. Locale is left to the
// browser deliberately — admins picking up an instance will read dates
// in their own locale, no surprise.
function fmtDate(iso: string): string {
  const d = new Date(iso + 'T00:00:00Z')
  return d.toLocaleDateString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric',
  })
}
</script>

<template>
  <AppModal
    title="What's new"
    :open="open"
    max-width="1080px"
    @close="$emit('close')"
  >
    <div class="cl-shell">
      <!-- ── Sidebar: version list ───────────────────────────── -->
      <aside class="cl-sidebar" aria-label="Version history">
        <ol class="cl-versions">
          <li
            v-for="(e, i) in entries"
            :key="e.version"
          >
            <button
              type="button"
              :class="[
                'cl-version',
                { 'cl-version--active': e.version === selectedVersion },
              ]"
              :aria-current="e.version === selectedVersion ? 'true' : undefined"
              @click="selectedVersion = e.version"
            >
              <span :class="['cl-bump', `cl-bump--${e.bumpKind}`]" :title="e.bumpKind" />
              <span class="cl-version-label">v{{ e.version }}</span>
              <span v-if="i === 0" class="cl-latest">Latest</span>
              <span class="cl-version-date">{{ fmtDate(e.date) }}</span>
            </button>
          </li>
        </ol>
      </aside>

      <!-- ── Content pane ────────────────────────────────────── -->
      <section class="cl-content" aria-live="polite">
        <header v-if="selected" class="cl-content-head">
          <div class="cl-content-head-id">
            <span :class="['cl-bump', 'cl-bump--lg', `cl-bump--${selected.bumpKind}`]" />
            <h2>v{{ selected.version }}</h2>
            <span v-if="selected.bumpKind !== 'unknown'" class="cl-bump-label">{{ selected.bumpKind }}</span>
          </div>
          <time class="cl-content-head-date" :datetime="selected.date">
            {{ fmtDate(selected.date) }}
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
/* ── Shell ─────────────────────────────────────────────────── */
.cl-shell {
  display: grid;
  grid-template-columns: 240px 1fr;
  gap: 0;
  /* Pull the modal's inner padding so the sidebar bleeds to the
     edge — visually heavier divider, more app-like. */
  margin: -1.25rem -1.5rem;
  min-height: min(70vh, 620px);
  max-height: 80vh;
}
@media (max-width: 720px) {
  .cl-shell {
    grid-template-columns: 1fr;
    grid-template-rows: auto 1fr;
  }
}

/* ── Sidebar ───────────────────────────────────────────────── */
.cl-sidebar {
  background: #fafbfc;
  border-right: 1px solid var(--border);
  overflow-y: auto;
  padding: .85rem .5rem;
}
@media (max-width: 720px) {
  .cl-sidebar {
    border-right: none;
    border-bottom: 1px solid var(--border);
    max-height: 30vh;
    padding: .65rem .5rem;
  }
}

.cl-versions {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: .15rem;
}

.cl-version {
  width: 100%;
  display: grid;
  grid-template-columns: 8px 1fr auto;
  grid-template-areas:
    "dot version  badge"
    "dot date     date";
  align-items: center;
  gap: .15rem .5rem;
  padding: .55rem .65rem .55rem .55rem;
  background: transparent;
  border: none;
  border-left: 2px solid transparent;
  border-radius: 0 6px 6px 0;
  text-align: left;
  cursor: pointer;
  font-family: inherit;
  color: var(--text);
  transition: background .12s, border-color .12s;
}
.cl-version:hover { background: rgba(46, 109, 164, .06); }
.cl-version--active {
  background: var(--bp-blue-pale);
  border-left-color: var(--bp-blue);
}
.cl-version--active .cl-version-label { color: var(--bp-blue-dark); }

.cl-bump {
  grid-area: dot;
  width: 8px; height: 8px;
  border-radius: 50%;
  background: var(--text-muted);
  align-self: center;
  /* Slight inner ring for hairline definition on white backgrounds. */
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, .08);
}
.cl-bump--patch    { background: #16a34a; }
.cl-bump--minor    { background: var(--bp-blue); }
.cl-bump--major    { background: #f59e0b; }
.cl-bump--unknown  { background: #cbd5e1; }
.cl-bump--lg {
  width: 10px; height: 10px;
  align-self: baseline;
  margin-top: 6px;
}

.cl-version-label {
  grid-area: version;
  font-family: 'DM Mono', 'Fira Code', monospace;
  font-size: 13px; font-weight: 700;
  font-variant-numeric: tabular-nums;
  letter-spacing: -.01em;
}

.cl-latest {
  grid-area: badge;
  font-size: 9px; font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .08em;
  color: #166534;
  background: #dcfce7;
  padding: .1rem .4rem;
  border-radius: 999px;
}

.cl-version-date {
  grid-area: date;
  font-size: 11px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

/* ── Content pane ──────────────────────────────────────────── */
.cl-content {
  overflow-y: auto;
  padding: 1.1rem 1.5rem 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.cl-content-head {
  display: flex; align-items: flex-start; justify-content: space-between;
  gap: 1rem;
  padding-bottom: .85rem;
  border-bottom: 1px solid var(--border);
  position: sticky; top: -1.1rem;
  /* Sticky header; subtle backdrop so content scrolling under it stays
     legible. The negative `top` cancels the container padding so the
     strip lands flush with the modal frame. */
  background: linear-gradient(
    to bottom,
    var(--bg-card) 0%,
    var(--bg-card) calc(100% - 4px),
    rgba(255,255,255,0) 100%
  );
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
  align-self: center;
}

/* ── Markdown body ─────────────────────────────────────────── */
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

/* ── Cross-version transition ──────────────────────────────── */
.cl-fade-enter-active, .cl-fade-leave-active {
  transition: opacity .12s ease, transform .12s ease;
}
.cl-fade-enter-from { opacity: 0; transform: translateY(4px); }
.cl-fade-leave-to   { opacity: 0; transform: translateY(-4px); }
</style>
