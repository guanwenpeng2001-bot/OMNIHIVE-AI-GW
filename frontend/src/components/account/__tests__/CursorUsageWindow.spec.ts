import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CursorUsageWindow from '../CursorUsageWindow.vue'
import type { CursorMonthlyUsage } from '@/types'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key} ${Object.values(params).join(' ')}` : key, locale: { value: 'en' } }) }))
const monthly: CursorMonthlyUsage = {
  start_of_month: '2026-08-28T17:01:12Z', resets_at: '2026-09-28T17:01:12Z',
  models: { 'gpt-4': { numRequests: 2, numRequestsTotal: 3, numTokens: 70, maxRequestUsage: null, maxTokenUsage: null } }
}
const render = (props = {}) => mount(CursorUsageWindow, { props, global: { stubs: { UsageProgressBar: { props: ['utilization'], template: '<div data-test="progress">{{ utilization }}</div>' } } } })

describe('Cursor monthly usage', () => {
  beforeEach(() => {
    // Keep the fixture's billing period current regardless of the wall clock.
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-01T00:00:00Z'))
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('uses official plan percentages instead of dividing legacy spend and preserves a zero percent', () => {
    const wrapper = render({ monthly: { ...monthly, plan_usage: { autoPercentUsed: 0, apiPercentUsed: 100, totalSpend: 49512, limit: 2000 }, limit_status: { isInSlowPool: true } } })
    expect(wrapper.findAll('[data-test="progress"]').map(bar => bar.text())).toEqual(['0', '100'])
    expect(wrapper.text()).toContain('admin.accounts.cursor.slowPool')
    expect(wrapper.text()).toContain('admin.accounts.cursor.resetDate')
  })
  it('does not turn missing or invalid group percentages into zero usage', () => {
    const wrapper = render({ monthly: { ...monthly, plan_usage: { autoPercentUsed: NaN, apiPercentUsed: -1 } } })
    expect(wrapper.findAll('[data-test="progress"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('admin.accounts.cursor.quotaUnknown')
  })

  it('shows counters and the period without inventing percentages for null limits', () => {
    const wrapper = render({ monthly })
    expect(wrapper.text()).toContain('gpt-4')
    expect(wrapper.html()).toContain('8/28/2026')
    expect(wrapper.text()).toContain('9/28/2026')
    expect(wrapper.text()).toContain('admin.accounts.cursor.quotaUnknown')
    expect(wrapper.findAll('[data-test="progress"]')).toHaveLength(0)
  })
  it('shows independent positive limits without inventing a percent for zero quotas', () => {
    const wrapper = render({ monthly: { ...monthly, models: { 'gpt-4': { ...monthly.models['gpt-4'], maxRequestUsage: 10, maxTokenUsage: 100 } } } })
    expect(wrapper.findAll('[data-test="progress"]').map(bar => bar.text())).toEqual(['20', '70'])
    const zero = render({ monthly: { ...monthly, models: { 'gpt-4': { ...monthly.models['gpt-4'], maxRequestUsage: 0 } } } })
    expect(zero.findAll('[data-test="progress"]')).toHaveLength(0)
  })
  it('shows no-data state and allows a query', async () => {
    const wrapper = render()
    expect(wrapper.text()).toContain('admin.accounts.cursor.noUsage')
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
  })
  it('shows query failure and allows retry without presenting old data as current', async () => {
    const wrapper = render({ monthly, error: 'unauthenticated' })
    expect(wrapper.text()).toContain('admin.accounts.cursor.usageFailed')
    expect(wrapper.text()).not.toContain('gpt-4')
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
  })
  it('retains counters with an unknown billing period', () => {
    const wrapper = render({ monthly: { ...monthly, start_of_month: null, resets_at: null } })
    expect(wrapper.text()).toContain('admin.accounts.cursor.periodUnknown')
    expect(wrapper.text()).toContain('gpt-4')
  })
})
