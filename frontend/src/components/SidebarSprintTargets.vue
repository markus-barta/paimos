<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { useSidebarSprints } from '@/composables/useSidebarSprints'
import AppIcon from '@/components/AppIcon.vue'

defineProps<{
  isExpanded: boolean
}>()

const route  = useRoute()
const router = useRouter()

const {
  sidebarSprints, sprintDragOver, sprintAssigning,
  isCurrentSprint, dropOnSprint,
} = useSidebarSprints()
</script>

<template>
  <div v-if="sidebarSprints.length" class="sprint-targets">
    <div v-if="isExpanded" class="sprint-targets-label">Sprints</div>
    <div
      v-for="s in sidebarSprints" :key="s.id"
      :class="[
        'sprint-target',
        { 'sprint-target--current': isCurrentSprint(s) },
        { 'sprint-target--active': route.path === '/sprint-board' && Number(route.query.sprint) === s.id },
        { 'sprint-target--dragover': sprintDragOver === s.id },
        { 'sprint-target--assigning': sprintAssigning === s.id },
        { 'sprint-target--collapsed': !isExpanded },
      ]"
      :title="isExpanded ? (isCurrentSprint(s) ? `${s.title} (current)` : s.title) : s.title"
      @click="router.push(`/sprint-board?sprint=${s.id}`)"
      @dragover.prevent="sprintDragOver = s.id"
      @dragleave="sprintDragOver = null"
      @drop.prevent="dropOnSprint(s)"
    >
      <span class="sprint-target-dot" :class="{ 'sprint-target-dot--current': isCurrentSprint(s) }"></span>
      <span class="sl sprint-target-name">{{ s.title }}</span>
      <span v-if="isExpanded && s.sprint_state" :class="['sprint-state-badge', `sprint-state--${s.sprint_state}`]">{{ s.sprint_state }}</span>
      <span v-if="sprintAssigning === s.id" class="sprint-assigning-tick sl">&#x2713;</span>
    </div>
  </div>
</template>

<style scoped>
/* ── Sprint drop targets ──────────────────────────────────────────────────── */
.sprint-targets {
  display: flex; flex-direction: column; gap: .1rem;
  margin-top: .85rem;
}
.sprint-targets-label {
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .08em; color: rgba(200,213,226,.4);
  padding: 0 .65rem; margin-bottom: .2rem;
}
.sprint-target {
  display: flex; align-items: center; gap: .55rem;
  padding: .35rem .65rem; border-radius: var(--radius);
  border: 1px dashed transparent;
  cursor: pointer; transition: background .12s, border-color .12s;
  font-size: 12px; color: #8fa7be;
  overflow: hidden;
}
.sprint-target:hover { background: color-mix(in srgb, var(--bp-blue) 12%, transparent); color: var(--sidebar-text, #c8d5e2); }
.sprint-target--active { background: color-mix(in srgb, var(--bp-blue) 18%, transparent); color: #fff; border-color: color-mix(in srgb, var(--bp-blue) 35%, transparent); }
.sprint-target--collapsed { justify-content: center; padding-left: 0; padding-right: 0; gap: 0; }
.sprint-target--current { color: var(--sidebar-text, #c8d5e2); }
.sprint-target--dragover {
  background: color-mix(in srgb, var(--bp-blue) 22%, transparent);
  border-color: color-mix(in srgb, var(--bp-blue) 50%, transparent);
  color: #fff;
}
.sprint-target--assigning { background: rgba(5,150,105,.18); border-color: rgba(5,150,105,.4); }
.sprint-target-dot {
  width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0;
  background: rgba(200,213,226,.3);
}
.sprint-target-dot--current {
  background: #4a8fc2;
  box-shadow: 0 0 0 2px rgba(74,143,194,.3);
}
.sprint-target-name { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-weight: 500; }
.sprint-state-badge {
  font-size: 9px; font-weight: 700; text-transform: uppercase; letter-spacing: .05em;
  border-radius: 3px; padding: 0 .3rem; flex-shrink: 0;
}
.sprint-state--active   { background: rgba(217,119,6,.25); color: #fbbf24; }
.sprint-state--planned  { background: rgba(200,213,226,.15); color: #8fa7be; }
.sprint-state--complete { background: rgba(5,150,105,.2); color: #34d399; }
.sprint-state--archived { background: rgba(200,213,226,.12); color: #6b7280; }
.sprint-assigning-tick {
  font-size: 12px; color: #34d399; flex-shrink: 0;
}
</style>
