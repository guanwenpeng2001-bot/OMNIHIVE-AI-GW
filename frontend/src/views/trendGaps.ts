import type { TrendDataPoint } from '@/types'

// Server bucket labels are calendar coordinates in the query zone, not instants
// in the browser zone. Use UTC only for arithmetic and retain their wire format.
function bucketTime(label: string): number {
  if (/^\d{4}-\d{2}-\d{2}$/.test(label)) return Date.parse(`${label}T00:00:00Z`)
  if (/^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2})?$/.test(label)) {
    return Date.parse(`${label.replace(' ', 'T')}Z`)
  }
  return Date.parse(label)
}

function nextLabel(label: string, timestamp: number): string {
  const iso = new Date(timestamp).toISOString()
  if (label.length === 10) return iso.slice(0, 10)
  if (/^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2})?$/.test(label)) {
    return iso.slice(0, label.length).replace('T', label[10])
  }
  return iso
}

/** Insert explicit unknown samples between missing buckets, never invent zero usage. */
export function withTrendGaps(points: TrendDataPoint[], granularity: string): TrendDataPoint[] {
  const step = granularity === 'hour' ? 3600000 : 86400000
  const result: TrendDataPoint[] = []
  for (const point of points) {
    const previous = result.at(-1)
    const start = previous ? bucketTime(previous.date) : NaN
    const end = bucketTime(point.date)
    if (Number.isFinite(start) && Number.isFinite(end) && end - start > step * 1.5) {
      result.push({ date: nextLabel(previous!.date, start + step), requests: NaN, input_tokens: NaN,
        output_tokens: NaN, cache_creation_tokens: NaN, cache_read_tokens: NaN, total_tokens: NaN, cost: NaN, actual_cost: NaN })
    }
    result.push(point)
  }
  return result
}

export interface HistoryGap {
  reason: 'USAGE_HISTORY_INCOMPLETE'
  rawRetainedFrom?: string
}

type TranslateFn = (key: string, params?: Record<string, unknown>) => string

/** The API client rejects a flat business error, not an Axios response. */
export function historyGapFromError(error: unknown): HistoryGap | null {
  if (!error || typeof error !== 'object') return null
  const failure = error as { status?: number; reason?: string; metadata?: { raw_retained_from?: unknown } }
  if (failure.status !== 409 || failure.reason !== 'USAGE_HISTORY_INCOMPLETE') return null
  return {
    reason: 'USAGE_HISTORY_INCOMPLETE',
    rawRetainedFrom: typeof failure.metadata?.raw_retained_from === 'string'
      ? failure.metadata.raw_retained_from : undefined,
  }
}

/**
 * 历史缺口导致的失败提示：把服务端 409 守卫给出的精确原因与原始记录保留起点告诉用户，
 * 不再只报一句通用失败。缺口之外、或缺少时间的错误返回 undefined，由调用方决定通用文案。
 */
export function usageExportFailureMessage(error: unknown, t: TranslateFn): string | undefined {
  const gap = historyGapFromError(error)
  if (!gap) return undefined
  const reason = t('usage.exportHistoryIncomplete')
  return gap.rawRetainedFrom ? `${reason} ${t('dashboard.rawRetainedFrom', { time: gap.rawRetainedFrom })}` : reason
}
