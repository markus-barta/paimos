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
import { computed, onMounted, onUnmounted, reactive, watch } from 'vue'
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
  /** Safe execution metadata returned by the AI action dispatcher. */
  executionProfileId?: string
  executionEffort?: string
  promptPresetRef?: string
  promptPresetLabel?: string
  contextPack?: string
  contextPackLabel?: string
  contextTruncated?: boolean
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
const executionMeta = computed(() => {
  const parts: string[] = []
  if (props.executionProfileId) parts.push(`Profile: ${props.executionProfileId}`)
  if (props.executionEffort) parts.push(`Effort: ${props.executionEffort}`)
  if (props.promptPresetRef && props.promptPresetRef !== 'default') {
    parts.push(`Prompt: ${props.promptPresetLabel || props.promptPresetRef}`)
  }
  if (props.contextPack && props.contextPack !== 'issue') {
    parts.push(`Context: ${props.contextPackLabel || props.contextPack}${props.contextTruncated ? ' (truncated)' : ''}`)
  }
  return parts.join(' · ')
})

// PAI-219. Per-hunk decision state. Each hunk defaults to 'accept'
// so the overall behaviour is identical to the old "Accept replaces
// everything" flow until the user toggles. A reactive Map gives us
// proxy reactivity in templates without needing to swap the whole
// ref on every mutation.
type Decision = 'accept' | 'reject'
const decisions = reactive(new Map<number, Decision>())

watch(
  diff,
  (d) => {
    // Re-key the decisions map whenever the diff structure changes
    // (model returned different text, retry produced new hunks, ...).
    // We deliberately drop prior selections rather than try to
    // re-correlate them with new hunk ids — there's no stable identity
    // to map old → new.
    decisions.clear()
    for (const h of d.hunks) decisions.set(h.id, 'accept')
  },
  { immediate: true },
)

function decisionFor(id: number): Decision {
  return decisions.get(id) ?? 'accept'
}
function setDecision(id: number, value: Decision) {
  decisions.set(id, value)
}
function toggleDecision(id: number) {
  setDecision(id, decisionFor(id) === 'accept' ? 'reject' : 'accept')
}
function setAllDecisions(value: Decision) {
  for (const h of diff.value.hunks) decisions.set(h.id, value)
}

// Map row index → hunk id (or null for eq rows), used by the row
// renderer to apply per-hunk visual state without re-walking the
// hunk array on every row.
const rowToHunk = computed<(number | null)[]>(() => {
  const len = diff.value.left.length
  const arr: (number | null)[] = new Array(len).fill(null)
  for (const h of diff.value.hunks) {
    for (let r = h.startRow; r < h.endRow; r++) arr[r] = h.id
  }
  return arr
})

const acceptedHunks = computed(() => {
  let n = 0
  for (const h of diff.value.hunks) if (decisionFor(h.id) === 'accept') n++
  return n
})
const totalHunks = computed(() => diff.value.hunks.length)
const allAccepted = computed(() => totalHunks.value > 0 && acceptedHunks.value === totalHunks.value)
const allRejected = computed(() => totalHunks.value > 0 && acceptedHunks.value === 0)

// PAI-219. Walk the aligned rows in order. For each eq row, emit its
// text. For each hunk, emit the chosen side (added text on accept,
// removed text on reject) once when we cross its end. This way the
// final text matches the visual ordering — eq anchors stay in place,
// per-hunk choices slot back in between them.
const finalText = computed(() => {
  const { left, hunks } = diff.value
  const out: string[] = []
  let hunkIdx = 0
  for (let row = 0; row < left.length; row++) {
    const hid = rowToHunk.value[row]
    if (hid == null) {
      // eq row — left.text === right.text by construction
      out.push(left[row].text)
      continue
    }
    // Inside a hunk. Emit its chosen content once, on the last row
    // of the hunk, so the relative position between anchors holds.
    const h = hunks[hunkIdx]
    if (row + 1 === h.endRow) {
      const chosen = decisionFor(h.id) === 'accept' ? h.added : h.removed
      for (const t of chosen) out.push(t)
      hunkIdx++
    }
  }
  return out.join('\n')
})

const finalUnchanged = computed(() => finalText.value === props.original)

function onAccept() {
  if (finalUnchanged.value) return
  emit('accept', finalText.value)
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
              <template v-if="totalHunks > 0">
                · <strong>{{ acceptedHunks }} of {{ totalHunks }}</strong> hunk<template v-if="totalHunks !== 1">s</template> kept
              </template>
              <span v-if="modelName" class="ai-model-tag"> · {{ modelName }}</span>
              <span v-if="executionMeta" class="ai-model-tag"> · {{ executionMeta }}</span>
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
          :class="['ai-line', 'ai-line--'+line.type, rowToHunk[idx] != null && decisionFor(rowToHunk[idx]!) === 'reject' ? 'ai-line--rejected' : '']"
        >{{ line.text || ' ' }}</span></code></pre>

        <div class="ai-pane-heading ai-pane-heading--optimized">Optimized</div>
        <pre class="ai-pane ai-pane--optimized"><code><span
          v-for="(line, idx) in diff.right"
          :key="'r'+idx"
          :class="['ai-line', 'ai-line--'+line.type, rowToHunk[idx] != null && decisionFor(rowToHunk[idx]!) === 'reject' ? 'ai-line--rejected' : '']"
        >{{ line.text || ' ' }}</span></code></pre>
      </div>

      <!-- PAI-219. Per-hunk decision panel. Lets the user keep some of
           the AI's edits while rejecting others, instead of all-or-
           nothing. Defaults to all-kept so the no-interaction path
           matches the original "Accept replaces everything" UX. -->
      <div v-if="totalHunks > 0" class="ai-hunks">
        <div class="ai-hunks-headrow">
          <span class="ai-hunks-title">
            Per-hunk decisions
            <span class="ai-hunks-counter">· {{ acceptedHunks }} of {{ totalHunks }} kept</span>
          </span>
          <div class="ai-hunks-bulk">
            <button
              type="button"
              class="ai-hunks-bulk-btn"
              :disabled="allAccepted"
              @click="setAllDecisions('accept')"
            >Keep all</button>
            <button
              type="button"
              class="ai-hunks-bulk-btn"
              :disabled="allRejected"
              @click="setAllDecisions('reject')"
            >Reject all</button>
          </div>
        </div>
        <ul class="ai-hunks-list">
          <li
            v-for="(h, idx) in diff.hunks"
            :key="h.id"
            :class="['ai-hunk', `ai-hunk--${decisionFor(h.id)}`]"
          >
            <span class="ai-hunk-tag">Hunk {{ idx + 1 }}</span>
            <div class="ai-hunk-preview">
              <div v-if="h.removed.length" class="ai-hunk-removed">
                <span class="ai-hunk-side-tag">−</span>
                <code>{{ h.removed.join(' / ') }}</code>
              </div>
              <div v-if="h.added.length" class="ai-hunk-added">
                <span class="ai-hunk-side-tag">+</span>
                <code>{{ h.added.join(' / ') }}</code>
              </div>
            </div>
            <button
              type="button"
              class="ai-hunk-toggle"
              :class="{ 'ai-hunk-toggle--active': decisionFor(h.id) === 'accept' }"
              :title="decisionFor(h.id) === 'accept' ? 'Reject this hunk to keep the original text' : 'Keep the AI rewrite for this hunk'"
              @click="toggleDecision(h.id)"
            >{{ decisionFor(h.id) === 'accept' ? 'Keep' : 'Reject' }}</button>
          </li>
        </ul>
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
          :disabled="finalUnchanged || retrying"
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
/* PAI-219: a rejected hunk dims its rows and strikes through the
   AI's proposed text on the right while the original stays
   readable on the left. Visible signal that this slice will fall
   back to the original on Accept. */
.ai-line--rejected { opacity: .45; }
.ai-pane--optimized .ai-line--rejected.ai-line--add { text-decoration: line-through; }

/* ── PAI-219 hunk decision panel ──────────────────────────────── */
.ai-hunks {
  border-top: 1px solid var(--border);
  padding: .85rem 1.25rem;
  background: #fafbfc;
  max-height: 220px;
  overflow-y: auto;
}
.ai-hunks-headrow {
  display: flex; align-items: baseline; justify-content: space-between;
  gap: .75rem;
  margin-bottom: .55rem;
}
.ai-hunks-title {
  font-size: 11px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--text-muted);
}
.ai-hunks-counter { font-weight: 500; text-transform: none; letter-spacing: 0; opacity: .8; }
.ai-hunks-bulk { display: flex; gap: .3rem; }
.ai-hunks-bulk-btn {
  font-size: 11.5px; font-weight: 500;
  background: transparent; color: var(--text);
  border: 1px solid var(--border); border-radius: 4px;
  padding: .15rem .55rem;
  cursor: pointer;
}
.ai-hunks-bulk-btn:disabled { opacity: .4; cursor: not-allowed; }
.ai-hunks-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: .35rem; }
.ai-hunk {
  display: grid; grid-template-columns: auto 1fr auto;
  align-items: center; gap: .65rem;
  padding: .4rem .55rem;
  background: #fff;
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 12.5px;
}
.ai-hunk--reject { opacity: .65; background: #fafafa; }
.ai-hunk-tag {
  font-size: 10.5px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--text-muted);
}
.ai-hunk-preview { display: flex; flex-direction: column; gap: .15rem; min-width: 0; }
.ai-hunk-removed, .ai-hunk-added {
  display: flex; gap: .35rem; align-items: baseline;
  font-family: 'DM Mono', 'Menlo', monospace;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.ai-hunk-removed code { color: #b91c1c; }
.ai-hunk-added code { color: #047857; }
.ai-hunk-side-tag { font-weight: 700; opacity: .6; font-family: inherit; }
.ai-hunk-toggle {
  font-size: 11.5px; font-weight: 600;
  background: transparent; color: var(--text-muted);
  border: 1px solid var(--border); border-radius: 4px;
  padding: .2rem .65rem;
  cursor: pointer;
  min-width: 64px;
}
.ai-hunk-toggle--active { background: var(--bp-blue-pale, #e8f1fb); color: var(--bp-blue-dark, #155078); border-color: var(--bp-blue, #4a7); }

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
