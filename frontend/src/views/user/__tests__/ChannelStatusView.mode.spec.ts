import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

const isV1 = vi.fn(() => false)

vi.mock('@/utils/featureFlags', () => ({
  isChannelProbeMode: () => isV1(),
}))

vi.mock('../ChannelProbeStatusView.vue', () => ({
  default: defineComponent({ name: 'ChannelProbeStatusView', setup: () => () => h('div', { 'data-testid': 'v1' }) }),
}))
vi.mock('../ChannelTelemetryStatusView.vue', () => ({
  default: defineComponent({ name: 'ChannelTelemetryStatusView', setup: () => () => h('div', { 'data-testid': 'v2' }) }),
}))

import ChannelStatusView from '../ChannelStatusView.vue'

describe('ChannelStatusView mode switch', () => {
  beforeEach(() => {
    isV1.mockReset()
  })

  it('renders V2 when not in v1 mode', () => {
    isV1.mockReturnValue(false)
    const wrapper = mount(ChannelStatusView)
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
  })

  it('renders V1 when in v1 mode', () => {
    isV1.mockReturnValue(true)
    const wrapper = mount(ChannelStatusView)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(false)
  })
})
