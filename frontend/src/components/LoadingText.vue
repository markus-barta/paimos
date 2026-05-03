<script setup lang="ts">
withDefaults(
  defineProps<{
    label?: string
    as?: string
    align?: 'left' | 'center'
    size?: 'sm' | 'md'
  }>(),
  {
    label: 'Loading…',
    as: 'div',
    align: 'left',
    size: 'md',
  },
)
</script>

<template>
  <component
    :is="as"
    class="loading-text"
    :class="[`loading-text--${align}`, `loading-text--${size}`]"
    role="status"
    aria-live="polite"
    aria-busy="true"
  >
    <span class="loading-text__label">{{ label }}</span>
  </component>
</template>

<style scoped>
.loading-text {
  min-width: 0;
  max-width: 100%;
  color: var(--text-muted);
  font-size: inherit;
  line-height: inherit;
}

.loading-text--center {
  display: block;
  text-align: center;
  width: 100%;
}

.loading-text--sm {
  font-size: 12px;
}

.loading-text__label {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: currentColor;
  background:
    linear-gradient(
      105deg,
      color-mix(in srgb, currentColor 46%, transparent) 0%,
      currentColor 38%,
      color-mix(in srgb, currentColor 72%, white) 50%,
      currentColor 62%,
      color-mix(in srgb, currentColor 46%, transparent) 100%
    );
  background-size: 240% 100%;
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  animation: loading-text-shimmer 1.45s linear infinite;
}

@keyframes loading-text-shimmer {
  from {
    background-position: 120% 0;
  }
  to {
    background-position: -120% 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .loading-text__label {
    color: currentColor;
    background: none;
    -webkit-text-fill-color: currentColor;
    animation: none;
  }
}
</style>
