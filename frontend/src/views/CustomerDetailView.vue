<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-58. Customer detail page. Manual customers and CRM-linked customers
 render through the same component — provider affordances are conditional,
 not a missing-state stub. Layout: sticky identity header → contact + rates
 (two-col on wide) → projects → documents.
-->
<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import AppFooter from '@/components/AppFooter.vue'
import AppIcon from '@/components/AppIcon.vue'
import AppModal from '@/components/AppModal.vue'
import ProviderBadge from '@/components/customer/ProviderBadge.vue'
import SyncButton from '@/components/customer/SyncButton.vue'
import DocumentsSection from '@/components/customer/DocumentsSection.vue'
import type { Customer, Project } from '@/types'
// PAI-146 expansion: AI optimize on customer notes (CRM context).
import AiOptimizeButton from '@/components/ai/AiOptimizeButton.vue'
import AiOptimizeOverlay from '@/components/ai/AiOptimizeOverlay.vue'
import AiOptimizeBanner from '@/components/ai/AiOptimizeBanner.vue'
import { useAiOptimize } from '@/composables/useAiOptimize'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.role === 'admin')

const customerId = computed(() => Number(route.params.id))

const customer = ref<Customer | null>(null)
const projects = ref<Project[]>([])
const loading = ref(true)
const loadError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    customer.value = await api.get<Customer>(`/customers/${customerId.value}`)
    // List the customer's projects via the existing /projects endpoint
    // and filter client-side. For 99% of installs this is fine; if a
    // tenant ever has thousands of projects we'll add a server-side
    // ?customer_id=… filter.
    const all = await api.get<Project[]>('/projects')
    projects.value = all.filter((p) => p.customer_id === customerId.value)
  } catch (e: unknown) {
    loadError.value = errMsg(e, 'Failed to load customer.')
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(customerId, load)

// ── Sync ───────────────────────────────────────────────────────────
async function doSync() {
  if (!customer.value) return
  await api.post(`/customers/${customer.value.id}/sync`, {})
  // Re-fetch so synced_at + provider-sourced fields refresh.
  customer.value = await api.get<Customer>(`/customers/${customer.value.id}`)
}

// ── Edit modal ─────────────────────────────────────────────────────
const showEdit = ref(false)
const editForm = ref({
  name: '', industry: '', contact_name: '', contact_email: '',
  address: '', country: '',
  rate_hourly: null as number | null, rate_lp: null as number | null,
  notes: '',
})
const editError = ref('')
const editSaving = ref(false)

// PAI-146 expansion: AI optimize on customer notes. CRM-tone reminder
// in the prompt enforces PII discipline and non-fabrication.
const aiOptimize = useAiOptimize()
function onCustomerNotesAccept(text: string) {
  editForm.value.notes = text
}

function openEdit() {
  if (!customer.value) return
  editForm.value = {
    name: customer.value.name,
    industry: customer.value.industry,
    contact_name: customer.value.contact_name,
    contact_email: customer.value.contact_email,
    address: customer.value.address,
    country: customer.value.country,
    rate_hourly: customer.value.rate_hourly,
    rate_lp: customer.value.rate_lp,
    notes: customer.value.notes,
  }
  editError.value = ''
  showEdit.value = true
}

async function saveEdit() {
  if (!customer.value) return
  editError.value = ''
  if (!editForm.value.name.trim()) { editError.value = 'Name required.'; return }
  editSaving.value = true
  try {
    customer.value = await api.put<Customer>(`/customers/${customer.value.id}`, editForm.value)
    showEdit.value = false
  } catch (e: unknown) {
    editError.value = errMsg(e, 'Save failed.')
  } finally {
    editSaving.value = false
  }
}

// ── Delete ─────────────────────────────────────────────────────────
const showDelete = ref(false)
const deleting = ref(false)
const deleteError = ref('')
async function doDelete() {
  if (!customer.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    await api.delete(`/customers/${customer.value.id}`)
    router.push('/customers')
  } catch (e: unknown) {
    deleteError.value = errMsg(e, 'Delete failed.')
  } finally {
    deleting.value = false
  }
}

// ── Helpers ────────────────────────────────────────────────────────
function fmtRate(v: number | null | undefined): string {
  if (v == null) return '—'
  return `€${v.toFixed(2)}`
}
function effectiveRate(p: Project, kind: 'hourly' | 'lp'): { value: number | null; inherited: boolean } {
  if (kind === 'hourly') {
    return {
      value: p.effective_rate_hourly ?? null,
      inherited: p.rate_hourly == null && p.effective_rate_hourly != null,
    }
  }
  return {
    value: p.effective_rate_lp ?? null,
    inherited: p.rate_lp == null && p.effective_rate_lp != null,
  }
}
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span v-if="customer" class="ah-title">
      <RouterLink to="/customers" class="ah-crumb">Customers</RouterLink>
      <span class="ah-sep">/</span>
      {{ customer.name }}
    </span>
  </Teleport>

  <div v-if="loading" class="cd-loading">Loading customer…</div>
  <div v-else-if="loadError" class="cd-error">{{ loadError }}</div>
  <div v-else-if="!customer" class="cd-error">Customer not found.</div>

  <template v-else>
    <!-- ── Sticky identity header ───────────────────────────────── -->
    <header class="cd-hero">
      <div class="cd-hero-id">
        <h1 class="cd-name">{{ customer.name }}</h1>
        <div class="cd-meta">
          <span v-if="customer.industry" class="cd-industry">{{ customer.industry }}</span>
          <ProviderBadge
            :provider-id="customer.external_provider"
            :external-url="customer.external_url"
            variant="full"
          />
        </div>
      </div>

      <div class="cd-hero-rates">
        <div class="cd-rate">
          <span class="cd-rate-value">{{ fmtRate(customer.rate_hourly) }}</span>
          <span class="cd-rate-unit">/h</span>
        </div>
        <span class="cd-rate-sep">·</span>
        <div class="cd-rate">
          <span class="cd-rate-value">{{ fmtRate(customer.rate_lp) }}</span>
          <span class="cd-rate-unit">/LP</span>
        </div>
      </div>

      <div class="cd-hero-actions">
        <SyncButton
          v-if="customer.external_provider"
          :provider-id="customer.external_provider"
          :synced-at="customer.synced_at"
          :on-sync="doSync"
        />
        <button v-if="isAdmin" class="btn btn-ghost btn-sm" @click="openEdit">
          <AppIcon name="pencil" :size="14" /> Edit
        </button>
        <button v-if="isAdmin" class="btn btn-ghost btn-sm cd-delete-btn" @click="showDelete = true">
          <AppIcon name="trash-2" :size="14" />
        </button>
      </div>
    </header>

    <!-- ── Contact + Address (two-col on wide) ─────────────────── -->
    <div class="cd-grid">
      <section class="cd-card">
        <h3 class="cd-card-title">Contact</h3>
        <dl class="cd-dl">
          <dt>Name</dt><dd>{{ customer.contact_name || '—' }}</dd>
          <dt>Email</dt><dd>
            <a v-if="customer.contact_email" :href="`mailto:${customer.contact_email}`">{{ customer.contact_email }}</a>
            <span v-else>—</span>
          </dd>
          <dt>Address</dt><dd>{{ customer.address || '—' }}</dd>
          <dt>Country</dt><dd>{{ customer.country || '—' }}</dd>
        </dl>
      </section>

      <section v-if="customer.notes" class="cd-card cd-notes">
        <h3 class="cd-card-title">Notes</h3>
        <p class="cd-notes-body">{{ customer.notes }}</p>
      </section>
    </div>

    <!-- ── Projects ─────────────────────────────────────────────── -->
    <section class="cd-section">
      <header class="cd-section-header">
        <h3 class="cd-section-title">
          Projects
          <span class="cd-section-count">{{ projects.length }}</span>
        </h3>
        <p class="cd-section-hint">
          Rates inherit from this customer unless overridden at the project level.
        </p>
      </header>

      <div v-if="projects.length === 0" class="cd-empty-row">
        No projects assigned to {{ customer.name }} yet.
      </div>

      <div v-else class="cd-projects">
        <RouterLink
          v-for="p in projects"
          :key="p.id"
          :to="`/projects/${p.id}`"
          class="cd-proj-card"
        >
          <div class="cd-proj-top">
            <span class="cd-proj-key">{{ p.key }}</span>
            <span :class="['badge', `badge-${p.status}`]">{{ p.status }}</span>
          </div>
          <div class="cd-proj-name">{{ p.name }}</div>
          <div class="cd-proj-rates">
            <template v-for="kind in (['hourly','lp'] as const)" :key="kind">
              <span
                :class="['cd-proj-rate', { 'cd-proj-rate--inherited': effectiveRate(p, kind).inherited }]"
                :title="effectiveRate(p, kind).inherited ? `Inherited from ${customer.name}` : 'Project override'"
              >
                {{ fmtRate(effectiveRate(p, kind).value) }}<span class="cd-proj-rate-unit">/{{ kind === 'hourly' ? 'h' : 'LP' }}</span>
                <AppIcon v-if="effectiveRate(p, kind).inherited" name="link" :size="10" class="cd-proj-rate-icon" />
              </span>
            </template>
          </div>
          <div class="cd-proj-footer">
            <span>{{ p.open_issue_count }} open</span>
            <span class="cd-proj-issues">{{ p.issue_count }} total</span>
          </div>
        </RouterLink>
      </div>
    </section>

    <!-- ── Documents ────────────────────────────────────────────── -->
    <DocumentsSection
      scope="customer"
      :scope-id="customer.id"
      :can-write="isAdmin"
    />
  </template>

  <AppFooter />

  <!-- ── Edit modal ─────────────────────────────────────────────── -->
  <AppModal title="Edit customer" :open="showEdit" @close="showEdit = false" confirm-key="s" @confirm="saveEdit">
    <form @submit.prevent="saveEdit" class="cd-form">
      <div class="cd-form-field">
        <label>Name</label>
        <input v-model="editForm.name" type="text" required autofocus />
      </div>
      <div class="cd-form-grid">
        <div class="cd-form-field"><label>Industry</label><input v-model="editForm.industry" type="text" /></div>
        <div class="cd-form-field"><label>Country</label><input v-model="editForm.country" type="text" /></div>
      </div>
      <div class="cd-form-grid">
        <div class="cd-form-field"><label>Contact name</label><input v-model="editForm.contact_name" type="text" /></div>
        <div class="cd-form-field"><label>Contact email</label><input v-model="editForm.contact_email" type="email" /></div>
      </div>
      <div class="cd-form-field"><label>Address</label><input v-model="editForm.address" type="text" /></div>
      <div class="cd-form-grid">
        <div class="cd-form-field"><label>Hourly rate (€/h)</label><input v-model.number="editForm.rate_hourly" type="number" step="0.01" /></div>
        <div class="cd-form-field"><label>LP rate (€/LP)</label><input v-model.number="editForm.rate_lp" type="number" step="0.01" /></div>
      </div>
      <div class="cd-form-field">
        <div class="cd-field-label-row">
          <label>Notes</label>
          <AiOptimizeButton
            field="customer_notes"
            field-label="Customer notes"
            :issue-id="0"
            :text="() => editForm.notes"
            :on-accept="onCustomerNotesAccept"
          />
        </div>
        <AiOptimizeBanner />
        <textarea v-model="editForm.notes" rows="3" />
      </div>

      <p v-if="editError" class="cd-form-error">{{ editError }}</p>
      <div class="cd-form-actions">
        <button type="button" class="btn btn-ghost" @click="showEdit = false"><u>C</u>ancel</button>
        <button type="submit" class="btn btn-primary" :disabled="editSaving">
          {{ editSaving ? 'Saving…' : 'Save changes' }}
        </button>
      </div>
    </form>
  </AppModal>

  <!-- ── Delete confirm ─────────────────────────────────────────── -->
  <AppModal title="Delete customer" :open="showDelete" @close="showDelete = false" confirm-key="d" @confirm="doDelete">
    <p style="margin-bottom:1.25rem;font-size:14px">
      Delete <strong>{{ customer?.name }}</strong>? This cannot be undone.
      <span v-if="(customer?.project_count ?? 0) > 0" class="cd-delete-warn">
        This customer has {{ customer?.project_count }} assigned project(s) — reassign or archive them first.
      </span>
    </p>
    <p v-if="deleteError" class="cd-form-error">{{ deleteError }}</p>
    <div class="cd-form-actions">
      <button class="btn btn-ghost" @click="showDelete = false"><u>C</u>ancel</button>
      <button
        class="btn btn-danger"
        :disabled="deleting || (customer?.project_count ?? 0) > 0"
        @click="doDelete"
      >
        {{ deleting ? 'Deleting…' : 'Delete' }}
      </button>
    </div>
  </AppModal>

  <!-- PAI-146 expansion: AI optimize overlay for customer notes. -->
  <AiOptimizeOverlay
    v-if="aiOptimize.overlay.visible"
    :original="aiOptimize.overlay.original"
    :optimized="aiOptimize.overlay.optimized"
    :field-label="aiOptimize.overlay.fieldLabel"
    :model-name="aiOptimize.overlay.modelName"
    :retrying="aiOptimize.overlay.retrying"
    @accept="aiOptimize.accept()"
    @reject="aiOptimize.reject()"
    @retry="aiOptimize.retry()"
  />
</template>

<style scoped>
/* PAI-146: per-field label row holds the label + the AI optimize
   button on the right. Namespaced to .cd-field-label-row to avoid
   colliding with the project-detail-view rule of the same purpose. */
.cd-field-label-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
  margin-bottom: .25rem;
}
.cd-field-label-row > label { margin-bottom: 0; }

.cd-loading { padding: 2rem; color: var(--text-muted); text-align: center; }
.cd-error {
  background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca;
  padding: .75rem 1rem; border-radius: var(--radius); font-size: 13px;
}

/* ── Hero ───────────────────────────────────────────────────────── */
.cd-hero {
  position: sticky; top: 0; z-index: 5;
  display: grid;
  grid-template-columns: 1fr auto auto;
  gap: 1.5rem;
  align-items: center;
  padding: 1.1rem 1.4rem;
  margin-bottom: 1.25rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow);
}
.cd-hero-id { min-width: 0; }
.cd-name {
  font-size: 22px; font-weight: 700; color: var(--text);
  margin: 0 0 .35rem; letter-spacing: -.02em;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.cd-meta { display: flex; align-items: center; gap: .5rem; flex-wrap: wrap; }
.cd-industry {
  font-size: 11px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .07em;
}

.cd-hero-rates {
  display: flex; align-items: baseline; gap: .35rem;
  font-variant-numeric: tabular-nums;
}
.cd-rate { display: flex; align-items: baseline; gap: .15rem; }
.cd-rate-value {
  font-size: 18px; font-weight: 700; color: var(--text);
  font-family: 'DM Mono', monospace;
}
.cd-rate-unit { font-size: 11px; color: var(--text-muted); font-weight: 600; }
.cd-rate-sep { color: var(--text-muted); margin: 0 .35rem; }

.cd-hero-actions { display: flex; align-items: center; gap: .5rem; }
.cd-delete-btn { color: var(--text-muted); }
.cd-delete-btn:hover { color: #b91c1c; border-color: #fecaca; }

/* ── Two-col contact/notes ─────────────────────────────────────── */
.cd-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 1rem;
  margin-bottom: 1.25rem;
}
@media (min-width: 880px) {
  .cd-grid { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
}

.cd-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 1.1rem 1.3rem;
  display: flex; flex-direction: column; gap: .75rem;
}
.cd-card-title {
  font-size: 11px; font-weight: 700; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .08em; margin: 0;
}
.cd-dl {
  display: grid;
  grid-template-columns: 90px 1fr;
  gap: .5rem 1rem;
  font-size: 13px;
  margin: 0;
}
.cd-dl dt { color: var(--text-muted); font-weight: 500; }
.cd-dl dd { margin: 0; color: var(--text); }
.cd-notes-body { font-size: 13px; line-height: 1.55; color: var(--text); white-space: pre-wrap; margin: 0; }

/* ── Projects section ──────────────────────────────────────────── */
.cd-section { margin-bottom: 1.25rem; }
.cd-section-header { margin-bottom: .65rem; }
.cd-section-title {
  font-size: 14px; font-weight: 700; color: var(--text);
  margin: 0; letter-spacing: -.01em;
  display: inline-flex; align-items: baseline; gap: .5rem;
}
.cd-section-count {
  font-size: 11px; font-weight: 600; color: var(--text-muted);
  background: var(--bg); border: 1px solid var(--border);
  padding: .05rem .45rem; border-radius: 999px;
}
.cd-section-hint { font-size: 12px; color: var(--text-muted); margin: .15rem 0 0; }

.cd-empty-row {
  background: var(--bg-card); border: 1px dashed var(--border);
  padding: 1rem; border-radius: 8px;
  font-size: 13px; color: var(--text-muted); text-align: center;
}

.cd-projects {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: .75rem;
}
.cd-proj-card {
  display: flex; flex-direction: column; gap: .4rem;
  padding: .85rem 1rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  text-decoration: none; color: var(--text);
  transition: border-color .15s, background .15s;
}
.cd-proj-card:hover { border-color: var(--bp-blue-light); background: #f4f7ff; }
.cd-proj-top { display: flex; align-items: center; gap: .35rem; }
.cd-proj-key {
  font-size: 10px; font-weight: 700; letter-spacing: .07em;
  font-family: 'DM Mono', monospace;
  background: var(--bp-blue); color: #fff;
  padding: .15rem .45rem; border-radius: 4px;
}
.cd-proj-name { font-size: 13px; font-weight: 600; color: var(--text); }
.cd-proj-rates { display: flex; gap: .65rem; font-variant-numeric: tabular-nums; }
.cd-proj-rate {
  display: inline-flex; align-items: center; gap: .15rem;
  font-size: 12px; color: var(--text);
  font-family: 'DM Mono', monospace;
}
.cd-proj-rate-unit { color: var(--text-muted); font-size: 10px; margin-left: 1px; }
.cd-proj-rate-icon { color: var(--text-muted); margin-left: 2px; }
.cd-proj-rate--inherited { color: var(--text-muted); }
.cd-proj-rate--inherited .cd-proj-rate-icon { color: var(--bp-blue); }
.cd-proj-footer {
  display: flex; justify-content: space-between;
  font-size: 11px; color: var(--text-muted);
  padding-top: .35rem; border-top: 1px dashed var(--border);
  margin-top: auto;
}

/* ── Form ──────────────────────────────────────────────────────── */
.cd-form { display: flex; flex-direction: column; gap: .85rem; }
.cd-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: .85rem; }
@media (max-width: 480px) { .cd-form-grid { grid-template-columns: 1fr; } }
.cd-form-field { display: flex; flex-direction: column; gap: .35rem; }
.cd-form-field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.cd-form-error { color: #b91c1c; font-size: 13px; margin: 0; }
.cd-form-actions { display: flex; justify-content: flex-end; gap: .5rem; padding-top: .25rem; }
.cd-delete-warn { display: block; margin-top: .5rem; color: #b45309; font-size: 12px; }

/* ── Header crumb ──────────────────────────────────────────────── */
.ah-crumb { color: var(--text-muted); font-weight: 500; }
.ah-crumb:hover { color: var(--text); }
.ah-sep { color: var(--text-muted); margin: 0 .35rem; }
</style>
