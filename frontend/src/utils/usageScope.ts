import type { UsageCoverage } from '@/types'

/** Old or incomplete payloads never imply lifetime coverage. */
export function usageScope(coverage?: UsageCoverage): 'all_time' | 'retained' | 'range' | 'unknown' {
  if (coverage?.complete === true) return 'all_time'
  const from = Date.parse(coverage?.window_from ?? '')
  const until = Date.parse(coverage?.window_until ?? '')
  if (!Number.isFinite(from) || !Number.isFinite(until) || until < from) return 'unknown'
  if (coverage?.retained_complete === true) return 'retained'
  if (coverage?.range_complete === true) return 'range'
  return 'unknown'
}
