import { describe, expect, it } from 'vitest'
import {
  calculateExactScenarioBreakdown,
  formatCatalogRateBand,
  formatTokenRatePerMillion,
  parseNonNegativeDecimal,
  scaleRateToPerMillion
} from '../pricePresentation'
import { publicTokenPrice } from '@/features/public/format'

describe('pricePresentation exact decimal scaling & scenario math', () => {
  it('scales per-token and per-1M strings without IEEE-754 float precision drift', () => {
    // In IEEE-754 float: 0.00000014 * 1_000_000 === 0.13999999999999999
    const deepseekPerToken = {
      currency: 'USD',
      amount: '0.00000014',
      per: '1',
      unit: 'input_token'
    }
    const scaled = scaleRateToPerMillion(deepseekPerToken)
    expect(scaled).toEqual({ num: 7n, den: 50n, currency: 'USD' })
    expect(formatTokenRatePerMillion(deepseekPerToken)).toBe('$0.14')

    // Scientific notation per-token string
    expect(
      formatTokenRatePerMillion({
        currency: 'USD',
        amount: '7.5e-8',
        per: '1',
        unit: 'input_token'
      })
    ).toBe('$0.075')

    // Sub-cent adaptive 2..4 significant decimals
    expect(
      formatTokenRatePerMillion({
        currency: 'USD',
        amount: '0.0375',
        per: '1000000',
        unit: 'cached_input_token'
      })
    ).toBe('$0.0375')

    // Standard 2-decimal clean display
    expect(
      formatTokenRatePerMillion({
        currency: 'USD',
        amount: '2.5',
        per: '1000000',
        unit: 'output_token'
      })
    ).toBe('$2.50')
  })

  it('enforces identical adaptive 2..4 precision across list and detail callers', () => {
    const flashRate = {
      currency: 'USD',
      amount: '0.075',
      per: '1000000',
      unit: 'input_token'
    }
    expect(publicTokenPrice(flashRate, 2)).toBe('$0.075')
    expect(publicTokenPrice(flashRate, 4)).toBe('$0.075')
    expect(publicTokenPrice(flashRate, 'adaptive')).toBe('$0.075')
  })

  it('handles zero, underflow (< $0.0001), non-USD currencies, and non-token rate bands', () => {
    expect(
      formatTokenRatePerMillion(
        { currency: 'USD', amount: '0', per: '1000000', unit: 'token' },
        { locale: 'en' }
      )
    ).toBe('Free')
    expect(
      formatTokenRatePerMillion(
        { currency: 'USD', amount: '0', per: '1000000', unit: 'token' },
        { locale: 'zh' }
      )
    ).toBe('免费')
    expect(
      formatTokenRatePerMillion({
        currency: 'USD',
        amount: '0.00001',
        per: '1000000',
        unit: 'input_token'
      })
    ).toBe('< $0.0001')
    expect(
      formatTokenRatePerMillion({
        currency: 'CNY',
        amount: '4',
        per: '1000000',
        unit: 'input_token'
      })
    ).toBe('CNY 4.00')
    expect(
      formatCatalogRateBand({
        currency: 'USD',
        amount: '0.04',
        per: '1',
        unit: 'image'
      })
    ).toBe('$0.04 / image')
    expect(parseNonNegativeDecimal('invalid')).toBeNull()
  })

  it('computes exact scenario cost breakdowns with cached input deduction and request multiplier', () => {
    const breakdown = calculateExactScenarioBreakdown({
      inputRate: { currency: 'USD', amount: '2.50', per: '1000000', unit: 'input_token' },
      cachedInputRate: { currency: 'USD', amount: '0.25', per: '1000000', unit: 'cache_read_token' },
      outputRate: { currency: 'USD', amount: '10.00', per: '1000000', unit: 'output_token' },
      inputTokens: '10000',
      cachedInputTokens: '4000',
      outputTokens: '2000',
      requests: '100'
    })

    expect(breakdown.state).toBe('estimated')
    expect(breakdown.uncachedInputTokens).toBe('6000')
    expect(breakdown.totalUncachedInputTokens).toBe('600000')
    expect(breakdown.totalCachedInputTokens).toBe('400000')
    expect(breakdown.totalOutputTokens).toBe('200000')
    expect(breakdown.uncachedInputCost).toBe('$1.50')
    expect(breakdown.cachedInputCost).toBe('$0.10')
    expect(breakdown.outputCost).toBe('$2.00')
    expect(breakdown.singleRequestTotal).toBe('$0.036')
    expect(breakdown.totalDisplay).toBe('$3.60')
  })
})
