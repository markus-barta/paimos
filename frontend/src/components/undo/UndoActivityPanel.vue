<script setup lang="ts">
import { computed, onMounted } from "vue";
import AppIcon from "@/components/AppIcon.vue";
import { fmtShortDateTime } from "@/utils/formatTime";
import { useUndoStore } from "@/stores/undo";
import type { MutationActivityRow } from "@/services/undoActivity";

const undo = useUndoStore();

const visible = computed(() => undo.panelOpen);

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

onMounted(() => {
  if (undo.panelOpen) void undo.refresh();
});
</script>

<template>
  <Teleport to="body">
    <div v-if="visible" class="undo-panel-wrap">
      <button class="undo-panel-backdrop" @click="undo.closePanel()" />
      <aside class="undo-panel">
        <header class="undo-panel__head">
          <div>
            <div class="undo-panel__eyebrow">
              Your last {{ undo.stackDepth }} actions
            </div>
            <h3>Recent activity</h3>
          </div>
          <div class="undo-panel__actions">
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              @click="undo.refresh()"
              :disabled="undo.loading"
            >
              <AppIcon name="refresh-cw" :size="12" /> Refresh
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              @click="undo.closePanel()"
            >
              Close
            </button>
          </div>
        </header>

        <div v-if="undo.error" class="undo-panel__empty">{{ undo.error }}</div>
        <div v-else-if="undo.loading" class="undo-panel__empty">
          Loading recent activity…
        </div>
        <div v-else class="undo-panel__body">
          <section class="undo-stack">
            <div class="undo-stack__label">Undo stack</div>
            <div v-if="!undo.undoRows.length" class="undo-panel__empty">
              No active undo entries.
            </div>
            <button
              v-for="(row, idx) in undo.undoRows"
              :key="row.id"
              type="button"
              class="undo-row"
              :class="[
                `undo-row--${rowTone(row)}`,
                { 'undo-row--inactive': idx > 0 },
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
                <span class="undo-row__chip">{{
                  idx === 0 ? "Undo" : "Queued"
                }}</span>
              </div>
            </button>
          </section>

          <section class="undo-stack">
            <div class="undo-stack__label">Redo stack</div>
            <div v-if="!undo.redoRows.length" class="undo-panel__empty">
              Nothing to redo.
            </div>
            <button
              v-for="(row, idx) in undo.redoRows"
              :key="row.id"
              type="button"
              class="undo-row"
              :class="[
                `undo-row--${rowTone(row)}`,
                { 'undo-row--inactive': idx > 0 },
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
                <span class="undo-row__chip">{{
                  idx === 0 ? "Redo" : "Waiting"
                }}</span>
              </div>
            </button>
          </section>

          <section class="undo-stack">
            <div class="undo-stack__label">History</div>
            <div v-if="!undo.historyRows.length" class="undo-panel__empty">
              No older entries yet.
            </div>
            <div
              v-for="row in undo.historyRows"
              :key="row.id"
              class="undo-row undo-row--history undo-row--inactive"
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
    </div>
  </Teleport>
</template>

<style scoped>
.undo-panel-wrap {
  position: fixed;
  inset: 0;
  z-index: 380;
}
.undo-panel-backdrop {
  position: absolute;
  inset: 0;
  border: 0;
  background: rgba(14, 24, 36, 0.2);
}
.undo-panel {
  position: absolute;
  top: 68px;
  right: 24px;
  width: min(540px, calc(100vw - 32px));
  max-height: calc(100vh - 92px);
  overflow: auto;
  border-radius: 22px;
  border: 1px solid rgba(46, 109, 164, 0.14);
  background:
    radial-gradient(
      circle at top right,
      rgba(46, 109, 164, 0.16),
      transparent 36%
    ),
    linear-gradient(
      180deg,
      rgba(255, 255, 255, 0.98),
      rgba(242, 245, 248, 0.98)
    );
  box-shadow: 0 22px 60px rgba(20, 34, 52, 0.16);
  padding: 1rem;
}
.undo-panel__head,
.undo-panel__actions,
.undo-row,
.undo-row__meta {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
.undo-panel__head {
  justify-content: space-between;
  margin-bottom: 0.8rem;
}
.undo-panel__eyebrow,
.undo-row__meta,
.undo-row__chip,
.undo-stack__label {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
  color: var(--text-muted);
}
.undo-panel__head h3 {
  font-family: "Bricolage Grotesque", serif;
  font-size: 1.2rem;
}
.undo-panel__body {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.undo-stack {
  display: flex;
  flex-direction: column;
  gap: 0.55rem;
}
.undo-stack__label {
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
.undo-row {
  width: 100%;
  justify-content: space-between;
  padding: 0.85rem 0.95rem;
  border-radius: 16px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  text-align: left;
}
.undo-row--undo {
  border-color: rgba(46, 109, 164, 0.18);
}
.undo-row--redo {
  border-color: rgba(22, 163, 74, 0.18);
}
.undo-row--inactive {
  opacity: 0.75;
}
.undo-row__main {
  display: flex;
  flex-direction: column;
  gap: 0.1rem;
}
.undo-row__main span,
.undo-panel__empty {
  color: var(--text-muted);
  font-size: 13px;
}
.undo-row__chip {
  padding: 0.18rem 0.45rem;
  border-radius: 999px;
  background: rgba(46, 109, 164, 0.08);
}
</style>
