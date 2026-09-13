import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
const sync = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin/qianwenPricing', () => ({ syncQianwenPricing: sync }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import QianwenPricingPanel from '../QianwenPricingPanel.vue'
async function setup() {
  const wrapper = mount(QianwenPricingPanel, { props: { accountId: 19 } })
  await wrapper.get('button').trigger('click')
  await wrapper.get('[data-testid="pricing-rate"]').setValue('0.14')
  await wrapper.get('[data-testid="pricing-source"]').setValue('Approved operations rate')
  await wrapper.get('[data-testid="pricing-effective"]').setValue('2026-09-13T12:00')
  await wrapper.get('[data-testid="pricing-group"]').setValue('13')
  return wrapper
}
const ready = { written: 0, group_id: null, result: { status: 'ready', imported: 2, models: [] } }
describe('Qianwen native pricing', () => {
  beforeEach(() => sync.mockReset())
  it('requires a preview, omits group for preview, and sends explicit FX and group on apply', async () => {
    const wrapper = await setup()
    expect(wrapper.get('[data-testid="pricing-apply"]').attributes('disabled')).toBeDefined()
    sync.mockResolvedValueOnce(ready)
    await wrapper.get('[data-testid="pricing-preview"]').trigger('click')
    await flushPromises()
    expect(sync).toHaveBeenCalledWith(19, expect.objectContaining({ usd_per_cny: 0.14, fx_source: 'Approved operations rate' }))
    expect(sync.mock.calls[0][1]).not.toHaveProperty('group_id')
    expect(sync.mock.calls[0][1].fx_effective_at).toMatch(/Z$/)
    sync.mockResolvedValueOnce({ ...ready, written: 2, group_id: 13 })
    await wrapper.get('[data-testid="pricing-apply"]').trigger('click')
    await flushPromises()
    expect(sync.mock.calls[1][1].group_id).toBe(13)
    expect(wrapper.findAll('[data-testid="pricing-result"]')).toHaveLength(2)
    await wrapper.get('[data-testid="pricing-rate"]').setValue('0.15')
    expect(wrapper.get('[data-testid="pricing-apply"]').attributes('disabled')).toBeDefined()
  })
  it('blocks invalid FX and exposes unsupported and failed sessions without enabling apply', async () => {
    const wrapper = await setup()
    await wrapper.get('[data-testid="pricing-rate"]').setValue('0')
    expect(wrapper.get('[data-testid="pricing-preview"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="pricing-rate"]').setValue('0.14')
    sync.mockResolvedValueOnce({ ...ready, result: { ...ready.result, status: 'unsupported', reason: 'Unsupported region' } })
    await wrapper.get('[data-testid="pricing-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Unsupported region')
    expect(wrapper.get('[data-testid="pricing-apply"]').attributes('disabled')).toBeDefined()
    sync.mockRejectedValueOnce(new Error('Network failed'))
    await wrapper.get('[data-testid="pricing-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Network failed')
  })
})
