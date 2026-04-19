<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useRecentProjects } from '@/composables/useRecentProjects'
import AppIcon from '@/components/AppIcon.vue'

defineProps<{
  isExpanded: boolean
}>()

const route  = useRoute()
const router = useRouter()
const auth   = useAuthStore()
const { recentProjects } = useRecentProjects()
</script>

<template>
  <div v-if="recentProjects.length && auth.user?.recent_projects_limit !== 0" class="recent-projects">
    <div v-if="isExpanded" class="recent-projects-label">Recent</div>
    <div
      v-for="(p, idx) in recentProjects"
      :key="p.id"
      :class="['recent-project-item', { 'recent-project-item--active': route.path === `/projects/${p.id}`, 'recent-project-item--collapsed': !isExpanded }]"
      :style="{ opacity: route.path === `/projects/${p.id}` ? 1 : 1 - (idx / Math.max(recentProjects.length - 1, 1)) * 0.5 }"
      :title="p.name"
      @click="router.push(`/projects/${p.id}`)"
    >
      <AppIcon name="folder" :size="14" />
      <span class="sl rp-name">{{ p.key }} — {{ p.name }}</span>
    </div>
  </div>
</template>

<style scoped>
/* ── Recent projects ──────────────────────────────────────────────────────── */
.recent-projects {
  display: flex; flex-direction: column; gap: .1rem;
  margin-top: .85rem;
}
.recent-projects-label {
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .08em; color: rgba(200,213,226,.4);
  padding: 0 .65rem; margin-bottom: .2rem;
}
.recent-project-item {
  display: flex; align-items: center; gap: .55rem;
  padding: .3rem .65rem; border-radius: var(--radius);
  cursor: pointer; transition: background .12s, color .12s;
  font-size: 12px; color: #8fa7be; overflow: hidden;
}
.recent-project-item:hover { background: color-mix(in srgb, var(--bp-blue) 12%, transparent); color: var(--sidebar-text, #c8d5e2); }
.recent-project-item--active { background: color-mix(in srgb, var(--bp-blue) 18%, transparent); color: #fff; }
.recent-project-item--collapsed { justify-content: center; padding-left: 0; padding-right: 0; gap: 0; }
.rp-name {
  flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-weight: 500;
}
</style>
