import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { UserBillingTimelinePage } from '@/api/billing'
import ProfileBillingTimelineCard from '../ProfileBillingTimelineCard.vue'

const { getMyBillingTimeline } = vi.hoisted(() => ({ getMyBillingTimeline: vi.fn() }))

vi.mock('@/api/billing', () => ({
  billingAPI: { getMyBillingTimeline },
}))

const messages: Record<string, string> = {
  'common.back': 'Back',
  'common.next': 'Next',
  'common.refresh': 'Refresh',
  'billing.kind.recharge': 'Recharge',
  'billing.kind.charge': 'Deduction',
  'billing.kind.refund': 'Refund',
  'billing.kind.rebate': 'Rebate',
  'billing.kind.redeem': 'Redeem',
  'billing.kind.adjustment': 'Adjustment',
  'billing.timeline.title': 'Billing Timeline',
  'billing.timeline.description': 'One axis',
  'billing.timeline.credits': 'Credited',
  'billing.timeline.debits': 'Deducted',
  'billing.timeline.net': 'Net change',
  'billing.timeline.accruedRebate': 'Rebate not transferred',
  'billing.timeline.accruedOnly': 'Accrued, not yet transferred',
  'billing.timeline.empty': 'No billing entries in this range.',
  'billing.timeline.loadFailed': 'Failed to load the billing timeline.',
  'billing.timeline.page': 'Page {page} / {pages}',
  'billing.timeline.requests': '{count} requests',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values: Record<string, string> = {}) =>
        (messages[key] ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? `{${name}}`)),
    }),
  }
})

const timelinePage = (overrides: Partial<UserBillingTimelinePage> = {}): UserBillingTimelinePage => ({
  entries: [
    {
      kind: 'recharge',
      amount: 100,
      affects_balance: true,
      occurred_at: '2026-09-01T10:00:00Z',
      reference: 'order-1',
      detail: 'alipay',
    },
    {
      kind: 'charge',
      amount: -7.5,
      affects_balance: true,
      occurred_at: '2026-09-06T23:00:00Z',
      reference: '2026-09-06',
      request_count: 3,
    },
    {
      kind: 'rebate',
      amount: 5,
      affects_balance: false,
      occurred_at: '2026-09-03T08:00:00Z',
      reference: '11',
      detail: 'rebate_accrue',
    },
  ],
  totals: {
    credits: 105,
    debits: -7.5,
    net: 97.5,
    accrued_rebate: 5,
    charge_request_count: 3,
  },
  total: 3,
  page: 1,
  page_size: 10,
  timezone: 'UTC',
  ...overrides,
})

const mountCard = async () => {
  const wrapper = mount(ProfileBillingTimelineCard)
  await flushPromises()
  return wrapper
}

describe('ProfileBillingTimelineCard', () => {
  beforeEach(() => {
    getMyBillingTimeline.mockReset()
    getMyBillingTimeline.mockResolvedValue(timelinePage())
  })

  it('renders every kind on one axis with signed amounts', async () => {
    const wrapper = await mountCard()

    const kinds = wrapper.findAll('[data-testid="billing-entry"] .badge').map((badge) => badge.text())
    expect(kinds).toEqual(['Recharge', 'Deduction', 'Rebate'])

    const amounts = wrapper.findAll('[data-testid="billing-amount"]').map((cell) => cell.text())
    expect(amounts).toEqual(['+$100.0000', '-$7.5000', '+$5.0000'])

    // The daily deduction entry reports how many requests it aggregates.
    expect(wrapper.get('[data-testid="billing-detail-charge"]').text()).toContain('3 requests')
    // Rebate accruals are visible without pretending to be wallet movements.
    expect(wrapper.get('[data-testid="billing-detail-rebate"]').text()).toContain('Accrued, not yet transferred')
    wrapper.unmount()
  })

  it('reconciles the window totals', async () => {
    const wrapper = await mountCard()

    expect(wrapper.get('[data-testid="billing-total-credits"]').text()).toContain('+$105.0000')
    expect(wrapper.get('[data-testid="billing-total-debits"]').text()).toContain('-$7.5000')
    expect(wrapper.get('[data-testid="billing-total-net"]').text()).toContain('+$97.5000')
    expect(wrapper.get('[data-testid="billing-total-accrued_rebate"]').text()).toContain('$5.0000')
    wrapper.unmount()
  })

  it('pages through the timeline with the reported entry count', async () => {
    getMyBillingTimeline.mockResolvedValueOnce(timelinePage({ total: 25, page: 1 }))
    const wrapper = await mountCard()

    expect(wrapper.get('[data-testid="billing-page"]').text()).toBe('Page 1 / 3')
    getMyBillingTimeline.mockResolvedValueOnce(timelinePage({ total: 25, page: 2 }))

    const next = wrapper.findAll('button').find((button) => button.text() === 'Next')
    await next!.trigger('click')
    await flushPromises()

    expect(getMyBillingTimeline).toHaveBeenLastCalledWith({ page: 2, page_size: 10 })
    expect(wrapper.get('[data-testid="billing-page"]').text()).toBe('Page 2 / 3')
    wrapper.unmount()
  })

  it('shows an empty range without inventing entries', async () => {
    getMyBillingTimeline.mockResolvedValueOnce(
      timelinePage({
        entries: [],
        totals: { credits: 0, debits: 0, net: 0, accrued_rebate: 0, charge_request_count: 0 },
        total: 0,
      })
    )
    const wrapper = await mountCard()

    expect(wrapper.text()).toContain('No billing entries in this range.')
    expect(wrapper.findAll('[data-testid="billing-entry"]')).toHaveLength(0)
    wrapper.unmount()
  })

  it('reports a failed load instead of stale totals', async () => {
    getMyBillingTimeline.mockRejectedValueOnce(new Error('boom'))
    const wrapper = await mountCard()

    expect(wrapper.get('[data-testid="billing-timeline-error"]').text()).toBe('Failed to load the billing timeline.')
    expect(wrapper.findAll('[data-testid="billing-entry"]')).toHaveLength(0)
    wrapper.unmount()
  })
})
