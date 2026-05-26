<!--
  PAI-474 — purpose-built side panel for the Customer Portal.

  Mirrors the look-and-feel of the internal IssueSidePanel (same right-
  slide chrome, same width) but is purpose-built for the customer
  data shape: it loads via the portal API (cleaned response — no cost
  or effort fields), shows only the fields a customer cares about
  (description, acceptance criteria, report summary, the customer-
  visible comments thread), and surfaces the Accept / Reject actions
  directly so the customer can act without leaving the project view.

  Why not reuse IssueSidePanel.vue? That component (2058 lines) is
  deeply wired into the internal app — sprint chips, attachment
  manager, time entries, AI action menu, inline editing. Even with its
  existing `readonly` prop, retrofitting it for portal API routing +
  external-only comments + accept/reject wiring is a much bigger
  surgery than building this dedicated panel. PAI-476 tracks the
  long-term unification follow-up.
-->
<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'

import AppIcon from '@/components/AppIcon.vue'
import StatusDot from '@/components/StatusDot.vue'
import { api, errMsg } from '@/api/client'
import { useMarkdown } from '@/composables/useMarkdown'
import { useSidePanelWidth } from '@/composables/useSidePanelWidth'

interface PortalIssueDetail {
  id: number
  issue_key: string
  title: string
  description: string
  acceptance_criteria: string
  report_summary: string
  status: string
  priority: string
  type: string
  accepted_at: string | null
  created_at: string
  updated_at: string
}

interface PortalComment {
  id: number
  author: string | null
  body: string
  visibility: string
  created_at: string
}

const props = defineProps<{
  /** Project id for the URL scope. */
  projectId: number
  /** Issue id to show, or null to keep the panel closed. */
  issueId: number | null
  /**
   * PAI-497: ordered list of issue ids the parent considers "the
   * current navigation context" (typically the active filter+tab set).
   * Drives the prev/next buttons in the header. Omit (or pass <2 ids)
   * to hide the buttons.
   */
  issueIds?: number[]
  /**
   * PAI-496: pinned flag, owned by the parent and mirrored to
   * localStorage. When true the panel ignores ESC dismissal; the
   * explicit close button still works.
   */
  pinned?: boolean
}>()

const emit = defineEmits<{
  close: []
  /** Emitted after a successful accept; lets the parent refetch. */
  accepted: [issueId: number]
  /** Emitted after a successful reject; lets the parent refetch. */
  rejected: [issueId: number]
  /** PAI-497: navigate to a sibling issue (parent swaps issueId). */
  navigate: [issueId: number]
  /** PAI-496: pin toggle; parent persists. */
  'update:pinned': [pinned: boolean]
}>()

const { t } = useI18n()

const issue = ref<PortalIssueDetail | null>(null)
const comments = ref<PortalComment[]>([])
const loading = ref(false)
const error = ref('')

// Accept / Reject in-flight state — only one action at a time.
const acceptInFlight = ref(false)
const rejectInFlight = ref(false)
const actionError = ref('')

// Markdown rendering. PAI-474 portal trust model: descriptions are
// authored internally before customer visibility is granted, so
// rendered HTML goes through DOMPurify (useMarkdown does this) but no
// extra editing affordance is exposed.
const markdownEnabled = ref(true)
const descSource = computed(() => issue.value?.description ?? '')
const acSource = computed(() => issue.value?.acceptance_criteria ?? '')
const summarySource = computed(() => issue.value?.report_summary ?? '')
const { html: descHtml } = useMarkdown(descSource, markdownEnabled)
const { html: acHtml } = useMarkdown(acSource, markdownEnabled)
const { html: summaryHtml } = useMarkdown(summarySource, markdownEnabled)

// Customer-friendly labels — mirrors PortalProjectView. Kept inline
// rather than re-exported so this component stays self-contained.
function portalStatusLabel(status: string): string {
  switch (status) {
    case 'new':
    case 'backlog':
      return t('portal.statusLabel.planned')
    case 'in-progress':
    case 'qa':
      return t('portal.statusLabel.inProgress')
    case 'done':
    case 'delivered':
      return t('portal.statusLabel.readyForReview')
    case 'accepted':
    case 'invoiced':
      return t('portal.statusLabel.accepted')
    default:
      return t('status.' + status)
  }
}
function portalTypeLabel(type: string): string {
  const key = `portal.typeLabel.${type}`
  const translated = t(key)
  if (translated === key) return type.charAt(0).toUpperCase() + type.slice(1)
  return translated
}

// PAI-496: share the internal panel's resizable width so AppLayout's
// inset (driven by useSidePanelPinned + useSidePanelWidth) and the
// portal panel's actual width stay in lock-step.
const { width: sidePanelWidth } = useSidePanelWidth()
const asideStyle = computed(() => ({ width: sidePanelWidth.value + 'px' }))

// PAI-497: prev/next navigation. Stays inside the parent-supplied
// list; no wrap-around — matches the internal IssueSidePanel.
const currentIdx = computed(() => {
  if (!props.issueIds || props.issueId == null) return -1
  return props.issueIds.indexOf(props.issueId)
})
const canPrev = computed(() => currentIdx.value > 0)
const canNext = computed(
  () => !!props.issueIds && currentIdx.value >= 0 && currentIdx.value < props.issueIds.length - 1,
)
const showNav = computed(() => !!props.issueIds && props.issueIds.length > 1)
function goPrev() {
  if (canPrev.value && props.issueIds) emit('navigate', props.issueIds[currentIdx.value - 1])
}
function goNext() {
  if (canNext.value && props.issueIds) emit('navigate', props.issueIds[currentIdx.value + 1])
}

// PAI-496: pin toggle.
function togglePin() {
  emit('update:pinned', !props.pinned)
}

// Accept / Reject are available only on the customer-actionable states.
const canAccept = computed(() => {
  if (!issue.value) return false
  return issue.value.status === 'done' || issue.value.status === 'delivered'
})
const canReject = computed(() => canAccept.value)

async function loadIssue() {
  if (props.issueId == null) {
    issue.value = null
    comments.value = []
    return
  }
  loading.value = true
  error.value = ''
  actionError.value = ''
  try {
    const [iss, cmts] = await Promise.all([
      api.get<PortalIssueDetail>(
        `/portal/projects/${props.projectId}/issues/${props.issueId}`,
      ),
      api.get<PortalComment[]>(`/portal/issues/${props.issueId}/comments`).catch(() => []),
    ])
    issue.value = iss
    comments.value = cmts
  } catch (e: unknown) {
    error.value = errMsg(e, 'Failed to load issue.')
  } finally {
    loading.value = false
  }
}

watch(() => props.issueId, () => void loadIssue(), { immediate: true })

// ESC to close — match the internal side-panel UX. PAI-496: when
// pinned, ESC is a no-op; only the explicit close button dismisses.
function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && props.issueId != null && !props.pinned) {
    emit('close')
  }
}
onMounted(() => {
  window.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
})

async function onAccept() {
  if (!issue.value || acceptInFlight.value) return
  acceptInFlight.value = true
  actionError.value = ''
  try {
    await api.post(`/portal/issues/${issue.value.id}/accept`, {})
    emit('accepted', issue.value.id)
    // Refresh local view so the status pill flips immediately.
    await loadIssue()
  } catch (e: unknown) {
    actionError.value = errMsg(e, 'Accept failed.')
  } finally {
    acceptInFlight.value = false
  }
}

async function onReject() {
  if (!issue.value || rejectInFlight.value) return
  // Minimal-friction reject: prompt for an optional reason via the
  // browser dialog. A richer reason modal can come later — for v1 the
  // value is just being able to express disagreement at all.
  const reason = window.prompt(t('portal.rejectPrompt'), '')
  if (reason === null) return // user cancelled
  rejectInFlight.value = true
  actionError.value = ''
  try {
    await api.post(`/portal/issues/${issue.value.id}/reject`, { reason: reason.trim() })
    emit('rejected', issue.value.id)
    await loadIssue()
  } catch (e: unknown) {
    actionError.value = errMsg(e, 'Reject failed.')
  } finally {
    rejectInFlight.value = false
  }
}

function fmtDate(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}
</script>

<template>
  <Transition name="psp-slide">
    <aside v-if="issueId != null" class="psp" :style="asideStyle" aria-modal="true" role="dialog">
      <header class="psp__header">
        <!-- PAI-496: pin toggle — stops ESC from dismissing the panel. -->
        <button
          type="button"
          class="psp__pin"
          :class="{ 'psp__pin--active': pinned }"
          @click="togglePin"
          :title="pinned ? t('portal.unpin') : t('portal.pin')"
          :aria-pressed="!!pinned"
        >
          <AppIcon :name="pinned ? 'pin' : 'pin-off'" :size="14" />
        </button>
        <div class="psp__crumbs">
          <span class="psp__type">{{
            issue ? portalTypeLabel(issue.type) : ''
          }}</span>
          <span v-if="issue" class="psp__key">{{ issue.issue_key }}</span>
        </div>
        <!-- PAI-497: prev/next siblings within the active filter. -->
        <button
          v-if="showNav"
          type="button"
          class="psp__nav"
          :disabled="!canPrev"
          @click="goPrev"
          :title="t('portal.prevIssue')"
          :aria-label="t('portal.prevIssue')"
        >
          <AppIcon name="chevron-up" :size="15" />
        </button>
        <button
          v-if="showNav"
          type="button"
          class="psp__nav"
          :disabled="!canNext"
          @click="goNext"
          :title="t('portal.nextIssue')"
          :aria-label="t('portal.nextIssue')"
        >
          <AppIcon name="chevron-down" :size="15" />
        </button>
        <button type="button" class="psp__close" @click="emit('close')" :aria-label="t('portal.close')">
          <AppIcon name="x" :size="16" />
        </button>
      </header>

      <div v-if="loading" class="psp__loading">{{ t('portal.loading') }}</div>
      <div v-else-if="error" class="psp__error">{{ error }}</div>
      <div v-else-if="!issue" class="psp__empty">{{ t('portal.noIssues') }}</div>

      <div v-else class="psp__body">
        <h2 class="psp__title">{{ issue.title }}</h2>

        <!-- Status + priority pills — customer-friendly labels. -->
        <div class="psp__pills">
          <span class="psp__pill psp__pill--status">
            <StatusDot :status="issue.status" />
            {{ portalStatusLabel(issue.status) }}
          </span>
          <span
            v-if="issue.priority"
            :class="['psp__pill', 'psp__pill--priority', `psp__pill--priority-${issue.priority}`]"
          >
            {{ issue.priority }}
          </span>
        </div>

        <!-- Description (markdown). -->
        <section v-if="descSource" class="psp__section">
          <h3 class="psp__section-title">{{ t('portal.issueDetail.description') }}</h3>
          <div class="psp__md" v-html="descHtml" />
        </section>

        <!-- Acceptance Criteria. -->
        <section v-if="acSource" class="psp__section">
          <h3 class="psp__section-title">{{ t('portal.issueDetail.acceptanceCriteria') }}</h3>
          <div class="psp__md" v-html="acHtml" />
        </section>

        <!-- Report Summary — customer-facing acceptance write-up. -->
        <section v-if="summarySource" class="psp__section">
          <h3 class="psp__section-title">{{ t('portal.reportSummary') }}</h3>
          <div class="psp__md" v-html="summaryHtml" />
        </section>

        <!-- Comments thread (external-only — PAI-475 filter at the
             backend; this component does not even attempt to surface
             a composer for portal users). -->
        <section class="psp__section">
          <h3 class="psp__section-title">
            {{ t('portal.comments') }}
            <span v-if="comments.length" class="psp__count">{{ comments.length }}</span>
          </h3>
          <div v-if="!comments.length" class="psp__empty-sub">
            {{ t('portal.noComments') }}
          </div>
          <ul v-else class="psp__comments">
            <li v-for="c in comments" :key="c.id" class="psp__comment">
              <div class="psp__comment-meta">
                <span class="psp__comment-author">{{ c.author ?? '—' }}</span>
                <span class="psp__comment-date">{{ fmtDate(c.created_at) }}</span>
              </div>
              <div class="psp__comment-body">{{ c.body }}</div>
            </li>
          </ul>
        </section>

        <!-- Accept / Reject footer — sticky so it stays in view even
             on long issue threads. Only visible when the issue is in
             a customer-actionable state. -->
        <div v-if="canAccept || canReject" class="psp__footer">
          <div v-if="actionError" class="psp__action-error">{{ actionError }}</div>
          <div class="psp__actions">
            <button
              type="button"
              class="psp__btn psp__btn--reject"
              :disabled="!canReject || rejectInFlight"
              @click="onReject"
            >
              {{ t('portal.reject') }}
            </button>
            <button
              type="button"
              class="psp__btn psp__btn--accept"
              :disabled="!canAccept || acceptInFlight"
              @click="onAccept"
            >
              {{ acceptInFlight ? t('portal.accepting') : t('portal.accept') }}
            </button>
          </div>
        </div>
      </div>
    </aside>
  </Transition>
</template>

<style scoped>
.psp {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  /* Width comes from useSidePanelWidth via :style; cap at 96vw as a
     safety net so very-narrow viewports never overflow. */
  max-width: 96vw;
  background: var(--bg-elevated, #ffffff);
  border-left: 1px solid var(--border, #e5e7eb);
  box-shadow: -12px 0 32px rgba(15, 23, 42, 0.10);
  display: flex;
  flex-direction: column;
  z-index: 90;
}

.psp__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.75rem 1rem;
  border-bottom: 1px solid var(--border, #e5e7eb);
  flex-shrink: 0;
}

.psp__crumbs {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  font-size: 12px;
}

.psp__type {
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted, #6b7280);
  font-weight: 600;
}

.psp__key {
  color: var(--bp-blue, #2563eb);
  font-family: 'DM Mono', 'Menlo', monospace;
  font-weight: 600;
}

.psp__close,
.psp__pin,
.psp__nav {
  background: none;
  border: none;
  cursor: pointer;
  padding: 0.25rem;
  color: var(--text-muted, #6b7280);
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.psp__close:hover,
.psp__pin:hover,
.psp__nav:hover:not(:disabled) {
  background: var(--bg-subtle, #f3f4f6);
  color: var(--text, #111827);
}
.psp__pin--active {
  color: var(--brand, #2563eb);
}
.psp__pin--active:hover {
  color: var(--brand, #2563eb);
}
.psp__nav:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.psp__body {
  flex: 1;
  overflow-y: auto;
  padding: 1rem 1.25rem 5rem; /* extra bottom space for sticky footer */
}

.psp__title {
  font-size: 1.25rem;
  font-weight: 600;
  line-height: 1.35;
  margin: 0 0 0.75rem;
}

.psp__pills {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
  margin-bottom: 1.25rem;
}

.psp__pill {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  padding: 0.2rem 0.6rem;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 500;
  background: var(--bg-subtle, #f3f4f6);
  border: 1px solid var(--border, #e5e7eb);
  color: var(--text, #111827);
}

.psp__pill--priority-high {
  background: #fee2e2;
  border-color: #fecaca;
  color: #991b1b;
}
.psp__pill--priority-medium {
  background: #fef3c7;
  border-color: #fde68a;
  color: #92400e;
}
.psp__pill--priority-low {
  background: #dbeafe;
  border-color: #bfdbfe;
  color: #1e40af;
}

.psp__section {
  margin-bottom: 1.5rem;
}

.psp__section-title {
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-muted, #6b7280);
  margin: 0 0 0.5rem;
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.psp__count {
  background: var(--bg-subtle, #f3f4f6);
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 20px;
  font-size: 11px;
  padding: 0.05rem 0.45rem;
  font-weight: 600;
}

.psp__md {
  font-size: 13px;
  line-height: 1.6;
  color: var(--text, #111827);
}
.psp__md :deep(h1),
.psp__md :deep(h2),
.psp__md :deep(h3) {
  font-weight: 700;
  margin: 0.5rem 0 0.25rem;
}
.psp__md :deep(p) { margin: 0 0 0.5rem; }
.psp__md :deep(ul),
.psp__md :deep(ol) { padding-left: 1.2rem; margin: 0 0 0.5rem; }
.psp__md :deep(code) {
  font-family: 'DM Mono', monospace;
  background: var(--bg-subtle, #f3f4f6);
  padding: 0.1rem 0.3rem;
  border-radius: 3px;
}
.psp__md :deep(pre) {
  background: var(--bg-subtle, #f3f4f6);
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 6px;
  padding: 0.6rem 0.8rem;
  overflow-x: auto;
}
.psp__md :deep(a) {
  color: var(--bp-blue, #2563eb);
  text-decoration: underline;
}

.psp__empty-sub {
  font-size: 13px;
  color: var(--text-muted, #6b7280);
  font-style: italic;
}

.psp__comments {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.9rem;
}

.psp__comment {
  background: var(--bg-subtle, #f9fafb);
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 8px;
  padding: 0.65rem 0.85rem;
}

.psp__comment-meta {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 0.35rem;
}

.psp__comment-author {
  font-size: 12px;
  font-weight: 600;
}
.psp__comment-date {
  font-size: 11px;
  color: var(--text-muted, #6b7280);
}
.psp__comment-body {
  font-size: 13px;
  color: var(--text, #111827);
  white-space: pre-wrap;
  line-height: 1.55;
}

.psp__footer {
  position: sticky;
  bottom: 0;
  margin: 1rem -1.25rem 0;
  padding: 0.85rem 1.25rem;
  background: var(--bg-elevated, #ffffff);
  border-top: 1px solid var(--border, #e5e7eb);
}

.psp__action-error {
  font-size: 12px;
  color: #b91c1c;
  margin-bottom: 0.45rem;
}

.psp__actions {
  display: flex;
  gap: 0.5rem;
  justify-content: flex-end;
}

.psp__btn {
  padding: 0.4rem 0.95rem;
  font-size: 13px;
  font-weight: 600;
  border-radius: 6px;
  cursor: pointer;
  border: 1px solid var(--border, #e5e7eb);
}

.psp__btn--accept {
  background: var(--bp-blue, #2563eb);
  color: #fff;
  border-color: var(--bp-blue, #2563eb);
}
.psp__btn--accept:hover:not(:disabled) {
  filter: brightness(0.95);
}

.psp__btn--reject {
  background: #fff;
  color: var(--text, #111827);
}
.psp__btn--reject:hover:not(:disabled) {
  background: var(--bg-subtle, #f3f4f6);
}

.psp__btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.psp__loading,
.psp__error,
.psp__empty {
  padding: 2rem;
  text-align: center;
  color: var(--text-muted, #6b7280);
}
.psp__error { color: #b91c1c; }

/* Slide-in transition — mirrors the internal IssueSidePanel feel. */
.psp-slide-enter-active,
.psp-slide-leave-active {
  transition:
    transform 220ms cubic-bezier(0.4, 0, 0.2, 1),
    opacity 200ms ease;
}
.psp-slide-enter-from,
.psp-slide-leave-to {
  transform: translateX(100%);
  opacity: 0;
}
</style>
