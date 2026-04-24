<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-145 follow-up. Slide-in side panel that mirrors IssueSidePanel's
 right-edge positioning + shared width (via useSidePanelWidth) but is
 generic — content goes in the slot. Used by ProjectDetailView for
 Documents and Cooperation toggles, so each gets a real focused
 surface instead of competing with the issue list for the page body.

 Width is the same singleton as IssueSidePanel — they line up. Only
 one panel ever shows at the right edge at a time; ProjectDetailView
 enforces mutual exclusion (toggling an aux panel closes the issue
 panel, opening an issue closes the aux panel).
-->
<script setup lang="ts">
import { onMounted, onBeforeUnmount, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import {
  useSidePanelWidth,
  SIDE_PANEL_DEFAULT_WIDTH,
  SIDE_PANEL_MIN_WIDTH,
  SIDE_PANEL_MAX_WIDTH_RATIO,
} from '@/composables/useSidePanelWidth'

const props = defineProps<{
  open: boolean
  title: string
  /** Optional subtitle / count next to the title (e.g. "3 documents"). */
  subtitle?: string
}>()
const emit = defineEmits<{ close: [] }>()

const { width } = useSidePanelWidth()

// ── Resize handle (matches IssueSidePanel's behaviour) ─────────────
// Local draft during a drag so the host page only reflows on commit.
import { ref } from 'vue'
const resizing = ref(false)
const draftWidth = ref(width.value)
watch(width, v => { if (!resizing.value) draftWidth.value = v })

function onResizeStart(e: MouseEvent) {
  e.preventDefault()
  resizing.value = true
  const startX = e.clientX
  const startW = draftWidth.value
  const maxW = Math.round(window.innerWidth * SIDE_PANEL_MAX_WIDTH_RATIO)
  function onMove(ev: MouseEvent) {
    const delta = startX - ev.clientX
    draftWidth.value = Math.min(maxW, Math.max(SIDE_PANEL_MIN_WIDTH, startW + delta))
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
  draftWidth.value = SIDE_PANEL_DEFAULT_WIDTH
  width.value = SIDE_PANEL_DEFAULT_WIDTH
}

// Escape key closes — same convention as AppModal so muscle memory
// transfers. Only active while open.
function onKey(e: KeyboardEvent) {
  if (!props.open) return
  if (e.key === 'Escape') {
    emit('close')
    e.preventDefault()
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <Transition name="aux-panel">
      <aside
        v-if="open"
        :class="['aux-panel', { 'aux-panel--resizing': resizing }]"
        :style="{ width: draftWidth + 'px' }"
        role="dialog"
        :aria-label="title"
      >
        <div
          class="aux-resize-handle"
          @mousedown="onResizeStart"
          @dblclick="resetWidth"
          title="Drag to resize · double-click to reset"
        />

        <header class="aux-head">
          <div class="aux-head-id">
            <h2 class="aux-title">{{ title }}</h2>
            <span v-if="subtitle" class="aux-subtitle">{{ subtitle }}</span>
          </div>
          <button class="aux-close" @click="emit('close')" title="Close (Esc)">
            <AppIcon name="x" :size="16" />
          </button>
        </header>

        <div class="aux-body">
          <slot />
        </div>
      </aside>
    </Transition>
  </Teleport>
</template>

<style scoped>
/* ── Frame ─────────────────────────────────────────────────────────
   Pinned-style positioning (z-index sits *between* the page content
   and IssueSidePanel's pinned z-150, so an issue side panel still
   layers in front if both happen to be open during a transition).
*/
.aux-panel {
  position: fixed; top: 0; right: 0; bottom: 0;
  z-index: 145;
  background: var(--bg-card);
  border-left: 2px solid var(--border);
  display: flex; flex-direction: column;
  min-width: 300px; max-width: 90vw;
  box-shadow: -2px 0 16px rgba(0, 0, 0, .04);
}
.aux-panel--resizing { user-select: none; }

/* Slim drag affordance on the left edge — same shape as IssueSidePanel's
   handle so users only need to learn the gesture once. */
.aux-resize-handle {
  position: absolute; top: 0; left: -3px; bottom: 0; width: 6px;
  cursor: col-resize; z-index: 5;
}
.aux-resize-handle::after {
  content: ''; position: absolute; top: 50%; left: 2px; width: 2px; height: 32px;
  transform: translateY(-50%); border-radius: 1px;
  background: var(--border); opacity: 0; transition: opacity .15s;
}
.aux-resize-handle:hover::after,
.aux-panel--resizing .aux-resize-handle::after {
  opacity: 1; background: var(--bp-blue);
}

/* ── Header ────────────────────────────────────────────────────── */
.aux-head {
  display: flex; align-items: center; justify-content: space-between;
  gap: .75rem;
  padding: .9rem 1.1rem .85rem;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.aux-head-id { display: flex; align-items: baseline; gap: .55rem; min-width: 0; }
.aux-title {
  font-size: 14px; font-weight: 700; color: var(--text);
  letter-spacing: -.01em; margin: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.aux-subtitle {
  font-size: 11px; color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.aux-close {
  background: none; border: none;
  display: inline-flex; align-items: center; justify-content: center;
  width: 28px; height: 28px;
  border-radius: 6px;
  color: var(--text-muted); cursor: pointer;
  transition: background .12s, color .12s;
}
.aux-close:hover { background: var(--bg); color: var(--text); }

/* ── Body ──────────────────────────────────────────────────────── */
.aux-body {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 1rem 1.1rem 1.4rem;
}

/* ── Slide-in transition ───────────────────────────────────────── */
.aux-panel-enter-active, .aux-panel-leave-active {
  transition: transform .22s cubic-bezier(.2, .7, .2, 1), opacity .18s;
}
.aux-panel-enter-from { transform: translateX(20px); opacity: 0; }
.aux-panel-leave-to   { transform: translateX(20px); opacity: 0; }
</style>
