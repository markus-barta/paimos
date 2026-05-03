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
import LoadingText from "@/components/LoadingText.vue";
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useExternalProvider } from '@/composables/useExternalProvider'
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

// ── PAI-266: remote CRM search fan-out ───────────────────────────────
// Triggered when local filter has 0 hits and the field has been idle
// 300ms with at least 2 chars entered. Renders below the search input
// as an overlay; never displaces local card matches above. State is
// inline here rather than in a separate composable because nothing
// else in the app needs it.
type RemoteHit = {
  external_id: string
  name: string
  industry?: string
  address?: string
  external_url?: string
  already_imported?: boolean
  local_customer_id?: number
}
type RemoteProviderResult = {
  id: string
  name: string
  logo_url: string
  hits: RemoteHit[]
  error?: string
}

const remoteResults = ref<RemoteProviderResult[]>([])
const remoteLoading = ref(false)
const remoteError = ref('')
const confirmingHit = ref<{ providerId: string; externalId: string } | null>(null)
const importingHit = ref<{ providerId: string; externalId: string } | null>(null)

let remoteDebounceTimer: ReturnType<typeof setTimeout> | null = null
// Sequence counter — most-recent-wins. Each runRemoteSearch captures
// its seq; if a newer call has bumped it by the time the response
// resolves, the stale result is discarded. Cheaper than wiring an
// AbortController through the api wrapper.
let remoteSeq = 0

const showRemoteDropdown = computed(() => {
  if (search.value.trim().length < 2) return false
  if (filtered.value.length > 0) return false
  if (!enabledProviders.value.length) return false
  return remoteLoading.value || remoteError.value !== '' || remoteResults.value.length > 0
})

function clearRemote() {
  remoteResults.value = []
  remoteError.value = ''
  remoteLoading.value = false
  confirmingHit.value = null
}

async function runRemoteSearch(q: string) {
  const seq = ++remoteSeq
  remoteLoading.value = true
  remoteError.value = ''
  try {
    const url = `/integrations/crm/search?q=${encodeURIComponent(q)}&limit=10`
    const res = await api.get<{ providers: RemoteProviderResult[] }>(url)
    if (seq !== remoteSeq) return
    remoteResults.value = Array.isArray(res?.providers) ? res.providers : []
  } catch (e: unknown) {
    if (seq !== remoteSeq) return
    remoteError.value = errMsg(e, 'Search failed.')
    remoteResults.value = []
  } finally {
    if (seq === remoteSeq) remoteLoading.value = false
  }
}

watch([search, filtered], () => {
  if (remoteDebounceTimer) {
    clearTimeout(remoteDebounceTimer)
    remoteDebounceTimer = null
  }
  confirmingHit.value = null
  const q = search.value.trim()
  // Fast path: hide remote results entirely when local has matches or
  // the query is too short to be meaningful upstream.
  if (q.length < 2 || filtered.value.length > 0 || !enabledProviders.value.length) {
    remoteSeq++ // invalidate any in-flight response
    clearRemote()
    return
  }
  remoteDebounceTimer = setTimeout(() => { void runRemoteSearch(q) }, 300)
})

async function importRemoteHit(providerId: string, externalId: string) {
  importingHit.value = { providerId, externalId }
  try {
    const ref_ = externalId
    const res = await api.post<{ id: number }>('/customers/import', { provider: providerId, ref: ref_ })
    router.push(`/customers/${res.id}`)
  } catch (e: unknown) {
    remoteError.value = errMsg(e, 'Import failed.')
  } finally {
    importingHit.value = null
    confirmingHit.value = null
  }
}

function isConfirming(providerId: string, externalId: string): boolean {
  return confirmingHit.value?.providerId === providerId && confirmingHit.value?.externalId === externalId
}
function isImporting(providerId: string, externalId: string): boolean {
  return importingHit.value?.providerId === providerId && importingHit.value?.externalId === externalId
}

// Click-outside closes the remote dropdown so it doesn't linger when
// the user shifts focus elsewhere on the page.
const searchWrapRef = ref<HTMLElement | null>(null)
function onDocumentMouseDown(e: MouseEvent) {
  if (!showRemoteDropdown.value) return
  const t = e.target as Node
  if (searchWrapRef.value && !searchWrapRef.value.contains(t)) {
    clearRemote()
  }
}
onMounted(() => document.addEventListener('mousedown', onDocumentMouseDown))
onUnmounted(() => {
  document.removeEventListener('mousedown', onDocumentMouseDown)
  if (remoteDebounceTimer) clearTimeout(remoteDebounceTimer)
  remoteSeq++ // invalidate any in-flight response
})

// PAI-266: "Advanced: paste URL/ID" affordance in the dropdown's empty
// state. Picks the first enabled provider; admin can pick a different
// one via the Add menu split-button if they have several configured.
function openAdvancedPaste() {
  const p = enabledProviders.value[0]
  if (!p) return
  importProvider.value = p
  showImport.value = true
  clearRemote()
}

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
    <div ref="searchWrapRef" class="cv-search-wrap">
      <div class="cv-search">
        <AppIcon name="search" :size="14" />
        <input v-model="search" type="text" placeholder="Search customers, industry, contact…" />
        <button
          v-if="search"
          type="button"
          class="cv-search-clear"
          title="Clear search"
          @click="search = ''"
        ><AppIcon name="x" :size="13" /></button>
      </div>
      <!-- PAI-266: remote CRM fan-out dropdown. Shows under the search
           input ONLY when local results are empty + query has 2+ chars. -->
      <div v-if="showRemoteDropdown" class="cv-remote">
        <div v-if="remoteLoading && !remoteResults.length" class="cv-remote-status">
          <LoadingText label="Searching CRM providers…" />
        </div>
        <div v-if="remoteError" class="cv-remote-status cv-remote-status--error">
          <AppIcon name="alert-triangle" :size="13" />
          <span>{{ remoteError }}</span>
        </div>
        <div v-for="g in remoteResults" :key="g.id" class="cv-remote-group">
          <div class="cv-remote-group-head">
            <img v-if="g.logo_url" :src="g.logo_url" :alt="g.name" class="cv-remote-logo" />
            <AppIcon v-else name="globe" :size="14" />
            <span>From {{ g.name }}</span>
            <span v-if="remoteLoading" class="cv-remote-group-loading">
              <AppIcon name="refresh-cw" :size="11" class="spinning" />
            </span>
          </div>
          <div v-if="g.error" class="cv-remote-group-error">
            <AppIcon name="alert-triangle" :size="12" />
            <span>{{ g.error }}</span>
          </div>
          <div v-else-if="!g.hits.length" class="cv-remote-group-empty">No matches.</div>
          <ul v-else class="cv-remote-hits">
            <li
              v-for="h in g.hits"
              :key="h.external_id"
              class="cv-remote-hit"
              :class="{ 'cv-remote-hit--imported': h.already_imported }"
            >
              <div class="cv-remote-hit-main">
                <div class="cv-remote-hit-name">{{ h.name }}</div>
                <div v-if="h.industry || h.address" class="cv-remote-hit-meta">
                  <span v-if="h.industry">{{ h.industry }}</span>
                  <span v-if="h.industry && h.address" class="cv-remote-hit-sep">·</span>
                  <span v-if="h.address">{{ h.address }}</span>
                </div>
              </div>
              <div class="cv-remote-hit-actions">
                <RouterLink
                  v-if="h.already_imported && h.local_customer_id"
                  :to="`/customers/${h.local_customer_id}`"
                  class="btn btn-ghost btn-sm"
                  @click="clearRemote()"
                >Open in PAIMOS</RouterLink>
                <template v-else-if="isConfirming(g.id, h.external_id)">
                  <span class="cv-remote-confirm">Import {{ h.name }}?</span>
                  <button
                    type="button"
                    class="btn btn-primary btn-sm"
                    :disabled="isImporting(g.id, h.external_id)"
                    @click="importRemoteHit(g.id, h.external_id)"
                  >
                    <AppIcon v-if="isImporting(g.id, h.external_id)" name="refresh-cw" :size="13" class="spinning" />
                    <span v-else>Yes, import</span>
                  </button>
                  <button
                    type="button"
                    class="btn btn-ghost btn-sm"
                    :disabled="isImporting(g.id, h.external_id)"
                    @click="confirmingHit = null"
                  >Cancel</button>
                </template>
                <button
                  v-else
                  type="button"
                  class="btn btn-ghost btn-sm"
                  @click="confirmingHit = { providerId: g.id, externalId: h.external_id }"
                >Import</button>
              </div>
            </li>
          </ul>
        </div>
        <!-- Empty state across all providers — surface the legacy paste flow
             as a small affordance instead of the primary action. -->
        <div
          v-if="!remoteLoading && !remoteError && remoteResults.every((g) => !g.hits.length && !g.error) && enabledProviders.length"
          class="cv-remote-paste"
        >
          <span>No matches in connected CRMs.</span>
          <button type="button" class="cv-remote-paste-link" @click="openAdvancedPaste">
            Have a {{ enabledProviders[0].name }} URL or ID? Paste it →
          </button>
        </div>
      </div>
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

  <LoadingText v-if="loading" class="cv-loading" label="Loading customers…" />
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
.cv-search-clear {
  background: none; border: none; padding: 2px; cursor: pointer;
  color: var(--text-muted); display: inline-flex; align-items: center;
  border-radius: 3px;
}
.cv-search-clear:hover { color: var(--text); background: var(--bg); }

/* PAI-266: search-wrap is the positioning context for the remote dropdown.
   The wrap takes the same flex slot the bare .cv-search used to occupy. */
.cv-search-wrap { position: relative; flex: 1; max-width: 480px; }
.cv-search-wrap > .cv-search { max-width: none; width: 100%; }

.cv-remote {
  position: absolute; top: calc(100% + .35rem); left: 0; right: 0;
  z-index: 30;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-md);
  padding: .35rem;
  display: flex; flex-direction: column; gap: .25rem;
  max-height: 60vh; overflow-y: auto;
}
.cv-remote-status {
  display: flex; align-items: center; gap: .45rem;
  padding: .55rem .65rem; font-size: 12.5px; color: var(--text-muted);
}
.cv-remote-status--error { color: #b91c1c; }
.spinning { animation: cv-spin 1s linear infinite; }
@keyframes cv-spin { to { transform: rotate(360deg); } }

.cv-remote-group { display: flex; flex-direction: column; gap: .15rem; }
.cv-remote-group + .cv-remote-group {
  border-top: 1px solid var(--border);
  padding-top: .35rem;
  margin-top: .15rem;
}
.cv-remote-group-head {
  display: flex; align-items: center; gap: .45rem;
  padding: .35rem .55rem .15rem;
  font-size: 11px; font-weight: 700; letter-spacing: .04em;
  text-transform: uppercase; color: var(--text-muted);
}
.cv-remote-logo { width: 14px; height: 14px; object-fit: contain; }
.cv-remote-group-loading { margin-left: auto; color: var(--text-muted); }
.cv-remote-group-error {
  display: flex; align-items: center; gap: .4rem;
  padding: .35rem .65rem; font-size: 12px; color: #b91c1c;
}
.cv-remote-group-empty {
  padding: .35rem .65rem; font-size: 12.5px; color: var(--text-muted);
  font-style: italic;
}

.cv-remote-hits { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 1px; }
.cv-remote-hit {
  display: flex; align-items: center; gap: .5rem;
  padding: .4rem .65rem; border-radius: var(--radius);
}
.cv-remote-hit:hover { background: var(--bp-blue-pale); }
.cv-remote-hit--imported { opacity: .75; }
.cv-remote-hit--imported:hover { background: var(--bg); opacity: 1; }
.cv-remote-hit-main { flex: 1; min-width: 0; }
.cv-remote-hit-name {
  font-size: 13px; font-weight: 500; color: var(--text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.cv-remote-hit-meta {
  font-size: 11.5px; color: var(--text-muted);
  display: flex; gap: .35rem;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.cv-remote-hit-sep { color: var(--border); }
.cv-remote-hit-actions {
  flex-shrink: 0; display: flex; align-items: center; gap: .35rem;
}
.cv-remote-confirm {
  font-size: 12px; color: var(--text); margin-right: .25rem;
}
.btn-sm { padding: .25rem .55rem; font-size: 12px; }

.cv-remote-paste {
  border-top: 1px solid var(--border);
  margin-top: .15rem; padding: .55rem .65rem;
  display: flex; flex-direction: column; gap: .25rem;
  font-size: 12px; color: var(--text-muted);
}
.cv-remote-paste-link {
  background: none; border: none; padding: 0;
  font: inherit; color: var(--bp-blue);
  cursor: pointer; text-align: left;
}
.cv-remote-paste-link:hover { color: var(--bp-blue-dark); text-decoration: underline; }

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
