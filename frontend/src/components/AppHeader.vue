<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from "vue";
import { useRoute } from "vue-router";
import { useRouter } from "vue-router";
import AppIcon from "@/components/AppIcon.vue";
import SearchPalette from "@/components/SearchPalette.vue";
import { useSearchStore } from "@/stores/search";
import { useUndoStore } from "@/stores/undo";
import { useIssueRefreshPromptStore } from "@/stores/issueRefreshPrompt";
import { useAuthStore } from "@/stores/auth";

const route = useRoute();
const router = useRouter();
const search = useSearchStore();
const undo = useUndoStore();
const issueRefresh = useIssueRefreshPromptStore();
const auth = useAuthStore();

const ISSUE_AUTO_REFRESH_DEFAULT_SECONDS = 60;
const ISSUE_AUTO_REFRESH_MIN_SECONDS = 10;
const ISSUE_AUTO_REFRESH_STEP_SECONDS = 10;

const searchFocused = ref(false);
const topbarInput = ref<HTMLInputElement | null>(null);
const searchWrap = ref<HTMLElement | null>(null);
const paletteRef = ref<InstanceType<typeof SearchPalette> | null>(null);
const paletteVisible = ref(false);
const refreshCountdownSeconds = ref(ISSUE_AUTO_REFRESH_DEFAULT_SECONDS);
let refreshCountdownTimer: number | null = null;

const hasQuery = computed(() => search.query.length >= 2);
const undoStackCount = computed(
  () => undo.undoRows.length + undo.redoRows.length + undo.historyRows.length,
);
const issueAutoRefreshEnabled = computed(
  () => auth.user?.issue_auto_refresh_enabled ?? true,
);
const issueAutoRefreshIntervalSeconds = computed(() => {
  const value = Number(auth.user?.issue_auto_refresh_interval_seconds);
  return Number.isFinite(value)
    ? Math.max(ISSUE_AUTO_REFRESH_MIN_SECONDS, Math.round(value))
    : ISSUE_AUTO_REFRESH_DEFAULT_SECONDS;
});
const refreshCountdownLabel = computed(
  () => `(refreshing in ${refreshCountdownSeconds.value}s)`,
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

function onGlobalKeydown(e: KeyboardEvent) {
  if (!issueRefresh.visible) return;
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "r") {
    if (issueRefresh.triggerRefresh()) {
      e.preventDefault();
      e.stopPropagation();
    }
  }
}

function stopRefreshCountdown() {
  if (refreshCountdownTimer !== null) {
    window.clearInterval(refreshCountdownTimer);
    refreshCountdownTimer = null;
  }
}

function startRefreshCountdown() {
  stopRefreshCountdown();
  refreshCountdownSeconds.value = issueAutoRefreshIntervalSeconds.value;
  if (!issueRefresh.visible || !issueAutoRefreshEnabled.value) return;

  refreshCountdownTimer = window.setInterval(() => {
    const next = refreshCountdownSeconds.value - ISSUE_AUTO_REFRESH_STEP_SECONDS;
    if (next <= 0) {
      stopRefreshCountdown();
      const refreshed = issueRefresh.triggerRefresh();
      if (refreshed && issueRefresh.visible && issueAutoRefreshEnabled.value) {
        startRefreshCountdown();
      }
      return;
    }
    refreshCountdownSeconds.value = next;
  }, ISSUE_AUTO_REFRESH_STEP_SECONDS * 1000);
}

function openIssueAutoRefreshSettings() {
  void router.push({
    path: "/settings",
    query: { tab: "account", focus: "issue-auto-refresh" },
  });
}

onMounted(() => {
  void undo.refresh();
  window.addEventListener("keydown", onGlobalKeydown);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onGlobalKeydown);
  stopRefreshCountdown();
});

watch(
  () => issueRefresh.visible,
  (visible) => {
    if (visible) paletteVisible.value = false;
  },
);

watch(
  [
    () => issueRefresh.visible,
    issueAutoRefreshEnabled,
    issueAutoRefreshIntervalSeconds,
  ],
  startRefreshCountdown,
  { immediate: true },
);

const refreshPromptTitle = computed(
  () => `${issueRefresh.label}. Refresh issue list (⌘R).`,
);

// Exposed so AppLayout can focus on / shortcut
defineExpose({
  focus() {
    if (issueRefresh.visible) return;
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
        <Transition name="ah-center-swap" mode="out-in">
          <div
            v-if="issueRefresh.visible"
            key="refresh"
            class="ah-refresh-cluster"
          >
            <button
              class="ah-refresh-prompt"
              type="button"
              :title="refreshPromptTitle"
              @click="issueRefresh.triggerRefresh"
            >
              <span class="ah-refresh-pulse" aria-hidden="true"></span>
              <span class="ah-refresh-label">{{ issueRefresh.label }}</span>
              <span class="ah-refresh-action">Refresh</span>
              <kbd class="ah-refresh-shortcut">⌘R</kbd>
            </button>
            <button
              v-if="issueAutoRefreshEnabled"
              class="ah-refresh-countdown"
              type="button"
              title="Open issue auto-refresh settings"
              aria-label="Open issue auto-refresh settings"
              @click="openIssueAutoRefreshSettings"
            >
              <span>{{ refreshCountdownLabel }}</span>
              <AppIcon
                name="settings"
                :size="12"
                class="ah-refresh-countdown-icon"
                aria-hidden="true"
              />
            </button>
          </div>
          <div
            v-else
            ref="searchWrap"
            key="search"
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
              :anchor="searchWrap"
              @navigate="onPaletteNavigate"
              @close="onPaletteClose"
            />
          </div>
        </Transition>
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
  /* Center column auto-sizes from the search wrapper (which has its own
     max-width clamp), so left/right share the rest. Avoids the prior
     `minmax(260px, 32vw)` floor that ignored container shrinkage. */
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: 0.75rem;
  padding: 0 2rem 0 2.35rem;
  /* Hard height — header is structural chrome, never wraps, never grows.
     Mobile layout (< 900px viewport) overrides this to allow the
     multi-row stack. */
  height: 52px;
  overflow: hidden;
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
  flex-wrap: nowrap;
  white-space: nowrap;
  /* Soft right-edge fade so breadcrumb truncation dissolves rather
     than hard-clipping mid-letter when content overflows. */
  mask-image: linear-gradient(to right, #000 calc(100% - 16px), transparent);
  -webkit-mask-image: linear-gradient(to right, #000 calc(100% - 16px), transparent);
}
.ah-left :deep(.ah-title),
.ah-left :deep(.ah-subtitle) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
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

.ah-search-wrap,
.ah-refresh-prompt {
  width: 280px;
  max-width: min(48vw, 420px);
}

.ah-refresh-cluster {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.45rem;
  min-width: 0;
  max-width: min(72vw, 620px);
}

.ah-search-wrap {
  position: relative;
  display: flex;
  align-items: center;
  transition: width 0.18s ease, max-width 0.18s ease;
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

.ah-refresh-prompt {
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  min-width: 0;
  padding: 0 0.7rem;
  border: 1px solid color-mix(in srgb, #2f9d58 28%, var(--border));
  border-radius: 20px;
  background: color-mix(in srgb, #dff7e6 72%, var(--bg-card));
  color: #255f33;
  font-family: inherit;
  font-size: 13px;
  font-weight: 600;
  line-height: 1;
  cursor: pointer;
  box-shadow: 0 1px 2px rgba(16, 88, 37, 0.08);
  transition:
    background 0.15s,
    border-color 0.15s,
    box-shadow 0.15s;
}
.ah-refresh-prompt:hover {
  background: color-mix(in srgb, #dff7e6 86%, var(--bg-card));
  border-color: color-mix(in srgb, #2f9d58 44%, var(--border));
  box-shadow: 0 2px 8px rgba(16, 88, 37, 0.12);
}
.ah-refresh-pulse {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #35a853;
  animation: headerRefreshBreath 3s ease-in-out infinite;
}
.ah-refresh-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ah-refresh-action {
  flex: 0 0 auto;
  font-weight: 800;
  text-decoration: underline;
  text-underline-offset: 2px;
}
.ah-refresh-shortcut {
  flex: 0 0 auto;
  min-width: 2rem;
  padding: 0.12rem 0.35rem;
  border: 1px solid color-mix(in srgb, #2f9d58 20%, var(--border));
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.62);
  color: color-mix(in srgb, #255f33 76%, var(--text-muted));
  font: inherit;
  font-size: 11px;
  font-weight: 700;
  text-align: center;
}
.ah-refresh-countdown {
  height: 32px;
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
  padding: 0 0.15rem;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted);
  font-family: inherit;
  font-size: 12px;
  font-weight: 500;
  line-height: 1;
  white-space: nowrap;
  cursor: pointer;
  transition:
    background 0.15s,
    color 0.15s;
}
.ah-refresh-countdown:hover,
.ah-refresh-countdown:focus-visible {
  background: color-mix(in srgb, var(--text-muted) 8%, transparent);
  color: var(--text);
  outline: none;
}
.ah-refresh-countdown-icon {
  flex: 0 0 auto;
  opacity: 0;
  transform: translateX(-2px);
  color: var(--text-muted);
  transition:
    opacity 0.15s,
    transform 0.15s;
}
.ah-refresh-countdown:hover .ah-refresh-countdown-icon,
.ah-refresh-countdown:focus-visible .ah-refresh-countdown-icon {
  opacity: 0.72;
  transform: translateX(0);
}
@keyframes headerRefreshBreath {
  0%,
  100% {
    background: #35a853;
    box-shadow: 0 0 0 0 rgba(53, 168, 83, 0);
  }
  50% {
    background: #6fc47a;
    box-shadow: 0 0 0 5px rgba(53, 168, 83, 0);
  }
}
.ah-center-swap-enter-active,
.ah-center-swap-leave-active {
  transition:
    opacity 0.16s ease,
    transform 0.16s ease;
}
.ah-center-swap-enter-from,
.ah-center-swap-leave-to {
  opacity: 0;
  transform: translateY(-2px);
}

/* RIGHT */
.ah-right {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.5rem;
  min-width: 0;
  flex-wrap: nowrap;
  white-space: nowrap;
}

/* Slot the per-view Teleport content (customer pill, meta text, status,
   Edit button …) lives in. Sits to the left of the global Undo control. */
.ah-right-slot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.5rem;
  min-width: 0;
  flex-wrap: nowrap;
  white-space: nowrap;
  overflow: hidden;
}
.ah-right-slot :deep(.pd-customer-pill),
.ah-right-slot :deep(.ah-meta-text) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
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

/* ── Tiered autolayout ──────────────────────────────────────────────────
   Container queries on `.main` (see AppLayout.vue) so the header reacts
   to its own width, not the viewport. Pinning the side panel shrinks
   `.main` without changing the viewport, which @media never sees.
   Each tier is additive: Tier 2 also gets Tier 1's rules, etc.
   ───────────────────────────────────────────────────────────────────── */

/* Smooth opacity-based hides for elements that fade out across tiers. */
.ah-left :deep(.ah-subtitle),
.ah-right-slot :deep(.ah-meta-prefix),
.ah-right-slot :deep(.pd-customer-pill span),
.ah-undo-button > span:not(.ah-undo-count) {
  transition: opacity 0.18s ease, max-width 0.18s ease;
}

/* Tier 1: pinned-panel & similar — shed decoration. */
@container appchrome (max-width: 1100px) {
  .ah-left :deep(.ah-subtitle) { display: none; }
  .ah-right-slot :deep(.tag-chip) { display: none; }
  .ah-search-wrap,
  .ah-refresh-prompt { width: 220px; }
  .ah-search-wrap.focused { width: 300px; }
}

/* Tier 2: narrower — title truncates harder, meta strips its prefix,
   customer pill drops its text and keeps the icon-link affordance. */
@container appchrome (max-width: 920px) {
  .ah-left :deep(.ah-title) { max-width: 14ch; }
  .ah-right-slot :deep(.ah-meta-prefix) { display: none; }
  .ah-right-slot :deep(.pd-customer-pill span) { display: none; }
  .ah-right-slot :deep(.pd-customer-pill) { padding-left: 0.35rem; padding-right: 0.35rem; }
}

/* Tier 3: tight — search collapses to icon-button, expands inline on
   focus (existing .focused width animation handles the expand). Undo
   button drops its text label, keeps the icon + count. */
@container appchrome (max-width: 760px) {
  .ah-search-wrap { width: 36px; max-width: 36px; }
  .ah-search-wrap .ah-search-input { padding-right: 0; }
  .ah-search-wrap.focused { width: 280px; max-width: 280px; }
  .ah-search-wrap.focused .ah-search-input { padding-right: 28px; }
  .ah-refresh-prompt { width: 160px; max-width: 32vw; }
  .ah-refresh-countdown { font-size: 11px; }
  .ah-refresh-label { display: none; }
  .ah-undo-button > span:not(.ah-undo-count) { display: none; }
}

/* Tier 4: minimal — title hides, project key badge alone identifies
   the view. Status badge becomes a flat dot via the existing styling. */
@container appchrome (max-width: 600px) {
  .ah-left :deep(.ah-title) { display: none; }
}

/* Mobile viewport — restore the multi-row stack. The hard 52px height
   is desktop-only; mobile needs to grow to fit three rows. */
@media (max-width: 900px) {
  .app-header {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    grid-template-areas:
      "left right"
      "center center";
    gap: 0.75rem 1rem;
    padding: 0.75rem 1.1rem 0.75rem 1.25rem;
    height: auto;
    overflow: visible;
  }
  /* Mobile layout owns its own truncation behaviour; drop the fade
     mask so wrapped breadcrumb segments are fully visible. */
  .ah-left {
    mask-image: none;
    -webkit-mask-image: none;
    flex-wrap: wrap;
    white-space: normal;
  }
  .ah-right { flex-wrap: wrap; white-space: normal; }
  .ah-right-slot { flex-wrap: wrap; white-space: normal; overflow: visible; }

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
  .ah-search-wrap.focused,
  .ah-refresh-prompt {
    width: 100%;
    max-width: none;
  }
  .ah-refresh-cluster {
    width: 100%;
    max-width: none;
  }
  .ah-refresh-cluster .ah-refresh-prompt {
    flex: 1 1 auto;
    width: auto;
  }
  .ah-refresh-label { display: inline; }
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
  .ah-refresh-cluster {
    flex-wrap: wrap;
    justify-content: flex-start;
  }
  .ah-refresh-countdown {
    height: 24px;
  }
}
</style>
