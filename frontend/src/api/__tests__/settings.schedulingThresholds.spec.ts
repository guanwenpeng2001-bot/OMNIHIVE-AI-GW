import { describe, expect, it, vi } from 'vitest'
import { normalizeAccountSchedulingThresholdsMap, sanitizeAccountSchedulingThresholdsMap, SCHEDULING_THRESHOLD_PLATFORMS } from '../admin/settings'

vi.mock('../client', () => ({ apiClient: {} }))

describe('Cursor scheduling threshold settings', () => {
  it('exposes Cursor and preserves its configured threshold when loading and saving', () => {
    expect(SCHEDULING_THRESHOLD_PLATFORMS).toContain('cursor')
    expect(normalizeAccountSchedulingThresholdsMap().cursor).toBe(100)
    const loaded = normalizeAccountSchedulingThresholdsMap({ cursor: 80 })
    expect(loaded.cursor).toBe(80)
    expect(sanitizeAccountSchedulingThresholdsMap(loaded).cursor).toBe(80)
    expect(SCHEDULING_THRESHOLD_PLATFORMS).not.toContain('qianwen')
    expect(SCHEDULING_THRESHOLD_PLATFORMS).not.toContain('commandcode')
  })
})
