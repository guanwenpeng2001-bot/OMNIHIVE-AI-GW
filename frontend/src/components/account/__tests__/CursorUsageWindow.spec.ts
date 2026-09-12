import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CursorUsageWindow from '../CursorUsageWindow.vue'
import type { CursorMonthlyUsage } from '@/types'

vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
const monthly: CursorMonthlyUsage = {
  start_of_month: '2026-08-28T17:01:12Z', resets_at: '2026-09-28T17:01:12Z',
  models: { 'gpt-4': { numRequests: 2, numRequestsTotal: 3, numTokens: 70, maxRequestUsage: null, maxTokenUsage: null } }
}
const render = (props = {}) => mount(CursorUsageWindow, { props, global: { stubs: { UsageProgressBar: { props: ['utilization'], template: '<div data-test="progress">{{ utilization }}</div>' } } } })

describe('Cursor monthly usage', () => {
  it('shows counters and the period without inventing percentages for null limits', () => {
    const wrapper = render({ monthly })
    expect(wrapper.text()).toContain('gpt-4')
    expect(wrapper.text()).toContain('8/28/2026')
    expect(wrapper.text()).toContain('9/28/2026')
    expect(wrapper.text()).toContain('admin.accounts.cursor.unlimited')
    expect(wrapper.findAll('[data-test="progress"]')).toHaveLength(0)
  })
  it('shows both independent limits, including explicit zero quotas', () => {
    const wrapper = render({ monthly: { ...monthly, models: { 'gpt-4': { ...monthly.models['gpt-4'], maxRequestUsage: 10, maxTokenUsage: 100 } } } })
    expect(wrapper.findAll('[data-test="progress"]').map(bar => bar.text())).toEqual(['20', '70'])
    const zero = render({ monthly: { ...monthly, models: { 'gpt-4': { ...monthly.models['gpt-4'], maxRequestUsage: 0 } } } })
    expect(zero.find('[data-test="progress"]').text()).toBe('100')
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
