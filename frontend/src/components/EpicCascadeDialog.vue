<script setup lang="ts">
import { watch } from 'vue'
import AppModal from '@/components/AppModal.vue'

const props = defineProps<{
  open: boolean
  childCount: number
  pendingStatus: string
}>()

const emit = defineEmits<{
  confirm: [cascade: boolean]
  close: []
}>()

function onCascadeKey(e: KeyboardEvent) {
  const tag = (e.target as HTMLElement)?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
  if (e.key.toLowerCase() === 'o') { emit('confirm', false); e.preventDefault() }
  if (e.key.toLowerCase() === 'c') { emit('close'); e.preventDefault() }
}

watch(() => props.open, (v) => {
  if (v) window.addEventListener('keydown', onCascadeKey)
  else   window.removeEventListener('keydown', onCascadeKey)
})
</script>

<template>
  <AppModal title="Cascade Status" :open="open" @close="emit('close')" confirm-key="m" @confirm="emit('confirm', true)">
    <p style="margin-bottom: .75rem">
      <strong>{{ childCount }}</strong> child issue{{ childCount !== 1 ? 's are' : ' is' }} still open.
      Move {{ childCount !== 1 ? 'them all' : 'it' }} to <strong>{{ pendingStatus }}</strong> as well?
    </p>
    <div class="form-actions">
      <button class="btn btn-ghost btn-sm" @click="emit('close')"><u>C</u>ancel</button>
      <button class="btn btn-ghost btn-sm" @click="emit('confirm', false)"><u>O</u>nly This</button>
      <button class="btn btn-primary btn-sm" @click="emit('confirm', true)"><u>M</u>ove All</button>
    </div>
  </AppModal>
</template>

<style scoped>
.form-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }
</style>
