<script setup lang="ts">
import { ref, computed } from "vue";
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
          class="ah-search-history"
          :title="undo.panelOpen ? 'Close recent activity' : 'Recent activity'"
          @mousedown.prevent="
            undo.panelOpen ? undo.closePanel() : undo.openPanel()
          "
        >
          <AppIcon name="rewind" :size="12" />
        </button>
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

    <!-- RIGHT: contextual actions — filled via Teleport from each view -->
    <div id="app-header-right" class="ah-right" />
  </header>
</template>

<style scoped>
.app-header {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 1rem;
  padding: 0 2.5rem;
  height: 52px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-card);
  flex-shrink: 0;
}

/* LEFT */
.ah-left {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
  overflow: hidden;
}

/* CENTER */
.ah-center {
  display: flex;
  justify-content: center;
}

.ah-search-wrap {
  position: relative;
  display: flex;
  align-items: center;
  width: 280px;
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
  padding: 0 56px 0 30px;
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

.ah-search-history,
.ah-search-clear {
  position: absolute;
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
.ah-search-history {
  right: 28px;
}
.ah-search-clear {
  right: 8px;
}
.ah-search-history:hover,
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
}
</style>
