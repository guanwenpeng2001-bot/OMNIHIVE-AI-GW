import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPlatformModelPresets, type PlatformModelPreset } from '@/api/admin/platformModelPresets'
import { selectPlatformModelPresets, usePlatformModelPresets } from '../usePlatformModelPresets'

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

const profileFixtures = Object.entries({
  kimi: ['moonshotai', 'moonshotai-cn', 'kimi-for-coding'],
  zhipu: ['zhipuai', 'zhipuai-coding-plan', 'zai', 'zai-coding-plan'],
  minimax: ['minimax', 'minimax-cn', 'minimax-coding-plan', 'minimax-cn-coding-plan'],
  qianwen: ['alibaba', 'alibaba-cn', 'alibaba-coding-plan', 'alibaba-coding-plan-cn'],
  gemini: ['google', 'vertex-gemini'], anthropic: ['anthropic', 'vertex-anthropic'],
  deepseek: ['deepseek'], openai: ['openai'], grok: ['xai'], bedrock: ['native']
}).flatMap(([platform, profiles]) => profiles.map(provider_profile => ({
  platform, provider_profile, models: [{ id: provider_profile + '-fixture' }]
}))) as PlatformModelPreset[]

describe('account profile selection', () => {
  it.each([
    ['kimi', '', '', 'moonshotai-cn'],
    ['kimi', '', 'coding', 'kimi-for-coding'],
    ['kimi', 'https://api.moonshot.ai/v1', 'payg', 'moonshotai'],
    ['kimi', 'https://API.KIMI.COM/coding/v1/', '', 'kimi-for-coding'],
    ['zhipu', '', '', 'zhipuai'],
    ['zhipu', '', 'coding', 'zhipuai-coding-plan'],
    ['zhipu', 'https://api.z.ai/api/paas/v4', '', 'zai'],
    ['zhipu', 'https://api.z.ai/api/coding/paas/v4', '', 'zai-coding-plan'],
    ['zhipu', 'https://open.bigmodel.cn/api/anthropic', 'coding', 'zhipuai-coding-plan'],
    ['minimax', '', '', 'minimax-cn'],
    ['minimax', '', 'coding', 'minimax-cn-coding-plan'],
    ['minimax', 'https://api.minimax.io/v1', '', 'minimax'],
    ['minimax', 'https://api.minimax.io/anthropic', 'coding', 'minimax-coding-plan'],
    ['qianwen', '', '', 'alibaba-cn'],
    ['qianwen', '', 'coding', 'alibaba-coding-plan-cn'],
    ['qianwen', 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', '', 'alibaba'],
    ['qianwen', 'https://coding.dashscope.aliyuncs.com/apps/anthropic', '', 'alibaba-coding-plan-cn'],
    ['qianwen', 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', 'coding', ''],
    ['qianwen', 'https://coding-intl.dashscope.aliyuncs.com/v1', 'coding', ''],
    ['kimi', 'https://api.kimi.com/coding/private', '', ''],
    ['kimi', 'https://api.moonshot.cn/v1', 'coding', ''],
    ['zhipu', 'https://api.z.ai.example/api/paas/v4', '', ''],
    ['minimax', 'https://api.minimax.io/private', '', ''],
    ['qianwen', 'https://relay.example/v1', '', '']
  ])('%s %s %s selects only %s', (platform, host, mode, profile) => {
    expect(selectPlatformModelPresets(profileFixtures, [platform], { host, mode })
      .map(p => p.provider_profile)).toEqual(profile ? [profile] : [])
  })

  it('honors explicit candidates, even on a custom host, without falling back for unknown profiles', () => {
    for (const platform of ['kimi', 'zhipu', 'minimax', 'qianwen']) {
      for (const preset of profileFixtures.filter(p => p.platform === platform)) {
        expect(selectPlatformModelPresets(profileFixtures, [platform], { profile: preset.provider_profile, host: 'https://relay.example', mode: 'coding' })).toEqual([preset])
      }
      expect(selectPlatformModelPresets(profileFixtures, [platform], { profile: 'missing' })).toEqual([])
    }
  })

  it('keeps hosted candidates scoped to existing profiles and bedrock empty', () => {
    for (const [platform, profile] of [['gemini', 'vertex-gemini'], ['vertex', 'vertex-gemini'], ['anthropic', 'vertex-anthropic']]) {
      expect(selectPlatformModelPresets(profileFixtures, [platform], { type: 'service_account', region: 'us-central1' }).map(p => p.provider_profile)).toEqual([profile])
      expect(selectPlatformModelPresets(profileFixtures.filter(p => !p.provider_profile.startsWith('vertex-')), [platform], { type: 'service_account' })).toEqual([])
    }
    expect(selectPlatformModelPresets(profileFixtures, ['bedrock'], { type: 'bedrock' })).toEqual([])
    expect(selectPlatformModelPresets(profileFixtures, ['anthropic'], { type: 'bedrock' })).toEqual([])
    for (const platform of ['openai', 'grok', 'deepseek']) {
      expect(selectPlatformModelPresets(profileFixtures, [platform], { type: 'oauth' })).toHaveLength(1)
    }
  })

  it.each(['create', 'edit'])('%s caller reacts to host/mode/profile/region without changing saved mappings', async () => {
    requestPresets.mockResolvedValue(profileFixtures)
    const account = ref({ platform: 'minimax', type: 'apikey', credentials: { base_url: '', provider_profile: '', account_mode: 'payg', region: '', model_mapping: { saved: 'historical-id' } } })
    const before = JSON.stringify(account.value.credentials.model_mapping)
    const scope = effectScope()
    const source = scope.run(() => usePlatformModelPresets(() => [account.value.platform], () => ({
      host: account.value.credentials.base_url, type: account.value.type,
      profile: account.value.credentials.provider_profile, mode: account.value.credentials.account_mode,
      region: account.value.credentials.region
    })))!
    await flushPromises()
    expect(source.models.value).toEqual(['minimax-cn-fixture'])
    account.value.credentials.base_url = 'https://api.minimax.io/v1'
    await flushPromises()
    expect(source.models.value).toEqual(['minimax-fixture'])
    account.value.credentials.account_mode = 'coding'
    await flushPromises()
    expect(source.models.value).toEqual(['minimax-coding-plan-fixture'])
    account.value.credentials.region = 'cn-beijing'
    await flushPromises()
    expect(source.models.value).toEqual(['minimax-coding-plan-fixture'])
    account.value.credentials.provider_profile = 'minimax-cn'
    await flushPromises()
    expect(source.models.value).toEqual(['minimax-cn-fixture'])
    expect(JSON.stringify(account.value.credentials.model_mapping)).toBe(before)
    scope.stop()
  })
})
