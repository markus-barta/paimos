<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-57. Customer list — flat card grid; same shape and density as
 ProjectsView so the two pages feel like siblings. The "Add Customer"
 affordance is a split button: primary action is always manual create
 (the no-CRM path, audience #1); the dropdown lights up only when the
 admin has at least one provider enabled + configured.
-->
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useExternalProvider } from '@/composables/useExternalProvider'
import AppFooter from '@/components/AppFooter.vue'
import AppIcon from '@/components/AppIcon.vue'
import ProviderBadge from '@/components/customer/ProviderBadge.vue'
import CustomerCreateModal from '@/components/customer/CustomerCreateModal.vue'
import CustomerImportModal from '@/components/customer/CustomerImportModal.vue'
import type { Customer, ExternalProvider } from '@/types'

const auth = useAuthStore()
const router = useRouter()
const isAdmin = computed(() => auth.user?.role === 'admin')

const customers = ref<Customer[]>([])
const loading = ref(true)
const loadError = ref('')
const search = ref('')

const showAddMenu = ref(false)
const showCreate = ref(false)
const showImport = ref(false)
const importProvider = ref<ExternalProvider | null>(null)

const { enabledProviders } = useExternalProvider()

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return customers.value
  return customers.value.filter((c) =>
    c.name.toLowerCase().includes(q)
    || c.industry.toLowerCase().includes(q)
    || c.contact_name.toLowerCase().includes(q)
    || c.contact_email.toLowerCase().includes(q),
  )
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    customers.value = await api.get<Customer[]>('/customers')
  } catch (e: unknown) {
    loadError.value = errMsg(e, 'Failed to load customers.')
  } finally {
    loading.value = false
  }
}
onMounted(load)

function openCreate() { showAddMenu.value = false; showCreate.value = true }
function openImport(p: ExternalProvider) {
  showAddMenu.value = false
  importProvider.value = p
  showImport.value = true
}

function onCreated(c: Customer) {
  showCreate.value = false
  customers.value.unshift(c)
  router.push(`/customers/${c.id}`)
}
function onImported(id: number) {
  showImport.value = false
  router.push(`/customers/${id}`)
}

function fmtRate(v: number | null | undefined): string {
  if (v == null) return '—'
  return `€${v.toFixed(0)}`
}
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span class="ah-title">Customers</span>
  </Teleport>

  <div class="cv-toolbar">
    <div class="cv-search">
      <AppIcon name="search" :size="14" />
      <input v-model="search" type="text" placeholder="Search customers, industry, contact…" />
    </div>

    <div v-if="isAdmin" class="cv-add-wrapper">
      <button class="btn btn-primary cv-add" @click="openCreate">
        <AppIcon name="plus" :size="14" />
        New customer
      </button>
      <button
        v-if="enabledProviders.length"
        class="btn btn-primary cv-add-caret"
        :title="`Import from a configured CRM`"
        @click="showAddMenu = !showAddMenu"
      >
        <AppIcon name="chevron-down" :size="14" />
      </button>
      <div v-if="showAddMenu && enabledProviders.length" class="cv-add-menu" @click.self="showAddMenu = false">
        <div class="cv-add-menu-label">Import from</div>
        <button
          v-for="p in enabledProviders"
          :key="p.id"
          class="cv-add-menu-item"
          @click="openImport(p)"
        >
          <img v-if="p.logo_url" :src="p.logo_url" :alt="p.name" class="cv-add-menu-logo" />
          <AppIcon v-else name="globe" :size="14" />
          <span>{{ p.name }}</span>
        </button>
      </div>
    </div>
  </div>

  <div v-if="loading" class="cv-loading">Loading customers…</div>
  <div v-else-if="loadError" class="cv-error">{{ loadError }}</div>

  <div v-else-if="customers.length === 0" class="cv-empty">
    <AppIcon name="building-2" :size="32" />
    <h2>No customers yet.</h2>
    <p>
      Add one manually below — or, if you've configured a CRM provider in
      Settings → Integrations → CRM, import directly from there.
    </p>
    <button v-if="isAdmin" class="btn btn-primary" @click="openCreate">
      <AppIcon name="plus" :size="14" /> Create first customer
    </button>
  </div>

  <div v-else-if="filtered.length === 0" class="cv-empty cv-empty--filtered">
    <p>No customers match "<strong>{{ search }}</strong>".</p>
  </div>

  <div v-else class="cv-grid">
    <RouterLink
      v-for="c in filtered"
      :key="c.id"
      :to="`/customers/${c.id}`"
      class="cv-card"
    >
      <div class="cv-card-top">
        <h3 class="cv-card-name">{{ c.name }}</h3>
        <ProviderBadge
          :provider-id="c.external_provider"
          :external-url="c.external_url"
          variant="compact"
        />
      </div>

      <div v-if="c.industry" class="cv-card-industry">{{ c.industry }}</div>

      <div v-if="c.contact_name || c.contact_email" class="cv-card-contact">
        <AppIcon name="user" :size="12" />
        <span>{{ c.contact_name || c.contact_email }}</span>
      </div>

      <div class="cv-card-stats">
        <div class="cv-stat">
          <span class="cv-stat-value">{{ c.project_count ?? 0 }}</span>
          <span class="cv-stat-label">{{ (c.project_count ?? 0) === 1 ? 'project' : 'projects' }}</span>
        </div>
        <div class="cv-stat" :class="{ 'cv-stat--muted': c.rate_hourly == null }">
          <span class="cv-stat-value">{{ fmtRate(c.rate_hourly) }}</span>
          <span class="cv-stat-label">/h</span>
        </div>
        <div class="cv-stat" :class="{ 'cv-stat--muted': c.rate_lp == null }">
          <span class="cv-stat-value">{{ fmtRate(c.rate_lp) }}</span>
          <span class="cv-stat-label">/LP</span>
        </div>
      </div>
    </RouterLink>
  </div>

  <AppFooter />

  <CustomerCreateModal :open="showCreate" @close="showCreate = false" @created="onCreated" />
  <CustomerImportModal
    :open="showImport"
    :provider="importProvider"
    @close="showImport = false"
    @imported="onImported"
  />
</template>

<style scoped>
.cv-toolbar {
  display: flex; align-items: center; gap: 1rem;
  margin-bottom: 1.25rem;
}
.cv-search {
  flex: 1;
  display: flex; align-items: center; gap: .5rem;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: var(--radius); padding: .35rem .65rem;
  color: var(--text-muted);
  max-width: 480px;
  transition: border-color .15s, box-shadow .15s;
}
.cv-search:focus-within {
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px rgba(46,109,164,.15);
}
.cv-search input {
  flex: 1; border: none; outline: none; background: transparent;
  padding: 0; font-size: 13px;
}
.cv-search input:focus { box-shadow: none; }

.cv-add-wrapper { position: relative; display: flex; }
.cv-add { border-top-right-radius: 0; border-bottom-right-radius: 0; }
.cv-add-caret {
  border-top-left-radius: 0; border-bottom-left-radius: 0;
  border-left: 1px solid var(--bp-blue-dark);
  padding: .45rem .55rem;
}
.cv-add-menu {
  position: absolute; top: calc(100% + .25rem); right: 0; z-index: 20;
  min-width: 200px;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: var(--radius); box-shadow: var(--shadow-md);
  padding: .35rem;
  display: flex; flex-direction: column; gap: .1rem;
}
.cv-add-menu-label {
  font-size: 10px; text-transform: uppercase; font-weight: 700;
  color: var(--text-muted); letter-spacing: .07em;
  padding: .35rem .5rem .15rem;
}
.cv-add-menu-item {
  display: flex; align-items: center; gap: .5rem;
  padding: .4rem .55rem; background: none; border: none;
  border-radius: var(--radius); font-size: 13px;
  color: var(--text); cursor: pointer; text-align: left;
}
.cv-add-menu-item:hover { background: var(--bp-blue-pale); color: var(--bp-blue-dark); }
.cv-add-menu-logo { width: 16px; height: 16px; object-fit: contain; }

.cv-loading { padding: 2rem; color: var(--text-muted); text-align: center; }
.cv-error {
  background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca;
  padding: .75rem 1rem; border-radius: var(--radius); font-size: 13px;
}

.cv-empty {
  display: flex; flex-direction: column; align-items: center; gap: .65rem;
  padding: 4rem 1.5rem; color: var(--text-muted); text-align: center;
}
.cv-empty h2 { font-size: 16px; color: var(--text); margin: .25rem 0 0; font-weight: 700; }
.cv-empty p { max-width: 420px; line-height: 1.55; margin: 0 0 .5rem; }
.cv-empty--filtered { padding: 2.5rem 1rem; }

.cv-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 1rem;
}
.cv-card {
  display: flex; flex-direction: column; gap: .55rem;
  padding: 1.1rem 1.2rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow);
  text-decoration: none;
  color: var(--text);
  transition: background .18s ease, box-shadow .18s ease, border-color .18s ease, transform .18s ease;
}
.cv-card:hover {
  background: #f4f7ff;
  border-color: #c5d5f5;
  box-shadow: 0 4px 18px rgba(46, 109, 200, .12);
  transform: translateY(-1px);
}

.cv-card-top {
  display: flex; align-items: flex-start; justify-content: space-between; gap: .5rem;
}
.cv-card-name {
  font-size: 15px; font-weight: 700; color: var(--text);
  margin: 0; letter-spacing: -.01em;
  overflow: hidden; text-overflow: ellipsis;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical;
}

.cv-card-industry {
  font-size: 11px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .07em;
}

.cv-card-contact {
  display: flex; align-items: center; gap: .35rem;
  font-size: 12px; color: var(--text-muted);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.cv-card-stats {
  display: flex; gap: 1.4rem;
  margin-top: auto;
  padding-top: .65rem;
  border-top: 1px dashed var(--border);
}
.cv-stat {
  display: flex; flex-direction: column; gap: .05rem;
  font-variant-numeric: tabular-nums;
}
.cv-stat-value { font-size: 15px; font-weight: 700; color: var(--text); line-height: 1; }
.cv-stat-label { font-size: 10px; color: var(--text-muted); text-transform: uppercase; letter-spacing: .06em; }
.cv-stat--muted .cv-stat-value { color: var(--text-muted); font-weight: 500; }
</style>
