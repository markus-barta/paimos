<template>
  <component :is="icon" :size="size" :stroke-width="strokeWidth" :aria-hidden="true" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CircleHelp } from 'lucide-vue-next'
import { appIcons } from '@/components/appIconRegistry'

const props = withDefaults(defineProps<{
  name: string
  size?: number
  strokeWidth?: number
}>(), {
  size: 16,
  strokeWidth: 1.75,
})

const icon = computed(() => {
  return appIcons[props.name] ?? CircleHelp
})
</script>
