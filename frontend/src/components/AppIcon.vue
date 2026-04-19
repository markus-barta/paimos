<template>
  <component :is="icon" :size="size" :stroke-width="strokeWidth" :aria-hidden="true" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import * as LucideIcons from 'lucide-vue-next'

const props = withDefaults(defineProps<{
  name: string
  size?: number
  strokeWidth?: number
}>(), {
  size: 16,
  strokeWidth: 1.75,
})

// Convert kebab-case to PascalCase: "git-branch-plus" → "GitBranchPlus"
const icon = computed(() => {
  const pascal = props.name
    .split('-')
    .map(s => s.charAt(0).toUpperCase() + s.slice(1))
    .join('')
  return (LucideIcons as Record<string, unknown>)[pascal] ?? LucideIcons.CircleHelp
})
</script>
