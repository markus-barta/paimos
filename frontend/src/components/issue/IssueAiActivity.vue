<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import AppIcon from "@/components/AppIcon.vue";
import { fmtShortDateTime } from "@/utils/formatTime";
import {
  loadIssueActivity,
  type MutationActivityResponse,
} from "@/services/undoActivity";
import { useUndoStore } from "@/stores/undo";

const props = defineProps<{ issueId: number }>();

const loading = ref(false);
const error = ref("");
const actingLogId = ref<number | null>(null);
const payload = ref<MutationActivityResponse | null>(null);
const undoStore = useUndoStore();

async function load() {
  loading.value = true;
  error.value = "";
  try {
    payload.value = await loadIssueActivity(props.issueId);
  } catch (e: any) {
    error.value = e?.message ?? "Failed to load issue activity.";
  } finally {
    loading.value = false;
  }
}

async function run(mode: "undo" | "redo", logId: number) {
  actingLogId.value = logId;
  error.value = "";
  try {
    const row = [
      ...(payload.value?.undo_rows ?? []),
      ...(payload.value?.redo_rows ?? []),
    ].find((entry) => entry.id === logId);
    if (!row) return;
    if (mode === "undo") await undoStore.undoRow(row);
    else await undoStore.redoRow(row);
    await load();
  } catch (e: any) {
    error.value = e?.message ?? `${mode} failed.`;
  } finally {
    actingLogId.value = null;
  }
}

watch(
  () => props.issueId,
  () => {
    void load();
  },
  { immediate: true },
);
onMounted(() => {
  if (!payload.value) void load();
});
</script>

<template>
  <details class="issue-ai">
    <summary class="issue-ai__summary">
      <span class="issue-ai__title">
        <AppIcon name="rewind" :size="14" />
        Activity
      </span>
      <span class="issue-ai__badges">
        <span class="issue-ai__badge">{{
          (payload?.undo_rows.length ?? 0) +
          (payload?.redo_rows.length ?? 0) +
          (payload?.history_rows.length ?? 0)
        }}</span>
        <span class="issue-ai__hint">undo + redo + history</span>
      </span>
    </summary>

    <div class="issue-ai__body">
      <div v-if="loading" class="issue-ai__empty">Loading activity…</div>
      <div v-else-if="error" class="issue-ai__empty">{{ error }}</div>
      <div
        v-else-if="
          !(
            payload?.undo_rows.length ||
            payload?.redo_rows.length ||
            payload?.history_rows.length
          )
        "
        class="issue-ai__empty"
      >
        No tracked activity for this issue yet.
      </div>
      <div v-else class="issue-ai__list">
        <div
          v-for="row in payload?.undo_rows"
          :key="`undo-${row.id}`"
          class="issue-ai__item issue-ai__item--undo"
        >
          <div class="issue-ai__head">
            <div class="issue-ai__head-main">
              <strong>{{ row.summary }}</strong>
              <span class="issue-ai__outcome">undo</span>
            </div>
            <button
              type="button"
              class="issue-ai__undo"
              :disabled="actingLogId === row.id"
              @click="run('undo', row.id)"
            >
              {{ actingLogId === row.id ? "Undoing…" : "Undo" }}
            </button>
          </div>
          <div class="issue-ai__meta">
            <span class="issue-ai__mono">{{ row.mutation_type }}</span>
            <span>{{ fmtShortDateTime(row.created_at) }}</span>
          </div>
        </div>

        <div
          v-for="row in payload?.redo_rows"
          :key="`redo-${row.id}`"
          class="issue-ai__item issue-ai__item--redo"
        >
          <div class="issue-ai__head">
            <div class="issue-ai__head-main">
              <strong>{{ row.summary }}</strong>
              <span class="issue-ai__outcome">redo</span>
            </div>
            <button
              type="button"
              class="issue-ai__undo"
              :disabled="actingLogId === row.id"
              @click="run('redo', row.id)"
            >
              {{ actingLogId === row.id ? "Redoing…" : "Redo" }}
            </button>
          </div>
          <div class="issue-ai__meta">
            <span class="issue-ai__mono">{{ row.mutation_type }}</span>
            <span>{{ fmtShortDateTime(row.created_at) }}</span>
          </div>
        </div>

        <div
          v-for="row in payload?.history_rows"
          :key="`history-${row.id}`"
          class="issue-ai__item issue-ai__item--history"
        >
          <div class="issue-ai__head">
            <div class="issue-ai__head-main">
              <strong>{{ row.summary }}</strong>
              <span class="issue-ai__outcome">{{
                row.undone ? "undone" : "history"
              }}</span>
            </div>
          </div>
          <div class="issue-ai__meta">
            <span class="issue-ai__mono">{{ row.mutation_type }}</span>
            <span>{{ fmtShortDateTime(row.created_at) }}</span>
          </div>
        </div>
      </div>
    </div>
  </details>
</template>

<style scoped>
.issue-ai {
  margin-top: 1rem;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--bg-card);
}
.issue-ai[open] {
  box-shadow: 0 8px 20px rgba(30, 50, 80, 0.06);
}
.issue-ai__summary {
  list-style: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.8rem 0.95rem;
  cursor: pointer;
}
.issue-ai__summary::-webkit-details-marker {
  display: none;
}
.issue-ai__title,
.issue-ai__badges,
.issue-ai__meta,
.issue-ai__head,
.issue-ai__head-main {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  flex-wrap: wrap;
}
.issue-ai__head {
  justify-content: space-between;
}
.issue-ai__title {
  font-weight: 600;
}
.issue-ai__badge,
.issue-ai__mono,
.issue-ai__hint,
.issue-ai__outcome {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
}
.issue-ai__badge {
  padding: 0.12rem 0.38rem;
  border-radius: 999px;
  background: rgba(46, 109, 164, 0.1);
  color: var(--bp-blue-dark);
}
.issue-ai__hint,
.issue-ai__meta,
.issue-ai__outcome {
  color: var(--text-muted);
}
.issue-ai__body {
  padding: 0 0.95rem 0.95rem;
}
.issue-ai__list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.issue-ai__item {
  padding: 0.65rem 0.75rem;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--bg);
}
.issue-ai__item--undo {
  border-color: rgba(46, 109, 164, 0.18);
}
.issue-ai__item--redo {
  border-color: rgba(22, 163, 74, 0.18);
}
.issue-ai__undo {
  border: 1px solid var(--border);
  background: var(--bg-card);
  border-radius: 999px;
  padding: 0.2rem 0.55rem;
  font-size: 11px;
  font-family: "DM Mono", "JetBrains Mono", monospace;
  cursor: pointer;
}
.issue-ai__undo:disabled {
  opacity: 0.65;
  cursor: wait;
}
.issue-ai__empty {
  font-size: 13px;
  color: var(--text-muted);
}
</style>
