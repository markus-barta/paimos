<script setup lang="ts">
/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-395 phase 4 — Knowledge entry side panel.
//
// Mirrors IssueSidePanel's chrome (overlay vs pinned modes, resize
// handle, slide transition) and reuses the same width + pinned
// singletons (useSidePanelWidth, useSidePanelPinned). AppLayout's
// existing right-inset machinery picks the new panel up for free —
// when pinned + visible, .main gets paddingRight equal to the panel
// width. Selection of which surface owns the panel is implicit: only
// one of IssueList / ProjectKnowledgeUnified is mounted at a time
// because /projects/:id renders one tab via v-else-if.
//
// The panel hosts KnowledgeEntryEditor unchanged. The editor's outer
// .ke-form padding is suppressed inside the panel via :deep() so the
// scopes don't double-pad.

import { computed, ref, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import KnowledgeEntryEditor from './KnowledgeEntryEditor.vue'
import {
  useSidePanelWidth,
  resetSidePanelWidth,
  SIDE_PANEL_DEFAULT_WIDTH,
  SIDE_PANEL_MIN_WIDTH,
  SIDE_PANEL_MAX_WIDTH_RATIO,
} from '@/composables/useSidePanelWidth'
import type { KnowledgeCategory, KnowledgeEntry, KnowledgeEntryInput } from '@/types'

interface CategoryDef {
  key: KnowledgeCategory
  label: string
  icon: string
}
const CATEGORY_META: Record<KnowledgeCategory, CategoryDef> = {
  memory:          { key: 'memory',          label: 'Memory',          icon: 'lightbulb'    },
  runbook:         { key: 'runbook',         label: 'Runbook',         icon: 'list-checks'  },
  external_system: { key: 'external_system', label: 'External system', icon: 'plug'         },
  related_project: { key: 'related_project', label: 'Related project', icon: 'link'         },
  guideline:       { key: 'guideline',       label: 'Guideline',       icon: 'shield-check' },
}

const props = defineProps<{
  entry: KnowledgeEntry | null
  creatingCategory: KnowledgeCategory | null
  draft: KnowledgeEntryInput
  saving: boolean
  saveError: string
  pinned: boolean
  canWrite: boolean
  projectId: number
}>()

const emit = defineEmits<{
  close: []
  save: [payload: KnowledgeEntryInput]
  delete: []
  promoted: [scope: string]
  reviewed: []
  'update:pinned': [v: boolean]
}>()

const activeCategory = computed<KnowledgeCategory | null>(
  () => props.creatingCategory ?? props.entry?.type ?? null,
)
const headerMeta = computed(() =>
  activeCategory.value ? CATEGORY_META[activeCategory.value] : null,
)
const visible = computed(
  () => props.entry !== null || props.creatingCategory !== null,
)

function togglePin() {
  emit('update:pinned', !props.pinned)
}

// ── Resize, mirrors IssueSidePanel. Shared singleton; `width` is the
// committed value, `draftWidth` is the smooth-drag local. ────────────
const { width } = useSidePanelWidth()
const draftWidth = ref(width.value)
const resizing = ref(false)

watch(width, (v) => {
  if (!resizing.value) draftWidth.value = v
})

function onResizeStart(e: MouseEvent) {
  e.preventDefault()
  resizing.value = true
  const startX = e.clientX
  const startW = draftWidth.value
  const maxW = Math.round(window.innerWidth * SIDE_PANEL_MAX_WIDTH_RATIO)

  function onMove(ev: MouseEvent) {
    const delta = startX - ev.clientX
    draftWidth.value = Math.min(
      maxW,
      Math.max(SIDE_PANEL_MIN_WIDTH, startW + delta),
    )
  }
  function onUp() {
    resizing.value = false
    width.value = draftWidth.value
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onUp)
  }
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
}

function resetWidth() {
  resetSidePanelWidth()
  draftWidth.value = SIDE_PANEL_DEFAULT_WIDTH
}

function onBackdropClick() {
  emit('close')
}
</script>

<template>
  <Transition name="sidepanel">
    <aside
      v-if="visible || pinned"
      :class="[
        'side-panel',
        { 'side-panel--pinned': pinned, 'side-panel--resizing': resizing },
      ]"
      :style="{ width: draftWidth + 'px' }"
    >
      <div
        v-if="!pinned && visible"
        class="sp-backdrop"
        @click="onBackdropClick"
      />
      <div
        class="sp-resize-handle"
        @mousedown="onResizeStart"
        @dblclick="resetWidth"
        title="Drag to resize · double-click to reset"
      />
      <div class="sp-content">
        <div class="sp-header">
          <button
            class="sp-pin"
            :class="{ 'sp-pin--active': pinned }"
            :title="pinned ? 'Unpin sidebar' : 'Pin sidebar'"
            @click="togglePin"
          >
            <AppIcon :name="pinned ? 'pin' : 'pin-off'" :size="14" />
          </button>
          <span v-if="headerMeta" class="sp-crumb">
            <AppIcon :name="headerMeta.icon" :size="13" />
            <span>{{ headerMeta.label }}</span>
            <span v-if="entry" class="sp-slug">· {{ entry.slug }}</span>
            <span v-else class="sp-new">· New</span>
          </span>
          <span class="sp-spacer" />
          <button
            v-if="entry && canWrite"
            class="sp-action-btn sp-action-btn--danger"
            :disabled="saving"
            title="Delete entry"
            @click="emit('delete')"
          >
            <AppIcon name="trash-2" :size="14" />
          </button>
          <button
            class="sp-action-btn"
            title="Close"
            @click="emit('close')"
          >
            <AppIcon name="x" :size="16" />
          </button>
        </div>

        <div v-if="visible && activeCategory" class="sp-body">
          <KnowledgeEntryEditor
            :category="activeCategory"
            :initial="draft"
            :current-slug="entry?.slug ?? null"
            :saving="saving"
            :save-error="saveError"
            :autosuggest-slug="creatingCategory !== null"
            :entry-id="entry?.id"
            :project-id="projectId"
            :needs-review="entry?.needs_review === true"
            :review-reason="entry?.review_reason ?? ''"
            @save="(p) => emit('save', p)"
            @cancel="emit('close')"
            @promoted="(s) => emit('promoted', s)"
            @reviewed="emit('reviewed')"
          />
        </div>
        <div v-else class="sp-empty">
          <AppIcon name="book-open" :size="20" />
          <p>Pick a knowledge entry from the list.</p>
        </div>
      </div>
    </aside>
  </Transition>
</template>

<style scoped>
/* Chrome mirrors IssueSidePanel.vue (PAI-275 / PAI-322 era). Keep
   geometry identical so AppLayout's --side-panel-width inset math
   works for both surfaces. */

.side-panel {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  z-index: 200;
}
.sp-backdrop {
  position: fixed;
  top: 0;
  left: 0;
  bottom: 0;
  right: 0;
  z-index: -1;
}
.side-panel--pinned {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  left: auto;
  z-index: 150;
  min-width: 300px;
}
.side-panel--resizing {
  user-select: none;
}
.sp-resize-handle {
  position: absolute;
  top: 0;
  left: -3px;
  bottom: 0;
  width: 6px;
  cursor: col-resize;
  z-index: 5;
}
.sp-resize-handle::after {
  content: '';
  position: absolute;
  top: 50%;
  left: 2px;
  width: 2px;
  height: 32px;
  transform: translateY(-50%);
  border-radius: 1px;
  background: var(--border);
  opacity: 0;
  transition: opacity .15s;
}
.sp-resize-handle:hover::after,
.side-panel--resizing .sp-resize-handle::after {
  opacity: 1;
  background: var(--bp-blue);
}
.sp-content {
  width: 100%;
  max-width: 90vw;
  height: 100%;
  background: var(--bg-card);
  border-left: 1px solid var(--border);
  box-shadow: -4px 0 24px rgba(0, 0, 0, 0.1);
  overflow-y: auto;
  padding: 1.25rem 1.5rem;
  display: flex;
  flex-direction: column;
  gap: .75rem;
}
.side-panel--pinned .sp-content {
  max-width: none;
  box-shadow: none;
  border-left: 2px solid var(--border);
  padding-left: 1.25rem;
}

.sp-header {
  display: flex;
  align-items: center;
  gap: .5rem;
  border-bottom: 1px solid var(--border);
  padding-bottom: .75rem;
  flex-shrink: 0;
}
.sp-pin {
  background: none;
  border: 1px solid transparent;
  cursor: pointer;
  padding: 3px;
  color: var(--text-muted);
  border-radius: 4px;
  display: flex;
  align-items: center;
}
.sp-pin:hover {
  background: var(--bg);
  color: var(--text);
}
.sp-pin--active {
  color: var(--bp-blue);
  border-color: var(--bp-blue);
  background: var(--bp-blue-pale);
}
.sp-crumb {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  font-size: 13px;
  color: var(--text-muted);
}
.sp-slug {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  color: var(--text);
}
.sp-new {
  font-style: italic;
  color: var(--text);
}
.sp-spacer {
  flex: 1;
}
.sp-action-btn {
  background: none;
  border: none;
  cursor: pointer;
  padding: 5px;
  color: var(--text-muted);
  border-radius: 50%;
  display: flex;
  align-items: center;
  transition: background .15s, color .15s;
}
.sp-action-btn:hover:not(:disabled) {
  background: var(--bg);
  color: var(--text);
}
.sp-action-btn:disabled {
  opacity: .3;
  cursor: default;
}
.sp-action-btn--danger {
  color: #dc2626;
}
.sp-action-btn--danger:hover:not(:disabled) {
  background: #fef2f2;
  color: #dc2626;
}

.sp-body {
  display: flex;
  flex-direction: column;
  gap: .5rem;
  min-height: 0;
}

/* Suppress the editor's outer .ke-form chrome inside the panel —
   the panel itself already provides the card frame + padding. Keep
   the editor's internal field rhythm. */
.sp-body :deep(.ke-form) {
  padding: 0;
  background: none;
  border: 0;
  border-radius: 0;
  gap: .65rem;
}

.sp-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: .5rem;
  padding: 3rem 1rem;
  color: var(--text-muted);
  font-size: 13px;
  text-align: center;
}

/* Slide transition — mirrors IssueSidePanel. */
.sidepanel-enter-active,
.sidepanel-leave-active {
  transition: opacity .2s, transform .2s;
}
.sidepanel-enter-active .sp-content,
.sidepanel-leave-active .sp-content {
  transition: transform .2s;
}
.sidepanel-enter-from { opacity: 0; }
.sidepanel-enter-from .sp-content { transform: translateX(100%); }
.sidepanel-leave-to { opacity: 0; }
.sidepanel-leave-to .sp-content { transform: translateX(100%); }
</style>
