import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ChannelProbeStatusView from '../ChannelProbeStatusView.vue'

const { list, showError } = vi.hoisted(() => ({ list: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/channelMonitor', () => ({ list, status: vi.fn() }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: { channel_monitor_enabled: true }, showError }),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

const mountView = () => shallowMount(ChannelProbeStatusView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      MonitorHero: { template: '<div />' },
      MonitorCardGrid: { template: '<div />' },
      MonitorDetailDialog: { template: '<div />' },
    },
  },
})

let wrapper: ReturnType<typeof mountView>
beforeEach(() => {
  localStorage.clear()
  list.mockReset()
  showError.mockReset()
})
afterEach(() => { wrapper?.unmount(); localStorage.clear() })

describe('channel status V1 canonical block reasons', () => {
  it('shows the disabled reason instead of an empty list plus toast', async () => {
    list.mockRejectedValue({ status: 403, reason: 'CHANNEL_MONITOR_DISABLED', message: 'disabled', metadata: {} })
    wrapper = mountView()
    await flushPromises()

    const banner = wrapper.get('[data-test="channel-monitor-blocked"]')
    expect(banner.text()).toContain('channelStatus.disabled')
    expect(showError).not.toHaveBeenCalled()
  })

  it('shows the mode mismatch reason when the deployment runs V2', async () => {
    list.mockRejectedValue({ status: 403, reason: 'CHANNEL_MONITOR_MODE_MISMATCH', message: 'mismatch', metadata: {} })
    wrapper = mountView()
    await flushPromises()

    const banner = wrapper.get('[data-test="channel-monitor-blocked"]')
    expect(banner.text()).toContain('channelStatus.modeMismatch')
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps generic failures on the toast path without the blocked banner', async () => {
    list.mockRejectedValue({ status: 500, reason: 'INTERNAL', message: 'boom', metadata: {} })
    wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-test="channel-monitor-blocked"]').exists()).toBe(false)
    expect(showError).toHaveBeenCalled()
  })
})
