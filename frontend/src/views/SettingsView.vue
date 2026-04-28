<script setup lang="ts">
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useSettingsTabs } from '@/composables/useSettingsTabs'

const auth    = useAuthStore()
const isAdmin = computed(() => auth.user?.role === 'admin')
const { tabs, activeTab, activeTabDef, activeTabProps, setTab } = useSettingsTabs(isAdmin)
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span class="ah-title">Settings</span>
  </Teleport>

  <!-- Tab bar -->
  <div class="tab-bar">
    <button
      v-for="t in tabs" :key="t.id"
      :class="['tab-btn', { active: activeTab === t.id }]"
      @click="setTab(t.id)"
    >{{ t.label }}</button>
  </div>

  <!-- Tab content — each component self-initialises on mount -->
  <div class="tab-content">
    <component :is="activeTabDef.component" v-bind="activeTabProps" />
  </div>
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
