<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-148. Diff-preview overlay for the AI optimize flow (PAI-146).

 The optimize flow MUST not blindly replace the field. The rule:
 nothing in the editor changes until the user clicks Accept here.
 This component is the gate.

 Layout:
   - Two-column line-aligned diff on desktop (≥720px). Left = current,
     right = optimized; matching lines align horizontally; deletions
     get a soft red tint, insertions a soft green tint.
   - Stacks to a single column with a "Current" / "Optimized" heading
     pair on mobile so each side is fully readable on one viewport
     width.
   - Markdown is shown as plain monospace text on both sides; we are
     reviewing wording, not rendered output, and a rendered-mode diff
     would hide whitespace-only changes that matter (e.g. checklist
     formatting). The host page re-renders the accepted text through
     its normal markdown pipeline after Accept.

 Diffing:
   - LCS-based line alignment, implemented inline so the bundle
     doesn't pick up a diff library for a single use site. Adequate
     for the multi-paragraph rewrites this feature handles. If we
     ever need word-level diff inside aligned lines, swap in
     diff-match-patch then.
-->
<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { lineDiff } from './lineDiff'

const props = defineProps<{
  /** Original field content. Read-only on this side. */
  original: string
  /** Model output. Becomes the new field content on Accept. */
  optimized: string
  /** Display only — shown in the header chip ("description", etc.). */
  fieldLabel?: string
  /** Display only — shown in the header below the title. */
  modelName?: string
  /** True while the parent is calling /api/ai/optimize for a retry. */
  retrying?: boolean
}>()

const emit = defineEmits<{
  (e: 'accept', text: string): void
  (e: 'reject'): void
  (e: 'retry'): void
}>()

const diff = computed(() => lineDiff(props.original, props.optimized))

const summary = computed(() => {
  let added = 0
  let removed = 0
  for (const l of diff.value.left) if (l.type === 'del') removed++
  for (const r of diff.value.right) if (r.type === 'add') added++
  return { added, removed }
})

const unchanged = computed(() => props.original === props.optimized)

function onAccept() {
  if (!unchanged.value) emit('accept', props.optimized)
}
function onReject() {
  emit('reject')
}
function onRetry() {
  emit('retry')
}

// Esc closes the overlay (= reject). Click on backdrop also closes.
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') onReject()
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div class="ai-overlay-backdrop" @click.self="onReject">
    <div class="ai-overlay" role="dialog" aria-modal="true" aria-label="AI optimization preview">
      <header class="ai-overlay-head">
        <div class="ai-overlay-head-titles">
          <h2 class="ai-overlay-title">
            AI optimization preview
            <span v-if="fieldLabel" class="ai-overlay-field-chip">{{ fieldLabel }}</span>
          </h2>
          <p class="ai-overlay-subtitle">
            <template v-if="unchanged">
              The model returned text identical to the current content — nothing to apply.
            </template>
            <template v-else>
              <strong class="ai-summary-add">+{{ summary.added }}</strong> /
              <strong class="ai-summary-rem">−{{ summary.removed }}</strong>
              line<template v-if="summary.added + summary.removed !== 1">s</template>
              <span v-if="modelName" class="ai-model-tag"> · {{ modelName }}</span>
            </template>
          </p>
        </div>
        <button class="ai-overlay-close" type="button" aria-label="Close" @click="onReject">×</button>
      </header>

      <div class="ai-overlay-body">
        <!-- Mobile-stacked headings; hidden on desktop where the columns
             have their own heading row. -->
        <div class="ai-pane-heading ai-pane-heading--current">Current</div>
        <pre class="ai-pane ai-pane--current"><code><span
          v-for="(line, idx) in diff.left"
          :key="'l'+idx"
          :class="['ai-line', 'ai-line--'+line.type]"
        >{{ line.text || ' ' }}</span></code></pre>

        <div class="ai-pane-heading ai-pane-heading--optimized">Optimized</div>
        <pre class="ai-pane ai-pane--optimized"><code><span
          v-for="(line, idx) in diff.right"
          :key="'r'+idx"
          :class="['ai-line', 'ai-line--'+line.type]"
        >{{ line.text || ' ' }}</span></code></pre>
      </div>

      <footer class="ai-overlay-foot">
        <button type="button" class="btn btn-ghost" @click="onReject">
          Reject
        </button>
        <button type="button" class="btn btn-ghost" :disabled="retrying" @click="onRetry">
          {{ retrying ? 'Retrying…' : 'Retry' }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="unchanged || retrying"
          @click="onAccept"
        >
          Accept &amp; replace
        </button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.ai-overlay-backdrop {
  position: fixed; inset: 0;
  background: rgba(15, 23, 42, .55);
  display: flex; align-items: center; justify-content: center;
  z-index: 1000;
  padding: 1rem;
}

.ai-overlay {
  background: var(--bg-card, #fff);
  border-radius: 12px;
  box-shadow: 0 20px 50px rgba(0,0,0,.25);
  width: min(1100px, 100%);
  max-height: calc(100vh - 2rem);
  display: flex; flex-direction: column;
  overflow: hidden;
}

.ai-overlay-head {
  display: flex; justify-content: space-between; align-items: flex-start;
  gap: 1rem;
  padding: 1.1rem 1.25rem .9rem;
  border-bottom: 1px solid var(--border);
}
.ai-overlay-head-titles { display: flex; flex-direction: column; gap: .15rem; min-width: 0; }
.ai-overlay-title {
  margin: 0; font-size: 16px; font-weight: 700; color: var(--text);
  display: flex; align-items: center; gap: .5rem; flex-wrap: wrap;
}
.ai-overlay-field-chip {
  background: var(--bp-blue-pale, #dce9f4); color: var(--bp-blue-dark, #1f4d75);
  padding: .1rem .55rem; border-radius: 999px;
  font-size: 11px; font-weight: 600; letter-spacing: .04em; text-transform: uppercase;
  font-family: 'DM Sans', sans-serif;
}
.ai-overlay-subtitle {
  margin: 0; font-size: 12px; color: var(--text-muted);
}
.ai-summary-add { color: #166534; }
.ai-summary-rem { color: #b91c1c; }
.ai-model-tag {
  font-family: 'DM Mono', monospace;
}

.ai-overlay-close {
  background: none; border: none; cursor: pointer;
  font-size: 24px; line-height: 1; color: var(--text-muted);
  padding: 0 .25rem;
}
.ai-overlay-close:hover { color: var(--text); }

/* ── Body / panes ───────────────────────────────────────────────── */
.ai-overlay-body {
  flex: 1 1 auto;
  overflow: auto;
  display: grid;
  /* Single-column on mobile; the headings sit above their pane. */
  grid-template-columns: 1fr;
  gap: 0;
}
@media (min-width: 720px) {
  .ai-overlay-body {
    grid-template-columns: 1fr 1fr;
    grid-template-rows: auto 1fr;
  }
  .ai-pane-heading--current   { grid-column: 1 / 2; grid-row: 1 / 2; }
  .ai-pane-heading--optimized { grid-column: 2 / 3; grid-row: 1 / 2; }
  .ai-pane--current   { grid-column: 1 / 2; grid-row: 2 / 3; border-right: 1px solid var(--border); }
  .ai-pane--optimized { grid-column: 2 / 3; grid-row: 2 / 3; }
}

.ai-pane-heading {
  font-size: 11px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .08em; color: var(--text-muted);
  padding: .55rem .85rem;
  background: var(--bg, #f8fafc);
  border-bottom: 1px solid var(--border);
}

.ai-pane {
  margin: 0;
  padding: .35rem 0;
  background: #fff;
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.55;
  white-space: pre-wrap; word-break: break-word;
  overflow: visible;
}
.ai-pane code { display: block; }

.ai-line {
  display: block;
  padding: 0 .85rem;
  border-left: 3px solid transparent;
}
.ai-line--del { background: #fef2f2; border-left-color: #fca5a5; }
.ai-line--add { background: #f0fdf4; border-left-color: #86efac; }
.ai-line--pad { color: transparent; user-select: none; }

/* ── Footer ─────────────────────────────────────────────────────── */
.ai-overlay-foot {
  display: flex; justify-content: flex-end; gap: .5rem;
  padding: .85rem 1.25rem;
  border-top: 1px solid var(--border);
  background: var(--bg, #f8fafc);
}

@media (max-width: 480px) {
  .ai-overlay-foot {
    flex-wrap: wrap;
  }
  .ai-overlay-foot .btn { flex: 1 1 30%; min-width: 0; }
}
</style>
