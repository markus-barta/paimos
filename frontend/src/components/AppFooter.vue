<script setup lang="ts">
import { useBranding } from '@/composables/useBranding'
const { branding } = useBranding()
</script>

<template>
  <footer class="app-footer">
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
.footer-logo { width: 16px; height: 16px; object-fit: contain; }
</style>
