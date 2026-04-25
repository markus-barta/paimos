<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AppFooter from '@/components/AppFooter.vue'
import SettingsAccountTab from '@/components/settings/SettingsAccountTab.vue'
import SettingsUsersTab from '@/components/settings/SettingsUsersTab.vue'
import SettingsTagsTab from '@/components/settings/SettingsTagsTab.vue'
import SettingsAppearanceTab from '@/components/settings/SettingsAppearanceTab.vue'
import SettingsViewsTab from '@/components/settings/SettingsViewsTab.vue'
import SettingsSprintsTab from '@/components/settings/SettingsSprintsTab.vue'
import SettingsDevelopmentTab from '@/components/settings/SettingsDevelopmentTab.vue'
import SettingsTrashTab from '@/components/settings/SettingsTrashTab.vue'
import SettingsPermissionsTab from '@/components/settings/SettingsPermissionsTab.vue'
import SettingsCRMTab from '@/components/settings/SettingsCRMTab.vue'
import SettingsAITab from '@/components/settings/SettingsAITab.vue'

const auth    = useAuthStore()
const isAdmin = computed(() => auth.user?.role === 'admin')
const route   = useRoute()
const router  = useRouter()

// ── Tabs ──────────────────────────────────────────────────────────────────────
type Tab = 'account' | 'tags' | 'appearance' | 'users' | 'permissions' | 'sprints' | 'views' | 'crm' | 'ai' | 'development' | 'trash'
type QueryTab = Tab | 'branding'

const ALL_TABS: { id: Tab; label: string; adminOnly?: boolean }[] = [
  { id: 'account',      label: 'Account' },
  { id: 'tags',         label: 'Tags' },
  { id: 'appearance',   label: 'Visual' },
  { id: 'users',        label: 'Users',        adminOnly: true },
  { id: 'permissions',  label: 'Permissions',  adminOnly: true },
  { id: 'sprints',      label: 'Sprints',      adminOnly: true },
  { id: 'views',        label: 'Views',        adminOnly: true },
  { id: 'crm',          label: 'CRM',          adminOnly: true },
  { id: 'ai',           label: 'AI',           adminOnly: true },
  { id: 'development',  label: 'Development',  adminOnly: true },
  { id: 'trash',        label: 'Trash',        adminOnly: true },
]

const visibleTabs = computed(() =>
  ALL_TABS.filter(t => !t.adminOnly || isAdmin.value)
)

const activeTab = computed<Tab>(() => {
  const q = route.query.tab as QueryTab | undefined
  if (q === 'branding') return 'appearance'
  const valid = ALL_TABS.map(t => t.id)
  return valid.includes(q as Tab) ? (q as Tab) : 'account'
})

function setTab(tab: Tab) {
  const next: LocationQueryRaw = { ...route.query, tab }
  if (tab !== 'appearance') delete next.section
  router.replace({ query: next })
}
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span class="ah-title">Settings</span>
  </Teleport>

  <!-- Tab bar -->
  <div class="tab-bar">
    <button
      v-for="t in visibleTabs" :key="t.id"
      :class="['tab-btn', { active: activeTab === t.id }]"
      @click="setTab(t.id)"
    >{{ t.label }}</button>
  </div>

  <!-- Tab content — each component self-initialises on mount -->
  <div class="tab-content">
    <SettingsAccountTab     v-if="activeTab === 'account'" />
    <SettingsUsersTab       v-else-if="activeTab === 'users' && isAdmin" />
    <SettingsPermissionsTab v-else-if="activeTab === 'permissions' && isAdmin" />
    <SettingsTagsTab        v-else-if="activeTab === 'tags'" />
    <SettingsAppearanceTab  v-else-if="activeTab === 'appearance'" :is-admin="isAdmin" />
    <SettingsViewsTab       v-else-if="activeTab === 'views' && isAdmin" />
    <SettingsSprintsTab     v-else-if="activeTab === 'sprints' && isAdmin" />
    <SettingsCRMTab         v-else-if="activeTab === 'crm' && isAdmin" />
    <SettingsAITab          v-else-if="activeTab === 'ai' && isAdmin" />
    <SettingsDevelopmentTab v-else-if="activeTab === 'development' && isAdmin" />
    <SettingsTrashTab       v-else-if="activeTab === 'trash' && isAdmin" />
  </div>

  <AppFooter />
</template>

<style scoped>
/* ── Tab bar ─────────────────────────────────────────────────────────────── */
.tab-bar {
  display: flex; gap: 0; margin-bottom: 1.75rem;
  border-bottom: 2px solid var(--border);
}
.tab-btn {
  background: none; border: none; border-bottom: 2px solid transparent;
  margin-bottom: -2px; padding: .55rem 1.1rem;
  font-size: 13px; font-weight: 500; color: var(--text-muted);
  cursor: pointer; transition: color .15s, border-color .15s;
  border-radius: var(--radius) var(--radius) 0 0;
}
.tab-btn:hover { color: var(--text); }
.tab-btn.active { color: var(--bp-blue-dark); border-bottom-color: var(--bp-blue); font-weight: 600; }

/* ── Layout ──────────────────────────────────────────────────────────────── */
.tab-content { display: flex; flex-direction: column; gap: 0; }
</style>
