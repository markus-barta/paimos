<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.
-->

<!--
 SyncButton — provider-aware re-sync, with a real state machine instead of
 the usual "click and hope". Idle → loading → success (sticky for 1.5s with
 a check icon) → idle, or → error (sticky with a tooltip). Reads the
 provider name from useExternalProvider so the label says "Sync from HubSpot"
 without mentioning HubSpot anywhere in this file.

 Also surfaces "synced N min ago" inline so the user knows whether a sync
 is even useful before clicking.
-->
<script setup lang="ts">
import { ref, computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { useExternalProvider } from '@/composables/useExternalProvider'

const props = defineProps<{
  providerId: string | null
  /** ISO/space-separated UTC stamp from `customers.synced_at`. */
  syncedAt: string | null
  /** Async sync action returning when the round-trip is complete. */
  onSync: () => Promise<void>
}>()

type State = 'idle' | 'loading' | 'success' | 'error'
const state = ref<State>('idle')
const errorMsg = ref('')

const { provider } = useExternalProvider(props.providerId)
const providerName = computed(() => provider.value?.name ?? '')

const label = computed(() => {
  if (state.value === 'loading') return 'Syncing…'
  if (state.value === 'success') return 'Synced'
  if (state.value === 'error')   return 'Retry sync'
  return providerName.value ? `Sync from ${providerName.value}` : 'Sync'
})

// Relative timestamp under the button — "synced 4 min ago" reads cleaner
// than the bare ISO and tells the user whether re-syncing is worthwhile.
const relativeSync = computed(() => {
  if (!props.syncedAt) return 'Never synced'
  const ms = Date.now() - new Date(props.syncedAt.replace(' ', 'T') + 'Z').getTime()
  const sec = Math.floor(ms / 1000)
  if (sec < 30) return 'synced just now'
  if (sec < 60) return `synced ${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `synced ${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `synced ${hr}h ago`
  const d = Math.floor(hr / 24)
  if (d < 30) return `synced ${d}d ago`
  return `synced ${new Date(props.syncedAt).toLocaleDateString()}`
})

async function trigger() {
  if (state.value === 'loading') return
  state.value = 'loading'
  errorMsg.value = ''
  try {
    await props.onSync()
    state.value = 'success'
    setTimeout(() => { if (state.value === 'success') state.value = 'idle' }, 1500)
  } catch (e: unknown) {
    state.value = 'error'
    errorMsg.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div class="sync-wrapper">
    <button
      type="button"
      :class="['btn', 'btn-ghost', 'btn-sm', 'sync-btn', `sync-btn--${state}`]"
      :disabled="state === 'loading'"
      :title="state === 'error' ? errorMsg : ''"
      @click="trigger"
    >
      <span class="sync-icon">
        <AppIcon v-if="state === 'success'" name="check" :size="14" />
        <AppIcon v-else-if="state === 'error'" name="triangle-alert" :size="14" />
        <AppIcon v-else name="refresh-cw" :size="14" :class="{ spinning: state === 'loading' }" />
      </span>
      <span>{{ label }}</span>
    </button>
    <span class="sync-rel">{{ relativeSync }}</span>
  </div>
</template>

<style scoped>
.sync-wrapper { display: inline-flex; flex-direction: column; align-items: flex-start; gap: .15rem; }
.sync-btn { transition: color .15s, border-color .15s, background .15s; }
.sync-btn--success {
  color: #15803d;
  border-color: #bbf7d0;
  background: #f0fdf4;
}
.sync-btn--error {
  color: #b91c1c;
  border-color: #fecaca;
  background: #fef2f2;
}
.sync-icon { display: inline-flex; align-items: center; }
.sync-rel {
  font-size: 11px; color: var(--text-muted); padding-left: .25rem;
  font-variant-numeric: tabular-nums;
}
.spinning { animation: spin 1s linear infinite; }
@keyframes spin {
  from { transform: rotate(0deg); }
  to   { transform: rotate(360deg); }
}
</style>
