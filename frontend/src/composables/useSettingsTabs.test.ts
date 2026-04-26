import { computed } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { resolveSettingsTab, visibleSettingsTabs } from '@/config/settingsTabs'

vi.mock('vue-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-router')>()
  return {
    ...actual,
    useRoute: () => ({ query: { tab: 'branding' } }),
    useRouter: () => ({ replace: vi.fn() }),
  }
})

describe('settings tab registry', () => {
  it('maps legacy branding query to appearance', () => {
    expect(resolveSettingsTab('branding')).toBe('appearance')
  })

  it('falls back to account on invalid query tabs', () => {
    expect(resolveSettingsTab('definitely-not-real' as never)).toBe('account')
  })

  it('filters admin tabs for non-admin users', () => {
    const tabs = visibleSettingsTabs(false)
    expect(tabs.some((tab) => tab.id === 'users')).toBe(false)
    expect(tabs.some((tab) => tab.id === 'account')).toBe(true)
  })

  it('keeps admin tabs visible for admins', () => {
    const tabs = visibleSettingsTabs(computed(() => true).value)
    expect(tabs.some((tab) => tab.id === 'users')).toBe(true)
    expect(tabs.some((tab) => tab.id === 'ai-prompts')).toBe(true)
  })
})
