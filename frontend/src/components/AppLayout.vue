<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useSearchStore } from '@/stores/search'
import { useSidebarColors } from '@/composables/useSidebarColors'
import { useBranding } from '@/composables/useBranding'
import IssuePreviewCard from '@/components/IssuePreviewCard.vue'
import { useNewIssueStore } from '@/stores/newIssue'
import { useTimerPanel } from '@/composables/useTimerPanel'
import { useSidebarSprints } from '@/composables/useSidebarSprints'
import { useRecentProjects } from '@/composables/useRecentProjects'
import { useKeyboardShortcuts } from '@/composables/useKeyboardShortcuts'
import { api } from '@/api/client'
import { instanceLabel, loadInstance } from '@/api/instance'
import AppIcon from '@/components/AppIcon.vue'
import AppHeader from '@/components/AppHeader.vue'
import { useSidePanelPinned } from '@/composables/useSidePanelPinned'
import { useSidePanelWidth } from '@/composables/useSidePanelWidth'
import SidebarTimerPanel from '@/components/SidebarTimerPanel.vue'
import SidebarSprintTargets from '@/components/SidebarSprintTargets.vue'
import SidebarRecentProjects from '@/components/SidebarRecentProjects.vue'
import SidebarFooter from '@/components/SidebarFooter.vue'
import AppFooter from '@/components/AppFooter.vue'
import GlobalNewIssueModal from '@/components/GlobalNewIssueModal.vue'
import AttachmentLightbox from '@/components/issue/AttachmentLightbox.vue'
import SessionExpiredBanner from '@/components/SessionExpiredBanner.vue'
import AppDevLoginBanner from '@/components/AppDevLoginBanner.vue'
import { LS_SIDEBAR_COLLAPSED as COLLAPSED_KEY } from '@/constants/storage'

const auth    = useAuthStore()
const search  = useSearchStore()
const route   = useRoute()
const router  = useRouter()
const newIssue = useNewIssueStore()

// ── Composables ──────────────────────────────────────────────────────────────
const { initTimerPanel } = useTimerPanel()
const { sidebarSprints, loadSidebarSprints } = useSidebarSprints()

const { loadRecentProjects, startVisitTracking } = useRecentProjects()

const appHeaderRef = ref<InstanceType<typeof AppHeader> | null>(null)
const { init: initKeyboardShortcuts } = useKeyboardShortcuts(appHeaderRef)
initKeyboardShortcuts()

// ── Local state ──────────────────────────────────────────────────────────────
const isAdmin = computed(() => auth.user?.role === 'admin')
const show2FAWarning = computed(() => auth.checked && !!auth.user && auth.totpChecked && !auth.totpEnabled)

function isActive(path: string) {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}

function goTo2FASetup() {
  router.push('/settings?tab=account#two-factor-authentication')
}

// ── Collapsible sidebar ──────────────────────────────────────────────────────
const sidebarCollapsed = ref(localStorage.getItem(COLLAPSED_KEY) === '1')
const isExpanded = computed(() => !sidebarCollapsed.value)

function toggleSidebar() {
  sidebarCollapsed.value = !sidebarCollapsed.value
  localStorage.setItem(COLLAPSED_KEY, sidebarCollapsed.value ? '1' : '0')
}

// Clear search when navigating to dashboard (which has no issue list)
watch(() => route.path, (p) => {
  if (p === '/') search.clear()
})

const { bgColor, patternColor } = useSidebarColors()
const { branding } = useBranding()

// ── Instance info (staging banner + feature flags) ───────────────────────────
// Shared module state — see `src/api/instance.ts`. `attachmentsEnabled` is
// consumed directly by IssueAttachments / IssueDetailView / CreateIssueModal /
// IssueSidePanel to hide drop zones on instances without MinIO wired up.
loadInstance()

// ── New Issue button — context-aware ─────────────────────────────────────────
function openNewIssue() {
  const p = route.path
  const issueMatch = p.match(/^\/projects\/(\d+)\/issues\/(\d+)$/)
  const projectMatch = p.match(/^\/projects\/(\d+)$/)

  if (issueMatch) {
    newIssue.requestCreate({ projectId: Number(issueMatch[1]), parentId: Number(issueMatch[2]) })
  } else if (projectMatch) {
    newIssue.requestCreate({ projectId: Number(projectMatch[1]), type: 'ticket' })
  } else {
    newIssue.requestCreate({})
  }
}

// ── Dev panel badge ──────────────────────────────────────────────────────────
const completeFailures = ref(0)
async function loadDevSummary() {
  if (!isAdmin.value) return
  try {
    const s = await api.get<{ failures?: number; complete_failures?: number }>('/dev/test-reports/summary')
    completeFailures.value = s.failures ?? s.complete_failures ?? 0
  } catch { /* ignore */ }
}

// ── Pinned side-panel inset ──────────────────────────────────────────────────
// IssueSidePanel is `position: fixed`, so when pinned it would otherwise paint
// on top of AppHeader and the main content. Mirroring the panel width as
// `padding-right` on `.main` shrinks both the header and the content together.
const { pinned: sidePanelPinned, visible: sidePanelVisible } = useSidePanelPinned()
const { width: sidePanelWidth } = useSidePanelWidth()
const mainStyle = computed(() => ({
  paddingRight: (sidePanelPinned.value && sidePanelVisible.value)
    ? sidePanelWidth.value + 'px'
    : '0px',
}))

// ── Sidebar style ────────────────────────────────────────────────────────────
const sidebarStyle = computed(() => {
  const bg  = bgColor.value
  const pat = patternColor.value
  const enc = (s: string) => s.replace(/#/g, '%23')
  const svg = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='49' viewBox='0 0 28 49'%3E%3Cg fill-rule='evenodd'%3E%3Cg fill='${enc(pat)}' fill-opacity='0.4' fill-rule='nonzero'%3E%3Cpath d='M13.99 9.25l13 7.5v15l-13 7.5L1 31.75v-15l12.99-7.5zM3 17.9v12.7l10.99 6.34 11-6.35V17.9l-11-6.34L3 17.9zM0 15l12.98-7.5V0h-2v6.35L0 12.69v2.3zm0 18.5L12.98 41v8h-2v-6.85L0 35.81v-2.3zM15 0v7.5L27.99 15H28v-2.31h-.01L17 6.35V0h-2zm0 49v-8l12.99-7.5H28v2.31h-.01L17 42.15V49h-2z'/%3E%3C/g%3E%3C/g%3E%3C/svg%3E")`
  return { backgroundColor: bg, backgroundImage: svg }
})

// ── Lifecycle ────────────────────────────────────────────────────────────────
startVisitTracking()
onMounted(() => {
  initTimerPanel()
  loadSidebarSprints()
  loadRecentProjects()
  loadDevSummary()
})
</script>

<template>
  <!-- Outer wrapper so the session-expired banner can push the grid
       layout down without fighting the sidebar/header geometry. The
       banner lives outside .layout; .layout still owns the grid. -->
  <div class="app-shell">
    <AppDevLoginBanner />
    <SessionExpiredBanner />
    <div :class="['layout', { 'sidebar-collapsed': sidebarCollapsed }]">

    <aside
      :class="['sidebar']"
      :style="sidebarStyle"
    >
      <div class="sidebar-top">
        <!-- Instance banner (e.g. STAGING) — overlaid, does not push content -->
        <div v-if="instanceLabel" class="instance-banner">{{ instanceLabel }}</div>

        <!-- Brand: logo always, text only when expanded -->
        <RouterLink to="/" class="brand" :title="isExpanded ? '' : 'Home'">
          <img :src="branding.logo" :alt="branding.company" class="brand-logo" />
          <span class="sl brand-name">Project Management</span>
        </RouterLink>

        <!-- New Issue button — very top, above nav -->
        <button
          class="new-issue-btn"
          :class="{ 'new-issue-btn--collapsed': !isExpanded }"
          :title="isExpanded ? '' : 'New Issue'"
          @click="openNewIssue"
        >
          <AppIcon name="plus" :size="14" class="new-issue-icon" />
          <span class="sl new-issue-label">New Issue</span>
        </button>

        <nav class="nav">
          <RouterLink to="/"         :class="['nav-item', { active: isActive('/') }]"       :title="isExpanded ? '' : 'Dashboard'">
            <AppIcon name="house" /><span class="sl">Dashboard</span>
          </RouterLink>
          <RouterLink to="/projects" :class="['nav-item', { active: isActive('/projects') }]" :title="isExpanded ? '' : 'Projects'">
            <AppIcon name="folder" /><span class="sl">Projects</span>
          </RouterLink>
          <RouterLink to="/customers" :class="['nav-item', { active: isActive('/customers') }]" :title="isExpanded ? '' : 'Customers'">
            <AppIcon name="building-2" /><span class="sl">Customers</span>
          </RouterLink>
          <RouterLink to="/issues"   :class="['nav-item', { active: isActive('/issues') }]"   :title="isExpanded ? '' : 'Issues'">
            <AppIcon name="layout-list" /><span class="sl">Issues</span>
          </RouterLink>
          <RouterLink v-if="sidebarSprints.length" to="/sprint-board" :class="['nav-item', { active: isActive('/sprint-board') }]" :title="isExpanded ? '' : 'Sprint Board'">
            <AppIcon name="layout-grid" /><span class="sl">Sprint Board</span>
          </RouterLink>
          <RouterLink to="/reporting" :class="['nav-item', { active: isActive('/reporting') }]" :title="isExpanded ? '' : 'Reporting'">
            <AppIcon name="bar-chart-2" /><span class="sl">Reporting</span>
          </RouterLink>
          <RouterLink v-if="auth.user?.role === 'admin'" to="/integrations" :class="['nav-item', { active: isActive('/integrations') }]" :title="isExpanded ? '' : 'Integrations'">
            <AppIcon name="plug" /><span class="sl">Integrations</span>
          </RouterLink>
        </nav>

        <SidebarRecentProjects :is-expanded="isExpanded" />

        <SidebarSprintTargets :is-expanded="isExpanded" />
      </div>

      <!-- Edge hover zone — VS Code style collapse/expand trigger -->
      <div
        class="sidebar-edge"
        @click.stop="toggleSidebar"
        :title="sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'"
      >
        <div class="sidebar-edge-chevron">
          <AppIcon :name="sidebarCollapsed ? 'chevron-right' : 'chevron-left'" :size="13" />
        </div>
      </div>

      <div class="sidebar-bottom">
        <SidebarTimerPanel :is-expanded="isExpanded" />
        <SidebarFooter :is-expanded="isExpanded" :is-admin="isAdmin" :complete-failures="completeFailures" />
      </div>
    </aside>

    <main class="main" :style="mainStyle">
      <AppHeader ref="appHeaderRef" />

      <div class="main-content">
        <div v-if="show2FAWarning" class="totp-warning" role="alert">
          <span class="totp-warning-pulse" aria-hidden="true"></span>
          <span class="totp-warning-label">Two-factor authentication is not enabled. <button class="totp-warning-link" type="button" @click="goTo2FASetup">Set it up now</button> to secure your account.</span>
        </div>
        <!-- PAI-263: views were each rendering <AppFooter /> as their own
             last child. Hoisted here so it's a single source of truth and
             every authenticated route gets a footer (incl. ProjectDetailView,
             which previously had none). Routes that render their own
             footer/colophon (e.g. AccrualsPrintView) opt out via
             route.meta.hideAppFooter. -->
        <div :class="['view-body', { 'view-body--self-scroll': route.meta.scrollMode === 'self' }]">
          <slot />
        </div>
        <AppFooter v-if="!route.meta.hideAppFooter" />
      </div>
    </main>
    </div>
  </div>

  <IssuePreviewCard />
  <GlobalNewIssueModal />
  <AttachmentLightbox />

</template>

<style scoped>
/* ── Outer shell ─────────────────────────────────────────────────────────── */
/* .app-shell is a vertical flex so the SessionExpiredBanner (when
   visible) naturally pushes the .layout grid downward without needing
   to rewrite the existing grid geometry. */
.app-shell {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
}
.app-shell > .layout {
  flex: 1;
}

/* ── Layout grid ──────────────────────────────────────────────────────────── */
.layout {
  --sidebar-width: 230px;
  display: grid;
  grid-template-columns: var(--sidebar-width) 1fr;
  min-height: 100vh;
  transition: grid-template-columns 0.2s ease;
}
.layout.sidebar-collapsed {
  --sidebar-width: 48px;
  grid-template-columns: var(--sidebar-width) 1fr;
}

/* ── Sidebar shell ────────────────────────────────────────────────────────── */
.sidebar {
  color: var(--sidebar-text, #c8d5e2);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  position: sticky;
  top: 0;
  height: 100vh;
  width: 230px;
  overflow: hidden;
  flex-shrink: 0;
  z-index: 10;
  transition: width 0.2s ease;
}

.layout.sidebar-collapsed .sidebar {
  width: 48px;
}

/* ── Edge hover zone — VS Code style collapse/expand trigger ─────────────── */
.sidebar-edge {
  position: absolute;
  right: 0;
  top: 0;
  width: 6px;
  height: 100%;
  cursor: col-resize;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: width .12s, background .12s;
}
.sidebar-edge:hover {
  width: 18px;
  background: rgba(255,255,255,.09);
}
.sidebar-edge-chevron {
  opacity: 0;
  transition: opacity .15s;
  display: flex; align-items: center; justify-content: center;
  color: rgba(255,255,255,.65);
  pointer-events: none;
}
.sidebar-edge:hover .sidebar-edge-chevron { opacity: 1; }

/* ── Instance banner (staging/dev) — overlaid, does not push content ────── */
.instance-banner {
  position: absolute; top: 0; left: 0; right: 0; z-index: 10;
  background: #dc2626; color: #fff;
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; text-align: center;
  padding: .15rem .5rem; white-space: nowrap;
  overflow: hidden; text-overflow: ellipsis;
  opacity: .85;
}

/* ── Section paddings ─────────────────────────────────────────────────────── */
.sidebar-top    { padding: 1.25rem 1rem .75rem; flex: 1; min-height: 0; overflow-y: auto; overflow-x: hidden; }
.sidebar-bottom { padding: .75rem 1rem 1rem; display: flex; flex-direction: column; gap: .4rem; }

/* Tighter padding when collapsed (icon mode) */
.layout.sidebar-collapsed .sidebar .sidebar-top,
.layout.sidebar-collapsed .sidebar .sidebar-bottom {
  padding-left: 0;
  padding-right: 0;
}

/* Brand centred in collapsed mode — also zero gap so logo sits dead-centre */
.layout.sidebar-collapsed .sidebar .brand {
  justify-content: center;
  gap: 0;
}

/* Collapsed search icon button centred */
.layout.sidebar-collapsed .sidebar .nav-item--icon-btn {
  justify-content: center;
  padding-left: 0; padding-right: 0;
}

/* ── Sidebar labels (.sl) ─────────────────────────────────────────────────── */
/* .sl = "sidebar label" — any text that should hide when collapsed */
/* :deep() needed because .sl is used in sub-components */
.sidebar :deep(.sl) {
  opacity: 1;
  transform: translateX(0);
  transition: opacity 0.2s ease, transform 0.2s ease, max-width 0.7s ease;
  white-space: nowrap;
  overflow: hidden;
  display: inline-block;
  max-width: 200px;
}
.layout.sidebar-collapsed .sidebar :deep(.sl) {
  opacity: 0;
  transform: translateX(-4px);
  max-width: 0;
  pointer-events: none;
}

/* ── Brand ────────────────────────────────────────────────────────────────── */
.brand {
  display: flex; align-items: center; gap: .55rem;
  margin-bottom: 1rem; color: #fff; text-decoration: none; overflow: hidden;
}
.brand-logo { width: 22px; height: 22px; object-fit: contain; flex-shrink: 0; }
.brand-name { font-size: 11px; font-weight: 700; letter-spacing: .01em; line-height: 1.25; text-transform: uppercase; }

/* ── New Issue button ─────────────────────────────────────────────────────── */
.new-issue-btn {
  display: flex; align-items: center; gap: .6rem;
  margin-bottom: .4rem; width: 100%;
  padding: .45rem .65rem; border-radius: var(--radius);
  background: transparent; border: 1px solid transparent;
  color: #8faabf; font-size: 13px; font-weight: 600;
  cursor: pointer; font-family: inherit;
  transition: background .15s, color .15s, border-color .15s;
  overflow: hidden;
}
.new-issue-btn:hover {
  background: color-mix(in srgb, var(--bp-blue) 32%, transparent); color: #fff; border-color: color-mix(in srgb, var(--bp-blue) 55%, transparent);
}
.new-issue-btn--collapsed {
  justify-content: center;
  padding-left: 0; padding-right: 0; gap: 0;
}
.new-issue-icon { flex-shrink: 0; }
.new-issue-label { /* inherits .sl hide/show behaviour */ }

/* ── Nav ──────────────────────────────────────────────────────────────────── */
.nav { display: flex; flex-direction: column; gap: .15rem; }
.nav-item {
  display: flex; align-items: center; gap: .6rem;
  padding: .5rem .65rem; border-radius: var(--radius);
  color: #8faabf; font-size: 13px; font-weight: 500;
  transition: background .15s, color .15s; text-decoration: none; overflow: hidden;
}
.dev-badge {
  margin-left: auto; background: #ef4444; color: #fff;
  font-size: 10px; font-weight: 700; border-radius: 99px;
  padding: .05rem .4rem; line-height: 1.6; flex-shrink: 0;
}
.nav-item svg { width: 16px; height: 16px; flex-shrink: 0; }
.nav-item:hover { background: rgba(255,255,255,.06); color: #fff; }
.nav-item.active { background: color-mix(in srgb, var(--bp-blue) 30%, transparent); color: #fff; }

/* Centre icons when collapsed — gap:0 kills the residual space from the zero-width .sl span */
/* :deep() needed because nav-items also exist in sub-components */
.layout.sidebar-collapsed .sidebar :deep(.nav-item) {
  justify-content: center;
  padding-left: 0; padding-right: 0;
  gap: 0;
}

/* Collapsed user row: stack avatar + logout icon vertically, centred */
.layout.sidebar-collapsed .sidebar :deep(.user-row) {
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: .4rem 0;
  gap: .3rem;
}
.layout.sidebar-collapsed .sidebar :deep(.user-profile-link) {
  justify-content: center;
  flex: unset;
}
/* Collapsed: centre icons, hide text via .sl */
.layout.sidebar-collapsed .sidebar :deep(.sidebar-meta-row) {
  flex-direction: column; gap: .2rem;
}
.layout.sidebar-collapsed .sidebar :deep(.meta-badge) {
  justify-content: center;
}

/* ── Main ─────────────────────────────────────────────────────────────────── */
.main {
  height: 100vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  min-width: 0;
  transition: padding-right 0.18s ease;
  /* Make .main a container-query root so AppHeader can react to its
     own width — needed because pinning the side panel shrinks .main
     without changing the viewport, which @media can't see. */
  container-type: inline-size;
  container-name: appchrome;
}

.main-content {
  padding: 2rem 2.5rem;
  flex: 1;
  min-height: 0;
  min-width: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}
/* PAI-262: by default, do NOT add `flex: 1` / `min-height: 0` here.
   Tall page-scroll views (Settings, IssueDetail, …) need .view-body
   to size to their natural content height so .main-content owns the
   scroll and AppFooter sits at the bottom of natural flow.

   PAI-274: views that own their internal scroll (IssueList table with
   sticky thead + frozen columns) opt into the .view-body--self-scroll
   variant via route.meta.scrollMode === 'self'. That re-establishes a
   flex-bounded box with overflow: hidden — bounded so children that
   declare `flex: 1; min-height: 0; overflow: auto` (e.g. .issue-table-wrap)
   actually have a viewport to be the scrolling ancestor of, and
   overflow: hidden so PAI-262's bleed-into-AppFooter problem stays
   fixed for self-scroll views too. */
.view-body {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.view-body--self-scroll {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* ── 2FA warning ─────────────────────────────────────────────────────────── */
.totp-warning {
  width: 100%; margin: 0 0 1.25rem; display: flex; align-items: center; gap: .65rem;
  padding: .6rem 1rem; border-radius: var(--radius);
  background: #fde8e8; border: 1px solid #f5c6cb; font-size: 13px; color: #7b1a22;
}
.totp-warning-pulse {
  flex-shrink: 0; width: 9px; height: 9px; border-radius: 50%;
  background: #c0392b; animation: warningBreath 3s ease-in-out infinite;
}
.totp-warning-label { line-height: 1.45; }
.totp-warning-link {
  background: none; border: none; padding: 0; margin: 0;
  font: inherit; color: #7b1a22; font-weight: 600; text-decoration: underline; cursor: pointer;
}
.totp-warning-link:hover { color: #c0392b; }
@keyframes warningBreath {
  0%, 100% { background: #c0392b; box-shadow: 0 0 0 0 rgba(192,57,43,0); }
  50%       { background: #e8a317; box-shadow: 0 0 0 5px rgba(192,57,43,0); }
}
@media (max-width: 900px) {
  .main-content {
    padding: 1.25rem 1rem;
  }
  .totp-warning { flex-wrap: wrap; gap: .5rem; }
}
</style>
