import { describe, expect, it } from 'vitest'
import type { UsageLog, UsagePricingSnapshot } from '@/types'
import { billingExplanationValues, pricingUnit } from '../usageBillingExplanation'
import { getBillingModeLabel } from '../billingMode'

const t = (key: string) => key
describe('frozen billing evidence', () => {
  it('does not infer success or free pricing from a historical amount', () => {
    for (const actual_cost of [0, 123]) {
      expect(billingExplanationValues({ actual_cost } as UsageLog, t).slice(0, 3)).toEqual(Array(3).fill('usage.evidence.unknown'))
    }
  })
  it('separates free completion and interruption and preserves eight decimal charge', () => {
    const log = { request_id: 'request', billing_explanation: { pricing_state: 'free', settlement_state: 'settled', completion_state: 'interrupted', snapshot: { mode: 'realtime', units: 0, unit_price: 0, charged_amount: 0.00000001, source: 'frozen', price_card_version: 'v1' } } } as UsageLog
    const values = billingExplanationValues(log, t)
    expect(values[0]).toBe('usage.evidence.states.free')
    expect(values[2]).toBe('usage.evidence.states.interrupted')
    expect(values[6]).toBe('0')
    expect(values[9]).toBe('0.00000001')
    expect(values[14]).toContain('"price_card_version":"v1"')
  })
  it.each([['realtime', '', 'characters'], ['asr', '', 'seconds'], ['3d', '', 'models'], ['audio', 'stt', 'hours'], ['audio', 'tts', 'million_characters'], ['video', '', 'seconds'], ['image', '', 'images']])('uses %s real units', (mode, size_tier, unit) => {
    expect(pricingUnit({ mode, size_tier } as UsagePricingSnapshot)).toBe(unit)
  })
  it.each(['file_batch', 'realtime', 'asr', '3d', 'audio'])('labels %s without pretending tokens', mode => {
    expect(getBillingModeLabel(mode, t)).toBe(`usage.evidence.modes.${mode}`)
  })
})
