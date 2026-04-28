<script setup lang="ts">
import { RouterLink } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'

const reports = [
  {
    id: 'lieferbericht',
    title: 'Lieferbericht',
    description: 'Delivery report — issues grouped by epic with AR calculations, subtotals, and PDF export.',
    icon: 'file-text',
    to: '/reporting/lieferbericht',
  },
]
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span class="ah-title">Reporting</span>
    <span class="ah-subtitle">Generate reports from your project data.</span>
  </Teleport>

  <div class="report-grid">
    <RouterLink
      v-for="r in reports"
      :key="r.id"
      :to="r.to"
      class="report-card"
    >
      <AppIcon :name="r.icon" :size="28" class="report-card-icon" />
      <h2 class="report-card-title">{{ r.title }}</h2>
      <p class="report-card-desc">{{ r.description }}</p>
    </RouterLink>
  </div>
</template>

<style scoped>
.report-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 1.25rem; margin-top: .5rem;
}
.report-card {
  display: flex; flex-direction: column; gap: .5rem;
  padding: 1.5rem; background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow);
  text-decoration: none; color: var(--text);
  transition: border-color .12s, box-shadow .12s;
}
.report-card:hover {
  border-color: var(--bp-blue); box-shadow: 0 0 0 2px var(--bp-blue-pale);
}
.report-card-icon { color: var(--bp-blue); }
.report-card-title { font-size: 15px; font-weight: 700; margin: 0; }
.report-card-desc { font-size: 13px; color: var(--text-muted); line-height: 1.5; margin: 0; }
</style>
