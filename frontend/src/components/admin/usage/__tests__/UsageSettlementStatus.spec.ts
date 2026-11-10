import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import UsageSettlementStatus from '../UsageSettlementStatus.vue'
import { apiClient } from '@/api/client'

vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn() } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const get = vi.mocked(apiClient.get)

function settlement(overrides: Record<string, unknown> = {}) {
  return {
    pending: 0,
    needs_review: 16282,
    historical_zero_review: 51,
    persistence_unconfirmed: 0,
    items: [{
      id: 1,
      request_id: 'client:abc',
      api_key_id: 2,
      user_id: 3,
      state: 'needs_review',
      pricing_state: 'unpriced',
      created_at: '2026-09-01T00:00:00Z',
    }],
    historical_items: [{
      id: 9,
      request_id: 'hist-1',
      api_key_id: 2,
      user_id: 3,
    }],
    ...overrides,
  }
}

beforeEach(() => {
  get.mockReset()
})

describe('UsageSettlementStatus', () => {
  it('keeps the panel collapsed with only the title and count summary visible', async () => {
    get.mockResolvedValue({ data: settlement() } as never)
    const wrapper = mount(UsageSettlementStatus)
    await flushPromises()

    const panel = wrapper.find('details')
    expect((panel.element as HTMLDetailsElement).open).toBe(false)
    const summary = panel.find('summary')
    expect(summary.text()).toContain('admin.usage.settlement.title')
    expect(summary.text()).toContain('admin.usage.settlement.pending')
    expect(summary.text()).toContain('16282')
    expect(summary.text()).toContain('51')
    expect(summary.text()).not.toContain('client:abc')
    expect(summary.text()).not.toContain('admin.usage.settlement.boundary')
    expect(summary.text()).not.toContain('admin.usage.settlement.locate')
  })

  it('reveals review details after the panel is expanded', async () => {
    get.mockResolvedValue({ data: settlement() } as never)
    const wrapper = mount(UsageSettlementStatus)
    await flushPromises()

    const panel = wrapper.find('details')
    ;(panel.element as HTMLDetailsElement).open = true
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('client:abc')
    expect(wrapper.text()).toContain('admin.usage.settlement.boundary')
    expect(wrapper.find('#settlement-request-id').exists()).toBe(true)
  })

  it('hides the receipt list when needs_review is 0', async () => {
    get.mockResolvedValue({
      data: settlement({
        needs_review: 0,
        items: [],
        historical_items: [],
      }),
    } as never)
    const wrapper = mount(UsageSettlementStatus)
    await flushPromises()

    const summary = wrapper.find('summary')
    expect(summary.text()).toContain('admin.usage.settlement.review')
    expect(summary.text()).toMatch(/:\s*0/)
    expect(wrapper.text()).not.toContain('admin.usage.settlement.empty')
    expect(wrapper.findAll('li')).toHaveLength(0)
    expect(wrapper.find('#settlement-request-id').exists()).toBe(true)
  })

  it('keeps load failures visible while the panel stays collapsed', async () => {
    get.mockRejectedValue(new Error('offline'))
    const wrapper = mount(UsageSettlementStatus)
    await flushPromises()

    expect((wrapper.find('details').element as HTMLDetailsElement).open).toBe(false)
    expect(wrapper.find('[role="alert"]').text()).toBe('admin.usage.settlement.failed')
  })
})
