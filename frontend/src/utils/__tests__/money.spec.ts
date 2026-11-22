import { describe, expect, it, vi } from 'vitest'
import { formatLedgerAmount, formatBalanceAmount, formatMoney, formatExactMoney, accountBilledAmount } from '../money'
import { formatCurrency, formatDateTimeWithZone, formatDateTime, formatRelativeTime, formatRelativeWithDateTime, resolveReportTimeZone, formatReportDateInput, formatAccountStatsCurrency } from '../format'
import { formatScaled } from '../pricing'
import { formatTokenPricePerMillion } from '../usagePricing'
import { billingExplanationValues } from '../usageBillingExplanation'
import type { UsageLog } from '@/types'

vi.mock('@/i18n', () => ({
  getLocale: () => 'en-GB',
  i18n: { global: { t: (key: string, params?: { n: number }) => params ? `${key}:${params.n}` : key } },
}))

describe('amount and unit contract', () => {
  it.each([null, undefined, NaN, Infinity, -Infinity])('keeps unknown %s separate from zero', value => {
    expect(formatLedgerAmount(value)).toBe('-')
    expect(formatBalanceAmount(value)).toBe('-')
    expect(formatCurrency(value, 'CNY')).toBe('-')
  })
  it.each([0, 1e-8, -1e-8, 123.12345678, -123.12345678])('shares exact ledger precision with billing evidence for %s', value => {
    const formatted = formatLedgerAmount(value)
    expect(formatted).toBe(value.toFixed(8))
    const log = { billing_explanation: { snapshot: { charged_amount: value } } } as UsageLog
    expect(billingExplanationValues(log, key => key)[9]).toBe(formatted)
    expect(formatMoney(value, 'USD', 'ledger')).toBe(`USD ${formatted}`)
  })
  it('separates overview, unit prices, and currency codes without converting amounts', () => {
    expect(formatBalanceAmount(123.12345678)).toBe('123.12')
    expect(formatBalanceAmount(-1e-8)).toBe('-0.00000001')
    expect(formatMoney(0, 'CNY')).toBe('CNY 0.00')
    expect(formatMoney(0, 'USD')).toBe('USD 0.00')
    expect(formatTokenPricePerMillion(1e-8, 1e6)).toBe('$0.00000001')
  })
  it('preserves micro-dollar (< 1e-8) and multi-currency precision without trailing-dot truncation', () => {
    expect(formatExactMoney(1e-9, 'USD')).toBe('$0.000000001')
    expect(formatExactMoney('0.000000004', 'USD')).toBe('$0.000000004')
    expect(formatExactMoney('-0.0000000012', 'USD')).toBe('-$0.0000000012')
    expect(formatExactMoney('0.0000000000001', 'CNY')).toBe('CNY 0.0000000000001')
    expect(formatExactMoney('-0.00000000', 'USD')).toBe('$0.00')
    expect(formatExactMoney('123.45000000', 'EUR')).toBe('€123.45')
    expect(formatExactMoney('0.004', 'USD', '-', 4)).toBe('$0.0040')
    expect(formatExactMoney(100, 'JPY', '-', 0)).toBe('JPY 100')
    expect(formatBalanceAmount(1e-10)).toBe('0.0000000001')
  })
  it('does not corrupt scientific exponents or non-finite price cards', () => {
    expect(formatScaled(1e-10, 1)).toBe('$1e-10')
    expect(formatScaled(1e10, 1)).toBe('$1e+10')
    expect(formatScaled(NaN, 1)).toBe('-')
    expect(formatScaled(0, 1)).toBe('$0')
  })
  it('preserves unknown and explicit zero for account charges and quantities', () => {
    expect(accountBilledAmount({})).toBeNull()
    expect(accountBilledAmount({ total_cost: 1, account_stats_cost: 0 })).toBe(0)
    expect(accountBilledAmount({ total_cost: -1e-8, account_rate_multiplier: 2 })).toBe(-2e-8)
    const log = { billing_explanation: { snapshot: {} } } as UsageLog
    expect(billingExplanationValues(log, key => key)[6]).toBe('usage.evidence.unknown')
  })
})

describe('visible display time zone contract', () => {
  it.each([
    ['Asia/Shanghai', '2026-03-08T07:30:00Z', '2026-03-08', '15:30'],
    ['UTC', '2026-03-08T07:30:00Z', '2026-03-08', '07:30'],
    ['America/Los_Angeles', '2026-03-08T07:30:00Z', '2026-03-07', '23:30'],
    ['America/Los_Angeles', '2026-03-08T09:59:00Z', '2026-03-08', '01:59'],
    ['America/Los_Angeles', '2026-03-08T10:00:00Z', '2026-03-08', '03:00'],
  ])('uses %s for date controls and the same instant in table/export', (zone, instant, date, time) => {
    expect(resolveReportTimeZone(zone)).toBe(zone)
    expect(formatReportDateInput(new Date(instant), zone)).toBe(date)
    expect(formatDateTimeWithZone(instant, zone)).toContain(time)
    expect(formatDateTimeWithZone(instant, zone)).toContain(zone)
  })
  it('disambiguates the repeated Los Angeles hour and retains the account currency format', () => {
    expect(formatDateTimeWithZone('2026-11-01T08:30:00Z', 'America/Los_Angeles')).toContain('GMT-07:00')
    expect(formatDateTimeWithZone('2026-11-01T09:30:00Z', 'America/Los_Angeles')).toContain('GMT-08:00')
    expect(formatAccountStatsCurrency(0.000001)).toContain('0.000001')
    expect(formatAccountStatsCurrency(null)).toBe('$0.00')
    expect(resolveReportTimeZone('invalid')).toBe('UTC')
  })
  it.each([
    ['2026-01-31T16:00:00Z', 'Asia/Shanghai', '2026-02-01 00:00 (Asia/Shanghai, GMT+08:00)'],
    ['2026-03-08T06:59:00Z', 'America/New_York', '2026-03-08 01:59 (America/New_York, GMT-05:00)'],
    ['2026-03-08T07:00:00Z', 'America/New_York', '2026-03-08 03:00 (America/New_York, GMT-04:00)'],
    ['2026-11-01T05:30:00Z', 'America/New_York', '2026-11-01 01:30 (America/New_York, GMT-04:00)'],
    ['2026-11-01T06:30:00Z', 'America/New_York', '2026-11-01 01:30 (America/New_York, GMT-05:00)'],
    ['2026-01-01T00:00:00Z', 'invalid', '2026-01-01 00:00 (UTC, GMT+00:00)'],
  ])('labels %s in %s', (date, zone, expected) => {
    const effectiveZone = zone === 'invalid' ? 'UTC' : zone
    const localDate = formatDateTime(date, { timeZone: effectiveZone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
    expect(formatDateTimeWithZone(date, zone)).toBe(`${localDate} ${expected.slice(expected.indexOf('('))}`)
    expect(formatDateTimeWithZone(date, zone)).toContain(expected.slice(11, 16))
  })
})

it('keeps invalid dates empty and relative time behavior stable', () => {
  expect(formatDateTimeWithZone('invalid')).toBe('')
  expect(formatDateTimeWithZone(null)).toBe('')
  vi.useFakeTimers()
  try {
    vi.setSystemTime(new Date('2026-09-18T12:00:00Z'))
    const date = '2026-09-18T11:55:00Z'
    expect(formatRelativeTime(date)).toBe('common.time.minutesAgo:5')
    expect(formatRelativeWithDateTime(date)).toBe(`${formatRelativeTime(date)} · ${formatDateTime(date)}`)
    expect(formatRelativeTime('invalid')).toBe(formatRelativeTime(null))
    expect(formatRelativeTime('2026-09-18T12:01:00Z')).toBe(formatRelativeTime(null))
  } finally { vi.useRealTimers() }
})
