import { describe, expect, it } from 'vitest'
import { normalizeConfig } from '../normalizeConfig'
import type { MonitorConfig } from '@/api/channelTelemetry'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'

describe('monitor config normalization', () => {
  it('is idempotent, fills disabled rows and preserves existing and extension dimensions', () => {
    const existing = { platform: 'openai', enabled: false, models: ['old-model'], dimensions: { region: 'legacy' } }
    const config = { enabled: false, platforms: [existing, { platform: 'future', enabled: true, models: ['future-model'] }], health_thresholds: { minimum_sample: 0 }, ignored_error_categories: [], extension: { key: 1 } } as unknown as MonitorConfig
    const original = JSON.stringify(config)
    const normalized = normalizeConfig(config)
    expect(normalizeConfig(normalized)).toEqual(normalized)
    expect(JSON.stringify(config)).toBe(original)
    expect(normalized.platforms[0]).toEqual(existing)
    expect(normalized.platforms[1]).toEqual(config.platforms[1])
    expect(normalized.enabled).toBe(false)
    expect(normalized.ignored_error_categories).toEqual([])
    expect(normalized.health_thresholds.minimum_sample).toBe(0)
    expect(normalized).toHaveProperty('extension', { key: 1 })
    for (const { value } of CONCRETE_PLATFORM_OPTIONS.filter(p => p.value !== 'openai')) expect(normalized.platforms.find(p => p.platform === value)).toEqual({ platform: value, enabled: false, models: [] })
  })
})
