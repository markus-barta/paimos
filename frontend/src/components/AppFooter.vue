<script setup lang="ts">
import { useBranding } from '@/composables/useBranding'
defineProps<{ compact?: boolean }>()
const { branding } = useBranding()
</script>

<template>
  <footer :class="['app-footer', { 'app-footer--compact': compact }]">
    <img :src="branding.logo" alt="" class="footer-logo" aria-hidden="true" />
    <span>{{ branding.company }}</span>
  </footer>
</template>

<style scoped>
.app-footer {
  display: flex;
  align-items: center;
  gap: .4rem;
  padding: 1.25rem 0 .5rem;
  margin-top: 2rem;
  border-top: 1px solid var(--border);
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: .08em;
  text-transform: uppercase;
  opacity: .5;
}
.app-footer--compact {
  padding: .3rem 0 .1rem;
  margin-top: .25rem;
  border-top: none;
  font-size: 10px;
  opacity: .42;
}
.footer-logo { width: 16px; height: 16px; object-fit: contain; }
</style>
