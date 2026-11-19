import type { LocationQuery } from 'vue-router'
import { opaqueNumericId, type Schema } from '@/features/requests/api'
import { dateFields, queryText, timeWindow } from '@/features/requests/filters'
import type { CostWindow } from './api'

export const settlementStates = ['estimated', 'reserved', 'pending', 'settled', 'needs_review', 'adjusted', 'refunded', 'unknown'] as const
export const fundingSources = ['wallet', 'subscription', 'byok', 'mixed', 'unknown'] as const

export function currencyCode(value: string): string | null {
  const code = value.trim().toUpperCase()
  return /^[A-Z]{3}$/.test(code) ? code : null
}

export function costFilters(query: LocationQuery) {
  const candidate = queryText(query, 'key_id')
  return {
    ...dateFields(query, 7),
    currency: currencyCode(queryText(query, 'currency')) ?? 'USD',
    key_id: opaqueNumericId(candidate) ? candidate : '',
    model_id: queryText(query, 'model_id').slice(0, 255),
    group_by: queryText(query, 'group_by') === 'key' ? 'key' : 'model',
    funding_source: fundingSources.find(v => v === queryText(query, 'funding_source')) ?? '',
    settlement_state: settlementStates.find(v => v === queryText(query, 'settlement_state')) ?? '',
  }
}

export type CostFilters = ReturnType<typeof costFilters>

export function costWindow(filters: CostFilters): CostWindow | null {
  const window = timeWindow(filters)
  const currency = currencyCode(filters.currency)
  return window?.from && window.to && currency ? {
    from: window.from, to: window.to, timezone: window.timezone, currency,
  } : null
}

function nonnegativeUnits(value: string | null): bigint | null {
  if (value === null) return null
  const match = /^(\d{1,28})(?:\.(\d{1,8}))?$/.exec(value)
  if (!match) return null
  return BigInt(match[1] + (match[2] ?? '').padEnd(8, '0'))
}

export function quotaUsage(rule: Pick<Schema['CostRule'], 'limit' | 'used' | 'reserved'>) {
  const limit = nonnegativeUnits(rule.limit)
  const used = nonnegativeUnits(rule.used)
  const held = nonnegativeUnits(rule.reserved)
  if (!limit || used === null || held === null) return null
  const usedShare = Number((used < limit ? used : limit) * 10000n / limit) / 100
  const remaining = used >= limit ? 0n : limit - used
  const heldShare = Number((held < remaining ? held : remaining) * 10000n / limit) / 100
  return { used: usedShare, reserved: heldShare }
}

const rank: Record<string, number> = {
  wallet: 0, gift: 1, budget: 2, subscription: 3, group: 4, key: 5, key_quota: 5, key_budget: 5,
}
export function orderedQuotaRules(quota: Schema['CostQuota']) {
  return [...quota.rules].sort((a, b) => (rank[a.source] ?? 9) - (rank[b.source] ?? 9))
}

export function validAdjustment(amount: string, note: string): boolean {
  const value = amount.trim()
  return /^-?(0|[1-9]\d{0,19})(?:\.\d{1,8})?$/.test(value) && !/^-?0(?:\.0+)?$/.test(value) &&
    note.trim().length > 0 && new TextEncoder().encode(note.trim()).byteLength <= 1024
}
