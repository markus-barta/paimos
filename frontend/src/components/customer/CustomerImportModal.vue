<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.
-->

<!--
 CustomerImportModal — provider-driven customer import (PAI-103). Pasted
 reference can be a URL or a bare external id; the provider decides what
 to accept. Provider name surfaces from useExternalProvider so this file
 mentions no specific CRM by name.
-->
<script setup lang="ts">
import { ref, watch } from 'vue'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'
import { api, errMsg } from '@/api/client'
import type { ExternalProvider } from '@/types'

const props = defineProps<{ open: boolean; provider: ExternalProvider | null }>()
const emit = defineEmits<{ close: []; imported: [customerId: number] }>()

const ref_ = ref('')
const error = ref('')
const importing = ref(false)

watch(() => props.open, (open) => {
  if (open) {
    ref_.value = ''
    error.value = ''
  }
})

async function submit() {
  if (!props.provider) return
  if (!ref_.value.trim()) { error.value = 'Paste a reference (URL or id) to import.'; return }
  error.value = ''
  importing.value = true
  try {
    const res = await api.post<{ id: number }>('/customers/import', {
      provider: props.provider.id,
      ref: ref_.value.trim(),
    })
    emit('imported', res.id)
  } catch (e: unknown) {
    error.value = errMsg(e, 'Import failed.')
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <AppModal
    :title="provider ? `Import from ${provider.name}` : 'Import customer'"
    :open="open"
    @close="emit('close')"
    confirm-key="i"
    @confirm="submit"
  >
    <div v-if="provider" class="import-body">
      <div class="provider-pill">
        <img v-if="provider.logo_url" :src="provider.logo_url" :alt="provider.name" class="pp-logo" />
        <AppIcon v-else name="globe" :size="16" />
        <span>{{ provider.name }}</span>
      </div>

      <p class="import-hint">
        Paste a {{ provider.name }} URL or a bare external id. PAIMOS will
        fetch the matching record and create a customer with the linked
        provider id stored.
      </p>

      <form @submit.prevent="submit" class="form">
        <div class="field">
          <label>Reference</label>
          <input
            v-model="ref_"
            type="text"
            :placeholder="`e.g. https://app.${provider.id}.com/…/company/12345 or 12345`"
            autofocus
          />
        </div>

        <p v-if="error" class="form-error">{{ error }}</p>

        <div class="form-actions">
          <button type="button" class="btn btn-ghost" @click="emit('close')"><u>C</u>ancel</button>
          <button type="submit" class="btn btn-primary" :disabled="importing">
            <span v-if="importing"><AppIcon name="refresh-cw" :size="14" class="spinning" /> Importing…</span>
            <span v-else>Import</span>
          </button>
        </div>
      </form>
    </div>

    <div v-else class="form-error">No provider selected.</div>
  </AppModal>
</template>

<style scoped>
.import-body { display: flex; flex-direction: column; gap: 1rem; }
.provider-pill {
  display: inline-flex; align-items: center; gap: .5rem;
  padding: .35rem .7rem;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  border: 1px solid var(--bp-blue-light); border-radius: 999px;
  font-size: 12px; font-weight: 600;
  align-self: flex-start;
}
.pp-logo { width: 14px; height: 14px; object-fit: contain; }
.import-hint { font-size: 13px; color: var(--text-muted); margin: 0; line-height: 1.5; }

.form { display: flex; flex-direction: column; gap: .85rem; }
.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.form-error { color: #b91c1c; font-size: 13px; margin: 0; }
.form-actions { display: flex; justify-content: flex-end; gap: .5rem; padding-top: .25rem; }
.spinning { animation: spin 1s linear infinite; vertical-align: middle; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
</style>
