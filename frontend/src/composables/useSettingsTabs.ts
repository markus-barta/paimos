import { computed, type ComputedRef } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'

import {
  resolveSettingsTab,
  SETTINGS_TABS,
  visibleSettingsTabs,
  type SettingsQueryTab,
  type SettingsTab,
  type SettingsTabDef,
} from '@/config/settingsTabs'

export function useSettingsTabs(isAdmin: ComputedRef<boolean>) {
  const route = useRoute()
  const router = useRouter()

  const tabs = computed(() => visibleSettingsTabs(isAdmin.value))
  const activeTab = computed<SettingsTab>(() =>
    resolveSettingsTab(route.query.tab as SettingsQueryTab | undefined),
  )
  const activeTabDef = computed<SettingsTabDef>(() =>
    SETTINGS_TABS.find((tab) => tab.id === activeTab.value) ?? SETTINGS_TABS[0],
  )
  const activeTabProps = computed<Record<string, unknown>>(() =>
    activeTab.value === 'appearance' ? { isAdmin: isAdmin.value } : {},
  )

  function setTab(tab: SettingsTab) {
    const next: LocationQueryRaw = { ...route.query, tab }
    if (tab !== 'appearance') delete next.section
    router.replace({ query: next })
  }

  return {
    tabs,
    activeTab,
    activeTabDef,
    activeTabProps,
    setTab,
  }
}
