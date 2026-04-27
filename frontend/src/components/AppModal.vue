<script setup lang="ts">
import { watch, onUnmounted } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
const props = defineProps<{
  title: string
  open: boolean
  /** Max width override, e.g. '640px'. Defaults to 480px. */
  maxWidth?: string
  /** Single-char keyboard shortcut for the confirm/primary action (e.g. 'd' for Delete). Ignored when an input is focused. */
  confirmKey?: string
}>()
const emit = defineEmits<{ close: [], confirm: [] }>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') { emit('close'); e.preventDefault(); return }
  if (!props.confirmKey) return
  if (e.key === 'Enter') { emit('confirm'); e.preventDefault(); return }
  const tag = (e.target as HTMLElement)?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
  if (e.key.toLowerCase() === props.confirmKey.toLowerCase()) { emit('confirm'); e.preventDefault() }
}
watch(() => props.open, (v) => {
  if (v) window.addEventListener('keydown', onKey)
  else   window.removeEventListener('keydown', onKey)
})
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="open" class="overlay" @click.self="emit('close')">
        <div class="modal" :style="maxWidth ? { maxWidth } : {}">
          <div class="modal-header">
            <h2 class="modal-title">{{ title }}</h2>
            <button class="close-btn" @click="emit('close')">
              <AppIcon name="x" :size="18" />
            </button>
          </div>
          <div class="modal-body">
            <slot />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed; inset: 0;
  background: rgba(10,20,35,.5);
  display: flex; align-items: flex-start; justify-content: center;
  z-index: 1000;
  padding: 2rem 1rem;
  overflow-y: auto;
}
.modal {
  background: var(--bg-card);
  border-radius: 8px;
  box-shadow: var(--shadow-md);
  width: 100%; max-width: 560px;
  overflow: visible;
  /* Stays in the scroll flow — no fixed height, grows with content */
  margin: auto;
}
.modal-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 1.25rem 1.5rem;
  border-bottom: 1px solid var(--border);
  position: sticky; top: 0;
  background: var(--bg-card);
  z-index: 1;
}
.modal-title { font-size: 15px; font-weight: 700; color: var(--text); }
.close-btn {
  background: none; border: none;
  color: var(--text-muted); padding: .25rem;
  border-radius: var(--radius); line-height: 0;
}
.close-btn:hover { background: var(--bg); color: var(--text); }
.modal-body { padding: 1.5rem; }

.modal-enter-active, .modal-leave-active { transition: opacity .15s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
</style>
