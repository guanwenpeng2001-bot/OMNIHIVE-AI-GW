import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MonitorSettingsPanel from '../MonitorSettingsPanel.vue'

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  getPresets: vi.fn(),
  error: vi.fn(),
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: {}, showError: mocks.error, showSuccess: vi.fn() }),
}))
vi.mock('@/utils/featureFlags', () => ({ getChannelMonitorMode: () => 'v2', isChannelMonitorV2Mode: () => true }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAllIncludingInactive: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/channelMonitorV2', () => ({
  getConfig: mocks.getConfig,
  updateConfig: mocks.updateConfig,
  MONITOR_ERROR_CATEGORIES: [],
}))
vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: mocks.getPresets }))

const baseConfig = {
  version: 3,
  enabled: true,
  refresh_interval_seconds: 60,
  group_ids: [],
  platforms: [{ platform: 'anthropic', enabled: true, models: [] }],
}

const mountPanel = () => mount(MonitorSettingsPanel, { global: { stubs: { Icon: true, RouterLink: { template: '<a><slot /></a>' } } } })

describe('MonitorSettingsPanel canonical block reasons', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getPresets.mockResolvedValue([])
  })

  it('surfaces CHANNEL_MONITOR_DISABLED with the settings entry instead of a bare toast', async () => {
    mocks.getConfig.mockRejectedValue({ status: 403, reason: 'CHANNEL_MONITOR_DISABLED', message: 'disabled', metadata: {} })
    const wrapper = mountPanel()
    await flushPromises()

    const banner = wrapper.get('[data-test="monitor-settings-blocked"]')
    expect(banner.text()).toContain('channelMonitorV2.settings.blockedDisabled')
    expect(banner.find('a').exists()).toBe(true)
    expect(mocks.error).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('surfaces CHANNEL_MONITOR_MODE_MISMATCH as its own reason', async () => {
    mocks.getConfig.mockRejectedValue({ status: 403, reason: 'CHANNEL_MONITOR_MODE_MISMATCH', message: 'mismatch', metadata: {} })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.get('[data-test="monitor-settings-blocked"]').text()).toContain(
      'channelMonitorV2.settings.blockedModeMismatch',
    )
    wrapper.unmount()
  })

  it('offers backend preset models as suggestions while keeping manual input', async () => {
    mocks.getConfig.mockResolvedValue(baseConfig)
    mocks.getPresets.mockResolvedValue([
      { platform: 'anthropic', models: [{ id: 'claude-sonnet-4-5' }, { id: 'claude-opus-4-1' }] },
    ])
    const wrapper = mountPanel()
    await flushPromises()

    const options = wrapper.findAll('datalist option').map((option) => option.attributes('value'))
    expect(options).toContain('claude-sonnet-4-5')
    expect(options).toContain('claude-opus-4-1')

    // 手填预设外模型（如自定义部署名）仍然生效
    const input = wrapper.get('input[list]')
    await input.setValue('my-custom-deployment')
    await input.trigger('change')
    expect((wrapper.vm as unknown as { draft: typeof baseConfig }).draft.platforms[0].models).toEqual([
      'my-custom-deployment',
    ])
    wrapper.unmount()
  })
})
