<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'

interface DecisionButton {
  label: string
  shortcut?: string
  action: () => void
}

const props = defineProps<{
  primary: DecisionButton
  secondary?: DecisionButton[]
  explain?: DecisionButton
}>()

const emit = defineEmits<{
  (e: 'decide', shortcut?: string): void
}>()

function invoke(btn?: DecisionButton) {
  if (!btn) return
  btn.action()
  emit('decide', btn.shortcut)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    invoke(props.primary)
    return
  }
  if (e.key === 'Escape') {
    e.preventDefault()
    if (props.secondary?.[0]) invoke(props.secondary[0])
    return
  }
  const key = e.key.toLowerCase()
  if (props.primary.shortcut?.toLowerCase() === key) {
    e.preventDefault()
    invoke(props.primary)
    return
  }
  for (const btn of props.secondary ?? []) {
    if (btn.shortcut?.toLowerCase() === key) {
      e.preventDefault()
      invoke(btn)
      return
    }
  }
  if (props.explain?.shortcut?.toLowerCase() === key) {
    e.preventDefault()
    invoke(props.explain)
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div class="aux-dec-row">
    <div class="aux-dec-copy">
      <slot />
    </div>
    <div class="aux-dec-actions">
      <button type="button" class="btn btn-primary aux-dec-btn" @click="invoke(primary)">
        {{ primary.label }}
      </button>
      <button
        v-for="btn in secondary ?? []"
        :key="btn.label"
        type="button"
        class="btn btn-ghost aux-dec-btn"
        @click="invoke(btn)"
      >
        {{ btn.label }}
      </button>
      <button v-if="explain" type="button" class="btn btn-ghost aux-dec-btn aux-dec-btn--explain" @click="invoke(explain)">
        {{ explain.label }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.aux-dec-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .75rem;
  padding-top: .65rem;
}
.aux-dec-copy {
  font-size: 13px;
  color: var(--text);
}
.aux-dec-actions {
  display: flex;
  align-items: center;
  gap: .4rem;
  flex-wrap: wrap;
}
.aux-dec-btn {
  min-height: 32px;
  font-size: 12px;
}
.aux-dec-btn--explain {
  margin-left: auto;
}
</style>
