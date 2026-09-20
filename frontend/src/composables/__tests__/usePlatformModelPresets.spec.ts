import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPlatformModelPresets, type PlatformModelPreset } from '@/api/admin/platformModelPresets'
import { usePlatformModelPresets } from '../usePlatformModelPresets'

vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: vi.fn() }))
const requestPresets = vi.mocked(getPlatformModelPresets)
const response = [
  { platform: 'openai', provider_profile: 'native', models: [{ id: 'server-openai' }] },
  { platform: 'gemini', provider_profile: 'native', models: [{ id: 'server-gemini' }] }
] as PlatformModelPreset[]

describe('platform preset source', () => {
  beforeEach(() => { requestPresets.mockReset().mockResolvedValue(response) })

  it('keeps successful empty data empty without reusing previous results', async () => {
    const scope = effectScope()
    const source = scope.run(() => usePlatformModelPresets(['openai']))!
    await flushPromises()
    expect(source.models.value).toEqual(['server-openai'])
    requestPresets.mockResolvedValueOnce([])
    await source.reload()
    expect(source.loaded.value).toBe(true)
    expect(source.models.value).toEqual([])
    expect(source.error.value).toBe('')
    scope.stop()
  })

  it('exposes normalized failures and retries without caching the failure', async () => {
    requestPresets.mockRejectedValueOnce({ message: 'server unavailable', reason: 'unavailable' })
    const scope = effectScope()
    const source = scope.run(() => usePlatformModelPresets(['openai']))!
    await flushPromises()
    expect(source.error.value).toBe('server unavailable')
    expect(source.models.value).toEqual([])
    expect(source.loaded.value).toBe(false)
    await source.reload()
    expect(source.error.value).toBe('')
    expect(source.models.value).toEqual(['server-openai'])
    scope.stop()
  })

  it('clears immediately on platform switches and ignores out-of-order responses including switch-back', async () => {
    const pending: Array<(presets: PlatformModelPreset[]) => void> = []
    requestPresets.mockImplementation(() => new Promise(resolve => { pending.push(resolve) }))
    const platforms = ref(['openai'])
    const scope = effectScope()
    const source = scope.run(() => usePlatformModelPresets(platforms))!
    platforms.value = ['gemini']
    pending[1](response)
    await flushPromises()
    expect(source.models.value).toEqual(['server-gemini'])
    platforms.value = ['openai']
    expect(source.models.value).toEqual([])
    pending[0](response)
    await flushPromises()
    expect(source.models.value).toEqual([])
    expect(source.loading.value).toBe(true)
    pending[2]([])
    await flushPromises()
    expect(source.models.value).toEqual([])
    scope.stop()
  })

  it('fences identity changes, closing and disposal, and never substitutes native hosted candidates', async () => {
    const identity = ref({ type: 'bedrock', profile: '', host: 'first', region: 'east', mode: 'native' })
    const active = ref(true)
    const scope = effectScope()
    const source = scope.run(() => usePlatformModelPresets(['openai'], identity, active))!
    await flushPromises()
    expect(source.models.value).toEqual([])
    identity.value.profile = 'native'
    await flushPromises()
    expect(source.models.value).toEqual(['server-openai'])
    let complete!: (presets: PlatformModelPreset[]) => void
    requestPresets.mockImplementation(() => new Promise(resolve => { complete = resolve }))
    identity.value.host = 'second'
    expect(source.models.value).toEqual([])
    active.value = false
    complete(response)
    await flushPromises()
    expect(source.models.value).toEqual([])
    active.value = true
    scope.stop()
    complete(response)
    await flushPromises()
    expect(source.models.value).toEqual([])
  })
})
