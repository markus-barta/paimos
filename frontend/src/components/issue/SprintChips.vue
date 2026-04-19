<script setup lang="ts">
/**
 * SprintChips — unified sprint-chip renderer.
 *
 * Three surfaces previously hand-rolled near-identical sprint chip markup.
 * This component owns the chip styling; each caller keeps its own
 * Add button / dropdown since those layouts differ (Teleport vs. inline).
 *
 * The `v-else` fallback (`#42`) handles the case where a sprint in
 * `sprintIds` isn't present in `sprints` — e.g. the sprint has been
 * deleted but a stale id still lives on the issue.
 */
import { computed } from 'vue'
import type { Sprint } from '@/types'

const props = defineProps<{
  sprintIds: number[]
  sprints: Sprint[]
  /** Show the × remove button on each chip. */
  removable?: boolean
  /** Tighter padding for narrow surfaces like the side panel. */
  compact?: boolean
}>()

defineEmits<{
  (e: 'remove', sprintId: number): void
}>()

const sprintById = computed(() => {
  const m = new Map<number, Sprint>()
  for (const s of props.sprints) m.set(s.id, s)
  return m
})
</script>

<template>
  <div :class="['sprint-chips-row', { 'sprint-chips-row--compact': compact }]">
    <span
      v-for="sid in sprintIds"
      :key="sid"
      class="sprint-chip"
    >
      <template v-if="sprintById.get(sid)">
        {{ sprintById.get(sid)!.title }}
        <span
          v-if="sprintById.get(sid)!.sprint_state"
          :class="['sprint-chip-state', `sprint-chip-state--${sprintById.get(sid)!.sprint_state}`]"
        >
          {{ sprintById.get(sid)!.sprint_state }}
        </span>
      </template>
      <template v-else>#{{ sid }}</template>
      <button
        v-if="removable"
        type="button"
        class="sprint-chip-x"
        title="Remove sprint"
        @click="$emit('remove', sid)"
      >×</button>
    </span>
  </div>
</template>

<style scoped>
.sprint-chips-row {
  display: flex;
  flex-wrap: wrap;
  gap: .3rem;
  align-items: center;
}
.sprint-chip {
  display: inline-flex;
  align-items: center;
  gap: .3rem;
  background: #e0eeff;
  color: #1e4a8a;
  border: 1px solid color-mix(in srgb, #2e6da4 25%, transparent);
  border-radius: 20px;
  font-size: 12px;
  font-weight: 600;
  padding: .15rem .5rem .15rem .65rem;
  line-height: 1.2;
}
.sprint-chips-row--compact .sprint-chip {
  font-size: 11px;
  padding: .1rem .45rem .1rem .55rem;
}
.sprint-chip-x {
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
  font-size: 14px;
  line-height: 1;
  color: #1e4a8a;
  opacity: .55;
  font-family: inherit;
  display: inline-flex;
  align-items: center;
  transition: opacity .12s;
}
.sprint-chip-x:hover { opacity: 1; }
.sprint-chip-state {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .04em;
  border-radius: 3px;
  padding: 0 .25rem;
}
.sprint-chip-state--active   { background: #fff3e0; color: #b45309; }
.sprint-chip-state--planned  { background: #f3f4f6; color: #6b7280; }
.sprint-chip-state--complete { background: #dcfce7; color: #166534; }
.sprint-chip-state--archived { background: #e5e7eb; color: #6b7280; }
</style>
