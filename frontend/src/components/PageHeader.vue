<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <div class="page-header">
    <div>
      <h1 class="page-title">{{ title }}</h1>
      <div v-if="$slots.subtitle" class="page-subtitle"><slot name="subtitle" /></div>
      <p v-else-if="subtitle" class="page-subtitle">{{ subtitle }}</p>
    </div>
    <slot />
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 1.75rem;
}
.page-title   { font-size: 20px; font-weight: 700; color: var(--text); letter-spacing: -.02em; }
.page-subtitle { font-size: 13px; color: var(--text-muted); margin-top: .2rem; }
</style>
