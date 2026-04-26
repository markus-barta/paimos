<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-180 — Smart issue picker for the AI prompts dry-run console.

 v-model is the issue id (number | null). The component owns its own
 search/popover/keyboard state and remembers the picked issue
 internally so it can render a chip without the parent having to pass
 the full row.

 Wire shape: GET /api/issues?q=<term>&fields=list&limit=10 — the
 same FTS-backed search used elsewhere in the app, so a "PAI-1" or
 "checkbox in modal" query both work.

 Selected state renders as a chip (type icon + key + status dot +
 title + ✕). Empty state is an input with a leading magnifier and
 a debounced 200ms search; results pop below with keyboard nav.
-->
<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { api } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import StatusDot from '@/components/StatusDot.vue'
import { TYPE_SVGS } from '@/composables/useIssueDisplay'
import type { Issue } from '@/types'

const props = withDefaults(defineProps<{
  modelValue: number | null
  placeholder?: string
}>(), {
  placeholder: 'Search issues by key or title…',
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  'select':           [issue: Issue]
}>()

// ── local state ─────────────────────────────────────────────────────
// `selectedIssue` carries the full row so we can render the chip
// without the parent caring about anything but the id.
const selectedIssue = ref<Issue | null>(null)
const query         = ref('')
const results       = ref<Issue[]>([])
const loading       = ref(false)
const open          = ref(false)
const highlight     = ref(0)
const root          = ref<HTMLElement | null>(null)
const inputEl       = ref<HTMLInputElement | null>(null)

let debounce: number | null = null

// External resets (parent clears modelValue → drop our chip).
watch(() => props.modelValue, (v) => {
  if (v == null) {
    selectedIssue.value = null
    query.value = ''
    results.value = []
  }
})

async function runSearch(q: string) {
  const term = q.trim()
  if (term.length < 2) {
    results.value = []
    loading.value = false
    return
  }
  loading.value = true
  try {
    const r = await api.get<{ issues: Issue[] }>(
      `/issues?q=${encodeURIComponent(term)}&fields=list&limit=10`,
    )
    results.value = r.issues ?? []
    highlight.value = 0
  } catch {
    // 401 / network — silent. The empty state ("No issues match …")
    // will render and the user retries.
    results.value = []
  } finally {
    loading.value = false
  }
}

function onInput(e: Event) {
  query.value = (e.target as HTMLInputElement).value
  open.value = true
  if (debounce != null) window.clearTimeout(debounce)
  debounce = window.setTimeout(() => runSearch(query.value), 200)
}

function pick(issue: Issue) {
  selectedIssue.value = issue
  emit('update:modelValue', issue.id)
  emit('select', issue)
  open.value = false
  query.value = ''
  results.value = []
}

function clear() {
  selectedIssue.value = null
  emit('update:modelValue', null)
  query.value = ''
  results.value = []
  // Re-focus the input so admins can immediately type a new query
  // after clearing the pinned chip.
  nextTick(() => inputEl.value?.focus())
}

function onKey(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'ArrowDown') {
    if (!results.value.length) return
    highlight.value = (highlight.value + 1) % results.value.length
    e.preventDefault()
  } else if (e.key === 'ArrowUp') {
    if (!results.value.length) return
    highlight.value = (highlight.value - 1 + results.value.length) % results.value.length
    e.preventDefault()
  } else if (e.key === 'Enter') {
    if (!results.value.length) return
    pick(results.value[highlight.value])
    e.preventDefault()
  } else if (e.key === 'Escape') {
    open.value = false
    e.preventDefault()
  }
}

// Outside-click dismissal. We attach to mousedown so a click on a
// result that bubbles up doesn't close the popover before the
// click handler on the result fires.
function onDocClick(e: MouseEvent) {
  if (!root.value) return
  if (!root.value.contains(e.target as Node)) open.value = false
}
onMounted(()         => document.addEventListener('mousedown', onDocClick))
onBeforeUnmount(()   => document.removeEventListener('mousedown', onDocClick))
</script>

<template>
  <div class="iss-root" ref="root">
    <!-- ── selected state: chip ─────────────────────────────────── -->
    <div v-if="selectedIssue" class="iss-chip" role="status">
      <span class="iss-chip-type" v-html="TYPE_SVGS[selectedIssue.type] ?? ''" />
      <code class="iss-chip-key">{{ selectedIssue.issue_key }}</code>
      <StatusDot :status="selectedIssue.status" />
      <span class="iss-chip-title" :title="selectedIssue.title">{{ selectedIssue.title }}</span>
      <button
        type="button"
        class="iss-chip-x"
        @click="clear"
        aria-label="Clear selection"
        title="Clear selection"
      >
        <AppIcon name="x" :size="13" />
      </button>
    </div>

    <!-- ── search state ─────────────────────────────────────────── -->
    <div v-else class="iss-search" :class="{ 'iss-search--open': open && (results.length || query.trim().length >= 2) }">
      <span class="iss-search-icon" aria-hidden="true">
        <AppIcon name="search" :size="14" />
      </span>
      <input
        ref="inputEl"
        :value="query"
        :placeholder="placeholder"
        class="iss-input"
        spellcheck="false"
        autocomplete="off"
        @input="onInput"
        @focus="open = true"
        @keydown="onKey"
      />
      <span v-if="loading" class="iss-search-spin" aria-hidden="true">
        <AppIcon name="loader-circle" :size="13" class="spin" />
      </span>

      <Transition name="iss-pop">
        <div
          v-if="open && (results.length || query.trim().length >= 2 || loading)"
          class="iss-popover"
          role="listbox"
        >
          <div v-if="loading && !results.length" class="iss-empty">
            Searching…
          </div>
          <div v-else-if="!results.length" class="iss-empty">
            No issues match <strong>{{ query }}</strong>.
          </div>
          <button
            v-for="(r, i) in results" :key="r.id"
            type="button"
            :class="['iss-result', { 'iss-result--active': i === highlight }]"
            @mouseenter="highlight = i"
            @mousedown.prevent="pick(r)"
            role="option"
            :aria-selected="i === highlight"
          >
            <span class="iss-result-type" v-html="TYPE_SVGS[r.type] ?? ''" />
            <code class="iss-result-key">{{ r.issue_key }}</code>
            <StatusDot :status="r.status" />
            <span class="iss-result-title">{{ r.title }}</span>
          </button>
        </div>
      </Transition>
    </div>
  </div>
</template>

<style scoped>
/* Container is positioned so the popover can anchor to it. */
.iss-root { position: relative; }

/* ── chip (selected state) ────────────────────────────────────── */
.iss-chip {
  display: flex; align-items: center;
  gap: .55rem;
  padding: .4rem .55rem .4rem .65rem;
  min-height: 40px;
  background: var(--bp-blue-pale);
  border: 1.5px solid var(--bp-blue-light);
  border-radius: 9px;
  color: var(--bp-blue-dark);
  animation: iss-chip-in .18s cubic-bezier(.2, .8, .2, 1);
}
@keyframes iss-chip-in {
  from { transform: scale(.98); opacity: 0; }
  to   { transform: scale(1);   opacity: 1; }
}
.iss-chip-type { display: inline-flex; align-items: center; line-height: 0; flex-shrink: 0; color: var(--bp-blue-dark); }
.iss-chip-type :deep(svg) { width: 14px; height: 14px; }
.iss-chip-key {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 11.5px;
  font-weight: 700;
  background: white;
  border: 1px solid var(--bp-blue-light);
  border-radius: 5px;
  padding: .12rem .42rem;
  flex-shrink: 0;
  color: var(--bp-blue-dark);
}
.iss-chip-title {
  flex: 1; min-width: 0;
  font-size: 13px;
  color: var(--text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.iss-chip-x {
  background: none; border: none;
  color: var(--bp-blue-dark);
  cursor: pointer;
  padding: 4px;
  border-radius: 5px;
  display: inline-flex; line-height: 0;
  transition: background .12s;
}
.iss-chip-x:hover { background: rgba(46, 109, 164, .15); }

/* ── search state (input shell) ───────────────────────────────── */
.iss-search {
  position: relative;
  display: flex; align-items: center;
  background: white;
  border: 1.5px solid var(--border);
  border-radius: 9px;
  padding: 0 .55rem 0 .7rem;
  min-height: 40px;
  transition: border-color .14s, box-shadow .14s, border-radius .14s;
}
.iss-search:focus-within {
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px var(--bp-blue-pale);
}
/* When the popover is open, flatten the bottom corners so the
   shell + popover read as one connected surface. */
.iss-search--open { border-bottom-left-radius: 0; border-bottom-right-radius: 0; }
.iss-search-icon {
  display: inline-flex; align-items: center;
  color: var(--text-muted);
  flex-shrink: 0;
}
.iss-input {
  flex: 1;
  font-family: 'DM Sans', sans-serif;
  font-size: 13px;
  border: none; outline: none; background: transparent;
  padding: .55rem .4rem;
  color: var(--text);
  min-width: 0;
}
.iss-input::placeholder { color: var(--text-muted); }
.iss-search-spin {
  display: inline-flex; align-items: center;
  color: var(--text-muted);
  flex-shrink: 0;
}

/* ── popover (results) ────────────────────────────────────────── */
.iss-popover {
  position: absolute;
  top: calc(100% - 1.5px);   /* overlap parent border so corners merge */
  left: 0; right: 0;
  background: white;
  border: 1.5px solid var(--bp-blue);
  border-top: 1.5px solid var(--border);
  border-bottom-left-radius: 9px;
  border-bottom-right-radius: 9px;
  box-shadow: 0 12px 28px rgba(15, 35, 65, .10), 0 1px 4px rgba(0,0,0,.04);
  max-height: 320px;
  overflow-y: auto;
  z-index: 200;
  padding: .25rem;
  display: flex; flex-direction: column;
  gap: 1px;
}
.iss-pop-enter-active, .iss-pop-leave-active { transition: opacity .12s, transform .12s; }
.iss-pop-enter-from, .iss-pop-leave-to { opacity: 0; transform: translateY(-3px); }

.iss-empty {
  padding: .75rem .85rem;
  font-size: 12.5px;
  color: var(--text-muted);
  font-family: 'DM Sans', sans-serif;
}

.iss-result {
  display: flex; align-items: center;
  gap: .55rem;
  padding: .55rem .7rem;
  background: none;
  border: none;
  border-radius: 6px;
  text-align: left;
  cursor: pointer;
  width: 100%;
  font-family: inherit;
  color: var(--text);
}
.iss-result:hover, .iss-result--active {
  background: var(--bp-blue-pale);
}
.iss-result-type {
  display: inline-flex; align-items: center; line-height: 0;
  color: var(--text-muted);
  flex-shrink: 0;
}
.iss-result--active .iss-result-type { color: var(--bp-blue-dark); }
.iss-result-type :deep(svg) { width: 14px; height: 14px; }
.iss-result-key {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 11.5px;
  font-weight: 700;
  color: var(--bp-blue-dark);
  background: white;
  border: 1px solid var(--border);
  border-radius: 5px;
  padding: .1rem .42rem;
  flex-shrink: 0;
}
.iss-result-title {
  flex: 1; min-width: 0;
  font-size: 13px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.spin { animation: iss-spin 1s linear infinite; }
@keyframes iss-spin { to { transform: rotate(360deg); } }
</style>
