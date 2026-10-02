import { describe, it, expect } from 'vitest'
import { usageScope } from '../usageScope'
import { formatReportDateTime } from '../format'
import type { UsageCoverage } from '@/types'

describe('usage scope contract', () => {
  const bounds = { window_from: '2026-09-12T00:00:00+08:00', window_until: '2026-10-02T13:00:00+08:00' }
  it.each([
    [undefined, 'unknown'], [{ complete: false }, 'unknown'], [{ complete: true }, 'all_time'],
    [{ complete: false, retained_complete: true, ...bounds }, 'retained'],
    [{ complete: false, range_complete: true, ...bounds }, 'range'],
    [{ complete: false, retained_complete: true }, 'unknown'],
    [{ retained_complete: true, ...bounds, window_until: 'bad' }, 'unknown'],
    [{ retained_complete: true, ...bounds, window_until: '2026-01-01T00:00:00Z' }, 'unknown'],
  ])('classifies %j as %s', (coverage, expected) => {
    expect(usageScope(coverage as UsageCoverage | undefined)).toBe(expected)
  })
})

it.each([
  ['Asia/Shanghai', '2026-09-12 00:00'],
  ['UTC', '2026-09-11 16:00'],
  ['America/Los_Angeles', '2026-09-11 09:00'],
])('formats scope bounds in %s', (zone, expected) => {
  expect(formatReportDateTime('2026-09-11T16:00:00Z', zone)).toBe(expected)
})
