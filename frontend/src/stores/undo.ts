import { defineStore } from "pinia";
import { computed, ref } from "vue";
import type {
  MutationActivityResponse,
  MutationActivityRow,
  UndoConflictResponse,
  UndoResolutionPayload,
} from "@/services/undoActivity";
import {
  loadUndoActivity,
  undoByLogId,
  redoByLogId,
  resolveUndo,
  resolveRedo,
} from "@/services/undoActivity";
import { ApiError } from "@/api/client";

interface UndoToastState {
  id: number;
  title: string;
  detail: string;
  mode: "undo" | "redo";
  expiresAt: number;
}

export const useUndoStore = defineStore("undo", () => {
  const panelOpen = ref(false);
  const loading = ref(false);
  const error = ref("");
  const payload = ref<MutationActivityResponse | null>(null);

  const toast = ref<UndoToastState | null>(null);
  let toastTimer: number | null = null;

  const conflict = ref<UndoConflictResponse | null>(null);
  const resolving = ref(false);

  const undoRows = computed(() => payload.value?.undo_rows ?? []);
  const redoRows = computed(() => payload.value?.redo_rows ?? []);
  const historyRows = computed(() => payload.value?.history_rows ?? []);
  const stackDepth = computed(() => payload.value?.stack_depth ?? 3);

  async function refresh() {
    loading.value = true;
    error.value = "";
    try {
      payload.value = await loadUndoActivity();
    } catch (e: any) {
      error.value = e?.message ?? "Failed to load recent activity.";
    } finally {
      loading.value = false;
    }
  }

  function openPanel() {
    panelOpen.value = true;
    void refresh();
  }

  function closePanel() {
    panelOpen.value = false;
  }

  function showToast(row: MutationActivityRow, mode: "undo" | "redo") {
    if (toastTimer) {
      window.clearTimeout(toastTimer);
      toastTimer = null;
    }
    toast.value = {
      id: row.id,
      title:
        mode === "undo" ? row.subject_label : `${row.subject_label} redone`,
      detail: row.summary,
      mode,
      expiresAt: Date.now() + 8000,
    };
    toastTimer = window.setTimeout(() => {
      toast.value = null;
      toastTimer = null;
    }, 8000);
  }

  function showSyntheticToast(
    input: { id: number; title: string; detail: string },
    mode: "undo" | "redo",
  ) {
    if (toastTimer) {
      window.clearTimeout(toastTimer);
      toastTimer = null;
    }
    toast.value = {
      id: input.id,
      title: input.title,
      detail: input.detail,
      mode,
      expiresAt: Date.now() + 8000,
    };
    toastTimer = window.setTimeout(() => {
      toast.value = null;
      toastTimer = null;
    }, 8000);
  }

  function dismissToast() {
    if (toastTimer) {
      window.clearTimeout(toastTimer);
      toastTimer = null;
    }
    toast.value = null;
  }

  function findRow(id: number, mode: "undo" | "redo"): MutationActivityRow | null {
    const source = mode === "undo" ? undoRows.value : redoRows.value;
    return source.find((row) => row.id === id) ?? null;
  }

  async function runWithConflict(
    row: MutationActivityRow,
    mode: "undo" | "redo",
  ) {
    try {
      if (mode === "undo") await undoByLogId(row.id);
      else await redoByLogId(row.id);
      showToast(row, mode === "undo" ? "redo" : "undo");
      await refresh();
    } catch (e: any) {
      const apiErr = e as ApiError;
      if (
        apiErr?.status === 409 &&
        typeof (e as any)?.conflicts !== "undefined"
      ) {
        conflict.value = e as UndoConflictResponse;
        return;
      }
      if (apiErr?.status === 409) {
        error.value = e?.message ?? "Undo conflict.";
        return;
      }
      error.value = e?.message ?? "Undo action failed.";
    }
  }

  async function undoRow(row: MutationActivityRow) {
    await runWithConflict(row, "undo");
  }

  async function redoRow(row: MutationActivityRow) {
    await runWithConflict(row, "redo");
  }

  async function resolveConflict(payloadBody: UndoResolutionPayload) {
    if (!conflict.value) return;
    resolving.value = true;
    try {
      if (conflict.value.mode === "undo")
        await resolveUndo(conflict.value.log_id, payloadBody);
      else await resolveRedo(conflict.value.log_id, payloadBody);
      conflict.value = null;
      await refresh();
    } catch (e: any) {
      error.value = e?.message ?? "Conflict resolution failed.";
    } finally {
      resolving.value = false;
    }
  }

  function clearConflict() {
    conflict.value = null;
  }

  async function actToast() {
    if (!toast.value) return;
    const row = findRow(toast.value.id, toast.value.mode);
    if (!row) {
      await refresh();
      const refreshed = findRow(toast.value.id, toast.value.mode);
      if (!refreshed) {
        openPanel();
        return;
      }
      if (toast.value.mode === "undo") await undoRow(refreshed);
      else await redoRow(refreshed);
      return;
    }
    if (toast.value.mode === "undo") await undoRow(row);
    else await redoRow(row);
  }

  return {
    panelOpen,
    loading,
    error,
    payload,
    toast,
    conflict,
    resolving,
    undoRows,
    redoRows,
    historyRows,
    stackDepth,
    refresh,
    openPanel,
    closePanel,
    showToast,
    showSyntheticToast,
    dismissToast,
    actToast,
    undoRow,
    redoRow,
    resolveConflict,
    clearConflict,
  };
});
