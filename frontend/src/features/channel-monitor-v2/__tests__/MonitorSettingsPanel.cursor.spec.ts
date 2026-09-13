import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MonitorSettingsPanel from '../MonitorSettingsPanel.vue'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), updateConfig: vi.fn(), error: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {}, showError: mocks.error, showSuccess: vi.fn() }) }))
vi.mock('@/utils/featureFlags', () => ({ getChannelMonitorMode: () => 'v2', isChannelMonitorV2Mode: () => true }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAllIncludingInactive: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/channelMonitorV2', () => ({ getConfig: mocks.getConfig, updateConfig: mocks.updateConfig, MONITOR_ERROR_CATEGORIES: [] }))

describe('Cursor monitor dimension option', () => {
  beforeEach(() => { vi.clearAllMocks() })
  it.each([false, true])('retains an existing Cursor selection (%s) and makes missing Cursor selectable', async (existing) => {
    mocks.getConfig.mockResolvedValue({ version: 2, enabled: true, refresh_interval_seconds: 60, group_ids: [], platforms: existing ? [{ platform: 'cursor', enabled: true, models: ['chosen'] }] : [{ platform: 'openai', enabled: true, models: [] }] })
    const wrapper = mount(MonitorSettingsPanel, { global: { stubs: { Icon: true, RouterLink: true } } })
    await flushPromises()
    const label = wrapper.findAll('strong').find((node) => node.text() === 'Cursor')!
    expect(label).toBeDefined()
    const row = label.element.parentElement!
    expect(row.querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe(String(existing))
    expect((row.querySelector('input') as HTMLInputElement).value).toBe(existing ? 'chosen' : '')
    expect(mocks.error).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
