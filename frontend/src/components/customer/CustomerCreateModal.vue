<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.
-->

<!--
 CustomerCreateModal — manual customer entry. The no-CRM path is
 first-class (PAI-28 audience #1), so this modal isn't framed as a
 fallback — it's the primary "+ New Customer" affordance.
-->
<script setup lang="ts">
import { ref, watch } from 'vue'
import AppModal from '@/components/AppModal.vue'
import { api, errMsg } from '@/api/client'
import type { Customer } from '@/types'
// PAI-146 expansion: AI optimize on customer notes.
import AiActionMenu from '@/components/ai/AiActionMenu.vue'
import AiSurfaceFeedback from '@/components/ai/AiSurfaceFeedback.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; created: [customer: Customer] }>()

const form = ref({
  name: '', industry: '', contact_name: '', contact_email: '',
  address: '', country: '',
  rate_hourly: null as number | null, rate_lp: null as number | null,
  notes: '',
})
const error = ref('')
const saving = ref(false)

// PAI-146 expansion: AI optimize on customer notes.
function onCustomerNotesAccept(text: string) {
  form.value.notes = text
}

async function applyCustomerAiResult(info: { action: string; intent?: string; values?: Record<string, unknown>; body?: any }) {
  if (info.intent !== 'replace-text') return
  if (info.action !== 'tone_check') return
  form.value.notes = String(info.values?.text ?? info.body?.optimized ?? info.body?.optimized_text ?? form.value.notes ?? '')
}

watch(() => props.open, (open) => {
  if (open) {
    form.value = { name: '', industry: '', contact_name: '', contact_email: '', address: '', country: '', rate_hourly: null, rate_lp: null, notes: '' }
    error.value = ''
  }
})

async function submit() {
  error.value = ''
  if (!form.value.name.trim()) { error.value = 'Name required.'; return }
  saving.value = true
  try {
    const c = await api.post<Customer>('/customers', form.value)
    emit('created', c)
  } catch (e: unknown) {
    error.value = errMsg(e, 'Failed to create customer.')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <AppModal title="New Customer" :open="open" @close="emit('close')" confirm-key="s" @confirm="submit">
    <form @submit.prevent="submit" class="form">
      <div class="field">
        <label>Name</label>
        <input v-model="form.name" type="text" placeholder="Acme Industries" required autofocus />
      </div>

      <div class="field-grid">
        <div class="field">
          <label>Industry <span class="label-hint">— optional</span></label>
          <input v-model="form.industry" type="text" placeholder="Manufacturing" />
        </div>
        <div class="field">
          <label>Country <span class="label-hint">— optional</span></label>
          <input v-model="form.country" type="text" placeholder="DE" />
        </div>
      </div>

      <div class="field-grid">
        <div class="field">
          <label>Primary contact</label>
          <input v-model="form.contact_name" type="text" placeholder="Jane Doe" />
        </div>
        <div class="field">
          <label>Email</label>
          <input v-model="form.contact_email" type="email" placeholder="jane@acme.com" />
        </div>
      </div>

      <div class="field">
        <label>Address <span class="label-hint">— optional</span></label>
        <input v-model="form.address" type="text" placeholder="Hauptstr. 1, 80331 München" />
      </div>

      <div class="field-grid">
        <div class="field">
          <label>Default hourly rate (€/h) <span class="label-hint">— cascades to projects</span></label>
          <input v-model.number="form.rate_hourly" type="number" step="0.01" placeholder="e.g. 120" />
        </div>
        <div class="field">
          <label>Default LP rate (€/LP) <span class="label-hint">— cascades to projects</span></label>
          <input v-model.number="form.rate_lp" type="number" step="0.01" placeholder="e.g. 80" />
        </div>
      </div>

      <div class="field">
        <div class="field-label-row">
          <label>Notes <span class="label-hint">— markdown supported</span></label>
          <AiActionMenu surface="customer"
            host-key="customer-create:notes"
            field="customer_notes"
            field-label="Customer notes"
            :issue-id="0"
            :text="() => form.notes"
            :on-accept="onCustomerNotesAccept"
          />
        </div>
        <AiSurfaceFeedback host-key="customer-create:notes" :apply="applyCustomerAiResult" />
        <textarea v-model="form.notes" rows="3" placeholder="Anything worth remembering about this customer." />
      </div>

      <p v-if="error" class="form-error">{{ error }}</p>

      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="emit('close')"><u>C</u>ancel</button>
        <button type="submit" class="btn btn-primary" :disabled="saving">
          {{ saving ? 'Creating…' : 'Create customer' }}
        </button>
      </div>
    </form>
  </AppModal>
</template>

<style scoped>
/* PAI-146: per-field label row. */
.field-label-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
}
.field-label-row > label { margin-bottom: 0; }

.form { display: flex; flex-direction: column; gap: .85rem; }
.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.label-hint { font-weight: 400; text-transform: none; letter-spacing: 0; font-size: 11px; color: var(--text-muted); }
.field-grid { display: grid; grid-template-columns: 1fr 1fr; gap: .85rem; }
@media (max-width: 480px) { .field-grid { grid-template-columns: 1fr; } }
.form-error { color: #b91c1c; font-size: 13px; margin: 0; }
.form-actions { display: flex; justify-content: flex-end; gap: .5rem; padding-top: .25rem; }
textarea { resize: vertical; min-height: 70px; }
</style>
