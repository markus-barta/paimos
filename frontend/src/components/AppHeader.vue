<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { useRoute } from "vue-router";
import { useRouter } from "vue-router";
import AppIcon from "@/components/AppIcon.vue";
import SearchPalette from "@/components/SearchPalette.vue";
import { useSearchStore } from "@/stores/search";
import { useUndoStore } from "@/stores/undo";

const route = useRoute();
const router = useRouter();
const search = useSearchStore();
const undo = useUndoStore();

const searchFocused = ref(false);
const topbarInput = ref<HTMLInputElement | null>(null);
const paletteRef = ref<InstanceType<typeof SearchPalette> | null>(null);
const paletteVisible = ref(false);

const hasQuery = computed(() => search.query.length >= 2);
const undoStackCount = computed(
  () => undo.undoRows.length + undo.redoRows.length + undo.historyRows.length,
);

function onFocus() {
  searchFocused.value = true;
  if (search.query.trim().length >= 2) paletteVisible.value = true;
}
function onBlur() {
  searchFocused.value = false;
  // Delay closing palette so mousedown on palette items fires first
  setTimeout(() => {
    paletteVisible.value = false;
  }, 200);
}

function onInput() {
  const q = search.query.trim();
  search.setQuery(q);
  paletteVisible.value = q.length >= 2;
}

function onKeydown(e: KeyboardEvent) {
  // Forward arrow keys and enter to palette when visible
  if (
    paletteVisible.value &&
    ["ArrowDown", "ArrowUp", "Enter"].includes(e.key)
  ) {
    paletteRef.value?.handleKeydown(e);
    return;
  }
  if (e.key === "Escape") {
    if (paletteVisible.value) {
      paletteVisible.value = false;
      e.preventDefault();
    } else {
      topbarInput.value?.blur();
    }
  }
  if (e.key === "Enter" && !paletteVisible.value) {
    // Navigate to issues page with current search
    if (route.path !== "/issues" && !route.path.startsWith("/projects/")) {
      router.push("/issues");
    }
  }
}

function clear() {
  search.clear();
  paletteVisible.value = false;
  topbarInput.value?.focus();
}

function onPaletteNavigate(path: string) {
  paletteVisible.value = false;
  router.push(path);
}

function onPaletteClose() {
  paletteVisible.value = false;
}

onMounted(() => {
  void undo.refresh();
});

// Exposed so AppLayout can focus on / shortcut
defineExpose({
  focus() {
    topbarInput.value?.focus();
    topbarInput.value?.select();
  },
});
</script>

<template>
  <header class="app-header">
    <!-- LEFT: breadcrumb or page title — filled via Teleport from each view -->
    <div id="app-header-left" class="ah-left" />

    <!-- CENTER: persistent search -->
    <div class="ah-center">
      <div class="ah-center-row">
        <div
          :class="[
            'ah-search-wrap',
            { focused: searchFocused, active: hasQuery },
          ]"
        >
          <AppIcon name="search" :size="13" class="ah-search-icon" />
          <input
            ref="topbarInput"
            v-model="search.query"
            type="search"
            class="ah-search-input"
            placeholder="Search issues… (/ or ⌘K)"
            autocomplete="off"
            spellcheck="false"
            @focus="onFocus"
            @blur="onBlur"
            @input="onInput"
            @keydown="onKeydown"
          />
          <button
            v-if="search.query"
            class="ah-search-clear"
            title="Clear search"
            @mousedown.prevent="clear"
          >
            <AppIcon name="x" :size="12" :stroke-width="2.5" />
          </button>
          <SearchPalette
            ref="paletteRef"
            :visible="paletteVisible"
            @navigate="onPaletteNavigate"
            @close="onPaletteClose"
          />
        </div>
      </div>
    </div>

    <!-- RIGHT: contextual actions (filled via Teleport from each view)
         followed by the Undo control, which sits to the far right
         next to whichever per-view "Edit" button the view teleports in. -->
    <div class="ah-right">
      <div id="app-header-right" class="ah-right-slot" />
      <button
        class="btn btn-ghost btn-sm ah-undo-button"
        :class="{ 'ah-undo-button--active': undo.panelOpen }"
        :title="undo.panelOpen ? 'Close undo history' : 'Open undo history'"
        @click="undo.panelOpen ? undo.closePanel() : undo.openPanel()"
      >
        <AppIcon name="rewind" :size="13" />
        <span>Undo</span>
        <span v-if="undoStackCount" class="ah-undo-count">
          {{ undoStackCount }}
        </span>
      </button>
    </div>
  </header>
</template>

<style scoped>
.app-header {
  display: grid;
  grid-template-columns: minmax(0, 1.3fr) minmax(260px, 32vw) minmax(0, 1fr);
  align-items: center;
  gap: 1rem;
  padding: 0 2rem 0 2.35rem;
  min-height: 52px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-card);
  flex-shrink: 0;
  width: 100%;
  min-width: 0;
  transition: padding 0.2s ease;
}

/* LEFT */
.ah-left {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
  overflow: hidden;
  padding-left: 0.15rem;
}

/* CENTER */
.ah-center {
  display: flex;
  justify-content: center;
  min-width: 0;
}

.ah-center-row {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.6rem;
  min-width: 0;
  width: 100%;
}

.ah-search-wrap {
  position: relative;
  display: flex;
  align-items: center;
  width: 280px;
  max-width: min(48vw, 420px);
  transition: width 0.2s;
}
.ah-search-wrap.focused {
  width: 380px;
}

.ah-search-icon {
  position: absolute;
  left: 9px;
  color: var(--text-muted);
  pointer-events: none;
}

.ah-search-input {
  width: 100%;
  height: 32px;
  padding: 0 28px 0 30px;
  border: 1px solid var(--border);
  border-radius: 20px;
  background: var(--bg);
  font-size: 13px;
  font-family: inherit;
  color: var(--text);
  outline: none;
  transition:
    border-color 0.15s,
    background 0.15s,
    box-shadow 0.15s;
  -webkit-appearance: none;
}
.ah-search-wrap.active .ah-search-input,
.ah-search-wrap.focused .ah-search-input {
  border-color: var(--bp-blue);
  background: var(--bg-card);
}
.ah-search-wrap.focused .ah-search-input {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--bp-blue) 15%, transparent);
}
.ah-search-input::-webkit-search-cancel-button {
  display: none;
}

.ah-search-clear {
  position: absolute;
  right: 8px;
  background: none;
  border: none;
  padding: 2px;
  cursor: pointer;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  border-radius: 50%;
  transition:
    color 0.15s,
    background 0.15s;
}
.ah-search-clear:hover {
  color: var(--text);
  background: var(--bg);
}

/* RIGHT */
.ah-right {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.5rem;
  min-width: 0;
  flex-wrap: wrap;
}

/* Slot the per-view Teleport content (customer pill, meta text, status,
   Edit button …) lives in. Sits to the left of the global Undo control. */
.ah-right-slot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.5rem;
  min-width: 0;
  flex-wrap: wrap;
}

/* PAI-246: `.btn-sm` is defined per-view (ProjectDetailView etc.) but
   not globally, so AppHeader's own buttons (Undo here) need their own
   copy to match the Edit button next to them. */
.btn-sm { padding: 0.3rem 0.65rem; font-size: 12px; }

/* Undo button now matches the ghost-button neighbours (Edit, import,
   export). Active state keeps a soft tint without a colored pill. */
.ah-undo-button.ah-undo-button--active {
  background: color-mix(in srgb, var(--bp-blue) 8%, transparent);
  color: var(--bp-blue-dark);
  border-color: color-mix(in srgb, var(--bp-blue) 25%, var(--border));
}
.ah-undo-button .ah-undo-count {
  min-width: 1rem;
  padding: 0 0.28rem;
  border-radius: 999px;
  background: color-mix(in srgb, var(--bp-blue) 14%, transparent);
  color: var(--bp-blue-dark);
  font-size: 10px;
  font-weight: 600;
  text-align: center;
  font-variant-numeric: tabular-nums;
  margin-left: 0.05rem;
}

@media (max-width: 900px) {
  .app-header {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    grid-template-areas:
      "left right"
      "center center";
    gap: 0.75rem 1rem;
    padding: 0.75rem 1.1rem 0.75rem 1.25rem;
  }

  .ah-left {
    grid-area: left;
  }

  .ah-center {
    grid-area: center;
    justify-content: stretch;
  }

  .ah-center-row {
    width: 100%;
    justify-content: stretch;
  }

  .ah-right {
    grid-area: right;
  }

  .ah-search-wrap,
  .ah-search-wrap.focused {
    width: 100%;
    max-width: none;
  }
}

@media (max-width: 640px) {
  .app-header {
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas:
      "left"
      "center"
      "right";
    gap: 0.65rem;
    padding: 0.75rem 0.9rem;
  }

  .ah-left,
  .ah-right {
    justify-content: flex-start;
  }

  .ah-center-row {
    flex-wrap: wrap;
  }
}
</style>
