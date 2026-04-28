<script setup lang="ts">
import { useBranding } from '@/composables/useBranding'
defineProps<{ compact?: boolean }>()
const { branding } = useBranding()
</script>

<template>
  <footer :class="['app-footer', { 'app-footer--compact': compact }]">
    <span>{{ branding.company }}</span>
    <a
      class="footer-logo-link"
      href="https://paimos.com"
      target="_blank"
      rel="noopener"
      title="paimos.com"
    >
      <img :src="branding.logo" alt="paimos" class="footer-logo" />
    </a>
  </footer>
</template>

<style scoped>
.app-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .4rem;
  padding: 1.25rem 0 .5rem;
  /* PAI-262: pin to bottom of `.main-content` flex column when view is short. */
  margin-top: auto;
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
.footer-logo-link {
  display: inline-flex;
  align-items: center;
  line-height: 0;
  opacity: .85;
  transition: opacity .15s;
}
.footer-logo-link:hover,
.footer-logo-link:focus-visible {
  opacity: 1;
}
.footer-logo { width: 16px; height: 16px; object-fit: contain; }
</style>
