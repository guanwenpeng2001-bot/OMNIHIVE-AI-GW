import { describe, expect, it } from 'vitest'
import { buildNativeProfileBases, nativeProfileBases, NATIVE_PROFILE_PRESETS } from '../nativeProfilePresets'
import { CN_BASE_URL_PRESETS, defaultCNAdaptiveBaseUrls } from '../credentialsBuilder'

describe('native profile boundaries', () => {
  it('exposes exact protocol roots without new platform identities', () => {
    expect(nativeProfileBases('zhipu', 'coding', 'https://api.z.ai/api/coding/paas/v4')).toEqual({
      chat_completions: 'https://api.z.ai/api/coding/paas/v4', anthropic: 'https://api.z.ai/api/anthropic', responses: 'https://api.z.ai/api/v1'
    })
    expect(nativeProfileBases('qianfan', 'coding', 'https://qianfan.baidubce.com/v2/coding')).not.toHaveProperty('responses')
    expect(nativeProfileBases('qianwen', 'coding', 'https://coding-intl.dashscope.aliyuncs.com/v1')).not.toHaveProperty('responses')
    expect(nativeProfileBases('kimi', 'payg', 'https://api.kimi.ai/coding/v1')).toBeUndefined()
    expect(nativeProfileBases('kimi', 'coding', 'https://api.kimi.ai.evil.test/coding/v1')).toBeUndefined()
    for (const [platform, presets] of Object.entries(NATIVE_PROFILE_PRESETS)) {
      expect(Object.keys(CN_BASE_URL_PRESETS)).toContain(platform)
      for (const preset of presets) expect(CN_BASE_URL_PRESETS[platform as keyof typeof CN_BASE_URL_PRESETS]).toContainEqual(expect.objectContaining({ mode: preset.mode, protocol: preset.protocol, url: preset.url }))
    }
  })
  it('creates a global protocol map without domestic defaults', () => {
    const defaults = defaultCNAdaptiveBaseUrls('kimi', 'coding')
    expect(buildNativeProfileBases('kimi', 'coding', 'https://api.kimi.ai/coding/v1', defaults, defaults)).toEqual({
      chat_completions: 'https://api.kimi.ai/coding/v1', anthropic: 'https://api.kimi.ai/coding', responses: 'https://api.kimi.ai/coding/v1'
    })
  })
  it('preserves explicit stored protocol values, invisible mappings and key references on edit', () => {
    const existing = { anthropic: 'https://relay.example/messages', extension: 'kept' }
    const credentials = { api_key_ref: 'vault:kept', model_mapping: { public: 'deployment' }, proxy_id: 7 }
    const defaults = defaultCNAdaptiveBaseUrls('qianwen', 'coding')
    const result = buildNativeProfileBases('qianwen', 'coding', 'https://coding-intl.dashscope.aliyuncs.com/v1', {}, defaults, existing)
    expect(result.anthropic).toBe(existing.anthropic)
    expect(result.extension).toBe('kept')
    expect(result).not.toHaveProperty('responses')
    expect(existing).toEqual({ anthropic: 'https://relay.example/messages', extension: 'kept' })
    expect(credentials).toEqual({ api_key_ref: 'vault:kept', model_mapping: { public: 'deployment' }, proxy_id: 7 })
  })
  it('serializes a Responses/Messages-only token profile without inventing Chat', () => {
    const draft = { anthropic: 'https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic', responses: 'https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1' }
    expect(buildNativeProfileBases('qianwen', 'coding', '', draft, defaultCNAdaptiveBaseUrls('qianwen', 'coding'))).toEqual(draft)
    expect(buildNativeProfileBases('kimi', 'payg', '', { anthropic: 'https://relay.example/messages' }, defaultCNAdaptiveBaseUrls('kimi', 'payg'))).toEqual({ anthropic: 'https://relay.example/messages' })
  })
  it('does not attach official defaults to custom or mismatched-mode URLs', () => {
    const defaults = defaultCNAdaptiveBaseUrls('kimi', 'payg')
    for (const base of ['https://relay.example/v1', 'https://api.kimi.ai/coding/v1']) {
      expect(buildNativeProfileBases('kimi', 'payg', base, defaults, defaults)).toEqual({ chat_completions: base })
    }
  })
})
