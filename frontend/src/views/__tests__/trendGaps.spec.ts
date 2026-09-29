import { describe, expect, it } from 'vitest'
import { historyGapFromError, usageExportFailureMessage, withTrendGaps } from '../trendGaps'
import type { TrendDataPoint } from '@/types'
import enDashboard from '@/i18n/locales/en/dashboard'
import zhDashboard from '@/i18n/locales/zh/dashboard'

const point = (date: string) => ({ date, requests: 2, total_tokens: 3 }) as TrendDataPoint

describe('trend gaps', () => {
  it.each(['day', 'hour'])('inserts an unknown %s bucket without inventing zeros', granularity => {
    const labels = granularity === 'day' ? ['2026-03-01', '2026-03-03'] : ['2026-03-01 01:00', '2026-03-01 03:00']
    const input = labels.map(point)
    const result = withTrendGaps(input, granularity)
    expect(result).toHaveLength(3)
    expect(result[0]).toBe(input[0])
    expect(result[2]).toBe(input[1])
    expect(Number.isNaN(result[1].total_tokens)).toBe(true)
    expect(Number.isNaN(result[1].requests)).toBe(true)
  })
  it('preserves complete, empty and invalid sequences', () => {
    expect(withTrendGaps([], 'day')).toEqual([])
    const input = [point('2026-03-01'), point('2026-03-02')]
    expect(withTrendGaps(input, 'day')).toEqual(input)
    expect(withTrendGaps([point('invalid')], 'day')).toEqual([point('invalid')])
  })
  it('reads the actual flat client error and metadata', () => {
    expect(historyGapFromError({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE', metadata: { raw_retained_from: '2026-03-05T00:00:00Z' } })).toEqual({ reason: 'USAGE_HISTORY_INCOMPLETE', rawRetainedFrom: '2026-03-05T00:00:00Z' })
  })
  it.each([null, {}, { status: 409, reason: 'OTHER_CONFLICT' }, { status: 401, reason: 'USAGE_HISTORY_INCOMPLETE' }, { status: 500, reason: 'USAGE_HISTORY_INCOMPLETE' }, { response: { status: 409 }, reason: 'USAGE_HISTORY_INCOMPLETE' }])('does not misclassify %j', error => {
    expect(historyGapFromError(error)).toBeNull()
  })
})

/** 用真实词条拼一个 t()，验证中英两边的实际文案而不是替身占位符。 */
const translateFrom = (locale: Record<string, unknown>) => (key: string, params?: Record<string, unknown>) => {
  const template = key.split('.').reduce<unknown>((node, part) =>
    typeof node === 'object' && node !== null ? (node as Record<string, unknown>)[part] : undefined, locale)
  if (typeof template !== 'string') return key
  return params
    ? template.replace(/\{(\w+)\}/g, (_whole, name: string) => String(params[name] ?? `{${name}}`))
    : template
}

describe('usage export failure messages', () => {
  const translateZh = translateFrom(zhDashboard as unknown as Record<string, unknown>)
  const translateEn = translateFrom(enDashboard as unknown as Record<string, unknown>)
  const historyGap = { status: 409, reason: 'USAGE_HISTORY_INCOMPLETE', metadata: { raw_retained_from: '2026-03-05T00:00:00Z' } }

  it('reports the precise reason and retention start in both locales', () => {
    expect(usageExportFailureMessage(historyGap, translateZh)).toContain('使用历史不完整')
    expect(usageExportFailureMessage(historyGap, translateZh)).toContain('2026-03-05T00:00:00Z')
    expect(usageExportFailureMessage(historyGap, translateEn)).toContain('Usage history is incomplete')
    expect(usageExportFailureMessage(historyGap, translateEn)).toContain('2026-03-05T00:00:00Z')
  })

  it('still names the reason when the server omits the retention start', () => {
    const zh = usageExportFailureMessage({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' }, translateZh)
    const en = usageExportFailureMessage({ status: 409, reason: 'USAGE_HISTORY_INCOMPLETE' }, translateEn)
    expect(zh).toContain('使用历史不完整')
    expect(en).toContain('Usage history is incomplete')
    expect(zh).not.toMatch(/\{/)
    expect(en).not.toMatch(/\{/)
  })

  it('leaves every other failure to the generic export message', () => {
    expect(usageExportFailureMessage({ status: 500, reason: 'INTERNAL_ERROR' }, translateZh)).toBeUndefined()
    expect(usageExportFailureMessage({ status: 409, reason: 'OTHER_CONFLICT' }, translateZh)).toBeUndefined()
    expect(usageExportFailureMessage(null, translateZh)).toBeUndefined()
  })
})
