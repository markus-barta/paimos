<script setup lang="ts">
import { ref, computed } from 'vue'

const props = defineProps<{
  modelValue: string
  suggestions: string[]
  placeholder?: string
}>()
const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const open = ref(false)
const filtered = computed(() =>
  props.suggestions.filter(s =>
    s.toLowerCase().includes(props.modelValue.toLowerCase())
  )
)

function select(s: string) {
  emit('update:modelValue', s)
  open.value = false
}

function onBlur() {
  setTimeout(() => { open.value = false }, 150)
}
</script>

<template>
  <div class="autocomplete">
    <input
      type="text"
      :value="modelValue"
      :placeholder="placeholder"
      @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      @focus="open = true"
      @blur="onBlur"
    />
    <ul v-if="open && filtered.length" class="suggestions">
      <li v-for="s in filtered" :key="s" @mousedown.prevent="select(s)">{{ s }}</li>
    </ul>
  </div>
</template>

<style scoped>
.autocomplete { position: relative; }
.suggestions {
  position: absolute;
  top: 100%;
  left: 0; right: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-top: none;
  border-radius: 0 0 var(--radius) var(--radius);
  box-shadow: var(--shadow-md);
  list-style: none;
  z-index: 50;
  max-height: 180px;
  overflow-y: auto;
}
.suggestions li {
  padding: .45rem .75rem;
  font-size: 13px;
  cursor: pointer;
  color: var(--text);
}
.suggestions li:hover { background: var(--bg); }
</style>
