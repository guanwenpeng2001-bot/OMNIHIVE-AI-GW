import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MonitorSettingsPanel from '../MonitorSettingsPanel.vue'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), updateConfig: vi.fn(), error: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {}, showError: mocks.error, showSuccess: vi.fn() }) }))
vi.mock('@/utils/featureFlags', () => ({ getChannelMonitorMode: () => 'v2', isChannelTelemetryMode: () => true }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAllIncludingInactive: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/channelTelemetry', () => ({ getConfig: mocks.getConfig, updateConfig: mocks.updateConfig, MONITOR_ERROR_CATEGORIES: [] }))
vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: vi.fn().mockResolvedValue([]) }))

describe('Cursor monitor dimension option', () => {
  beforeEach(() => { vi.clearAllMocks() })
  it('shows migrated models disabled and saves an explicit passive selection', async () => {
    const migratedConfig = {
      version: 11, enabled: false, refresh_interval_seconds: 300, group_ids: [],
      platforms: [{ platform: 'cursor', enabled: false, models: ['legacy-model', 'template-model'] }],
    }
    mocks.getConfig.mockResolvedValue(migratedConfig)
    mocks.updateConfig.mockImplementation(async (config) => ({ ...JSON.parse(JSON.stringify(config)), version: 12 }))
    const wrapper = mount(MonitorSettingsPanel, { global: { stubs: { Icon: true, RouterLink: true } } })
    await flushPromises()
    const cursorLabel = wrapper.findAll('strong').find((node) => node.text() === 'Cursor')!
    const cursorRow = cursorLabel.element.parentElement!
    const cursorSwitch = wrapper.findAll('[role="switch"]').find((node) => node.element.parentElement === cursorRow)!
    expect(cursorSwitch.attributes('aria-checked')).toBe('false')
    expect((cursorRow.querySelector('input') as HTMLInputElement).value).toBe('legacy-model, template-model')
    await cursorSwitch.trigger('click')
    await wrapper.find('header button').trigger('click')
    await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      version: 11, enabled: false,
      platforms: expect.arrayContaining([{ platform: 'cursor', enabled: true, models: ['legacy-model', 'template-model'] }]),
    }))
    expect(mocks.error).not.toHaveBeenCalled()
    wrapper.unmount()
  })
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
