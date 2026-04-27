<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted, onUnmounted, defineExpose, nextTick } from 'vue'
import { useSort } from '@/composables/useSort'
import type { ColDefs } from '@/composables/useSort'
import { useRouter, useRoute } from 'vue-router'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'
import IssueSidePanel from '@/components/IssueSidePanel.vue'
import { api } from '@/api/client'
import { useConfirm } from '@/composables/useConfirm'
import { useNewIssueStore } from '@/stores/newIssue'
import { useDraggedIssue } from '@/stores/draggedIssue'
import { storeToRefs } from 'pinia'
import type { Issue, Tag, SavedView, Sprint, User } from '@/types'
import type { Project } from '@/types'
import { useViews, getLastViewId, setLastViewId } from '@/composables/useViews'
import { useAuthStore } from '@/stores/auth'
import { useColumnConfig } from '@/composables/useColumnConfig'
import { useTimeUnit } from '@/composables/useTimeUnit'
import { useSearchStore } from '@/stores/search'
import { useTableAppearance } from '@/composables/useTableAppearance'
import { useIssueContext } from '@/composables/useIssueContext'

// ── Extracted composables ──────────────────────────────────────────────────
import { useIssueFilter } from '@/composables/useIssueFilter'
import { normalizeSavedFiltersJSON } from '@/composables/useIssueFilter'
import { useSprintNav } from '@/composables/useSprintNav'
import { useTreeView } from '@/composables/useTreeView'
import { useSelection } from '@/composables/useSelection'
import { useInlineEdit } from '@/composables/useInlineEdit'

// ── Extracted sub-components ──────────────────────────────────────────────
import IssueTable from '@/components/IssueTable.vue'
import IssueTreeView from '@/components/IssueTreeView.vue'
import CreateIssueModal from '@/components/CreateIssueModal.vue'
import BulkChangeModal from '@/components/BulkChangeModal.vue'
import IssueFilterPanel from '@/components/IssueFilterPanel.vue'
import IssueViewsPanel from '@/components/IssueViewsPanel.vue'
import EpicCascadeDialog from '@/components/EpicCascadeDialog.vue'
import IssueListRefreshBanner from '@/components/IssueListRefreshBanner.vue'
import {
  LS_EPIC_DISPLAY_MODE as EPIC_MODE_KEY,
  lsFiltersKey,
} from '@/constants/storage'
import {
  useSidePanelPinned,
  setSidePanelPinned,
  setSidePanelVisible,
} from '@/composables/useSidePanelPinned'
import {
  notifySidePanelOpened,
  onOtherSidePanelOpened,
} from '@/composables/useSidePanelExclusion'

const props = defineProps<{
  projectId?: number
  issues: Issue[]
  compact?: boolean
  title?: string
  initialProjectIds?: number[]
  initialType?: string
  defaultParentId?: number
  projectAllIssues?: Issue[]
  initialViewId?: number
  initialPanelIssueId?: number
  refreshStale?: boolean
  refreshCount?: number | null
}>()

const { users, allTags, costUnits, releases, projects, sprints } = useIssueContext()

const emit = defineEmits<{
  created: [issue: Issue]
  updated: [issue: Issue]
  deleted: [id: number]
  'cost-unit-added': [value: string]
  'release-added':   [value: string]
  'view-applied':    [viewId: number]
  'views-changed':   []
  'refresh-list':    []
}>()

const router = useRouter()
const route  = useRoute()
const { confirm } = useConfirm()

const searchStore = useSearchStore()
const searchQuery = computed(() => searchStore.query.trim())

const { formatHours, label: timeLabel, toggle: toggleTimeUnit } = useTimeUnit()
const authStore = useAuthStore()
const isAdmin   = computed(() => authStore.user?.role === 'admin')

const colScope = computed(() => `project-${props.projectId ?? 'global'}`)
const { ALL_COLUMNS, isVisible, toggle: toggleCol, reset: resetCols, setFromJSON: setColsFromJSON, toJSON: colsToJSON } = useColumnConfig(colScope)

const colPanelOpen   = ref(false)
const colPanelEl     = ref<HTMLElement | null>(null)
const colBtnEl       = ref<HTMLElement | null>(null)
const filterPanelEl  = ref<HTMLElement | null>(null)
const viewsPanelOpen = ref(false)
const viewsPanelEl   = ref<HTMLElement | null>(null)
const viewsBtnEl     = ref<HTMLElement | null>(null)

// ── Refs for composable inputs ──────────────────────────────────────────────
const projectIdRef = computed(() => props.projectId)
const issuesRef = computed(() => props.issues)
const compactRef = computed(() => !!props.compact)
const projectsRef = projects
const usersRef = users
const allTagsRef = allTags
const costUnitsRef = costUnits
const releasesRef = releases
const sprintsRef = sprints

const sortKey = ref('')
const sortDir = ref<string>('asc')
const loadedSprints = ref<Sprint[]>([])

function allSprintsGetter(): Sprint[] {
  return sprints.value.length ? sprints.value : loadedSprints.value
}

// Sprint nav needs to be created first so we can pass its toolbarSprintIds to filter.
// But sprint nav needs showArchivedSprints from filter. Break the cycle by creating
// showArchivedSprints as a standalone ref, shared by both.
const showArchivedSprintsRef = ref(false)

const sprintNav = useSprintNav({
  projectId: projectIdRef,
  allSprints: allSprintsGetter,
  showArchivedSprints: showArchivedSprintsRef,
})

const filterReal = useIssueFilter({
  projectId: projectIdRef,
  issues: issuesRef,
  compact: compactRef,
  projects: projectsRef,
  users: usersRef,
  allTags: allTagsRef,
  costUnits: costUnitsRef,
  releases: releasesRef,
  sprints: sprintsRef,
  toolbarSprintIds: sprintNav.toolbarSprintIds,
  sortKey,
  sortDir,
})

// Sync the shared ref
watch(filterReal.showArchivedSprints, v => { showArchivedSprintsRef.value = v })

const {
  filterStatus, filterPriority, filterType, filterCostUnit, filterRelease,
  filterAssignee, filterTags, filterProjects, filterSprints, filterEpic,
  showArchivedSprints, treeView, filterPanelOpen,
  complexTab, complexTabSearch,
  availableTags, assignableUsers, complexTabs, complexBadge,
  activeFilterCount, filterChipGroups, filteredIssues,
  assigneeIsAny,
  pickerProjects, pickerUsers, pickerTags, pickerCostUnits, pickerReleases, pickerSprints,
  clearAllFilters, removeChip, clearChipGroup,
  toggleChipNegation,
  loadFilters, saveFilters, currentFiltersJSON,
  switchComplexTab, setAssigneeAny,
  filterWatchSources,
} = filterReal

// ── Selection composable ──────────────────────────────────────────────────
const { selectionMode, selectedIds, toggleSelectionMode, toggleSelect, toggleSelectAll, allSelected } = useSelection(filteredIssues)

// ── Sort ──────────────────────────────────────────────────────────────────
const ISSUE_COLS: ColDefs<Issue> = {
  key:          { value: i => (i.issue_key?.split('-')[0] ?? '') + String(i.issue_number).padStart(8, '0'), type: 'string' },
  type:         { value: i => i.type,                      type: { order: ['epic','cost_unit','release','sprint','ticket','task'] } },
  title:        { value: i => i.title,                     type: 'string' },
  status:       { value: i => i.status,                    type: { order: ['backlog','in-progress','complete','canceled'] } },
  priority:     { value: i => i.priority,                  type: { order: ['high','medium','low'] } },
  cost_unit:    { value: i => i.cost_unit,                 type: 'string' },
  release:      { value: i => i.release,                   type: 'string' },
  assignee:     { value: i => i.assignee?.username ?? '',  type: 'string' },
  billing_type: { value: i => i.billing_type ?? '',        type: 'string' },
  total_budget: { value: i => i.total_budget ?? 0,         type: 'number' },
  rate_hourly:  { value: i => i.rate_hourly ?? 0,          type: 'number' },
  rate_lp: { value: i => i.rate_lp ?? 0,         type: 'number' },
  estimate_hours: { value: i => i.estimate_hours ?? 0,     type: 'number' },
  estimate_lp:    { value: i => i.estimate_lp ?? 0,        type: 'number' },
  ar_hours:       { value: i => i.ar_hours ?? 0,           type: 'number' },
  ar_lp:          { value: i => i.ar_lp ?? 0,              type: 'number' },
  start_date:   { value: i => i.start_date ?? '',          type: 'string' },
  end_date:     { value: i => i.end_date ?? '',            type: 'string' },
  group_state:  { value: i => i.group_state ?? '',         type: 'string' },
  sprint_state: { value: i => i.sprint_state ?? '',        type: 'string' },
  jira_id:      { value: i => i.jira_id ?? '',             type: 'string' },
  jira_version: { value: i => i.jira_version ?? '',        type: 'string' },
  jira_text:    { value: i => i.jira_text ?? '',           type: 'string' },
}

const sortResult = useSort(filteredIssues, ISSUE_COLS)
// Sync sort composable refs with our local sortKey/sortDir (used by filter persistence)
watch(sortResult.sortKey, v => { sortKey.value = v })
watch(sortResult.sortDir, v => { sortDir.value = v })
watch(sortKey, v => { sortResult.sortKey.value = v })
watch(sortDir, v => { sortResult.sortDir.value = v as 'asc' | 'desc' })

// ── Tree view ─────────────────────────────────────────────────────────────
const issueTree = computed(() => {
  const map = new Map<number, Issue & { children: (Issue & { children: Issue[] })[] }>()
  filteredIssues.value.forEach(i => map.set(i.id, { ...i, children: [] }))
  const roots: (Issue & { children: (Issue & { children: Issue[] })[] })[] = []
  filteredIssues.value.forEach(i => {
    const node = map.get(i.id)!
    if (i.parent_id && map.has(i.parent_id)) {
      map.get(i.parent_id)!.children.push(node as any)
    } else {
      roots.push(node)
    }
  })
  const sortedRoots = sortResult.sortArray(roots) as (Issue & { children: (Issue & { children: Issue[] })[] })[]
  sortedRoots.forEach(epic => {
    epic.children = sortResult.sortArray(epic.children ?? []) as any
    epic.children.forEach(ticket => {
      ticket.children = sortResult.sortArray(ticket.children ?? [])
    })
  })
  return sortedRoots
})

const tree = useTreeView(treeView, filterType, issuesRef, issueTree, selectedIds)
const { treeExpanded, toggleTreeNode, expandAllTreeNodes, collapseAllTreeNodes, toggleTreeSelect,
        derivedCreateType, expandedGroupIds, isGroupExpandView, toggleGroupExpand, childrenOf } = tree

// ── Inline edit composable ────────────────────────────────────────────────
const inlineEdit = useInlineEdit({
  issues: issuesRef,
  users: usersRef,
  sprints: sprintsRef,
  loadedSprints,
  childrenOf,
  emit: { updated: (issue: Issue) => emit('updated', issue) },
})

const { editingCell, cellEditValue, openCell, closeCell, saveCellEdit,
        cascadeDialogOpen, cascadePendingStatus, cascadeChildCount, cascadeConfirm,
        sprintPickerSearch, sprintPickerPos, sprintPickerRef,
        sprintPickerFiltered, openSprintPicker, toggleSprint,
        inlineAssigneeOptions, onGlobalMousedownCell } = inlineEdit

// ── Mutual-exclusion panel toggle ────────────────────────────────────────────
function openPanel(name: 'views' | 'filter' | 'columns' | null) {
  viewsPanelOpen.value = name === 'views'
  filterPanelOpen.value = name === 'filter'
  colPanelOpen.value    = name === 'columns'
}

function toggleFilterPanel() { openPanel(filterPanelOpen.value ? null : 'filter') }

// ── Views ────────────────────────────────────────────────────────────────
const {
  views: savedViews, loading: viewsLoading,
  activeId: activeViewId, activeView,
  myViews, basicsViews, sharedViews,
  effectiveDefaultView,
  load: loadViews, create: createView, update: updateView,
  remove: deleteView, selectView, copyToMine,
  pinView: _pinView, unpinView: _unpinView,
} = useViews(() => authStore.user?.id)

async function pinView(id: number) { await _pinView(id); emit('views-changed') }
async function unpinView(id: number) { await _unpinView(id); emit('views-changed') }

// View edit modal state
const viewModalOpen   = ref(false)
const viewModalMode   = ref<'save' | 'edit'>('save')
const viewEditTarget  = ref<SavedView | null>(null)
const viewFormTitle   = ref('')
const viewFormDesc    = ref('')
const viewFormShared  = ref(false)
const viewFormAdmin   = ref(false)
const viewFormLoading = ref(false)

function openSaveView() {
  viewModalMode.value  = 'save'
  viewEditTarget.value = null
  viewFormTitle.value  = ''
  viewFormDesc.value   = ''
  viewFormShared.value = false
  viewFormAdmin.value  = false
  viewsPanelOpen.value = false
  viewModalOpen.value  = true
}

function openEditView(v: SavedView) {
  viewModalMode.value  = 'edit'
  viewEditTarget.value = v
  viewFormTitle.value  = v.title
  viewFormDesc.value   = v.description
  viewFormShared.value = v.is_shared
  viewFormAdmin.value  = v.is_admin_default
  viewsPanelOpen.value = false
  viewModalOpen.value  = true
}

function currentColumnsJSON(): string { return colsToJSON() }

async function submitViewForm() {
  if (!viewFormTitle.value.trim()) return
  viewFormLoading.value = true
  try {
    if (viewModalMode.value === 'save') {
      const v = await createView({
        title:            viewFormTitle.value.trim(),
        description:      viewFormDesc.value,
        columns_json:     currentColumnsJSON(),
        filters_json:     currentFiltersJSON(),
        is_shared:        viewFormShared.value,
        is_admin_default: viewFormAdmin.value,
      })
      selectView(v.id)
    } else if (viewEditTarget.value) {
      await updateView(viewEditTarget.value.id, {
        title:            viewFormTitle.value.trim(),
        description:      viewFormDesc.value,
        is_shared:        viewFormShared.value,
        is_admin_default: viewFormAdmin.value,
      })
    }
    viewModalOpen.value = false
  } finally {
    viewFormLoading.value = false
  }
}

async function handleDeleteView(v: SavedView) {
  if (!await confirm({ message: `Delete view "${v.title}"?`, confirmLabel: 'Delete', danger: true })) return
  await deleteView(v.id)
  viewsPanelOpen.value = false
}

async function handleCopyView(v: SavedView) {
  const copy = await copyToMine(v)
  selectView(copy.id)
  viewsPanelOpen.value = false
}

const viewScope = computed(() => `project-${props.projectId ?? 'global'}`)

let applyingView = false

function applyView(v: SavedView, closePanel = true) {
  applyingView = true
  setColsFromJSON(v.columns_json)
  try {
    localStorage.setItem(lsFiltersKey(props.projectId), normalizeSavedFiltersJSON(v.filters_json))
    loadFilters()
  } catch { /* ignore */ }
  if (v.id >= 0) {
    selectView(v.id)
    setLastViewId(authStore.user?.id, viewScope.value, v.id)
  }
  if (closePanel) viewsPanelOpen.value = false
  nextTick(() => {
    viewBaseline.cols = colsToJSON()
    viewBaseline.filts = currentFiltersJSON()
    applyingView = false
  })
  emit('view-applied', v.id)
}

function clearActiveView() {
  selectView(null)
  setLastViewId(authStore.user?.id, viewScope.value, null)
  viewsPanelOpen.value = false
}

const viewBaseline = reactive({ cols: '', filts: '' })
const viewIsModified = computed(() => {
  if (activeViewId.value === null || !viewBaseline.cols) return false
  return colsToJSON() !== viewBaseline.cols || currentFiltersJSON() !== viewBaseline.filts
})

async function updateCurrentView() {
  if (!activeViewId.value || activeViewId.value < 0) return
  const nextCols  = colsToJSON()
  const nextFilts = currentFiltersJSON()
  try {
    await updateView(activeViewId.value, {
      columns_json: nextCols,
      filters_json: nextFilts,
    })
    viewBaseline.cols  = nextCols
    viewBaseline.filts = nextFilts
    flash('View updated')
  } catch {
    flash('Failed to update view')
  }
}

// ── Click-outside handler ───────────────────────────────────────────────
function onMousedown(e: MouseEvent) {
  if (colPanelOpen.value && colPanelEl.value && !colPanelEl.value.contains(e.target as Node)
      && !(colBtnEl.value && colBtnEl.value.contains(e.target as Node))) {
    colPanelOpen.value = false
  }
  if (filterPanelOpen.value && filterPanelEl.value && !filterPanelEl.value.contains(e.target as Node)) {
    filterPanelOpen.value = false
  }
  if (viewsPanelOpen.value && viewsPanelEl.value && !viewsPanelEl.value.contains(e.target as Node)
      && !(viewsBtnEl.value && viewsBtnEl.value.contains(e.target as Node))) {
    viewsPanelOpen.value = false
  }
  if (sprintNav.sprintNavOpen.value) {
    sprintNav.sprintNavOpen.value = false
  }
}

const flashToast = ref('')
let flashToastTimer: ReturnType<typeof setTimeout> | null = null

function flash(msg: string, duration = 2000) {
  if (flashToastTimer) clearTimeout(flashToastTimer)
  flashToast.value = msg
  flashToastTimer = setTimeout(() => { flashToast.value = '' }, duration)
}

function copyKey(key: string, event?: MouseEvent) {
  event?.stopPropagation()
  navigator.clipboard.writeText(key).catch(() => {})
  flash(`'${key}' copied to clipboard`)
}

type EpicMode = 'key' | 'title' | 'abbreviated'
const epicDisplayMode = ref<EpicMode>((localStorage.getItem(EPIC_MODE_KEY) as EpicMode) || 'key')

function setEpicMode(m: EpicMode) {
  epicDisplayMode.value = m
  localStorage.setItem(EPIC_MODE_KEY, m)
}

// ── Filter persistence watchers ──────────────────────────────────────────
onMounted(() => { loadFilters(); sprintNav.loadSprintNav() })
watch(filterWatchSources, () => { if (!applyingView) saveFilters() })
watch([sortKey, sortDir], () => { if (!applyingView) saveFilters() })

// ── Table appearance ──────────────────────────────────────────────────────
const { showBorders, showStripes } = useTableAppearance()

// ── Global drag state ────────────────────────────────────────────────────
const dragStore = useDraggedIssue()
const { draggedIssue, updatedIssue } = storeToRefs(dragStore)
const { setDragging, notifyUpdated } = dragStore
function onRowDragEnd() { setTimeout(() => setDragging(null), 200) }
watch(updatedIssue, (issue) => { if (issue) emit('updated', issue) })

// ── Global new-issue signal ──────────────────────────────────────────────
const newIssueStore = useNewIssueStore()
const showCreate = ref(false)
const createModalRef = ref<InstanceType<typeof CreateIssueModal> | null>(null)

function openCreate(parentIssue?: Issue, overrideType?: string, overrideParentId?: number) {
  showCreate.value = true
  nextTick(() => {
    createModalRef.value?.openCreate(parentIssue, overrideType, overrideParentId)
  })
}

watch(() => newIssueStore.trigger, () => {
  if (props.compact) return
  const ctx = newIssueStore.context
  if (ctx.projectId !== undefined && props.projectId !== undefined && ctx.projectId !== props.projectId) return
  openCreate(undefined, ctx.type, ctx.parentId)
})

function onCreateClose() {
  showCreate.value = false
}

function onCreated(issue: Issue) {
  emit('created', issue)
}

// ── Bulk operations ──────────────────────────────────────────────────────
const showBulkDelete  = ref(false)
const bulkDeleting    = ref(false)
const showBulkChange  = ref(false)
const bulkChangeRef   = ref<InstanceType<typeof BulkChangeModal> | null>(null)

async function openBulkChange() {
  if (!loadedSprints.value.length) {
    loadedSprints.value = await api.get<Sprint[]>('/sprints').catch(() => [])
  }
  bulkChangeRef.value?.reset()
  showBulkChange.value = true
}

function onBulkChangeDone() {
  selectedIds.value    = new Set()
  selectionMode.value  = false
  showBulkChange.value = false
}

async function confirmBulkDelete() {
  bulkDeleting.value = true
  const ids = [...selectedIds.value]
  for (const id of ids) {
    try {
      await api.delete(`/issues/${id}`)
      emit('deleted', id)
    } catch { /* already deleted or not found */ }
  }
  selectedIds.value = new Set()
  selectionMode.value = false
  showBulkDelete.value = false
  bulkDeleting.value = false
}

// ── Single-issue delete (soft-delete — recoverable from Settings → Trash) ──
async function deleteRow(issue: Issue) {
  if (!await confirm({
    message: `Move ${issue.issue_key} "${issue.title}" to Trash? Any child tasks will be moved too. You can restore from Settings → Trash.`,
    confirmLabel: 'Move to trash',
    danger: true,
  })) return
  try {
    await api.delete(`/issues/${issue.id}`)
    emit('deleted', issue.id)
  } catch { /* already gone */ }
}

// ── Side panel ──────────────────────────────────────────────────────────
// Pinned state lives in `useSidePanelPinned` (singleton) so AppLayout
// can apply the right-edge inset on `.main`, which shrinks both the
// AppHeader and the main content together. Width still flows through
// `useSidePanelWidth` (consumed by AppLayout for the inset value).
const sidePanelIssueId = ref<number | null>(null)
const sidePanelEdit    = ref(false)
const { pinned: sidePanelPinned } = useSidePanelPinned()

const sidePanelIssueIds = computed(() => finalIssues.value.map(i => i.id))

watch(() => props.initialPanelIssueId, (id) => {
  if (id) sidePanelIssueId.value = id
}, { immediate: true })

function openSidePanel(issue: Issue, edit = false) {
  if (issue.type === 'sprint') {
    router.push(`/sprint-board?sprint=${issue.id}`)
    return
  }
  sidePanelIssueId.value = issue.id
  sidePanelEdit.value = edit
  notifySidePanelOpened('issue')
}

function closeSidePanel() {
  sidePanelIssueId.value = null
  sidePanelEdit.value = false
  if (route.query.panel !== undefined) {
    router.replace({ query: { ...route.query, panel: undefined } })
  }
}

watch(filteredIssues, (issues) => {
  if (sidePanelIssueId.value !== null && !issues.some(i => i.id === sidePanelIssueId.value)) {
    closeSidePanel()
  }
})

function onSidePanelUpdated(updated: Issue) {
  emit('updated', updated)
  const idx = props.issues.findIndex(i => i.id === updated.id)
  if (idx >= 0) props.issues[idx] = updated
}

function onSidePanelNavigate(id: number) {
  sidePanelIssueId.value = id
  sidePanelEdit.value = false
  nextTick(() => {
    const row = document.querySelector(`tr[data-issue-id="${id}"]`)
    row?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  })
}

function onPinnedChange(pinned: boolean) {
  setSidePanelPinned(pinned)
}

watch(sidePanelIssueId, (id) => setSidePanelVisible(!!id), { immediate: true })
onUnmounted(() => setSidePanelVisible(false))

function navigateTo(issue: Issue) { openSidePanel(issue) }

// Close the issue side panel when another right-edge panel (undo or
// project workspace) opens — only one sidebar is visible at a time.
let unbindIssuePanelExclusion: (() => void) | null = null
onMounted(() => {
  unbindIssuePanelExclusion = onOtherSidePanelOpened('issue', closeSidePanel)
})
onUnmounted(() => {
  unbindIssuePanelExclusion?.()
  unbindIssuePanelExclusion = null
})

// ── Progressive rendering ─────────────────────────────────────────────────
const RENDER_BATCH = 100
const renderLimit = ref(RENDER_BATCH)
const scrollSentinel = ref<HTMLElement | null>(null)

const finalIssues = computed(() => {
  if (sprintNav.toolbarSprintIds.value.length <= 1) return sortResult.sorted.value
  const sprintOrder = sprintNav.orderedSprints.value.filter(s => sprintNav.toolbarSprintIds.value.includes(s.id))
  const groups = new Map<number, Issue[]>()
  const noSprint: Issue[] = []
  for (const s of sprintOrder) groups.set(s.id, [])
  for (const issue of sortResult.sorted.value) {
    const ids = issue.sprint_ids ?? []
    const match = sprintOrder.find(s => ids.includes(s.id))
    if (match) groups.get(match.id)!.push(issue)
    else noSprint.push(issue)
  }
  const result: Issue[] = []
  for (const s of sprintOrder) result.push(...(groups.get(s.id) ?? []))
  result.push(...noSprint)
  return result
})

const sprintGroupHeads = computed<Map<number, Sprint>>(() => {
  const heads = new Map<number, Sprint>()
  if (sprintNav.toolbarSprintIds.value.length <= 1) return heads
  const sprintOrder = sprintNav.orderedSprints.value.filter(s => sprintNav.toolbarSprintIds.value.includes(s.id))
  let currentGroupId: number | null = null
  for (const issue of finalIssues.value) {
    const ids = issue.sprint_ids ?? []
    const match = sprintOrder.find(s => ids.includes(s.id))
    if (match && match.id !== currentGroupId) {
      currentGroupId = match.id
      heads.set(issue.id, match)
    }
  }
  return heads
})

const backlogHeadId = computed<number | null>(() => {
  if (sprintNav.toolbarSprintIds.value.length <= 1) return null
  for (const issue of finalIssues.value) {
    if (!(issue.sprint_ids ?? []).some(sid => sprintNav.toolbarSprintIds.value.includes(sid))) return issue.id
  }
  return null
})

const issueSprintGroup = computed<Map<number, number | 'backlog'>>(() => {
  const map = new Map<number, number | 'backlog'>()
  if (sprintNav.toolbarSprintIds.value.length <= 1) return map
  const sprintOrder = sprintNav.orderedSprints.value.filter(s => sprintNav.toolbarSprintIds.value.includes(s.id))
  let currentGroup: number | 'backlog' = 'backlog'
  for (const issue of finalIssues.value) {
    const ids = issue.sprint_ids ?? []
    const match = sprintOrder.find(s => ids.includes(s.id))
    if (match) currentGroup = match.id
    else if (currentGroup !== 'backlog' && !ids.some(sid => sprintNav.toolbarSprintIds.value.includes(sid))) currentGroup = 'backlog'
    map.set(issue.id, currentGroup)
  }
  return map
})

// Sprint section drag-drop
const dragOverSprintId = ref<number | 'backlog' | null>(null)

function onSectionDragOver(e: DragEvent, groupId: number | 'backlog') {
  e.preventDefault()
  dragOverSprintId.value = groupId
}

function onSectionDragLeave(e: DragEvent, groupId: number | 'backlog') {
  const related = e.relatedTarget as HTMLElement | null
  if (related?.closest?.('tr')) {
    const row = related.closest('tr')
    const issueId = Number(row?.getAttribute('data-issue-id'))
    if (issueId && issueSprintGroup.value.get(issueId) === groupId) return
    if (row?.classList.contains('sprint-separator-row') && row?.getAttribute('data-sprint-group') === String(groupId)) return
  }
  if (dragOverSprintId.value === groupId) dragOverSprintId.value = null
}

async function onSectionDrop(e: DragEvent, groupId: number | 'backlog') {
  e.preventDefault()
  dragOverSprintId.value = null
  const issue = draggedIssue.value
  if (!issue) return
  try {
    if (groupId === 'backlog') {
      const sprintIds = issue.sprint_ids ?? []
      for (const sid of sprintIds) {
        await api.delete(`/issues/${issue.id}/relations`, { target_id: sid, type: 'sprint' })
      }
    } else {
      const existingSprintIds = issue.sprint_ids ?? []
      for (const sid of existingSprintIds) {
        if (sid !== groupId) await api.delete(`/issues/${issue.id}/relations`, { target_id: sid, type: 'sprint' })
      }
      if (!existingSprintIds.includes(groupId)) {
        await api.post(`/issues/${issue.id}/relations`, { target_id: groupId, type: 'sprint' })
      }
    }
    const updated = await api.get<Issue>(`/issues/${issue.id}`)
    notifyUpdated(updated)
  } catch { /* silent */ }
  finally { setDragging(null) }
}

const renderedIssues = computed(() => finalIssues.value.slice(0, renderLimit.value))
const hasMore = computed(() => renderLimit.value < finalIssues.value.length)

watch(finalIssues, () => { renderLimit.value = RENDER_BATCH })

if (!sprints.value.length) {
  api.get<Sprint[]>('/sprints').then(s => { loadedSprints.value = s }).catch(() => {})
}

// ── Table scroll / resize ─────────────────────────────────────────────────
const tableWrapRef = ref<HTMLElement | null>(null)
const actionsCollapsed = ref(false)

function recalcTableHeight() {
  const el = tableWrapRef.value
  if (!el || props.compact) return
  const h = window.innerHeight - el.getBoundingClientRect().top - 24
  el.style.setProperty('--table-max-h', Math.max(h, 200) + 'px')
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') openPanel(null)
}

onMounted(async () => {
  document.addEventListener('keydown', onKeydown)
  document.addEventListener('mousedown', onMousedown)
  document.addEventListener('mousedown', onGlobalMousedownCell)
  if (!props.compact) {
    await loadViews()
    if (props.initialViewId !== undefined) {
      const initView = savedViews.value.find(v => v.id === props.initialViewId)
      applyView(initView ?? effectiveDefaultView.value, false)
    } else {
      const lastId = getLastViewId(authStore.user?.id, viewScope.value)
      const lastView = lastId !== null ? savedViews.value.find(v => v.id === lastId) : null
      applyView(lastView ?? effectiveDefaultView.value, false)
    }
  }
})
onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
  document.removeEventListener('mousedown', onMousedown)
  document.removeEventListener('mousedown', onGlobalMousedownCell)
})

// Progressive render observer — uses viewport (root: null) so it fires
// regardless of which ancestor is the scroll container.
onMounted(() => {
  const observer = new IntersectionObserver((entries) => {
    if (entries[0]?.isIntersecting && hasMore.value) {
      renderLimit.value += RENDER_BATCH
    }
  }, { rootMargin: '400px' })
  watch(scrollSentinel, (el, oldEl) => {
    if (oldEl) observer.unobserve(oldEl)
    if (el) observer.observe(el)
  }, { immediate: true })
  onUnmounted(() => observer.disconnect())
})

// Table height recalc
onMounted(() => {
  const check = setInterval(() => {
    if (tableWrapRef.value) {
      clearInterval(check)
      recalcTableHeight()
      const ro = new ResizeObserver(recalcTableHeight)
      ro.observe(tableWrapRef.value.parentElement!)
      onUnmounted(() => ro.disconnect())
      const mainEl = tableWrapRef.value.closest('.main')
      if (mainEl) {
        let raf = 0
        const onMainScroll = () => { cancelAnimationFrame(raf); raf = requestAnimationFrame(recalcTableHeight) }
        mainEl.addEventListener('scroll', onMainScroll, { passive: true })
        onUnmounted(() => mainEl.removeEventListener('scroll', onMainScroll))
      }
    }
  }, 100)
  onUnmounted(() => clearInterval(check))
})

// Responsive actions collapse
onMounted(() => {
  if (!tableWrapRef.value) return
  const el = tableWrapRef.value
  function checkOverflow() {
    const slack = el.clientWidth - el.scrollWidth
    if (!actionsCollapsed.value && slack < -8) actionsCollapsed.value = true
    else if (actionsCollapsed.value && slack > 100) actionsCollapsed.value = false
  }
  const ro = new ResizeObserver(checkOverflow)
  ro.observe(el)
  const table = el.querySelector('table')
  if (table) ro.observe(table)
  checkOverflow()
  onUnmounted(() => ro.disconnect())
})

defineExpose({ selectionMode, selectedIds, toggleSelectionMode, activeFilterCount, filteredIssues, openCreate, applyView })
</script>

<template>
  <div class="issue-list-root">
  <!-- Transient flash toast (copy-to-clipboard, view saved, …) -->
  <Transition name="flash-toast">
    <div v-if="flashToast" class="flash-toast">{{ flashToast }}</div>
  </Transition>
  <div class="issue-list-main">

    <!-- Section title (compact mode) -->
    <div v-if="compact && title" class="compact-header">
      <h3 class="compact-title">{{ title }}</h3>
      <button class="btn btn-primary btn-sm" @click="openCreate()">+ Add {{ title }}</button>
    </div>

    <!-- Filter bar (full mode only) -->
    <div v-if="!compact" class="filters">
      <button v-if="projectId !== undefined" class="btn btn-primary btn-sm" @click="openCreate()">+ New issue</button>

      <!-- Views toggle button -->
      <button
        ref="viewsBtnEl"
        :class="['btn btn-ghost btn-sm filter-btn views-btn', { active: viewsPanelOpen, 'views-btn--has-view': activeViewId !== null }]"
        :aria-expanded="viewsPanelOpen"
        @mousedown.stop
        @click="openPanel(viewsPanelOpen ? null : 'views')"
      >
        <AppIcon name="eye" :size="12" />
        <span class="views-btn-label">{{ activeView ? activeView.title : 'Views' }}</span>
        <span class="views-modified-dot" :class="{ 'views-modified-dot--active': viewIsModified }" title="Unsaved changes">&#8226;</span>
        <AppIcon v-if="activeViewId !== null" name="x" :size="10" :stroke-width="2.5"
          class="views-clear-x" @click.stop="clearActiveView" title="Clear view" />
      </button>

      <!-- Sprint navigator -->
      <div v-if="inlineEdit.allSprints().length" :class="['sprint-nav', { 'sprint-nav--active': sprintNav.toolbarSprintIds.value.length > 0 }]">
        <button class="sn-btn" :disabled="!sprintNav.canPrev.value" @click="sprintNav.navPrev" title="Previous sprint">
          <AppIcon name="chevron-left" :size="13" />
        </button>
        <button class="sn-btn" :disabled="!sprintNav.canNext.value" @click="sprintNav.navNext" title="Next sprint">
          <AppIcon name="chevron-right" :size="13" />
        </button>
        <button ref="sprintNav.snSelectorEl.value" :class="['sn-selector', { 'sn-selector--active': sprintNav.sprintNavOpen.value }]" @click.stop="sprintNav.sprintNavOpen.value = !sprintNav.sprintNavOpen.value">
          {{ sprintNav.navLabel.value }} <AppIcon name="chevron-down" :size="11" />
        </button>
        <button v-for="n in [1, 2, 3]" :key="n"
          :class="['sn-range', { 'sn-range--active': sprintNav.activeRange.value === n }]"
          @click="sprintNav.navRange(n)">{{ n }}S</button>
        <button v-if="sprintNav.toolbarSprintIds.value.length" class="sn-btn sn-clear" @click="sprintNav.navClear" title="Clear sprint filter">
          <AppIcon name="x" :size="12" />
        </button>
      </div>

      <!-- Filter toggle button -->
      <button
        :class="['btn btn-ghost btn-sm filter-btn', { active: filterPanelOpen, 'has-filters': activeFilterCount > 0 }]"
        :aria-expanded="filterPanelOpen"
        @mousedown.stop
        @click="toggleFilterPanel"
      >
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="6" x2="20" y2="6"/><line x1="8" y1="12" x2="16" y2="12"/><line x1="11" y1="18" x2="13" y2="18"/></svg>
        Filter
        <span v-if="activeFilterCount > 0" class="filter-count">{{ activeFilterCount }}</span>
        <span v-if="activeFilterCount > 0" class="filter-clear-x" @click.stop="clearAllFilters" title="Clear all filters">
          <AppIcon name="x" :size="10" :stroke-width="2.5" />
        </span>
      </button>

      <!-- Active filter chips -->
      <div v-if="filterChipGroups.length" class="filter-chips">
        <template v-for="grp in filterChipGroups" :key="grp.group">
          <span
            v-for="chip in grp.chips" :key="chip.value"
            :class="['filter-chip', `filter-chip--${grp.group}`, { 'filter-chip--neg': chip.negated }]"
            :title="chip.negated ? 'Click to include' : 'Click to exclude'"
            @click.stop="toggleChipNegation(chip.group, chip.value)"
          >
            <span :class="['chip-bang', { 'chip-bang--active': chip.negated }]">!</span>
            <span :class="{ 'chip-label--neg': chip.negated }">{{ chip.label }}</span>
            <button class="chip-x" @click.stop="removeChip(chip.group, chip.value)" title="Remove filter"><AppIcon name="x" :size="10" :stroke-width="2.5" /></button>
          </span>
        </template>
      </div>

      <div class="filter-right">
        <span class="issue-count">
          {{ filteredIssues.length }} issue{{ filteredIssues.length !== 1 ? 's' : '' }}<template v-if="hasMore"> · showing {{ renderedIssues.length }}</template>
        </span>
        <button
          v-if="selectionMode && selectedIds.size > 0"
          class="btn btn-ghost btn-sm"
          @click="openBulkChange"
        >
          Change {{ selectedIds.size }} issue{{ selectedIds.size !== 1 ? 's' : '' }}...
        </button>
        <button
          v-if="isAdmin && selectionMode && selectedIds.size > 0"
          class="btn btn-danger btn-sm"
          @click="showBulkDelete = true"
        >
          Delete {{ selectedIds.size }}
        </button>
        <button :class="['btn btn-ghost btn-sm', { active: selectionMode }]" @click="toggleSelectionMode" title="Toggle selection mode">
          {{ selectionMode ? `✓ ${selectedIds.size} selected` : 'Select' }}
        </button>
        <template v-if="treeView">
          <button class="btn btn-ghost btn-sm" @click="collapseAllTreeNodes" title="Collapse all">
            <AppIcon name="chevrons-up" :size="13" />
          </button>
          <button class="btn btn-ghost btn-sm" @click="expandAllTreeNodes" title="Expand all">
            <AppIcon name="chevrons-down" :size="13" />
          </button>
        </template>
        <button :class="['btn btn-ghost btn-sm', { active: treeView }]" @click="treeView=!treeView">
          {{ treeView ? '≡ Flat' : '⌥ Tree' }}
        </button>
        <!-- Slot for parent-injected toolbar buttons (e.g. ProjectDetailView's
             Documents / Cooperation aux-panel toggles, PAI-145). Lives next
             to Tree/Flat so toggles all share one visual cluster. -->
        <slot name="toolbar-extra" />
        <button
          ref="colBtnEl"
          :class="['btn btn-ghost btn-sm filter-btn', { active: colPanelOpen }]"
          @mousedown.stop
          @click="openPanel(colPanelOpen ? null : 'columns')"
          title="Show/hide columns"
        >
          <AppIcon name="columns-3" :size="12" />
          Columns
        </button>
      </div>
    </div>

    <Transition name="issue-refresh-banner">
      <IssueListRefreshBanner
        v-if="refreshStale"
        :count="refreshCount"
        @refresh="emit('refresh-list')"
      />
    </Transition>

    <!-- Views panel -->
    <IssueViewsPanel
      v-if="!compact && viewsPanelOpen"
      ref="viewsPanelEl"
      :my-views="myViews"
      :basics-views="basicsViews"
      :shared-views="sharedViews"
      :active-view-id="activeViewId"
      :view-is-modified="viewIsModified"
      :is-admin="isAdmin"
      :epic-display-mode="epicDisplayMode"
      @apply-view="v => applyView(v)"
      @open-save-view="openSaveView"
      @open-edit-view="openEditView"
      @delete-view="handleDeleteView"
      @copy-view="handleCopyView"
      @pin-view="pinView"
      @unpin-view="unpinView"
      @update-current-view="updateCurrentView"
      @set-epic-mode="setEpicMode"
    />

    <!-- Filter panel -->
    <IssueFilterPanel
      v-if="!compact && filterPanelOpen"
      ref="filterPanelEl"
      :filter-type="filterType"
      :filter-status="filterStatus"
      :filter-priority="filterPriority"
      :filter-projects="filterProjects"
      :filter-assignee="filterAssignee"
      :filter-tags="filterTags"
      :filter-cost-unit="filterCostUnit"
      :filter-release="filterRelease"
      :filter-sprints="filterSprints"
      :filter-epic="filterEpic"
      :show-archived-sprints="showArchivedSprints"
      :complex-tab="complexTab"
      :complex-tab-search="complexTabSearch"
      :complex-tabs="complexTabs"
      :complex-badge="complexBadge"
      :active-filter-count="activeFilterCount"
      :issues="issues"
      :projects="projects"
      :picker-projects="pickerProjects"
      :picker-users="pickerUsers"
      :picker-tags="pickerTags"
      :picker-cost-units="pickerCostUnits"
      :picker-releases="pickerReleases"
      :picker-sprints="pickerSprints"
      :assignee-is-any="assigneeIsAny"
      @update:filter-type="v => filterType = v"
      @update:filter-status="v => filterStatus = v"
      @update:filter-priority="v => filterPriority = v"
      @update:filter-projects="v => filterProjects = v"
      @update:filter-assignee="v => filterAssignee = v"
      @update:filter-tags="v => filterTags = v"
      @update:filter-cost-unit="v => filterCostUnit = v"
      @update:filter-release="v => filterRelease = v"
      @update:filter-sprints="v => filterSprints = v"
      @update:filter-epic="v => filterEpic = v"
      @update:show-archived-sprints="v => showArchivedSprints = v"
      @update:complex-tab="v => complexTab = v"
      @update:complex-tab-search="v => complexTabSearch = v"
      @clear-all="clearAllFilters"
      @set-assignee-any="setAssigneeAny"
    />

    <!-- Columns panel -->
    <div v-if="!compact && colPanelOpen" ref="colPanelEl" class="columns-panel">
      <div class="col-panel-header">
        <span class="col-panel-title">Columns</span>
        <button class="fp-clear" @click="resetCols">Reset</button>
      </div>
      <div class="columns-panel-grid">
        <label v-for="col in ALL_COLUMNS" :key="col.key" :class="['col-panel-option', { 'col-panel-option--pinned': col.pinned }]">
          <input type="checkbox" :checked="isVisible(col.key)" :disabled="col.pinned" @change="toggleCol(col.key)" />
          <span>{{ col.label }}</span>
          <span v-if="col.pinned" class="col-pin-badge">always</span>
        </label>
      </div>
    </div>

    <!-- Empty -->
    <div v-if="filteredIssues.length === 0" class="empty-state">
      <div v-if="searchQuery && !compact" class="empty-search-term">"{{ searchQuery }}"</div>
      {{ compact ? 'No child issues.' : 'No issues match the current filters.' }}
    </div>

    <!-- FLAT TABLE -->
    <div v-else-if="compact || !treeView" ref="tableWrapRef" class="issue-table-wrap" :class="{ compact, 'table-borders': showBorders, 'table-stripes': showStripes }">
      <IssueTable
        :issues="renderedIssues"
        :all-issues="issues"
        :loaded-sprints="loadedSprints"
        :compact="!!compact"
        :selection-mode="selectionMode"
        :selected-ids="selectedIds"
        :all-selected="allSelected"
        :is-admin="isAdmin"
        :project-id="projectId"
        :sort-result="sortResult"
        :is-visible="isVisible"
        :editing-cell="editingCell"
        :cell-edit-value="cellEditValue"
        :sprint-picker-search="sprintPickerSearch"
        :sprint-picker-pos="sprintPickerPos"
        :sprint-picker-filtered="sprintPickerFiltered"
        :sprint-picker-ref="sprintPickerRef"
        :sprint-group-heads="sprintGroupHeads"
        :backlog-head-id="backlogHeadId"
        :issue-sprint-group="issueSprintGroup"
        :drag-over-sprint-id="dragOverSprintId"
        :is-group-expand-view="isGroupExpandView"
        :expanded-group-ids="expandedGroupIds"
        :children-of="childrenOf"
        :show-borders="showBorders"
        :show-stripes="showStripes"
        :actions-collapsed="actionsCollapsed"
        :side-panel-issue-id="sidePanelIssueId"
        :search-query="searchQuery"
        :epic-display-mode="epicDisplayMode"
        :inline-assignee-options="inlineAssigneeOptions"
        :format-hours="formatHours"
        :time-label="timeLabel"
        @toggle-select="toggleSelect"
        @toggle-select-all="toggleSelectAll"
        @open-cell="(issue, field, event) => openCell(issue, field, event)"
        @close-cell="(save) => closeCell(save)"
        @save-cell-edit="(issue, field, value) => saveCellEdit(issue, field, value)"
        @open-sprint-picker="(issue, event) => openSprintPicker(issue, event)"
        @toggle-sprint="(issue, sprintId) => toggleSprint(issue, sprintId)"
        @copy-key="(key, event) => copyKey(key, event)"
        @navigate-to="navigateTo"
        @open-create="(issue) => openCreate(issue)"
        @open-side-panel="(issue, edit) => openSidePanel(issue, edit)"
        @delete-row="deleteRow"
        @set-dragging="setDragging"
        @drag-end="onRowDragEnd"
        @section-drag-over="onSectionDragOver"
        @section-drag-leave="onSectionDragLeave"
        @section-drop="onSectionDrop"
        @toggle-group-expand="toggleGroupExpand"
        @toggle-time-unit="toggleTimeUnit"
        @update:cell-edit-value="v => cellEditValue = v"
        @update:sprint-picker-search="v => sprintPickerSearch = v"
      />
      <div v-if="hasMore" ref="scrollSentinel" class="scroll-sentinel">Loading more...</div>
    </div>

    <!-- TREE VIEW (full mode only) — wrapped in a scrollable flex child so
         the tree owns its own overflow, keeping AppFooter in flow beneath the
         list instead of being pushed below the viewport. Mirrors the flat
         .issue-table-wrap pattern. -->
    <div v-else class="issue-tree-wrap">
      <IssueTreeView
        :issue-tree="issueTree"
        :selection-mode="selectionMode"
        :selected-ids="selectedIds"
        :tree-expanded="treeExpanded"
        :is-admin="isAdmin"
        :side-panel-issue-id="sidePanelIssueId"
        @toggle-tree-node="toggleTreeNode"
        @toggle-tree-select="toggleTreeSelect"
        @toggle-select="toggleSelect"
        @navigate-to="navigateTo"
        @copy-key="(key, event) => copyKey(key, event)"
        @open-create="(issue) => openCreate(issue)"
        @open-side-panel="(issue, edit) => openSidePanel(issue, edit)"
        @delete-row="deleteRow"
      />
    </div>

    <!-- Create modal -->
    <CreateIssueModal
      ref="createModalRef"
      :open="showCreate"
      :project-id="projectId"
      :issues="issues"
      :initial-type="initialType"
      :default-parent-id="defaultParentId"
      :project-all-issues="projectAllIssues"
      :derived-create-type="derivedCreateType"
      @close="onCreateClose"
      @created="onCreated"
      @cost-unit-added="v => emit('cost-unit-added', v)"
      @release-added="v => emit('release-added', v)"
    />

    <!-- Bulk change modal -->
    <BulkChangeModal
      ref="bulkChangeRef"
      :open="showBulkChange"
      :selected-ids="selectedIds"
      :issues="issues"
      :loaded-sprints="loadedSprints"
      @close="showBulkChange = false"
      @updated="issue => emit('updated', issue)"
      @done="onBulkChangeDone"
    />

    <!-- Bulk delete confirm -->
    <AppModal title="Delete Issues" :open="showBulkDelete" @close="showBulkDelete=false" confirm-key="d" @confirm="confirmBulkDelete">
      <p style="font-size:14px;color:var(--text);margin-bottom:1.25rem">
        Permanently delete <strong>{{ selectedIds.size }} issue{{ selectedIds.size !== 1 ? 's' : '' }}</strong>? This cannot be undone.
      </p>
      <div class="form-actions">
        <button class="btn btn-ghost" @click="showBulkDelete=false" :disabled="bulkDeleting"><u>C</u>ancel</button>
        <button class="btn btn-danger" @click="confirmBulkDelete" :disabled="bulkDeleting">
          <template v-if="bulkDeleting">Deleting...</template>
          <template v-else><u>D</u>elete {{ selectedIds.size }} issue{{ selectedIds.size !== 1 ? 's' : '' }}</template>
        </button>
      </div>
    </AppModal>

    <!-- View save / edit modal -->
    <AppModal
      :title="viewModalMode === 'save' ? 'Save View' : 'Edit View'"
      :open="viewModalOpen"
      @close="viewModalOpen = false"
    >
      <form class="view-form" @submit.prevent="submitViewForm">
        <div class="form-group">
          <label class="form-label">Name</label>
          <input v-model="viewFormTitle" class="form-input" placeholder="View name" required autofocus />
        </div>
        <div class="form-group">
          <label class="form-label">Description <span class="form-label-optional">(optional)</span></label>
          <input v-model="viewFormDesc" class="form-input" placeholder="What is this view for?" />
        </div>
        <div class="form-check-row">
          <label class="form-check">
            <input type="checkbox" v-model="viewFormShared" />
            <span>Shared — visible to all users</span>
          </label>
          <label v-if="isAdmin" class="form-check">
            <input type="checkbox" v-model="viewFormAdmin" />
            <span>Basics — pinned in Basics section for all users</span>
          </label>
        </div>
        <div class="form-actions">
          <button type="button" class="btn btn-ghost" @click="viewModalOpen = false">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="viewFormLoading || !viewFormTitle.trim()">
            {{ viewFormLoading ? 'Saving...' : (viewModalMode === 'save' ? 'Save View' : 'Update View') }}
          </button>
        </div>
      </form>
    </AppModal>

    <!-- Epic cascade confirmation dialog -->
    <EpicCascadeDialog
      :open="cascadeDialogOpen"
      :child-count="cascadeChildCount"
      :pending-status="cascadePendingStatus"
      @confirm="cascadeConfirm"
      @close="cascadeDialogOpen = false"
    />

  </div><!-- /issue-list-main -->

    <!-- Side panel (peek / quick edit) -->
    <IssueSidePanel
      :issue-id="sidePanelIssueId"
      :issue-ids="sidePanelIssueIds"
      :start-in-edit="sidePanelEdit"
      :pinned="sidePanelPinned"
      @close="closeSidePanel"
      @updated="onSidePanelUpdated"
      @deleted="id => { emit('deleted', id); closeSidePanel() }"
      @navigate="onSidePanelNavigate"
      @update:pinned="onPinnedChange"
    />

    <!-- Sprint nav dropdown -->
    <Teleport to="body">
      <div v-if="sprintNav.sprintNavOpen.value" class="sn-dropdown" :style="sprintNav.snDropdownStyle.value" @click.stop>
        <input v-model="sprintNav.sprintNavSearch.value" class="sn-search" placeholder="Search sprints..." @keydown.escape="sprintNav.sprintNavOpen.value = false" ref="sprintNav.snSearchEl.value" />
        <div class="sn-list">
          <label v-for="s in sprintNav.filteredNavSprints.value" :key="s.id"
            :class="['sn-opt', { 'sn-opt--current': s.id === sprintNav.currentSprintId.value, 'sn-opt--selected': sprintNav.toolbarSprintIds.value.includes(s.id) }]">
            <input type="checkbox" :checked="sprintNav.toolbarSprintIds.value.includes(s.id)" @change="sprintNav.toggleNavSprint(s.id)" />
            <span class="sn-opt-title">{{ s.title }}</span>
            <span v-if="s.start_date" class="sn-opt-date">{{ s.start_date }}</span>
          </label>
        </div>
      </div>
    </Teleport>

  </div>
</template>

<style scoped>
.issue-list-root { display: flex; flex-direction: column; gap: 0; flex: 1; min-height: 0; overflow: hidden; }
.issue-list-main { display: flex; flex-direction: column; gap: 0; flex: 1; min-width: 0; min-height: 0; overflow: hidden; }

.compact-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: .75rem; }
.compact-title { font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: .07em; color: var(--text-muted); }

.filters { display: flex; align-items: center; gap: .6rem; margin-bottom: 0; flex-wrap: wrap; }
.filter-right { display: flex; align-items: center; gap: .5rem; margin-left: auto; }
.issue-count { font-size: 12px; color: var(--text-muted); }
.issue-refresh-banner-enter-active,
.issue-refresh-banner-leave-active {
  transition: opacity .14s ease, transform .14s ease;
}
.issue-refresh-banner-enter-from,
.issue-refresh-banner-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
.btn-sm { padding: .3rem .65rem; font-size: 12px; }
.btn-sm.active { background: var(--bp-blue-pale); color: var(--bp-blue-dark); border-color: var(--bp-blue-pale); }

.filter-btn { display: inline-flex; align-items: center; gap: .35rem; }
.filter-btn.has-filters { border-color: var(--bp-blue); color: var(--bp-blue-dark); background: var(--bp-blue-pale); }
.filter-count { display: inline-flex; align-items: center; justify-content: center; background: var(--bp-blue); color: #fff; border-radius: 20px; font-size: 10px; font-weight: 700; min-width: 16px; height: 16px; padding: 0 4px; }
.filter-clear-x { display: inline-flex; align-items: center; justify-content: center; width: 14px; height: 14px; border-radius: 50%; opacity: .55; margin-left: -.1rem; transition: opacity .1s, background .1s; }
.filter-clear-x:hover { opacity: 1; background: rgba(0,0,0,.1); }

.filter-chips { display: flex; align-items: center; gap: .35rem; flex-wrap: wrap; }
.filter-chip { display: inline-flex; align-items: center; gap: .25rem; border-radius: 20px; font-size: 11px; font-weight: 600; padding: .15rem .5rem .15rem .5rem; white-space: nowrap; line-height: 1.4; cursor: pointer; background: var(--chip-default-bg); color: #475569; border: 1px solid color-mix(in srgb, #94a3b8 20%, transparent); transition: background .1s, color .1s; }
.filter-chip--type { background: color-mix(in srgb, var(--chip-type-tint) 12%, var(--chip-default-bg)); color: color-mix(in srgb, var(--chip-type-tint) 55%, #334155); border-color: color-mix(in srgb, var(--chip-type-tint) 18%, transparent); }
.filter-chip--status { background: color-mix(in srgb, var(--chip-status-tint) 10%, var(--chip-default-bg)); color: color-mix(in srgb, var(--chip-status-tint) 50%, #334155); border-color: color-mix(in srgb, var(--chip-status-tint) 16%, transparent); }
.filter-chip--priority { background: color-mix(in srgb, var(--chip-priority-tint) 12%, var(--chip-default-bg)); color: color-mix(in srgb, var(--chip-priority-tint) 50%, #334155); border-color: color-mix(in srgb, var(--chip-priority-tint) 18%, transparent); }
.filter-chip--neg { opacity: .85; }
.chip-bang { font-weight: 800; font-size: 11px; color: transparent; transition: color .15s; user-select: none; line-height: 1; }
.chip-bang--active { color: inherit; }
.chip-label--neg { text-decoration: line-through; opacity: .8; }
.chip-x { background: none; border: none; padding: 0; margin: 0; cursor: pointer; font-size: 14px; line-height: 1; color: inherit; display: inline-flex; align-items: center; opacity: .5; font-family: inherit; }
.chip-x:hover { opacity: 1; }

.empty-state { padding: 1.5rem; text-align: center; color: var(--text-muted); font-size: 13px; margin-top: 1.25rem; }
.empty-search-term { font-size: 16px; font-weight: 700; color: var(--text); margin-bottom: .5rem; word-break: break-all; }

.flash-toast { position: fixed; bottom: 1.5rem; left: 50%; transform: translateX(-50%); background: rgba(30, 50, 80, .9); color: #fff; font-size: 12px; font-weight: 500; padding: .4rem .85rem; border-radius: 20px; z-index: 9999; pointer-events: none; white-space: nowrap; }
.flash-toast-enter-active, .flash-toast-leave-active { transition: opacity .2s, transform .2s; }
.flash-toast-enter-from, .flash-toast-leave-to { opacity: 0; transform: translateX(-50%) translateY(4px); }

.issue-table-wrap { background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; box-shadow: var(--shadow); margin-top: 1.25rem; overflow: auto; flex: 1; min-height: 0; }
.issue-table-wrap.compact { box-shadow: none; margin-top: 0; overflow: hidden; max-height: none; }

/* Tree-view scroll container — mirrors .issue-table-wrap's flex-child scroll
   pattern so tree rows stay contained within the issue-list area and AppFooter
   is never overlapped. Without this wrapper, .tree-wrap had no scroll and rows
   overflowed .main-content. */
.issue-tree-wrap { overflow: auto; flex: 1; min-height: 0; margin-top: 1.25rem; }

.table-borders :deep(.issue-table tbody tr.issue-row td) { border-bottom: 1px solid var(--table-row-border); }
.table-borders :deep(.issue-table tbody tr.issue-row:last-child td) { border-bottom: none; }
.table-stripes :deep(.issue-table tbody tr.issue-row.row-even) { background: var(--table-row-alt); }
.table-stripes :deep(.issue-table tbody tr.issue-row.row-even .col-key) { background: var(--table-row-alt); }
.table-stripes :deep(.issue-table tbody tr.issue-row.row-even .col-actions) { background: var(--table-row-alt); }

.form-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }

.columns-panel { margin-top: .75rem; margin-bottom: 1.25rem; background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; box-shadow: var(--shadow); padding: 1rem 1.25rem; }
.columns-panel-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: .1rem .75rem; }
.col-panel-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: .6rem; }
.col-panel-title { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .07em; color: var(--text-muted); }
.col-panel-option { display: flex; align-items: center; gap: .45rem; font-size: 13px; color: var(--text); cursor: pointer; padding: .2rem 0; user-select: none; }
.col-panel-option input[type="checkbox"] { width: 14px; height: 14px; flex-shrink: 0; accent-color: var(--bp-blue); cursor: pointer; margin: 0; }
.col-panel-option--pinned { color: var(--text-muted); }
.col-panel-option--pinned input { opacity: .4; cursor: not-allowed; }
.col-pin-badge { font-size: 10px; color: var(--text-muted); margin-left: auto; font-style: italic; }
.fp-clear { background: none; border: none; font-size: 12px; color: var(--bp-blue); cursor: pointer; padding: 0; font-family: inherit; }
.fp-clear:hover { text-decoration: underline; }

.views-btn--has-view { background: #e9ecef !important; color: var(--text) !important; border-color: #ced4da !important; }
.views-btn--has-view:hover { background: #dde1e6 !important; }
.views-btn-label { max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.views-modified-dot { color: #f59e0b; font-size: 16px; line-height: 1; margin-left: .1rem; flex-shrink: 0; margin-top: -2px; opacity: 0; transition: opacity .15s; }
.views-modified-dot--active { opacity: 1; }
.views-clear-x { margin-left: .1rem; opacity: .5; flex-shrink: 0; display: inline-flex; align-items: center; transition: opacity .1s; }
.views-clear-x:hover { opacity: 1; }

.view-form { display: flex; flex-direction: column; gap: .85rem; }
.form-group { display: flex; flex-direction: column; gap: .3rem; }
.form-label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.form-label-optional { font-weight: 400; font-style: italic; text-transform: none; letter-spacing: 0; }
.form-input { border: 1px solid var(--border); border-radius: var(--radius); padding: .4rem .65rem; font-size: 13px; font-family: inherit; background: var(--bg); color: var(--text); outline: none; width: 100%; }
.form-input:focus { border-color: var(--bp-blue); }
.form-check-row { display: flex; flex-direction: column; gap: .5rem; }
.form-check { display: flex; align-items: center; gap: .5rem; font-size: 13px; color: var(--text); cursor: pointer; user-select: none; }
.form-check input[type="checkbox"] { width: 14px; height: 14px; flex-shrink: 0; accent-color: var(--bp-blue); cursor: pointer; margin: 0; }
</style>

<style>
/* Sprint picker — teleported to body, not scoped */
.sprint-picker { background: var(--bg-card, #fff); border: 1px solid var(--border, #e5e7eb); border-radius: 8px; box-shadow: 0 4px 16px rgba(0,0,0,.12); width: 220px; display: flex; flex-direction: column; }
.sprint-picker-search { border: none; border-bottom: 1px solid var(--border, #e5e7eb); padding: .5rem .75rem; font-size: 13px; font-family: inherit; outline: none; background: transparent; color: var(--text, #1f2937); border-radius: 8px 8px 0 0; }
.sprint-picker-list { max-height: 240px; overflow-y: auto; }
.sprint-picker-empty { padding: .65rem .75rem; font-size: 13px; color: var(--text-muted, #6b7280); }
.sprint-picker-opt { display: flex; align-items: center; gap: .5rem; width: 100%; padding: .4rem .75rem; font-size: 13px; background: none; border: none; cursor: pointer; font-family: inherit; color: var(--text, #1f2937); text-align: left; transition: background .1s; }
.sprint-picker-opt:hover { background: #f0f2f4; }
.sprint-picker-check { width: 16px; text-align: center; font-size: 13px; color: var(--bp-green, #16a34a); flex-shrink: 0; }
.sprint-picker-title { font-weight: 500; }

.sprint-nav { display: inline-flex; align-items: stretch; border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; height: 28px; background: var(--bg-card); }
.sprint-nav--active { border-color: var(--bp-blue); background: color-mix(in srgb, var(--bp-blue) 4%, var(--bg-card)); }
.sn-btn { display: inline-flex; align-items: center; justify-content: center; width: 24px; border: none; border-right: 1px solid var(--border); background: transparent; color: var(--text-muted); cursor: pointer; padding: 0; line-height: 0; }
.sn-btn:hover:not(:disabled) { background: var(--bg); color: var(--text); }
.sn-btn:disabled { opacity: .3; cursor: not-allowed; }
.sn-clear { border-right: none; border-left: 1px solid var(--border); width: 22px; }
.sn-clear:hover { color: var(--text); background: var(--bg); }
.sn-selector { display: inline-flex; align-items: center; gap: .2rem; padding: 0 .45rem; border: none; border-right: 1px solid var(--border); font-size: 12px; font-weight: 500; background: transparent; color: var(--text); cursor: pointer; white-space: nowrap; }
.sn-selector:hover { background: var(--bg); }
.sn-range { display: inline-flex; align-items: center; justify-content: center; min-width: 26px; border: none; border-right: 1px solid var(--border); padding: 0 .3rem; font-size: 10px; font-weight: 700; letter-spacing: .02em; background: transparent; color: var(--text-muted); cursor: pointer; }
.sn-range:last-of-type { border-right: none; }
.sn-range:hover { background: var(--bg); color: var(--text); }
.sn-range--active { background: var(--bp-blue); color: #fff; }
.sn-range--active:hover { background: var(--bp-blue-dark); }

.sn-dropdown { position: fixed; z-index: 10000; background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px; box-shadow: var(--shadow-md); width: 240px; max-height: 300px; display: flex; flex-direction: column; }
.sn-search { margin: .4rem; padding: .25rem .4rem; font-size: 11px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg); }
.sn-list { overflow-y: auto; padding: 0 .2rem .4rem; }
.sn-opt { display: flex; align-items: center; gap: .4rem; padding: .2rem .4rem; border-radius: var(--radius); font-size: 11px; cursor: pointer; }
.sn-opt:hover { background: var(--bg); }
.sn-opt--current { font-weight: 700; }
.sn-opt--selected { background: color-mix(in srgb, var(--bp-blue) 6%, var(--bg-card)); }
.sn-opt input[type="checkbox"] { accent-color: var(--bp-blue); width: 13px; height: 13px; }
.sn-opt-title { font-weight: 500; color: var(--text); }
.sn-opt-date { font-size: 9px; color: var(--text-muted); margin-left: auto; }

.sprint-separator-row td { padding: 1rem .85rem .35rem !important; border-bottom: none !important; background: transparent !important; }
.sprint-section-dragover { background: color-mix(in srgb, var(--bp-blue) 6%, var(--bg-card)) !important; }
.sprint-section-dragover .col-key, .sprint-section-dragover .col-actions { background: color-mix(in srgb, var(--bp-blue) 6%, var(--bg-card)) !important; }
.sprint-separator-row.sprint-section-dragover td { background: color-mix(in srgb, var(--bp-blue) 6%, var(--bg-card)) !important; }
.sprint-sep-label { display: flex; align-items: center; gap: .5rem; font-size: 11px; font-weight: 700; color: var(--text-muted); text-transform: uppercase; letter-spacing: .04em; padding: .3rem .75rem; background: color-mix(in srgb, var(--border) 30%, var(--bg-card)); border-radius: var(--radius); }
.sprint-sep-dates { font-weight: 400; font-size: 10px; letter-spacing: 0; text-transform: none; color: var(--text-muted); }
.scroll-sentinel { padding: .75rem; text-align: center; color: var(--text-muted); font-size: 12px; }
</style>
