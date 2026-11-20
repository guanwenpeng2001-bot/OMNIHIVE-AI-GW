import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MonitorSettingsPanel from '../MonitorSettingsPanel.vue'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), updateConfig: vi.fn(), getPresets: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {}, showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/utils/featureFlags', () => ({ getChannelMonitorMode: () => 'v2', isChannelTelemetryMode: () => true }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAllIncludingInactive: vi.fn().mockResolvedValue([]) } } }))
vi.mock('@/api/channelTelemetry', () => ({ getConfig: mocks.getConfig, updateConfig: mocks.updateConfig, MONITOR_ERROR_CATEGORIES: [] }))
vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: mocks.getPresets }))

describe('MonitorSettingsPanel preset lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getConfig.mockResolvedValue({
      version: 3, enabled: true, refresh_interval_seconds: 60, group_ids: [],
      platforms: [{ platform: 'anthropic', enabled: true, models: ['saved-retired'] }],
    })
    mocks.getPresets.mockResolvedValue([
      { platform: 'anthropic', models: [
        { id: 'saved-retired', lifecycle: 'retired' },
        { id: 'manual-retired', lifecycle: 'retired' },
        { id: 'active-model', lifecycle: 'active' },
        { id: 'preview-model', lifecycle: 'preview' },
        { id: 'deprecated-model', lifecycle: 'deprecated' },
        { id: 'unknown-model', lifecycle: 'unknown' },
        { id: 'missing-status-model' },
      ] },
    ])
    mocks.updateConfig.mockImplementation(async (config) => JSON.parse(JSON.stringify(config)))
  })

  it('excludes only explicitly retired models from new suggestions', async () => {
    const wrapper = mount(MonitorSettingsPanel, { global: { stubs: { Icon: true, RouterLink: true } } })
    await flushPromises()

    expect(wrapper.findAll('#monitor-settings-models-anthropic option').map(option => option.attributes('value')))
      .toEqual(['active-model', 'preview-model', 'deprecated-model', 'unknown-model', 'missing-status-model'])
    wrapper.unmount()
  })

  it('preserves saved retired text and accepts manual retired and custom models through save', async () => {
    const wrapper = mount(MonitorSettingsPanel, { global: { stubs: { Icon: true, RouterLink: true } } })
    await flushPromises()

    const input = wrapper.get<HTMLInputElement>('input[list="monitor-settings-models-anthropic"]')
    expect(input.element.value).toBe('saved-retired')
    expect(wrapper.get('header button').attributes('disabled')).toBeDefined()
    await input.setValue('saved-retired, manual-retired, custom-deployment')
    await input.trigger('change')
    await wrapper.get('header button').trigger('click')
    await flushPromises()

    expect(mocks.updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      platforms: expect.arrayContaining([{
        platform: 'anthropic', enabled: true, models: ['custom-deployment', 'manual-retired', 'saved-retired'],
      }]),
    }))
    expect(input.element.value).toBe('custom-deployment, manual-retired, saved-retired')
    wrapper.unmount()
  })
})
