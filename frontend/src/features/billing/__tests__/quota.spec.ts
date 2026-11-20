import { baseCompile } from '@intlify/message-compiler'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en/costLedger'
import type { Schema } from '@/features/requests/api'
import { moneyText } from '@/features/requests/presentation'
import QuotaWaterfall from '../components/QuotaWaterfall.vue'
import { quotaUsage, validAdjustment } from '../presentation'

const quotaRule: Schema['CostRule'] = {
  id: 'project-USD', version: '3', scope: { type: 'project', id: '2' },
  owner_id: '1', shared_allowance_id: 'project-USD', source: 'budget', mode: 'hard', unit: 'USD',
  limit: '12.00000000', used: '0.00000000', reserved: null, available: null, overrun: null,
  window: 'calendar_day', timezone: 'UTC', resets_at: '2026-11-20T00:00:00Z', freshness: 'unknown',
  observed_at: null, allow_extra_spend: false, include_byok_external_estimates: false, adjust_action: null, can_adjust: false,
}

describe('effective quota', () => {
  it('keeps exact zero visible and does not draw a progress share for unknown reservations', () => {
    expect(quotaUsage(quotaRule)).toBeNull()
    expect(quotaUsage({ ...quotaRule, limit: '12', used: '0', reserved: '3' })).toEqual({ used: 0, reserved: 25 })
    const wrapper = mount(QuotaWaterfall, {
      props: { quota: {
        rules: [quotaRule], as_of: '2026-11-19T08:00:00Z', funding_account_id: null,
        blocked: true, blocking_rule_ids: [quotaRule.id], external_exposure: { state: 'unknown', risk_limit: null },
      } },
      global: { plugins: [createI18n({ legacy: false, locale: 'en',
        messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(),
        messages: { en: { costLedger: en } }, })] },
    })
    expect(wrapper.findAll('li dd').map(node => node.text())).toEqual(['12.00000000', '0.00000000', '—', '—'])
    expect(wrapper.text()).toContain('Blocking limit')
  })

  it('preserves currency and ledger precision and requires a nonzero signed correction with a reason', () => {
    expect(moneyText({ currency: 'USD', amount: '9007199254740993.00000001' }, 'en')).toBe('USD 9,007,199,254,740,993.00000001')
    expect(moneyText({ currency: 'CNY', amount: '0.00000000' }, 'en')).toBe('CNY 0.00000000')
    expect(validAdjustment('-0.00000001', 'Statement reference')).toBe(true)
    expect(validAdjustment('-0.00000000', 'Statement reference')).toBe(false)
    expect(validAdjustment('0.1', ' ')).toBe(false)
    expect(validAdjustment('0.000000001', 'Statement reference')).toBe(false)
  })
})
