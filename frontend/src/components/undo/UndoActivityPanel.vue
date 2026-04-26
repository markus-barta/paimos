<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from "vue";
import AppIcon from "@/components/AppIcon.vue";
import { fmtShortDateTime } from "@/utils/formatTime";
import { useUndoStore } from "@/stores/undo";
import { onOtherSidePanelOpened } from "@/composables/useSidePanelExclusion";
import {
  useSidePanelWidth,
  SIDE_PANEL_DEFAULT_WIDTH,
  SIDE_PANEL_MIN_WIDTH,
  SIDE_PANEL_MAX_WIDTH_RATIO,
} from "@/composables/useSidePanelWidth";
import { ref } from "vue";
import type { MutationActivityRow } from "@/services/undoActivity";

const undo = useUndoStore();

const visible = computed(() => undo.panelOpen);
const stackCount = computed(
  () => undo.undoRows.length + undo.redoRows.length + undo.historyRows.length,
);
const subtitle = computed(() =>
  stackCount.value
    ? `${stackCount.value} action${stackCount.value === 1 ? "" : "s"}`
    : "No recent activity",
);

function rowTone(row: MutationActivityRow) {
  if (row.redoable) return "redo";
  if (row.on_user_stack) return "undo";
  return "history";
}

function historyLabel(row: MutationActivityRow) {
  if (!row.undoable) return "Irreversible";
  if (row.undone) return "Undone";
  return "History";
}

// ── Right-edge sidebar plumbing (mirrors ProjectAuxPanel) ─────────────
// Reusing the same width singleton means the undo panel lines up with
// the issue side panel and project aux panels — they all share one
// resizable right-edge slot, and only one is ever visible at a time
// (enforced via useSidePanelExclusion).
const { width } = useSidePanelWidth();
const resizing = ref(false);
const draftWidth = ref(width.value);
watch(width, (v) => {
  if (!resizing.value) draftWidth.value = v;
});

function onResizeStart(e: MouseEvent) {
  e.preventDefault();
  resizing.value = true;
  const startX = e.clientX;
  const startW = draftWidth.value;
  const maxW = Math.round(window.innerWidth * SIDE_PANEL_MAX_WIDTH_RATIO);
  function onMove(ev: MouseEvent) {
    const delta = startX - ev.clientX;
    draftWidth.value = Math.min(maxW, Math.max(SIDE_PANEL_MIN_WIDTH, startW + delta));
  }
  function onUp() {
    resizing.value = false;
    width.value = draftWidth.value;
    document.removeEventListener("mousemove", onMove);
    document.removeEventListener("mouseup", onUp);
  }
  document.addEventListener("mousemove", onMove);
  document.addEventListener("mouseup", onUp);
}
function resetWidth() {
  draftWidth.value = SIDE_PANEL_DEFAULT_WIDTH;
  width.value = SIDE_PANEL_DEFAULT_WIDTH;
}

function onKey(e: KeyboardEvent) {
  if (!visible.value) return;
  if (e.key === "Escape") {
    undo.closePanel();
    e.preventDefault();
  }
}

let unbindExclusion: (() => void) | null = null;
onMounted(() => {
  if (undo.panelOpen) void undo.refresh();
  window.addEventListener("keydown", onKey);
  unbindExclusion = onOtherSidePanelOpened("undo", () => undo.closePanel());
});
onUnmounted(() => {
  window.removeEventListener("keydown", onKey);
  unbindExclusion?.();
  unbindExclusion = null;
});
</script>

<template>
  <Teleport to="body">
    <Transition name="undo-panel">
      <aside
        v-if="visible"
        :class="['undo-panel', { 'undo-panel--resizing': resizing }]"
        :style="{ width: draftWidth + 'px' }"
        role="dialog"
        aria-label="Recent activity"
      >
        <div
          class="undo-panel__resize"
          @mousedown="onResizeStart"
          @dblclick="resetWidth"
          title="Drag to resize · double-click to reset"
        />

        <header class="undo-panel__head">
          <div class="undo-panel__head-id">
            <h2 class="undo-panel__title">Recent activity</h2>
            <span class="undo-panel__subtitle">{{ subtitle }}</span>
          </div>
          <div class="undo-panel__head-actions">
            <button
              type="button"
              class="undo-panel__icon-btn"
              :disabled="undo.loading"
              title="Refresh"
              @click="undo.refresh()"
            >
              <AppIcon name="refresh-cw" :size="14" />
            </button>
            <button
              type="button"
              class="undo-panel__icon-btn"
              title="Close (Esc)"
              @click="undo.closePanel()"
            >
              <AppIcon name="x" :size="16" />
            </button>
          </div>
        </header>

        <div v-if="undo.error" class="undo-panel__notice undo-panel__notice--error">
          {{ undo.error }}
        </div>
        <div v-else-if="undo.loading && !undo.payload" class="undo-panel__notice">
          Loading recent activity…
        </div>

        <div class="undo-panel__body">
          <section class="undo-section">
            <header class="undo-section__head">
              <span class="undo-section__label">Undo stack</span>
              <span class="undo-section__count">{{ undo.undoRows.length }}</span>
            </header>
            <p v-if="!undo.undoRows.length" class="undo-section__empty">
              No active undo entries.
            </p>
            <button
              v-for="(row, idx) in undo.undoRows"
              :key="row.id"
              type="button"
              class="undo-row"
              :class="[
                `undo-row--${rowTone(row)}`,
                { 'undo-row--queued': idx > 0 },
              ]"
              :disabled="idx > 0"
              @click="idx === 0 && undo.undoRow(row)"
            >
              <div class="undo-row__main">
                <strong>{{ row.subject_label }}</strong>
                <span>{{ row.summary }}</span>
              </div>
              <div class="undo-row__meta">
                <span>{{ fmtShortDateTime(row.created_at) }}</span>
                <span class="undo-row__chip">
                  {{ idx === 0 ? "Undo" : "Queued" }}
                </span>
              </div>
            </button>
          </section>

          <section class="undo-section">
            <header class="undo-section__head">
              <span class="undo-section__label">Redo stack</span>
              <span class="undo-section__count">{{ undo.redoRows.length }}</span>
            </header>
            <p v-if="!undo.redoRows.length" class="undo-section__empty">
              Nothing to redo.
            </p>
            <button
              v-for="(row, idx) in undo.redoRows"
              :key="row.id"
              type="button"
              class="undo-row"
              :class="[
                `undo-row--${rowTone(row)}`,
                { 'undo-row--queued': idx > 0 },
              ]"
              :disabled="idx > 0"
              @click="idx === 0 && undo.redoRow(row)"
            >
              <div class="undo-row__main">
                <strong>{{ row.subject_label }}</strong>
                <span>{{ row.summary }}</span>
              </div>
              <div class="undo-row__meta">
                <span>{{ fmtShortDateTime(row.created_at) }}</span>
                <span class="undo-row__chip">
                  {{ idx === 0 ? "Redo" : "Waiting" }}
                </span>
              </div>
            </button>
          </section>

          <section class="undo-section">
            <header class="undo-section__head">
              <span class="undo-section__label">History</span>
              <span class="undo-section__count">{{ undo.historyRows.length }}</span>
            </header>
            <p v-if="!undo.historyRows.length" class="undo-section__empty">
              No older entries yet.
            </p>
            <div
              v-for="row in undo.historyRows"
              :key="row.id"
              class="undo-row undo-row--history undo-row--queued"
              :title="
                row.undoable
                  ? undefined
                  : 'Hard-delete is irreversible; restore from a backup if needed.'
              "
            >
              <div class="undo-row__main">
                <strong>{{ row.subject_label }}</strong>
                <span>{{ row.summary }}</span>
              </div>
              <div class="undo-row__meta">
                <span>{{ fmtShortDateTime(row.created_at) }}</span>
                <span class="undo-row__chip">{{ historyLabel(row) }}</span>
              </div>
            </div>
          </section>
        </div>
      </aside>
    </Transition>
  </Teleport>
</template>

<style scoped>
/* Frame — same right-edge slot pattern as ProjectAuxPanel.
   z-index 145 sits between the page body and the issue side panel
   (z-150 pinned / z-200 unpinned). Mutual exclusion via the side-panel
   exclusion bus prevents stacking, but the lower z keeps the issue
   panel layered cleanly during the close transition. */
.undo-panel {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  z-index: 145;
  background: var(--bg-card);
  border-left: 2px solid var(--border);
  display: flex;
  flex-direction: column;
  min-width: 300px;
  max-width: 90vw;
  box-shadow: -2px 0 16px rgba(0, 0, 0, 0.04);
}
.undo-panel--resizing {
  user-select: none;
}

/* Drag handle on the left edge — same affordance as IssueSidePanel
   and ProjectAuxPanel so the gesture is identical across panels. */
.undo-panel__resize {
  position: absolute;
  top: 0;
  left: -3px;
  bottom: 0;
  width: 6px;
  cursor: col-resize;
  z-index: 5;
}
.undo-panel__resize::after {
  content: "";
  position: absolute;
  top: 50%;
  left: 2px;
  width: 2px;
  height: 32px;
  transform: translateY(-50%);
  border-radius: 1px;
  background: var(--border);
  opacity: 0;
  transition: opacity 0.15s;
}
.undo-panel__resize:hover::after,
.undo-panel--resizing .undo-panel__resize::after {
  opacity: 1;
  background: var(--bp-blue);
}

/* Header — matches ProjectAuxPanel proportions. */
.undo-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.9rem 1.1rem 0.85rem;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.undo-panel__head-id {
  display: flex;
  align-items: baseline;
  gap: 0.55rem;
  min-width: 0;
}
.undo-panel__title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.01em;
  margin: 0;
  white-space: nowrap;
}
.undo-panel__subtitle {
  font-size: 11px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.undo-panel__head-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}
.undo-panel__icon-btn {
  background: none;
  border: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  color: var(--text-muted);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.undo-panel__icon-btn:hover:not(:disabled) {
  background: var(--bg);
  color: var(--text);
}
.undo-panel__icon-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Notice strip for loading / error states. */
.undo-panel__notice {
  padding: 0.75rem 1.1rem;
  font-size: 12px;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
}
.undo-panel__notice--error {
  color: #b91c1c;
  background: #fef2f2;
}

/* Scrollable body. */
.undo-panel__body {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 1rem 1.1rem 1.4rem;
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}

.undo-section {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.undo-section__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
}
.undo-section__label {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 10px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.undo-section__count {
  font-size: 11px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  min-width: 1.25rem;
  padding: 0.05rem 0.4rem;
  border-radius: 999px;
  background: color-mix(in srgb, var(--bp-blue) 8%, transparent);
  text-align: center;
}
.undo-section__empty {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0;
  padding: 0.4rem 0;
}

.undo-row {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.6rem;
  padding: 0.65rem 0.75rem;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  text-align: left;
  transition: border-color 0.15s, background 0.15s, transform 0.05s;
}
.undo-row:not(:disabled):hover {
  background: var(--bg);
  border-color: color-mix(in srgb, var(--bp-blue) 28%, var(--border));
}
.undo-row:not(:disabled):active {
  transform: translateY(1px);
}
.undo-row:disabled {
  cursor: default;
}
.undo-row--undo {
  border-left: 3px solid color-mix(in srgb, var(--bp-blue) 55%, transparent);
}
.undo-row--redo {
  border-left: 3px solid color-mix(in srgb, var(--bp-green) 55%, transparent);
}
.undo-row--history {
  opacity: 0.78;
}
.undo-row--queued {
  opacity: 0.6;
}

.undo-row__main {
  display: flex;
  flex-direction: column;
  gap: 0.1rem;
  min-width: 0;
}
.undo-row__main strong {
  font-size: 13px;
  color: var(--text);
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.undo-row__main span {
  font-size: 12px;
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.undo-row__meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.2rem;
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 10px;
  color: var(--text-muted);
  flex-shrink: 0;
}
.undo-row__chip {
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  background: color-mix(in srgb, var(--bp-blue) 10%, transparent);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

/* Slide-in transition — matches ProjectAuxPanel for visual coherence. */
.undo-panel-enter-active,
.undo-panel-leave-active {
  transition:
    transform 0.22s cubic-bezier(0.2, 0.7, 0.2, 1),
    opacity 0.18s;
}
.undo-panel-enter-from {
  transform: translateX(20px);
  opacity: 0;
}
.undo-panel-leave-to {
  transform: translateX(20px);
  opacity: 0;
}
</style>
