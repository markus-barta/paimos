<script setup lang="ts">
import { computed, onUnmounted, watch } from 'vue'
import { useConfirm } from '@/composables/useConfirm'

const { visible, options, resolve } = useConfirm()

const confirmLabel = computed(() => options.value.confirmLabel ?? 'Confirm')
const cancelLabel = computed(() => options.value.cancelLabel ?? 'Cancel')
const confirmChar = computed(() => confirmLabel.value[0]?.toLowerCase() ?? '')
const cancelChar = computed(() => cancelLabel.value[0]?.toLowerCase() ?? '')

function onKeydown(e: KeyboardEvent) {
  if (!visible.value) return
  if (e.key === 'Escape') { resolve(false); e.preventDefault(); return }
  if (e.key === 'Enter')  { resolve(true);  e.preventDefault(); return }
  const tag = (e.target as HTMLElement)?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
  const k = e.key.toLowerCase()
  if (confirmChar.value && k === confirmChar.value && confirmChar.value !== cancelChar.value) { resolve(true); e.preventDefault() }
  if (cancelChar.value && k === cancelChar.value && confirmChar.value !== cancelChar.value) { resolve(false); e.preventDefault() }
}

watch(visible, (v) => {
  if (v) window.addEventListener('keydown', onKeydown)
  else   window.removeEventListener('keydown', onKeydown)
})
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <Transition name="confirm-fade">
      <div v-if="visible" class="confirm-overlay" @click.self="resolve(false)">
        <div class="confirm-dialog">
          <div v-if="options.title" class="confirm-title">{{ options.title }}</div>
          <div class="confirm-message">{{ options.message }}</div>
          <div class="confirm-actions">
            <button class="btn btn-ghost btn-sm" @click="resolve(false)">
              <u>{{ cancelLabel[0] }}</u>{{ cancelLabel.slice(1) }}
            </button>
            <button
              :class="['btn btn-sm', options.danger ? 'btn-danger' : 'btn-primary']"
              @click="resolve(true)"
            >
              <u>{{ confirmLabel[0] }}</u>{{ confirmLabel.slice(1) }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.confirm-overlay {
  position: fixed; inset: 0;
  background: rgba(10,20,35,.5);
  display: flex; align-items: center; justify-content: center;
  z-index: 10000;
  padding: 1rem;
}
.confirm-dialog {
  background: var(--bg-card);
  border-radius: 8px;
  box-shadow: var(--shadow-md);
  padding: 1.5rem;
  max-width: 400px; width: 100%;
}
.confirm-title {
  font-size: 15px; font-weight: 700; color: var(--text);
  margin-bottom: .5rem;
}
.confirm-message {
  font-size: 14px; color: var(--text); line-height: 1.5;
  margin-bottom: 1.25rem;
}
.confirm-actions {
  display: flex; justify-content: flex-end; gap: .5rem;
}
.btn-danger {
  background: #dc2626; color: #fff; border: 1px solid #dc2626;
}
.btn-danger:hover { background: #b91c1c; border-color: #b91c1c; }

.confirm-fade-enter-active, .confirm-fade-leave-active { transition: opacity .12s; }
.confirm-fade-enter-from, .confirm-fade-leave-to { opacity: 0; }
</style>
