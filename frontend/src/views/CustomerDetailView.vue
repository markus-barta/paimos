<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-272 redesign. Identity slab + side rail. The hero is a left-anchored
 masthead (monogram + name + meta) with a stat / sync rail on the right;
 below it an asymmetric 12-col grid splits primary content (Contacts →
 Projects → Documents) from a sticky About / Notes / Sync provenance rail.

 PAI-273 wired in: Contacts now reads from /api/customers/:id/contacts;
 Add / Edit / Delete / Promote-primary all live. About card surfaces the
 new metadata fields (website / VAT / employees / revenue / phone)
 row-by-row; rows hide when empty so callers stuck on the v1 schema get
 the same layout as today.
-->
<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import AppIcon from '@/components/AppIcon.vue'
import AppModal from '@/components/AppModal.vue'
import DocumentsSection from '@/components/customer/DocumentsSection.vue'
import { useExternalProvider } from '@/composables/useExternalProvider'
import type { Customer, Contact, Project } from '@/types'
// PAI-146 expansion: AI optimize on customer notes (CRM context).
import AiActionMenu from '@/components/ai/AiActionMenu.vue'
import AiSurfaceFeedback from '@/components/ai/AiSurfaceFeedback.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.role === 'admin')

const customerId = computed(() => Number(route.params.id))

const customer = ref<Customer | null>(null)
const contacts = ref<Contact[]>([])
const projects = ref<Project[]>([])
const loading = ref(true)
const loadError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [c, list, all] = await Promise.all([
      api.get<Customer>(`/customers/${customerId.value}`),
      api.get<Contact[]>(`/customers/${customerId.value}/contacts`),
      api.get<Project[]>('/projects'),
    ])
    customer.value = c
    contacts.value = list
    projects.value = all.filter((p) => p.customer_id === customerId.value)
  } catch (e: unknown) {
    loadError.value = errMsg(e, 'Failed to load customer.')
  } finally {
    loading.value = false
  }
}

async function reloadContacts() {
  contacts.value = await api.get<Contact[]>(`/customers/${customerId.value}/contacts`)
}

onMounted(load)
watch(customerId, load)

// ── Provider lookup (for the sync rail + sync provenance card) ──────
// useExternalProvider takes a static id; we want the lookup to track
// `customer.value` as it loads asynchronously, so we just grab the
// providers list and resolve reactively.
const { providers: providerList } = useExternalProvider()
const provider = computed(() => {
  const id = customer.value?.external_provider
  if (!id) return null
  return providerList.value.find((p) => p.id === id) ?? null
})
const providerName = computed(() => provider.value?.name ?? customer.value?.external_provider ?? '')
const providerLogo  = computed(() => provider.value?.logo_url ?? '')

// ── Identity helpers ────────────────────────────────────────────────
function initialsOf(s: string | null | undefined, fallback = '··'): string {
  if (!s) return fallback
  const parts = s.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return fallback
  const head = parts[0]?.[0] ?? ''
  const tail = parts.length > 1 ? (parts[parts.length - 1][0] ?? '') : (parts[0][1] ?? '')
  return (head + tail).toUpperCase() || fallback
}
const customerInitials = computed(() => initialsOf(customer.value?.name, '··'))

// PAI-273: contacts come from a real endpoint now. Keep the v-for in
// the template and let the empty state cover the "no contacts yet"
// case.

// Helpers for surfacing the visit address on contact rows: HubSpot
// gives us city/country at the customer level, which is more useful as
// "where to reach the company" than per-contact unless the user added
// a per-contact address (out of scope for PAI-273).
const customerLocation = computed(() => {
  const c = customer.value
  if (!c) return ''
  return [c.address, c.country].filter(Boolean).join(', ')
})

// PAI-273: format a EUR-cents value as a human-readable revenue band
// for the About card. Locked to EUR (multi-currency is out of scope).
function fmtRevenue(cents: number | null | undefined): string {
  if (cents == null) return ''
  const v = cents / 100
  if (v >= 1_000_000) return `€${(v / 1_000_000).toFixed(1)}M`
  if (v >= 1_000)     return `€${(v / 1_000).toFixed(0)}k`
  return `€${v.toFixed(0)}`
}

// ── Sync state machine ──────────────────────────────────────────────
type SyncState = 'idle' | 'loading' | 'success' | 'error'
const syncState = ref<SyncState>('idle')
const syncError = ref('')

const syncRelative = computed(() => {
  const at = customer.value?.synced_at
  if (!at) return 'Never synced'
  const ms = Date.now() - new Date(at.replace(' ', 'T') + 'Z').getTime()
  const sec = Math.floor(ms / 1000)
  if (sec < 30) return 'Synced just now'
  if (sec < 60) return `Synced ${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `Synced ${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `Synced ${hr}h ago`
  const d = Math.floor(hr / 24)
  if (d < 30) return `Synced ${d}d ago`
  return `Synced ${new Date(at).toLocaleDateString()}`
})

const syncIcon = computed<'refresh-cw' | 'check' | 'triangle-alert'>(() => {
  if (syncState.value === 'success') return 'check'
  if (syncState.value === 'error') return 'triangle-alert'
  return 'refresh-cw'
})

const syncStatusLine = computed(() => {
  if (syncState.value === 'loading') return 'Syncing…'
  if (syncState.value === 'success') return 'Sync complete'
  if (syncState.value === 'error')   return 'Sync failed'
  return syncRelative.value
})

async function doSync() {
  if (!customer.value || syncState.value === 'loading') return
  syncState.value = 'loading'
  syncError.value = ''
  try {
    await api.post(`/customers/${customer.value.id}/sync`, {})
    customer.value = await api.get<Customer>(`/customers/${customer.value.id}`)
    syncState.value = 'success'
    setTimeout(() => { if (syncState.value === 'success') syncState.value = 'idle' }, 1500)
  } catch (e: unknown) {
    syncState.value = 'error'
    syncError.value = e instanceof Error ? e.message : String(e)
  }
}

// ── Overflow menu (Edit / Delete) ───────────────────────────────────
// Mirrors the PAI-246 / PAI-265 pattern from ProjectDetailView: panel is
// teleported to <body> with position:fixed so it can't be clipped by an
// ancestor overflow:hidden. Trigger lives in the hero rail.
const overflowOpen = ref(false)
const overflowTriggerRef = ref<HTMLElement | null>(null)
const overflowPanelRef = ref<HTMLElement | null>(null)
const overflowPanelStyle = ref<{ top: string; right: string }>({ top: '0px', right: '0px' })
function recomputeOverflowPosition() {
  const el = overflowTriggerRef.value
  if (!el) return
  const r = el.getBoundingClientRect()
  overflowPanelStyle.value = {
    top: `${r.bottom + 6}px`,
    right: `${window.innerWidth - r.right}px`,
  }
}
function closeOverflow() { overflowOpen.value = false }
function toggleOverflow() {
  overflowOpen.value = !overflowOpen.value
  if (overflowOpen.value) void nextTick(recomputeOverflowPosition)
}
function onOverflowOutsideClick(e: MouseEvent) {
  if (!overflowOpen.value) return
  const target = e.target as Node
  const inTrigger = overflowTriggerRef.value?.contains(target) ?? false
  const inPanel = overflowPanelRef.value?.contains(target) ?? false
  if (!inTrigger && !inPanel) closeOverflow()
}
function onOverflowKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && overflowOpen.value) closeOverflow()
}
onMounted(() => {
  document.addEventListener('mousedown', onOverflowOutsideClick)
  document.addEventListener('keydown', onOverflowKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onOverflowOutsideClick)
  document.removeEventListener('keydown', onOverflowKey)
})

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

function onCustomerNotesAccept(text: string) {
  editForm.value.notes = text
}

async function applyCustomerAiResult(info: { action: string; intent?: string; values?: Record<string, unknown>; body?: any }) {
  if (info.intent !== 'replace-text') return
  if (info.action !== 'tone_check') return
  editForm.value.notes = String(info.values?.text ?? info.body?.optimized ?? info.body?.optimized_text ?? editForm.value.notes ?? '')
}

function openEdit() {
  if (!customer.value) return
  closeOverflow()
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
function openDelete() { closeOverflow(); showDelete.value = true }
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

// ── Contact CRUD (PAI-273) ─────────────────────────────────────────
// One modal handles both "add" and "edit"; the editing ref tells the
// rest of the page which mode the modal is in. The is_primary checkbox
// only appears in add mode — promote/demote uses the dedicated atomic
// endpoint after creation so the wire never sees two primaries.
const showContactModal = ref(false)
const editingContact = ref<Contact | null>(null)
const contactForm = ref({
  name: '', email: '', phone: '', role: '', is_primary: false, notes: '',
})
const contactError = ref('')
const contactSaving = ref(false)

function openAddContact() {
  if (!isAdmin.value) return
  editingContact.value = null
  contactForm.value = { name: '', email: '', phone: '', role: '', is_primary: contacts.value.length === 0, notes: '' }
  contactError.value = ''
  showContactModal.value = true
}
function openEditContact(c: Contact) {
  if (!isAdmin.value) return
  editingContact.value = c
  contactForm.value = {
    name: c.name, email: c.email, phone: c.phone, role: c.role,
    is_primary: c.is_primary, notes: c.notes,
  }
  contactError.value = ''
  showContactModal.value = true
}
async function saveContact() {
  if (!customer.value) return
  contactError.value = ''
  if (!contactForm.value.name.trim()) { contactError.value = 'Name required.'; return }
  contactSaving.value = true
  try {
    if (editingContact.value) {
      await api.put(`/contacts/${editingContact.value.id}`, {
        name: contactForm.value.name,
        email: contactForm.value.email,
        phone: contactForm.value.phone,
        role: contactForm.value.role,
        notes: contactForm.value.notes,
      })
    } else {
      await api.post(`/customers/${customer.value.id}/contacts`, contactForm.value)
    }
    await reloadContacts()
    showContactModal.value = false
  } catch (e: unknown) {
    contactError.value = errMsg(e, 'Save failed.')
  } finally {
    contactSaving.value = false
  }
}
async function deleteContact(c: Contact) {
  if (!isAdmin.value) return
  if (!confirm(`Delete contact "${c.name || c.email}"?`)) return
  try {
    await api.delete(`/contacts/${c.id}`)
    await reloadContacts()
  } catch (e: unknown) {
    alert(errMsg(e, 'Delete failed.'))
  }
}
async function promoteContact(c: Contact) {
  if (!isAdmin.value || c.is_primary) return
  try {
    await api.post(`/contacts/${c.id}/promote-primary`, {})
    await reloadContacts()
  } catch (e: unknown) {
    alert(errMsg(e, 'Promote failed.'))
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

  <LoadingText v-if="loading" class="cd-loading" label="Loading customer…" />
  <div v-else-if="loadError" class="cd-error">{{ loadError }}</div>
  <div v-else-if="!customer" class="cd-error">Customer not found.</div>

  <template v-else>
    <!-- ── Hero: identity slab + stat/sync rail ──────────────────── -->
    <header class="cd-hero">
      <div class="cd-hero-id">
        <div class="cd-monogram" :title="customer.name">
          <!-- 24×24 corner-mark accent — the page's signature flourish.
               Only raw SVG anywhere; everything else flows through AppIcon. -->
          <svg class="cd-corner-mark" width="24" height="24" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M0 0 L14 0 L0 14 Z" fill="currentColor" />
          </svg>
          <span class="cd-monogram-text">{{ customerInitials }}</span>
        </div>
        <div class="cd-hero-text">
          <h1 class="cd-name">{{ customer.name }}</h1>
          <div class="cd-meta">
            <span v-if="customer.industry" class="badge cd-industry">{{ customer.industry }}</span>
            <a
              v-if="customer.external_provider && customer.external_url"
              :href="customer.external_url"
              target="_blank"
              rel="noopener noreferrer"
              class="cd-provider-link"
              :title="`Open in ${providerName}`"
            >
              <img v-if="providerLogo" :src="providerLogo" :alt="providerName" class="cd-provider-logo" />
              <AppIcon v-else name="external-link" :size="13" />
              <span>{{ providerName }}<span v-if="customer.external_id"> · #{{ customer.external_id }}</span></span>
              <AppIcon name="external-link" :size="11" class="cd-provider-arrow" />
            </a>
            <span
              v-else-if="customer.external_provider"
              class="cd-provider-link cd-provider-link--static"
            >
              <AppIcon name="globe" :size="13" />
              <span>{{ providerName || customer.external_provider }}</span>
            </span>
          </div>
        </div>
      </div>

      <div class="cd-hero-rail">
        <div class="cd-stat-row">
          <div class="cd-stat-card">
            <span class="cd-stat-value">{{ fmtRate(customer.rate_hourly) }}</span>
            <span class="cd-stat-label">per hour</span>
          </div>
          <div class="cd-stat-card">
            <span class="cd-stat-value">{{ fmtRate(customer.rate_lp) }}</span>
            <span class="cd-stat-label">per LP</span>
          </div>
        </div>

        <div class="cd-sync-row">
          <template v-if="customer.external_provider">
            <span :class="['cd-sync-icon', `cd-sync-icon--${syncState}`]">
              <AppIcon
                :name="syncIcon"
                :size="14"
                :class="{ 'cd-spin': syncState === 'loading' }"
              />
            </span>
            <span class="cd-sync-text" :title="syncState === 'error' ? syncError : ''">
              {{ syncStatusLine }}
            </span>
            <button
              v-if="isAdmin"
              type="button"
              class="btn btn-ghost btn-sm cd-sync-btn"
              :disabled="syncState === 'loading'"
              :title="syncState === 'error' ? syncError : `Re-sync from ${providerName}`"
              @click="doSync"
            >
              {{ syncState === 'error' ? 'Retry' : 'Sync' }}
            </button>
          </template>
          <span v-else class="cd-sync-text cd-sync-text--manual">
            Manually managed customer — no CRM link.
          </span>

          <button
            v-if="isAdmin"
            ref="overflowTriggerRef"
            class="btn btn-ghost btn-sm icon-only cd-overflow-trigger"
            :class="{ active: overflowOpen }"
            :title="overflowOpen ? 'Close menu' : 'More customer actions'"
            @click="toggleOverflow"
          >
            <AppIcon name="more-horizontal" :size="14" />
          </button>
        </div>
      </div>
    </header>

    <!-- Overflow panel — teleported so it can't be clipped. -->
    <Teleport to="body">
      <div
        v-if="overflowOpen"
        ref="overflowPanelRef"
        class="cd-overflow-menu"
        role="menu"
        :style="overflowPanelStyle"
      >
        <button class="cd-overflow-item" @click="openEdit">
          <AppIcon name="pencil" :size="14" />
          <span>Edit customer</span>
        </button>
        <button class="cd-overflow-item cd-overflow-item--danger" @click="openDelete">
          <AppIcon name="trash-2" :size="14" />
          <span>Delete customer</span>
        </button>
      </div>
    </Teleport>

    <!-- ── Body: 8/4 grid → 7/5 → stacked ────────────────────────── -->
    <div class="cd-body">
      <!-- Primary column ─────────────────────────────────────────── -->
      <div class="cd-primary">
        <!-- Contacts (PAI-273: real multi-contact list) -->
        <section class="cd-card">
          <header class="cd-card-header">
            <h3 class="cd-card-title">
              Contacts
              <span class="cd-card-count">{{ contacts.length }}</span>
            </h3>
            <button
              v-if="isAdmin"
              class="btn btn-ghost btn-sm"
              @click="openAddContact"
            >
              <AppIcon name="plus" :size="14" /> Add contact
            </button>
          </header>

          <ul v-if="contacts.length > 0" class="cd-contact-list">
            <li v-for="c in contacts" :key="c.id" class="cd-contact-row">
              <div class="cd-contact-avatar">{{ initialsOf(c.name || c.email, '?') }}</div>
              <div class="cd-contact-body">
                <div class="cd-contact-name-row">
                  <span class="cd-contact-name">{{ c.name || 'Unnamed contact' }}</span>
                  <span v-if="c.is_primary" class="cd-contact-primary-badge" title="Primary contact">
                    <AppIcon name="star" :size="11" /> Primary
                  </span>
                </div>
                <div v-if="c.role" class="cd-contact-role">{{ c.role }}</div>
                <div v-if="c.email" class="cd-contact-line">
                  <AppIcon name="mail" :size="13" />
                  <a :href="`mailto:${c.email}`">{{ c.email }}</a>
                </div>
                <div v-if="c.phone" class="cd-contact-line">
                  <AppIcon name="phone" :size="13" />
                  <a :href="`tel:${c.phone}`">{{ c.phone }}</a>
                </div>
                <div v-if="customerLocation" class="cd-contact-line">
                  <AppIcon name="map-pin" :size="13" />
                  <span>{{ customerLocation }}</span>
                </div>
              </div>
              <div v-if="isAdmin" class="cd-contact-actions">
                <button
                  v-if="!c.is_primary"
                  class="btn btn-ghost btn-sm icon-only"
                  title="Promote to primary contact"
                  @click="promoteContact(c)"
                >
                  <AppIcon name="star" :size="13" />
                </button>
                <button
                  class="btn btn-ghost btn-sm icon-only"
                  title="Edit contact"
                  @click="openEditContact(c)"
                >
                  <AppIcon name="pencil" :size="13" />
                </button>
                <button
                  class="btn btn-ghost btn-sm icon-only cd-contact-delete"
                  title="Delete contact"
                  @click="deleteContact(c)"
                >
                  <AppIcon name="trash-2" :size="13" />
                </button>
              </div>
            </li>
          </ul>

          <div v-else class="cd-empty">
            <AppIcon name="user-plus" :size="24" />
            <p class="cd-empty-text">No contacts yet.</p>
            <button v-if="isAdmin" class="cd-empty-cta" @click="openAddContact">Add the first contact</button>
          </div>
        </section>

        <!-- Projects -->
        <section class="cd-card">
          <header class="cd-card-header">
            <h3 class="cd-card-title">
              Projects
              <span class="cd-card-count">{{ projects.length }}</span>
            </h3>
            <span class="cd-card-hint">Rates inherit unless overridden</span>
          </header>

          <div v-if="projects.length === 0" class="cd-empty">
            <AppIcon name="folder-plus" :size="24" />
            <p class="cd-empty-text">No projects yet — issues you create will inherit this customer's rates.</p>
            <RouterLink v-if="isAdmin" to="/projects?new=1" class="cd-empty-cta">New project</RouterLink>
          </div>

          <ul v-else class="cd-project-list">
            <li v-for="p in projects" :key="p.id">
              <RouterLink :to="`/projects/${p.id}`" class="cd-project-row">
                <div class="cd-project-top">
                  <span class="cd-project-key">{{ p.key }}</span>
                  <span class="cd-project-name">{{ p.name }}</span>
                  <span :class="['badge', `badge-${p.status}`]">{{ p.status }}</span>
                </div>
                <div class="cd-project-sub">
                  <span class="cd-project-meta">
                    <AppIcon name="briefcase" :size="12" />
                    {{ p.open_issue_count }} open · {{ p.issue_count }} total
                  </span>
                  <span class="cd-project-meta">
                    <AppIcon name="euro" :size="12" />
                    <template v-for="kind in (['hourly','lp'] as const)" :key="kind">
                      <span
                        :class="['cd-project-rate', { 'cd-project-rate--inherited': effectiveRate(p, kind).inherited }]"
                        :title="effectiveRate(p, kind).inherited ? `Inherited from ${customer.name}` : 'Project override'"
                      >
                        {{ fmtRate(effectiveRate(p, kind).value) }}<span class="cd-project-rate-unit">/{{ kind === 'hourly' ? 'h' : 'LP' }}</span>
                      </span>
                    </template>
                  </span>
                </div>
              </RouterLink>
            </li>
          </ul>
        </section>

        <!-- Documents (existing component) -->
        <DocumentsSection
          scope="customer"
          :scope-id="customer.id"
          :can-write="isAdmin"
        />
      </div>

      <!-- Side rail ──────────────────────────────────────────────── -->
      <aside class="cd-side">
        <!-- About: data-driven row stack. PAI-273 added website / VAT /
             employees / revenue / phone — each row hides when empty so
             a sparsely-populated customer reads cleanly and a
             fully-populated one fills in without re-layout. -->
        <section class="cd-card cd-side-card">
          <header class="cd-card-header">
            <h3 class="cd-card-title">About</h3>
          </header>
          <dl class="cd-info-list">
            <template v-if="customer.industry">
              <dt><AppIcon name="tag" :size="13" /> Industry</dt>
              <dd>{{ customer.industry }}</dd>
            </template>
            <template v-if="customer.website">
              <dt><AppIcon name="link" :size="13" /> Website</dt>
              <dd>
                <a :href="customer.website" target="_blank" rel="noopener noreferrer" class="cd-info-link">
                  {{ customer.domain || customer.website }}
                </a>
              </dd>
            </template>
            <template v-if="customer.phone">
              <dt><AppIcon name="phone" :size="13" /> Phone</dt>
              <dd><a :href="`tel:${customer.phone}`" class="cd-info-link">{{ customer.phone }}</a></dd>
            </template>
            <template v-if="customer.vat_id">
              <dt><AppIcon name="hash" :size="13" /> VAT</dt>
              <dd>{{ customer.vat_id }}</dd>
            </template>
            <template v-if="customer.employee_count != null">
              <dt><AppIcon name="users" :size="13" /> Employees</dt>
              <dd>{{ customer.employee_count.toLocaleString() }}</dd>
            </template>
            <template v-if="customer.annual_revenue_cents != null">
              <dt><AppIcon name="trending-up" :size="13" /> Revenue</dt>
              <dd>{{ fmtRevenue(customer.annual_revenue_cents) }}</dd>
            </template>
            <template v-if="customer.country">
              <dt><AppIcon name="globe" :size="13" /> Country</dt>
              <dd>{{ customer.country }}</dd>
            </template>
            <template v-if="customer.address">
              <dt><AppIcon name="map-pin" :size="13" /> Address</dt>
              <dd>{{ customer.address }}</dd>
            </template>
          </dl>
          <p
            v-if="!customer.industry && !customer.country && !customer.address
                  && !customer.website && !customer.phone && !customer.vat_id
                  && customer.employee_count == null && customer.annual_revenue_cents == null"
            class="cd-info-empty"
          >
            No customer details yet.
          </p>

          <!-- Description: paragraph-length copy lives below the row
               list to avoid breaking the dl's tabular alignment. -->
          <p v-if="customer.description" class="cd-info-description">
            {{ customer.description }}
          </p>
        </section>

        <!-- Notes: only when present. -->
        <section v-if="customer.notes" class="cd-card cd-side-card">
          <header class="cd-card-header">
            <h3 class="cd-card-title">Notes</h3>
          </header>
          <p class="cd-notes-body">{{ customer.notes }}</p>
        </section>

        <!-- Sync provenance: only for CRM-linked customers. -->
        <section v-if="customer.external_provider" class="cd-card cd-side-card">
          <header class="cd-card-header">
            <h3 class="cd-card-title">Sync</h3>
          </header>
          <div class="cd-sync-card">
            <div class="cd-sync-card-row">
              <img v-if="providerLogo" :src="providerLogo" :alt="providerName" class="cd-sync-card-logo" />
              <AppIcon v-else name="globe" :size="14" />
              <span class="cd-sync-card-name">
                {{ providerName }}<span v-if="customer.external_id"> · #{{ customer.external_id }}</span>
              </span>
            </div>
            <div class="cd-sync-card-row cd-sync-card-meta">
              <AppIcon name="refresh-cw" :size="13" />
              <span>{{ syncRelative }}</span>
            </div>
            <a
              v-if="customer.external_url"
              :href="customer.external_url"
              target="_blank"
              rel="noopener noreferrer"
              class="cd-sync-card-link"
            >
              <AppIcon name="external-link" :size="13" />
              Open in {{ providerName }}
            </a>
          </div>
        </section>
      </aside>
    </div>
  </template>

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
          <AiActionMenu surface="customer"
            host-key="customer-detail:notes"
            field="customer_notes"
            field-label="Customer notes"
            :issue-id="0"
            :text="() => editForm.notes"
            :on-accept="onCustomerNotesAccept"
          />
        </div>
        <AiSurfaceFeedback host-key="customer-detail:notes" :apply="applyCustomerAiResult" />
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

  <!-- ── Contact add/edit modal (PAI-273) ───────────────────────── -->
  <AppModal
    :title="editingContact ? 'Edit contact' : 'Add contact'"
    :open="showContactModal"
    @close="showContactModal = false"
    confirm-key="s"
    @confirm="saveContact"
  >
    <form @submit.prevent="saveContact" class="cd-form">
      <div class="cd-form-field">
        <label>Name</label>
        <input v-model="contactForm.name" type="text" required autofocus />
      </div>
      <div class="cd-form-field">
        <label>Role (Ansprechpartner-Funktion)</label>
        <input v-model="contactForm.role" type="text" placeholder="e.g. Geschäftsführung, Buchhaltung, Tech" />
      </div>
      <div class="cd-form-grid">
        <div class="cd-form-field"><label>Email</label><input v-model="contactForm.email" type="email" /></div>
        <div class="cd-form-field"><label>Phone</label><input v-model="contactForm.phone" type="tel" /></div>
      </div>
      <div class="cd-form-field">
        <label>Notes</label>
        <textarea v-model="contactForm.notes" rows="2" />
      </div>
      <div v-if="!editingContact" class="cd-form-field cd-form-checkbox">
        <label>
          <input v-model="contactForm.is_primary" type="checkbox" />
          Primary contact
          <span class="cd-form-hint">Demotes the existing primary, if any.</span>
        </label>
      </div>
      <p v-if="contactError" class="cd-form-error">{{ contactError }}</p>
      <div class="cd-form-actions">
        <button type="button" class="btn btn-ghost" @click="showContactModal = false"><u>C</u>ancel</button>
        <button type="submit" class="btn btn-primary" :disabled="contactSaving">
          {{ contactSaving ? 'Saving…' : (editingContact ? 'Save changes' : 'Add contact') }}
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
</template>

<style scoped>
/* ── Loading / error states ─────────────────────────────────────── */
.cd-loading { padding: 2rem; color: var(--text-muted); text-align: center; }
.cd-error {
  background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca;
  padding: .75rem 1rem; border-radius: var(--radius); font-size: 13px;
}

/* ── Hero (identity slab + stat/sync rail) ──────────────────────── */
.cd-hero {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 1.5rem;
  align-items: stretch;
  padding: 1.25rem 1.4rem;
  margin-bottom: 1.25rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.cd-hero-id {
  display: flex; align-items: center; gap: 1rem;
  min-width: 0;
}

/* Monogram tile + corner-mark accent — the page's signature visual. */
.cd-monogram {
  position: relative;
  width: 56px; height: 56px;
  flex-shrink: 0;
  background: var(--bp-blue-pale);
  color: var(--bp-blue);
  border-radius: var(--radius);
  display: flex; align-items: center; justify-content: center;
  font-family: 'DM Sans', system-ui, sans-serif;
  font-weight: 600;
  font-size: 24px;
  letter-spacing: .02em;
  user-select: none;
  overflow: hidden;
}
.cd-corner-mark {
  position: absolute; top: 0; left: 0;
  color: var(--bp-blue);
  pointer-events: none;
}
.cd-monogram-text { position: relative; z-index: 1; }

.cd-hero-text { min-width: 0; display: flex; flex-direction: column; gap: .35rem; }
.cd-name {
  font-size: 1.5rem; font-weight: 600; color: var(--text);
  margin: 0; letter-spacing: -.02em; line-height: 1.15;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.cd-meta {
  display: flex; align-items: center; gap: .65rem; flex-wrap: wrap;
  font-size: 12px;
}
.cd-industry {
  max-width: 14rem;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.cd-provider-link {
  display: inline-flex; align-items: center; gap: .35rem;
  color: var(--text-muted); text-decoration: none;
  font-weight: 500;
  transition: color .15s;
}
.cd-provider-link:hover { color: var(--bp-blue-dark); }
.cd-provider-link--static { cursor: default; }
.cd-provider-link--static:hover { color: var(--text-muted); }
.cd-provider-logo {
  width: 14px; height: 14px;
  object-fit: contain;
  filter: grayscale(1) opacity(.7);
}
.cd-provider-link:hover .cd-provider-logo { filter: none; }
.cd-provider-arrow { opacity: .55; }
.cd-provider-link:hover .cd-provider-arrow { opacity: 1; }

/* Stat / sync rail (right side of hero). */
.cd-hero-rail {
  display: flex; flex-direction: column; gap: .65rem;
  align-items: flex-end;
}
.cd-stat-row { display: flex; gap: .5rem; }
.cd-stat-card {
  display: flex; flex-direction: column;
  padding: .55rem .9rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  min-width: 100px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.cd-stat-value {
  font-size: 1.5rem; font-weight: 600; color: var(--text);
  font-family: 'DM Mono', monospace;
  line-height: 1.1;
}
.cd-stat-label {
  font-size: 0.75rem; color: var(--text-muted);
  letter-spacing: .03em;
  margin-top: .1rem;
}

.cd-sync-row {
  display: flex; align-items: center; gap: .5rem;
  font-size: 12px;
}
.cd-sync-icon {
  display: inline-flex; align-items: center;
  color: var(--text-muted);
  transition: color .2s;
}
.cd-sync-icon--success { color: #15803d; }
.cd-sync-icon--error   { color: #b91c1c; }
.cd-sync-text {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.cd-sync-text--manual { font-style: italic; }
.cd-sync-btn { padding-left: .55rem; padding-right: .55rem; }
.cd-overflow-trigger { margin-left: .35rem; }
.cd-overflow-trigger.active { background: var(--bg); color: var(--text); }
.cd-spin { animation: cd-spin 1s linear infinite; }
@keyframes cd-spin {
  from { transform: rotate(0deg); }
  to   { transform: rotate(360deg); }
}

/* ── Overflow menu (teleported to body) ─────────────────────────── */
.cd-overflow-menu {
  position: fixed; z-index: 60;
  min-width: 180px;
  padding: .25rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 6px 20px rgba(15, 23, 42, .08);
  display: flex; flex-direction: column; gap: 1px;
}
.cd-overflow-item {
  display: flex; align-items: center; gap: .55rem;
  padding: .45rem .6rem;
  font-size: 12.5px; color: var(--text);
  background: transparent; border: none; border-radius: 6px;
  cursor: pointer; text-align: left; font-family: inherit;
  white-space: nowrap;
}
.cd-overflow-item:hover:not(:disabled) { background: var(--bg); }
.cd-overflow-item :deep(svg) { color: var(--text-muted); flex-shrink: 0; }
.cd-overflow-item--danger:hover { background: #fef2f2; color: #b91c1c; }
.cd-overflow-item--danger:hover :deep(svg) { color: #b91c1c; }

/* ── Body grid: 8/4 → 7/5 → stacked ─────────────────────────────── */
.cd-body {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 1.25rem;
  align-items: start;
}
@media (min-width: 768px) and (max-width: 1023px) {
  .cd-body { grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr); }
}
@media (min-width: 1024px) {
  .cd-body { grid-template-columns: minmax(0, 2fr) minmax(0, 1fr); }
}
.cd-primary { display: flex; flex-direction: column; gap: 1.25rem; min-width: 0; }
.cd-side    { display: flex; flex-direction: column; gap: 1.25rem; min-width: 0; }
@media (min-width: 1024px) {
  .cd-side { position: sticky; top: 1rem; }
}

/* ── Card baseline ──────────────────────────────────────────────── */
.cd-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.1rem 1.3rem;
  box-shadow: var(--shadow);
  display: flex; flex-direction: column; gap: .85rem;
}
.cd-card-header {
  display: flex; align-items: center; justify-content: space-between;
  gap: .75rem;
}
.cd-card-title {
  font-size: 0.75rem; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .04em; margin: 0;
  display: inline-flex; align-items: center; gap: .5rem;
}
.cd-card-count {
  font-size: 11px; font-weight: 600; color: var(--text-muted);
  background: var(--bg); border: 1px solid var(--border);
  padding: .05rem .45rem; border-radius: 999px;
  letter-spacing: 0;
}
.cd-card-hint {
  font-size: 11.5px; color: var(--text-muted);
  letter-spacing: 0;
  text-transform: none;
  font-weight: 400;
}

/* ── Empty states (dashed box, centered icon + copy + CTA) ──────── */
.cd-empty {
  display: flex; flex-direction: column; align-items: center;
  gap: .55rem;
  padding: 1.5rem 1rem;
  border: 1px dashed var(--border);
  border-radius: 8px;
  color: var(--text-muted);
  text-align: center;
}
.cd-empty :deep(svg) { color: var(--text-muted); }
.cd-empty-text { font-size: 13px; margin: 0; max-width: 32ch; line-height: 1.45; }
.cd-empty-cta {
  font-size: 13px; font-weight: 500;
  color: var(--bp-blue); text-decoration: none;
  background: none; border: none; padding: 0; cursor: pointer;
  font-family: inherit;
}
.cd-empty-cta:hover { color: var(--bp-blue-dark); text-decoration: underline; }

/* ── Contacts list ──────────────────────────────────────────────── */
.cd-contact-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: .65rem; }
.cd-contact-row {
  display: grid;
  grid-template-columns: auto 1fr auto;
  gap: .85rem;
  align-items: start;
  padding: .15rem 0;
}
.cd-contact-row + .cd-contact-row { border-top: 1px solid var(--border); padding-top: .85rem; }
.cd-contact-avatar {
  width: 36px; height: 36px;
  border-radius: 50%;
  background: var(--bp-blue-pale); color: var(--bp-blue);
  display: inline-flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 600;
  font-family: 'DM Sans', system-ui, sans-serif;
  user-select: none;
}
.cd-contact-body { display: flex; flex-direction: column; gap: .15rem; min-width: 0; }
.cd-contact-name-row {
  display: flex; align-items: center; gap: .5rem; flex-wrap: wrap;
}
.cd-contact-name { font-size: 13.5px; font-weight: 600; color: var(--text); }
.cd-contact-primary-badge {
  display: inline-flex; align-items: center; gap: .2rem;
  font-size: 10px; font-weight: 600;
  padding: .1rem .4rem; border-radius: 999px;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  letter-spacing: .03em; text-transform: uppercase;
}
.cd-contact-primary-badge :deep(svg) { color: var(--bp-blue); }
.cd-contact-role {
  font-size: 11.5px; color: var(--text-muted);
  font-style: italic;
  letter-spacing: .01em;
}
.cd-contact-line {
  display: inline-flex; align-items: center; gap: .35rem;
  font-size: 12.5px; color: var(--text-muted);
}
.cd-contact-line :deep(svg) { color: var(--text-muted); flex-shrink: 0; }
.cd-contact-line a { color: var(--text-muted); text-decoration: none; }
.cd-contact-line a:hover { color: var(--bp-blue-dark); text-decoration: underline; }
.cd-contact-actions {
  display: flex; gap: .15rem; align-items: center;
  opacity: 0; transition: opacity .15s;
}
.cd-contact-row:hover .cd-contact-actions { opacity: 1; }
.cd-contact-delete:hover { color: #b91c1c; }

/* ── Projects list ──────────────────────────────────────────────── */
.cd-project-list {
  list-style: none; padding: 0; margin: 0;
  display: flex; flex-direction: column; gap: .55rem;
}
.cd-project-row {
  display: flex; flex-direction: column; gap: .35rem;
  padding: .75rem .9rem;
  border: 1px solid var(--border);
  border-radius: 8px;
  text-decoration: none;
  background: var(--bg-card);
  transition: border-color .15s, background .15s, box-shadow .15s;
}
.cd-project-row:hover {
  border-color: var(--bp-blue-light);
  background: #f4f7ff;
  box-shadow: var(--shadow-md);
}
.cd-project-top {
  display: flex; align-items: center; gap: .55rem; min-width: 0;
}
.cd-project-key {
  font-size: 10px; font-weight: 700; letter-spacing: .07em;
  font-family: 'DM Mono', monospace;
  background: var(--bp-blue); color: #fff;
  padding: .15rem .45rem; border-radius: 4px;
  flex-shrink: 0;
}
.cd-project-name {
  font-size: 13.5px; font-weight: 600; color: var(--text);
  flex: 1; min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.cd-project-sub {
  display: flex; align-items: center; gap: 1rem; flex-wrap: wrap;
  font-size: 12px; color: var(--text-muted);
}
.cd-project-meta { display: inline-flex; align-items: center; gap: .35rem; }
.cd-project-meta :deep(svg) { color: var(--text-muted); }
.cd-project-rate {
  display: inline-flex; align-items: baseline; gap: .1rem;
  font-family: 'DM Mono', monospace;
  font-variant-numeric: tabular-nums;
  color: var(--text);
}
.cd-project-rate + .cd-project-rate { margin-left: .55rem; }
.cd-project-rate-unit { font-size: 10px; color: var(--text-muted); margin-left: 1px; }
.cd-project-rate--inherited { color: var(--text-muted); }

/* ── Side rail cards ────────────────────────────────────────────── */
.cd-side-card { padding: 1rem 1.15rem; }
.cd-info-list {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: .55rem .85rem;
  margin: 0;
  font-size: 13px;
}
.cd-info-list dt {
  display: inline-flex; align-items: center; gap: .4rem;
  color: var(--text-muted); font-weight: 500;
}
.cd-info-list dt :deep(svg) { color: var(--text-muted); }
.cd-info-list dd { margin: 0; color: var(--text); word-break: break-word; }
.cd-info-empty { font-size: 13px; color: var(--text-muted); margin: 0; font-style: italic; }
.cd-info-link { color: var(--bp-blue); text-decoration: none; }
.cd-info-link:hover { color: var(--bp-blue-dark); text-decoration: underline; }
.cd-info-description {
  font-size: 13px; line-height: 1.55; color: var(--text-muted);
  white-space: pre-wrap; margin: .5rem 0 0;
  padding-top: .65rem; border-top: 1px solid var(--border);
}
.cd-notes-body { font-size: 13px; line-height: 1.55; color: var(--text); white-space: pre-wrap; margin: 0; }

.cd-sync-card { display: flex; flex-direction: column; gap: .5rem; font-size: 13px; }
.cd-sync-card-row { display: flex; align-items: center; gap: .45rem; color: var(--text); }
.cd-sync-card-meta { color: var(--text-muted); }
.cd-sync-card-row :deep(svg) { color: var(--text-muted); flex-shrink: 0; }
.cd-sync-card-logo { width: 14px; height: 14px; object-fit: contain; }
.cd-sync-card-name { font-weight: 500; }
.cd-sync-card-link {
  display: inline-flex; align-items: center; gap: .35rem;
  font-size: 12.5px;
  color: var(--bp-blue); text-decoration: none;
  margin-top: .1rem;
}
.cd-sync-card-link:hover { color: var(--bp-blue-dark); text-decoration: underline; }

/* ── Mobile (<768): hero collapses ──────────────────────────────── */
@media (max-width: 767px) {
  .cd-hero {
    grid-template-columns: 1fr;
    gap: 1rem;
  }
  .cd-hero-rail { align-items: stretch; }
  .cd-stat-row { width: 100%; }
  .cd-stat-card { flex: 1; min-width: 0; }
  .cd-sync-row { flex-wrap: wrap; }
}

/* ── Form (modal) ───────────────────────────────────────────────── */
.cd-field-label-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
  margin-bottom: .25rem;
}
.cd-field-label-row > label { margin-bottom: 0; }
.cd-form { display: flex; flex-direction: column; gap: .85rem; }
.cd-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: .85rem; }
@media (max-width: 480px) { .cd-form-grid { grid-template-columns: 1fr; } }
.cd-form-field { display: flex; flex-direction: column; gap: .35rem; }
.cd-form-field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.cd-form-error { color: #b91c1c; font-size: 13px; margin: 0; }
.cd-form-actions { display: flex; justify-content: flex-end; gap: .5rem; padding-top: .25rem; }
.cd-delete-warn { display: block; margin-top: .5rem; color: #b45309; font-size: 12px; }
.cd-form-checkbox label {
  display: flex; align-items: center; gap: .5rem;
  font-size: 13px; font-weight: 500; color: var(--text);
  text-transform: none; letter-spacing: 0;
  cursor: pointer;
}
.cd-form-hint { font-size: 11.5px; color: var(--text-muted); font-weight: 400; margin-left: .25rem; }

/* ── Header crumb ───────────────────────────────────────────────── */
.ah-crumb { color: var(--text-muted); font-weight: 500; }
.ah-crumb:hover { color: var(--text); }
.ah-sep { color: var(--text-muted); margin: 0 .35rem; }
</style>
