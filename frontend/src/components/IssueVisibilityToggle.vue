<!--
  PAI-463 — Visibility-into-customer-portal toggle for IssueDetailView.

  Renders a compact eye-icon switch + an audit line. Clicking calls the
  standard issue-tag API; the parent (IssueDetailView) drives the
  CUSTOMERPORTAL attach/detach via `addIssueTag` / `removeIssueTag` and
  patches its own tag list on success. We refetch the audit line after
  each toggle so the "Last toggled by X · 3s ago" updates immediately.
-->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import AppIcon from '@/components/AppIcon.vue'
import { formatRelativeTimeWithLocale } from '@/composables/useDateFormat'
import {
  loadIssuePortalVisibility,
  type PortalVisibilityEvent,
} from '@/services/issueDetail'

const props = defineProps<{
  issueId: number
  /** Current CUSTOMERPORTAL attachment state (derived from issue.tags
   *  by the parent — kept as a prop so the toggle's checked indicator
   *  stays in sync with the rest of the tag UI without a duplicate
   *  fetch). */
  visible: boolean
  /** When true the toggle is interactive; when false it renders as a
   *  read-only indicator with a tooltip explaining why. Maps to the
   *  user's tag:write permission on the issue's project. */
  canEdit: boolean
}>()

const emit = defineEmits<{
  toggle: [visible: boolean]
}>()

const { t, locale } = useI18n()

const lastEvent = ref<PortalVisibilityEvent | null>(null)
const loading = ref(false)

async function refresh() {
  try {
    const data = await loadIssuePortalVisibility(props.issueId)
    lastEvent.value = data.last_event
  } catch (_e) {
    // Audit-line is best-effort; the toggle still works.
    lastEvent.value = null
  }
}

onMounted(refresh)
watch(() => props.issueId, refresh)

function onClick() {
  if (!props.canEdit || loading.value) return
  loading.value = true
  emit('toggle', !props.visible)
  // Parent does the API call; we just re-read the audit line after a
  // small delay so the new mutation_log row has time to land.
  window.setTimeout(() => {
    loading.value = false
    void refresh()
  }, 400)
}

const auditText = computed(() => {
  const ev = lastEvent.value
  if (!ev) return ''
  const when = relativeTime(ev.at, locale.value)
  if (ev.type === 'auto_tag') return t('visibility.auditAuto', { when })
  if (ev.type === 'migration_backfill')
    return t('visibility.auditMigration', { when })
  return t('visibility.auditLine', {
    actor: ev.actor || '?',
    when,
  })
})

const hintText = computed(() =>
  props.visible ? t('visibility.hint') : t('visibility.hintOff'),
)

function relativeTime(iso: string, lang: string): string {
  if (!iso) return ''
  return formatRelativeTimeWithLocale(iso, lang)
}
</script>

<template>
  <div class="vis-toggle" :class="{ 'vis-toggle--off': !visible }">
    <button
      type="button"
      class="vis-toggle__switch"
      :class="{
        'vis-toggle__switch--on': visible,
        'vis-toggle__switch--disabled': !canEdit,
      }"
      :aria-pressed="visible"
      :aria-label="t('visibility.label')"
      :title="!canEdit ? t('visibility.disabledTooltip') : undefined"
      :disabled="!canEdit || loading"
      @click="onClick"
    >
      <AppIcon name="eye" :size="12" />
      <span class="vis-toggle__label">{{ t('visibility.label') }}</span>
      <span class="vis-toggle__pip" />
    </button>
    <span class="vis-toggle__hint">{{ hintText }}</span>
    <span v-if="auditText" class="vis-toggle__audit">{{ auditText }}</span>
  </div>
</template>

<style scoped>
.vis-toggle {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  font-size: 0.8125rem;
  color: var(--text-muted, #6b7280);
}

.vis-toggle__switch {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.25rem 0.625rem;
  border-radius: 999px;
  border: 1px solid var(--border, #e5e7eb);
  background: var(--bg-subtle, #f9fafb);
  color: var(--text-muted, #6b7280);
  font-weight: 500;
  cursor: pointer;
  transition:
    background 120ms,
    border-color 120ms,
    color 120ms;
}

.vis-toggle__switch:hover:not(.vis-toggle__switch--disabled) {
  border-color: var(--brand, #2563eb);
  color: var(--brand, #2563eb);
}

.vis-toggle__switch--on {
  background: color-mix(in srgb, var(--brand, #2563eb) 12%, transparent);
  border-color: var(--brand, #2563eb);
  color: var(--brand, #2563eb);
}

.vis-toggle__switch--disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.vis-toggle__pip {
  display: inline-block;
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 50%;
  background: var(--text-muted, #9ca3af);
}

.vis-toggle__switch--on .vis-toggle__pip {
  background: var(--brand, #2563eb);
}

.vis-toggle__hint {
  color: var(--text-muted, #6b7280);
}

.vis-toggle__audit {
  font-size: 0.75rem;
  color: var(--text-faint, #9ca3af);
}
</style>
