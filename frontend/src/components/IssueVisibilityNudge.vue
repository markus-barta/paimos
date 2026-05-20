<!--
  PAI-464 — status-transition nudge banner.

  Soft amber inline banner that appears on IssueDetailView when an issue
  has moved into a terminal-ish status (delivered or done) but isn't
  attached to the CUSTOMERPORTAL tag — i.e. the customer can't see it
  yet. Clicking the link attaches the tag through the same path the
  toggle uses (the parent owns the API call).

  By design the banner has no close-without-action button: dismissing
  the nudge without attaching would mask the discipline we're trying
  to build. It disappears on its own when the tag is attached, when
  status leaves the (delivered, done) set, or when the issue is
  cancelled.
-->
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import AppIcon from '@/components/AppIcon.vue'

const props = defineProps<{
  status: string
  visible: boolean
  /** Whether the current user has permission to attach the tag.
   *  Banner still renders when false so the customer-impact is
   *  visible to viewers — they just can't act on it from here. */
  canEdit: boolean
}>()

const emit = defineEmits<{
  makeVisible: []
}>()

const { t } = useI18n()

// AC: render only when status ∈ (delivered, done) AND tag is absent.
// Cancelled, accepted, invoiced, and earlier statuses are out of scope.
const TERMINAL_STATUSES = new Set(['delivered', 'done'])

const shouldShow = computed(
  () => !props.visible && TERMINAL_STATUSES.has(props.status),
)

function onMakeVisible() {
  if (!props.canEdit) return
  emit('makeVisible')
}
</script>

<template>
  <div v-if="shouldShow" class="vis-nudge" role="status">
    <AppIcon name="eye" :size="14" />
    <span class="vis-nudge__text">{{ t('visibility.nudge') }}</span>
    <button
      type="button"
      class="vis-nudge__action"
      :disabled="!canEdit"
      @click="onMakeVisible"
    >
      {{ t('visibility.nudgeAction') }}
    </button>
  </div>
</template>

<style scoped>
.vis-nudge {
  /* Soft amber per the design brief — distinct enough to notice on a
     terminal-status issue but not so loud it competes with the title
     or the priority pill above it. */
  display: flex;
  align-items: center;
  gap: 0.625rem;
  margin: 0.5rem 0 0.875rem;
  padding: 0.625rem 0.875rem;
  background: #fef3c7;
  border: 1px solid #fde68a;
  border-radius: 8px;
  color: #92400e;
  font-size: 0.875rem;
}

.vis-nudge__text {
  flex: 1;
  min-width: 0;
}

.vis-nudge__action {
  border: none;
  background: transparent;
  color: inherit;
  font-weight: 600;
  cursor: pointer;
  padding: 0;
  text-decoration: underline;
  text-underline-offset: 2px;
}

.vis-nudge__action:hover:not(:disabled) {
  color: #78350f;
}

.vis-nudge__action:disabled {
  opacity: 0.55;
  cursor: not-allowed;
  text-decoration: none;
}
</style>
