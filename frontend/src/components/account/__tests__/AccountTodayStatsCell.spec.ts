import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountTodayStatsCell from '../AccountTodayStatsCell.vue'
import { formatCurrency } from '@/utils/format'

const locale = vi.hoisted(() => ({ value: 'zh' }))
vi.mock('@/i18n', () => ({ getLocale: () => locale.value, i18n: { global: { t: (key: string) => key } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('AccountTodayStatsCell production display compatibility', () => {
  it.each([
    ['zh', 0, 'US$0.00'],
    ['zh', 1.25, 'US$1.25'],
    ['zh', 0.00000123, 'US$0.000001'],
    ['zh', 0.00000001, 'US$0.000000'],
    ['zh', 0.00999999, 'US$0.010000'],
    ['zh', 0.01, 'US$0.01'],
    ['zh', -0.00000123, '-US$0.00'],
    ['en', 0, '$0.00'],
    ['en', 1234.56, '$1,234.56'],
    ['en', 0.00000123, '$0.000001']
  ])('renders both cost rows with the original %s contract for %s', (language, amount, expected) => {
    locale.value = language as string
    const wrapper = mount(AccountTodayStatsCell, { props: { stats: { requests: 12, tokens: 1500, cost: amount as number, user_cost: amount as number } } })
    const rows = wrapper.findAll('.flex')
    expect(rows.map(row => row.findAll('span')[1].text())).toEqual(['12', '1.5K', expected, expected])
    // The usage/billing formatter retains its separate currency-code and 8-digit contract.
    expect(formatCurrency(0.00000123)).toBe('USD 0.00000123')
    wrapper.unmount()
  })
})
