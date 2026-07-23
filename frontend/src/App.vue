<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { computed, onMounted, onBeforeUnmount } from "vue";
import { RouterView, useRoute } from "vue-router";
import AppLayout from "@/components/AppLayout.vue";
import PortalLayout from "@/components/PortalLayout.vue";
import AppConfirmDialog from "@/components/AppConfirmDialog.vue";
import UndoToast from "@/components/undo/UndoToast.vue";
import UndoActivityPanel from "@/components/undo/UndoActivityPanel.vue";
import UndoConflictModal from "@/components/undo/UndoConflictModal.vue";
import { useAuthStore } from "@/stores/auth";
import { announceSessionExpired } from "@/api/client";
import { useUndoStore } from "@/stores/undo";
import { useChangesStream } from "@/composables/useChangesStream";

const auth = useAuthStore();
const undo = useUndoStore();
const route = useRoute();
const internalChromeEnabled = computed(
  () => auth.checked && !!auth.user && auth.user.role !== "external" && !route.meta.portal && !route.meta.public,
);
useChangesStream(internalChromeEnabled);

// ── Session-death heartbeat (PAI-322) ────────────────────────
// Two complementary triggers:
//
//   1. visibilitychange — fires when the tab regains focus after a
//      sleep / hidden period. Catches "laptop was closed all night"
//      cases without polling overhead while hidden.
//
//   2. 5-minute interval while visible — catches a slowly-dying
//      session for users who keep a tab in the foreground without
//      clicking. Stops while the tab is hidden so we don't burn
//      battery / CPU on a backgrounded tab. /auth/me is cheap and
//      hits the same sliding-renewal path as any other request, so
//      it doubles as a keep-alive for active users.
//
// /auth/me is in client.ts's carve-out list (so a fresh-load 401
// doesn't pop the modal), which means the global 401 interceptor
// does NOT fire here. We detect the was-logged-in → now-not
// transition explicitly and call announceSessionExpired so all
// open tabs converge on the modal via BroadcastChannel.
const HEARTBEAT_MS = 5 * 60 * 1000;
let heartbeatTimer: ReturnType<typeof setInterval> | null = null;

async function probeSession() {
  if (!auth.user) return;
  const wasLoggedIn = !!auth.user;
  try {
    await auth.fetchMe();
  } catch {
    /* fetchMe already swallows errors internally */
  }
  if (wasLoggedIn && !auth.user) {
    announceSessionExpired();
  }
}

function startHeartbeat() {
  if (heartbeatTimer != null) return;
  heartbeatTimer = setInterval(() => {
    if (document.visibilityState === "visible") probeSession();
  }, HEARTBEAT_MS);
}

function stopHeartbeat() {
  if (heartbeatTimer != null) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  }
}

async function handleVisibilityChange() {
  if (document.visibilityState !== "visible") {
    stopHeartbeat();
    return;
  }
  startHeartbeat();
  await probeSession();
}

onMounted(() => {
  document.addEventListener("visibilitychange", handleVisibilityChange);
  if (document.visibilityState === "visible") startHeartbeat();
});
onBeforeUnmount(() => {
  document.removeEventListener("visibilitychange", handleVisibilityChange);
  stopHeartbeat();
});
</script>

<template>
  <AppConfirmDialog />
  <UndoToast v-if="internalChromeEnabled" />
  <UndoActivityPanel v-if="internalChromeEnabled" />
  <UndoConflictModal
    v-if="internalChromeEnabled"
    :conflict="undo.conflict"
    :loading="undo.resolving"
    @cancel="undo.clearConflict()"
    @apply="undo.resolveConflict($event)"
  />
  <!-- Gate on auth.checked to prevent layout flash (sidebar visible before redirect) -->
  <LoadingText v-if="!auth.checked" class="app-loading" label="Loading…" />
  <RouterView v-else v-slot="{ Component, route }">
    <PortalLayout v-if="route.meta.portal">
      <component :is="Component" />
    </PortalLayout>
    <AppLayout v-else-if="!route.meta.public">
      <component :is="Component" />
    </AppLayout>
    <component v-else :is="Component" />
  </RouterView>
</template>

<style>
/* PAI-118: DM Sans is bundled via @fontsource in src/main.ts. */

*,
*::before,
*::after {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

:root {
  --brand-blue: #2e6da4;
  --brand-green: #16a34a;
  --brand-blue-dark: #1f4d75;
  --brand-blue-light: #4a8fc2;
  --brand-blue-pale: #dce9f4;
  --bg: #f2f5f8;
  --bg-card: #ffffff;
  --text: #1a2636;
  --text-muted: #637383;
  --border: #d1dce8;
  --radius: 6px;
  --shadow: 0 1px 3px rgba(30, 50, 80, 0.1), 0 1px 2px rgba(30, 50, 80, 0.06);
  --shadow-md: 0 4px 12px rgba(30, 50, 80, 0.12);

  /* Filter chip category tints (themeable) */
  --chip-type-tint: #3b82f6;
  --chip-status-tint: #ef4444;
  --chip-priority-tint: #f59e0b;
  --chip-default-bg: #f1f5f9;

  /* Accruals report accent — themeable via Settings → Appearance */
  --accruals-accent: #006497;
  --accruals-accent-soft: #e6f0f6;
  --accruals-accent-dark: #00466b;

  font-family: "DM Sans", system-ui, sans-serif;
  font-size: 14px;
  color: var(--text);
  background: var(--bg);
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
}

a {
  color: var(--brand-blue);
  text-decoration: none;
}
a:hover {
  color: var(--brand-blue-dark);
}

button {
  font-family: inherit;
  cursor: pointer;
}

/* Global icon vertical alignment — Lucide SVGs next to text */
svg.lucide {
  vertical-align: middle;
}

/* PAI-245: wrap the type negations in `:where()` so this rule keeps
   single-element specificity (0,0,1). Without `:where()`, the chain of
   `:not(...)` selectors stacks to (0,5,1) and starts overriding
   component-scoped padding (e.g. the 30px left-pad that clears the
   search icon in AppHeader). */
input:where(:not([type="checkbox"]):not([type="radio"]):not([type="file"]):not([type="range"]):not([type="color"])),
select,
textarea {
  font-family: inherit;
  font-size: 14px;
  line-height: 1.4;
  color: var(--text);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 0.5rem 0.75rem;
  outline: none;
  transition: border-color 0.15s;
  width: 100%;
}
input:where(:not([type="checkbox"]):not([type="radio"]):not([type="file"]):not([type="range"]):not([type="color"])):focus,
select:focus,
textarea:focus {
  border-color: var(--brand-blue);
  box-shadow: 0 0 0 3px rgba(46, 109, 164, 0.15);
}

/* PAI-396: unified placeholder treatment. Empty-state hint text reads
   as half-strength + slightly smaller than entered values so an empty
   form is visually distinct from a filled one at a glance. Italic
   was rejected for readability (sans-serif obliques + dyslexic
   readers); size + opacity carry the signal without that cost. The
   "revert on edit" semantic is automatic — placeholders disappear
   the moment a character lands. Custom-select empty-state spans
   (e.g. .meta-select-placeholder, .ms-label.muted in MetaSelect.vue)
   apply the same opacity / size locally so the visual language
   matches across native + custom inputs. */
::placeholder {
  opacity: 0.5;
  font-size: 0.9em;
  font-style: normal;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.45rem 1rem;
  font-size: 13px;
  font-weight: 500;
  border-radius: var(--radius);
  border: 1px solid transparent;
  transition:
    background 0.15s,
    border-color 0.15s,
    opacity 0.15s;
}
.btn-primary {
  background: var(--brand-blue);
  color: #fff;
  border-color: var(--brand-blue-dark);
}
.btn-primary:hover {
  background: var(--brand-blue-dark);
}
.btn-ghost {
  background: transparent;
  color: var(--text-muted);
  border-color: var(--border);
}
.btn-ghost:hover {
  background: var(--bg);
  color: var(--text);
}
.btn-danger {
  background: #c0392b;
  color: #fff;
  border-color: #a93226;
}
.btn-danger:hover {
  background: #a93226;
}
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Hotkey underline — used in dialog buttons to indicate keyboard shortcut.
   Buttons are inline-flex with gap, so <u> becomes a separate flex child.
   Zero the gap on text-only shortcut buttons; icon buttons don't use <u>. */
.btn:has(u) {
  gap: 0;
}
.btn u {
  text-decoration: underline;
  text-underline-offset: 2px;
  text-decoration-thickness: 1px;
}

/* Project status badges (active/archived) — still pill-shaped, used on project cards */
.badge {
  display: inline-block;
  padding: 0.15rem 0.55rem;
  font-size: 11px;
  font-weight: 600;
  border-radius: 20px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.badge-active {
  background: #d4edda;
  color: #155724;
}
.badge-archived {
  background: #e9ecef;
  color: #495057;
}

/* Issue status — dot + text, no background pill */
.issue-status {
  display: inline-flex;
  align-items: center;
  vertical-align: middle;
  gap: 0.35rem;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  white-space: nowrap;
  line-height: 1;
}
/* Filled dot (in-progress, done) */
.issue-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  display: inline-block;
  position: relative;
}
/* Outline ring variant (new, backlog, cancelled) — border only, transparent fill */
.issue-status-dot--outline {
  background: transparent !important;
  border: 2px solid;
  width: 8px;
  height: 8px;
}
/* Cancelled: diagonal strikethrough line */
.issue-status-dot--cancelled::after {
  content: "";
  position: absolute;
  top: 50%;
  left: -1px;
  right: -1px;
  height: 1.5px;
  background: #6b7280;
  transform: rotate(-45deg);
}

/* Issue priority — colored arrow + text */
.issue-priority {
  display: inline-flex;
  align-items: center;
  vertical-align: middle;
  gap: 0.25rem;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  white-space: nowrap;
  line-height: 1;
}
.issue-priority-arrow {
  font-size: 12px;
  line-height: 1;
  font-weight: 700;
}

/* Issue type — icon + text, colored per type */
.issue-type {
  display: inline-flex;
  align-items: center;
  vertical-align: middle;
  gap: 0.35rem;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  line-height: 1;
}
.issue-type svg {
  flex-shrink: 0;
  display: block; /* removes inline baseline gap */
}
.issue-type--epic {
  color: var(--type-epic, #5e35b1);
}
.issue-type--ticket {
  color: var(--type-ticket, var(--brand-blue-dark));
}
.issue-type--task {
  color: var(--type-task, #2e7d32);
}

/* ── AppHeader Teleport content — used from every view ─────────────────── */
/* Left zone: breadcrumb or title */
.ah-back {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  color: var(--text-muted);
  text-decoration: none;
  font-size: 13px;
  transition: color 0.15s;
  flex-shrink: 0;
}
.ah-back:hover {
  color: var(--brand-blue);
}
.ah-sep {
  color: var(--border);
  font-size: 13px;
  margin: 0 0.2rem;
  flex-shrink: 0;
}
.ah-crumb {
  color: var(--text-muted);
  font-size: 13px;
  text-decoration: none;
  transition: color 0.15s;
  flex-shrink: 0;
}
.ah-crumb:hover {
  color: var(--text);
}
.ah-crumb--current {
  color: var(--text);
  font-weight: 600;
}
.ah-key-badge {
  display: inline-flex;
  align-items: center;
  background: var(--brand-blue-pale);
  color: var(--brand-blue-dark);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.03em;
  padding: 0.15rem 0.5rem;
  border-radius: 4px;
  flex-shrink: 0;
}
.ah-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.02em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex-shrink: 1;
  min-width: 0;
}
.ah-subtitle {
  font-size: 12px;
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex-shrink: 1;
}
/* Right zone */
.ah-meta-text {
  font-size: 11.5px;
  color: var(--text-muted);
  white-space: nowrap;
  font-weight: 500;
}
/* PAI-256: Vue's default template-whitespace rule strips the space between
   `<span class="ah-meta-prefix">` and `<RouterLink class="ah-meta-link">`
   when they sit on different lines, so the prefix runs straight into the
   issue-key chip. Give the prefix a small right-margin so the two read
   as separate elements. (Tier 2 hides the prefix entirely; the margin
   collapses with display:none, no extra padding leaks through.) */
.ah-meta-prefix {
  margin-right: 0.35rem;
}
.ah-meta-link {
  color: var(--text);
  text-decoration: none;
  font-weight: 600;
}
.ah-meta-link:hover {
  color: var(--brand-blue-dark);
  text-decoration: underline;
}

/* PAI-245: status badges inside the app-header right slot read at the
   same visual weight as the ghost-styled Edit / Undo buttons next to
   them — outline style, muted color, no oversized pill. */
.ah-right-slot .badge {
  background: transparent;
  color: var(--text-muted);
  border: 1px solid var(--border);
  padding: 0.1rem 0.5rem;
  font-size: 10px;
  letter-spacing: 0.06em;
  font-weight: 600;
}
.ah-right-slot .badge-active {
  color: #15803d;
  border-color: #bbf7d0;
  background: transparent;
}
.ah-right-slot .badge-archived {
  color: var(--text-muted);
  border-color: var(--border);
  background: transparent;
}

/* Global search term highlight — used with v-html + useHighlight composable */
.search-highlight {
  background: #fef08a;
  color: inherit;
  border-radius: 2px;
  padding: 0 1px;
  font-style: normal;
}

/* ── Shared markdown rendering ──────────────────────────────────────────────── */
/* Single source of truth for all v-html markdown containers (detail, sidebar, portal). */
.md-rendered {
  white-space: normal !important;
}
.md-rendered h1,
.md-rendered h2,
.md-rendered h3 {
  font-weight: 700;
  margin: 0.5rem 0 0.2rem;
  line-height: 1.3;
}
.md-rendered h1 {
  font-size: 17px;
}
.md-rendered h2 {
  font-size: 15px;
}
.md-rendered h3 {
  font-size: 14px;
}
.md-rendered p {
  margin: 0 0 0.2rem;
}
.md-rendered > :last-child,
.md-rendered li > :last-child {
  margin-bottom: 0;
}
.md-rendered ul,
.md-rendered ol {
  padding-left: 1.4rem;
  margin: 0 0 0.2rem;
}
.md-rendered li {
  margin: 0.05rem 0;
}
.md-rendered br {
  content: "";
  display: block;
  margin-top: 0.1rem;
}
.md-rendered li > p {
  margin: 0;
}
.md-rendered li > p + p {
  margin-top: 0.25rem;
}
.md-rendered li:has(> input[type="checkbox"]) {
  list-style: none;
  margin-left: -1.4rem;
}
.md-rendered li > input[type="checkbox"] {
  width: auto;
  padding: 0;
  border: revert;
  border-radius: 0;
  background: revert;
  margin-right: 0.4rem;
  vertical-align: middle;
  display: inline;
  cursor: default;
}
.md-rendered code {
  font-family: "DM Mono", monospace;
  font-size: 12px;
  background: var(--bg);
  padding: 0.1rem 0.3rem;
  border-radius: 3px;
}
.md-rendered pre {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 0.75rem 1rem;
  overflow-x: auto;
  margin: 0.5rem 0;
}
.md-rendered pre code {
  background: none;
  padding: 0;
  font-size: 12px;
}
.md-rendered blockquote {
  border-left: 3px solid var(--border);
  padding-left: 0.75rem;
  color: var(--text-muted);
  margin: 0.5rem 0;
}
.md-rendered a {
  color: var(--brand-blue);
  text-decoration: underline;
}
.md-rendered img {
  max-width: 100%;
  height: auto;
}
/* Alignment + size classes emitted by the lightbox "Copy reference" button. */
.md-rendered .md-img {
  max-width: 100%;
  height: auto;
  display: block;
  margin: 0.5rem auto;
  border-radius: 4px;
}
.md-rendered .md-img--left {
  float: left;
  margin: 0.25rem 1rem 0.5rem 0;
}
.md-rendered .md-img--right {
  float: right;
  margin: 0.25rem 0 0.5rem 1rem;
}
.md-rendered .md-img--center {
  display: block;
  margin: 0.5rem auto;
}
.md-rendered .md-img--full {
  display: block;
  width: 100%;
  max-width: 100%;
}
.md-rendered .md-img--sm {
  max-width: 200px;
}
.md-rendered .md-img--md {
  max-width: 400px;
}
.md-rendered .md-img--lg {
  max-width: 600px;
}
/* Clear floats so a floated image doesn't bleed out of its paragraph. */
.md-rendered p::after {
  content: "";
  display: table;
  clear: both;
}
.md-rendered hr {
  border: none;
  border-top: 1px solid var(--border);
  margin: 0.75rem 0;
}
.md-rendered table {
  border-collapse: collapse;
  width: 100%;
  font-size: 13px;
  margin: 0.5rem 0;
}
.md-rendered th,
.md-rendered td {
  border: 1px solid var(--border);
  padding: 0.3rem 0.5rem;
  text-align: left;
}
.md-rendered th {
  background: var(--bg);
  font-weight: 600;
}

/* App-level loading (auth check gate) */
.app-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100vh;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
