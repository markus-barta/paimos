<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, onMounted } from 'vue'
import { api } from '@/api/client'

// Capability row from GET /api/permissions/matrix.
interface Capability {
  key: string
  label: string
  description: string
  viewer: boolean
  editor: boolean
  admin: boolean
}

interface MatrixResponse {
  levels: string[]
  capabilities: Capability[]
}

const loading = ref(false)
const capabilities = ref<Capability[]>([])
const levels = ref<string[]>(['viewer', 'editor', 'admin'])

async function load() {
  loading.value = true
  try {
    const resp = await api.get<MatrixResponse>('/permissions/matrix')
    capabilities.value = resp.capabilities
    if (resp.levels?.length) levels.value = resp.levels
  } catch { /* ignore */ }
  finally { loading.value = false }
}

onMounted(load)

function allowed(cap: Capability, level: string): boolean {
  if (level === 'viewer') return cap.viewer
  if (level === 'editor') return cap.editor
  if (level === 'admin') return cap.admin
  return false
}
</script>

<template>
  <div class="section">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">Permissions</h2>
        <p class="section-desc">
          What each access level can do within a project. This page is
          read-only — grants are managed on the Users tab.
        </p>
      </div>
    </div>

    <LoadingText v-if="loading" class="empty" label="Loading…" />
    <div v-else class="card" style="padding:0;overflow:hidden">
      <table class="settings-table perm-matrix">
        <thead>
          <tr>
            <th>Capability</th>
            <th v-for="lvl in levels" :key="lvl" class="lvl">{{ lvl }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="cap in capabilities" :key="cap.key">
            <td>
              <div class="cap-label">{{ cap.label }}</div>
              <div class="cap-desc">{{ cap.description }}</div>
              <div class="cap-key">{{ cap.key }}</div>
            </td>
            <td v-for="lvl in levels" :key="lvl" class="lvl">
              <span v-if="allowed(cap, lvl)" class="yes" aria-label="allowed">●</span>
              <span v-else class="no" aria-label="denied">·</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.section-title { font-size: 18px; font-weight: 700; }
.section-desc  { color: var(--text-muted); font-size: 13px; margin-top: .2rem; }
.section-header-row { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 1rem; }
.empty { color: var(--text-muted); font-size: 13px; padding: 1rem 0; }
.perm-matrix th, .perm-matrix td { padding: .6rem .9rem; border-bottom: 1px solid var(--border); vertical-align: top; }
.perm-matrix th.lvl, .perm-matrix td.lvl { text-align: center; text-transform: capitalize; width: 7rem; }
.cap-label { font-weight: 600; font-size: 14px; }
.cap-desc  { font-size: 12px; color: var(--text-muted); margin-top: .15rem; }
.cap-key   { font-family: monospace; font-size: 11px; color: var(--text-muted); margin-top: .2rem; }
.yes { color: var(--bp-green, #0a7a35); font-weight: 700; font-size: 16px; }
.no  { color: var(--text-muted); font-size: 14px; }
</style>
