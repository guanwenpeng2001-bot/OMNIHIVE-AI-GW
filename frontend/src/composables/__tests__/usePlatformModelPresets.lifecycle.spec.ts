import { effectScope } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { getPlatformModelPresets, type PlatformModelPreset } from '@/api/admin/platformModelPresets'
import { usePlatformModelPresets } from '../usePlatformModelPresets'

vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: vi.fn() }))

describe('preset lifecycle candidates', () => {
  it('excludes only explicit retirement from fill candidates and preserves historical metadata', async () => {
    const response = [{
      platform: 'openai', provider_profile: 'openai', models: [
        { id: 'retired-id', lifecycle: 'retired' },
        { id: 'active-id', lifecycle: 'active' },
        { id: 'preview-id', lifecycle: 'preview' },
        { id: 'deprecated-id', lifecycle: 'deprecated', retirement_date: '2099-11-30' },
        { id: 'unknown-id', lifecycle: 'unknown' },
        { id: 'legacy-without-status' }
      ]
    }] as PlatformModelPreset[]
    const original = JSON.stringify(response)
    vi.mocked(getPlatformModelPresets).mockResolvedValue(response)
    const scope = effectScope()
    try {
      const source = scope.run(() => usePlatformModelPresets(['openai']))!
      await flushPromises()
      expect(source.models.value).toEqual([
        'active-id', 'preview-id', 'deprecated-id', 'unknown-id', 'legacy-without-status'
      ])
      expect(source.presets.value[0].models.find(model => model.id === 'retired-id')).toBeDefined()
      expect(JSON.stringify(response)).toBe(original)
    } finally {
      scope.stop()
    }
  })
})
