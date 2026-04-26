<script setup lang="ts">
defineProps<{
  count?: number | null;
}>();

defineEmits<{
  refresh: [];
}>();
</script>

<template>
  <div class="refresh-banner" role="status">
    <span>
      {{ count && count > 0 ? `${count} issue${count === 1 ? "" : "s"} updated` : "Issue list changed" }}
    </span>
    <button class="refresh-banner__button" @click="$emit('refresh')">
      Refresh
    </button>
  </div>
</template>

<style scoped>
.refresh-banner {
  position: sticky;
  top: 0.75rem;
  z-index: 5;
  display: inline-flex;
  align-items: center;
  gap: 0.65rem;
  margin: 0 0 0.85rem;
  padding: 0.45rem 0.75rem;
  border: 1px solid color-mix(in srgb, var(--bp-blue) 18%, var(--border));
  border-radius: 999px;
  background: color-mix(in srgb, var(--bp-blue-pale) 72%, white);
  color: var(--bp-blue-dark);
  font-size: 12px;
  box-shadow: var(--shadow);
}

.refresh-banner__button {
  border: none;
  background: none;
  color: inherit;
  font: inherit;
  font-weight: 700;
  text-decoration: underline;
  text-underline-offset: 2px;
}
</style>
