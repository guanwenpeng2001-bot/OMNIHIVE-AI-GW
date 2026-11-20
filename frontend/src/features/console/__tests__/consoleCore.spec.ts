import { describe, expect, it } from 'vitest'
import { classifyConsoleError } from '../asyncState'
import {
  EM_DASH,
  formatChargeObject,
  formatExactMoney,
  formatExpirationCountdown,
  formatNullableBytes,
  formatNullableInteger,
  formatNullableLatencyMs,
  formatNullablePercent
} from '../format'

describe('console tabular formatters and null honesty', () => {
  it('renders em-dash for null or undefined metrics instead of fabricating zero', () => {
    expect(formatNullableInteger(null)).toBe(EM_DASH)
    expect(formatNullableInteger(undefined)).toBe(EM_DASH)
    expect(formatNullablePercent(null)).toBe(EM_DASH)
    expect(formatNullableLatencyMs(null)).toBe(EM_DASH)
    expect(formatExactMoney(null)).toBe(EM_DASH)
    expect(formatChargeObject(null)).toBe(EM_DASH)
    expect(formatNullableBytes(null)).toBe(EM_DASH)
  })

  it('preserves exact decimal money and observed zero values', () => {
    expect(formatNullableInteger(0)).toBe('0')
    expect(formatExactMoney('0.00000000', 'USD')).toBe('$0.00')
    expect(formatExactMoney('0.40000000', 'USD')).toBe('$0.40')
    expect(formatExactMoney('0.00012500', 'USD')).toBe('$0.000125')
    expect(formatChargeObject({ currency: 'USD', amount: '12.34500000' })).toBe('$12.345')
  })

  it('computes artifact expiration countdown states accurately', () => {
    const now = Date.parse('2026-11-19T12:00:00Z')
    expect(formatExpirationCountdown(null, now).state).toBe('none')
    expect(formatExpirationCountdown('2026-11-19T11:59:59Z', now).state).toBe('expired')
    expect(formatExpirationCountdown('2026-11-19T12:30:00Z', now)).toMatchObject({
      state: 'expiring_soon'
    })
    expect(formatExpirationCountdown('2026-11-20T06:00:00Z', now)).toMatchObject({
      state: 'active'
    })
  })
})

describe('classifyConsoleError across async error states', () => {
  it('maps 403, 409, 504 unknown, stale cached error, and plain error distinctly', () => {
    expect(
      classifyConsoleError({ status: 403, reason: 'INSUFFICIENT_GRANT', message: 'Denied' }, {
        requiredGrantFallback: 'playground.run'
      })
    ).toMatchObject({
      kind: 'permission',
      requiredGrant: 'playground.run',
      retryable: false
    })

    expect(
      classifyConsoleError({ status: 409, reason: 'VERSION_CONFLICT', message: 'Stale ETag' })
    ).toMatchObject({
      kind: 'conflict',
      retryable: true
    })

    expect(
      classifyConsoleError({ status: 504, reason: 'OUTCOME_UNKNOWN', message: 'Gateway timeout' })
    ).toMatchObject({
      kind: 'unknown',
      retryable: true
    })

    expect(
      classifyConsoleError(new Error('network blip'), {
        hasCachedData: true,
        lastSyncedAt: '2026-11-19T09:00:00Z'
      })
    ).toMatchObject({
      kind: 'stale',
      lastSyncedAt: '2026-11-19T09:00:00Z'
    })

    expect(classifyConsoleError({ status: 500, message: 'Internal failure' })).toMatchObject({
      kind: 'error',
      retryable: true
    })
  })
})
