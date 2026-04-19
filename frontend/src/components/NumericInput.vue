<script setup lang="ts">
/**
 * NumericInput — locale-aware numeric input that accepts both dot and comma
 * as decimal separators. Emits a number (or null) on change.
 *
 * Replaces `<input type="number" v-model.number="x">` with
 * `<NumericInput v-model="x" />` for locale support.
 */
import { ref, watch } from 'vue'
import { parseLocaleNumber } from '@/composables/useDurationInput'

const props = defineProps<{
  modelValue: number | null | undefined
  placeholder?: string
  min?: number
  step?: number
}>()

const emit = defineEmits<{ 'update:modelValue': [value: number | null] }>()

function format(v: number | null | undefined): string {
  return v != null ? String(v) : ''
}

const text = ref(format(props.modelValue))

watch(() => props.modelValue, (v) => {
  // Only sync from parent if the parsed text doesn't already match
  const current = parseLocaleNumber(text.value)
  if (current !== v) text.value = format(v)
})

function onBlur() {
  const n = parseLocaleNumber(text.value)
  text.value = n != null ? String(n) : ''
  emit('update:modelValue', n)
}
</script>

<template>
  <input
    v-model="text"
    type="text"
    inputmode="decimal"
    :placeholder="placeholder"
    @blur="onBlur"
  />
</template>
