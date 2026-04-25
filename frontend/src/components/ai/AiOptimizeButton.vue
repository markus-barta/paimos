<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-147. The ghost-style "AI" action that appears on supported
 multiline editors. Reusable; used by every multiline field that
 wants AI-optimization. The host:

   1. mounts the button anywhere along the field's edge,
   2. passes the current field text + an onAccept callback,
   3. renders <AiOptimizeOverlay> once below the editor, bound to
      the same useAiOptimize() return value.

 The button itself is purely presentational. All state (availability,
 in-flight, error) lives in the composable so two buttons on the same
 page share one source of truth.
-->
<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { useAiOptimize } from '@/composables/useAiOptimize'

const props = defineProps<{
  /** The field identifier the backend allow-lists (description, …). */
  field: string
  /** Pretty label for the diff overlay header. Defaults to `field`. */
  fieldLabel?: string
  /** Current field content. Read at click time, not at mount time. */
  text: () => string
  /** Optional issue id for context assembly. 0 = no context. */
  issueId?: number
  /** Called with the optimized text when the user clicks Accept. */
  onAccept: (text: string) => void
  /** Optional override of the disabled-state tooltip. */
  disabledTooltip?: string
}>()

const { available, isOptimizing, run } = useAiOptimize()

const disabled = computed(() => !available.value || isOptimizing.value)

const tooltip = computed(() => {
  if (!available.value) {
    return props.disabledTooltip
      ?? 'AI optimization is not configured. An admin can enable it under Settings → AI.'
  }
  if (isOptimizing.value) return 'Optimization in progress…'
  return 'Optimize wording with AI'
})

async function onClick() {
  if (disabled.value) return
  const current = props.text()
  if (!current.trim()) return
  await run({
    field: props.field,
    fieldLabel: props.fieldLabel ?? props.field,
    text: current,
    issueId: props.issueId,
    onAccept: props.onAccept,
  })
}
</script>

<template>
  <button
    type="button"
    class="ai-optimize-btn"
    :class="{ 'ai-optimize-btn--busy': isOptimizing }"
    :disabled="disabled"
    :title="tooltip"
    :aria-label="tooltip"
    @click="onClick"
  >
    <AppIcon :name="isOptimizing ? 'loader-circle' : 'sparkles'" :size="12" :class="{ spin: isOptimizing }" />
    <span class="ai-optimize-btn-label">AI</span>
  </button>
</template>

<style scoped>
.ai-optimize-btn {
  display: inline-flex; align-items: center; gap: .25rem;
  background: transparent;
  border: 1px solid transparent;
  color: var(--text-muted);
  padding: .15rem .45rem;
  font-size: 11px; font-weight: 600;
  letter-spacing: .04em;
  border-radius: 999px;
  cursor: pointer;
  font-family: 'DM Sans', sans-serif;
  transition: background .12s, color .12s, border-color .12s;
}
.ai-optimize-btn:hover:not(:disabled) {
  background: var(--bp-blue-pale, #dce9f4);
  color: var(--bp-blue-dark, #1f4d75);
  border-color: var(--bp-blue-light, #4a8fc2);
}
.ai-optimize-btn:disabled {
  cursor: not-allowed;
  opacity: .55;
}
.ai-optimize-btn--busy {
  /* Hover styling stays muted while the call is in flight. */
  color: var(--bp-blue-dark, #1f4d75);
  background: var(--bp-blue-pale, #dce9f4);
  border-color: var(--bp-blue-light, #4a8fc2);
  opacity: 1;
}
.ai-optimize-btn-label { line-height: 1; }

.spin { animation: ai-optimize-spin 1s linear infinite; }
@keyframes ai-optimize-spin {
  from { transform: rotate(0); }
  to   { transform: rotate(360deg); }
}
</style>
