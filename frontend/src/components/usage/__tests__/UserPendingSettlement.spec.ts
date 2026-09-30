import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import UserPendingSettlement from '../UserPendingSettlement.vue'
import { listPendingSettlement } from '@/api/pendingSettlement'
vi.mock('@/api/pendingSettlement', () => ({ listPendingSettlement: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const list = vi.mocked(listPendingSettlement)
const record = { source: 'receipt' as const, receipt_id: 12, request_id: 'owned-request', api_key_id: 5, requested_model: 'public', upstream_model: 'wire', upstream_response_model: null, input_tokens: 12, output_tokens: 3, pricing_state: 'error' as const, state: 'needs_review' as const, amount: null, amount_state: 'pending_pricing' as const, created_at: '2026-10-01T00:00:00Z', completion_status: 'complete' }
beforeEach(() => { list.mockReset() })
describe('UserPendingSettlement', () => {
  it('shows unknown amounts, model facts, source and pricing errors with independent paging', async () => {
    list.mockImplementation(async (_filter, source, cursor) => source === 'upstream_job' ? { items: [], total: 0, next_cursor: 0 } : { items: [{ ...record, receipt_id: cursor ? 11 : 12 }], total: 2, next_cursor: cursor ? 0 : 12 })
    const wrapper = mount(UserPendingSettlement, { props: { filters: { model: 'public' } } })
    await flushPromises()
    expect(wrapper.text()).toContain('(2)')
    expect(wrapper.text()).toContain('usage.pending.unknown')
    expect(wrapper.text()).toContain('usage.pending.pricingError')
    expect(wrapper.text()).toContain('usage.notCollected')
    expect(wrapper.text()).toContain('receipt #12')
    expect(wrapper.text()).toContain('wire')
    await wrapper.findAll('button')[1].trigger('click'); await flushPromises()
    expect(list).toHaveBeenLastCalledWith({ model: 'public' }, 'receipt', 12)
    expect(wrapper.findAll('details')).toHaveLength(2)
    await wrapper.setProps({ filters: { model: 'other' } }); await flushPromises()
    expect(wrapper.findAll('details')).toHaveLength(1)
  })
  it('keeps load failures visible rather than reporting an empty population', async () => {
    list.mockRejectedValue(new Error('offline'))
    const wrapper = mount(UserPendingSettlement, { props: { filters: {} } })
    await flushPromises()
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('usage.pending.empty')
  })
})
