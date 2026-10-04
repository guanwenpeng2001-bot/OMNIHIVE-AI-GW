import type { PlatformModelPreset } from '@/api/admin/platformModelPresets'
import { modelSyncErrorKey, validateModelRestrictions } from '../useModelWhitelist'
import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import { buildModelMappingObject, getModelsByPlatform, modelCandidateKey, getPresetMappingsByPlatform, splitModelMappingObject } from '../useModelWhitelist'

describe('useModelWhitelist', () => {
  it('excludes only retired editorial candidates without mutating metadata or saved mappings', () => {
    const models = [
      { id: 'retired', lifecycle: 'retired' },
      ...['active', 'preview', 'deprecated', 'unknown'].map(lifecycle => ({ id: lifecycle, lifecycle })),
      { id: 'missing-status' },
    ]
    const presets = [{ platform: 'openai', provider_profile: 'native', models }] as PlatformModelPreset[]
    const before = JSON.stringify(presets)
    const expected = ['active', 'preview', 'deprecated', 'unknown', 'missing-status']
    expect(getModelsByPlatform('openai', {}, presets)).toEqual(expected)
    expect(getPresetMappingsByPlatform('openai', {}, presets).map(mapping => mapping.from)).toEqual(expected)
    expect(JSON.stringify(presets)).toBe(before)
    const saved = { retired: 'retired', alias: 'retired' }
    const selection = splitModelMappingObject(saved)
    expect(buildModelMappingObject('combined', selection.allowedModels, selection.modelMappings)).toEqual(saved)
  })
  it('projects server candidates without inventing IDs or aliases', () => {
    const presets = [{ platform: 'openai', provider_profile: 'native', models: [{ id: 'server-model', display_name: 'Server model' }] }] as PlatformModelPreset[]
    expect(getModelsByPlatform('openai', {}, presets)).toEqual(['server-model'])
    expect(getPresetMappingsByPlatform('openai', {}, presets)).toEqual([
      expect.objectContaining({ label: 'Server model', from: 'server-model', to: 'server-model' })
    ])
    expect(getModelsByPlatform('gemini', {}, presets)).toEqual([])
    expect(getModelsByPlatform('openai', {}, [])).toEqual([])
    expect(getPresetMappingsByPlatform('openai', {}, [])).toEqual([])
  })

  it('combined 模式支持 Grok 4.5 官方别名映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['grok-4.5'],
      [
        { from: 'grok-latest', to: 'grok-4.5' },
        { from: 'grok-4.5-latest', to: 'grok-4.5' },
        { from: 'grok-build-latest', to: 'grok-4.5' }
      ]
    )

    expect(mapping).toEqual({
      'grok-4.5': 'grok-4.5',
      'grok-latest': 'grok-4.5',
      'grok-4.5-latest': 'grok-4.5',
      'grok-build-latest': 'grok-4.5'
    })
  })

  it('whitelist 模式会忽略通配符条目', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gemini-3.1-flash-image'], [])
    expect(mapping).toEqual({
      'gemini-3.1-flash-image': 'gemini-3.1-flash-image'
    })
  })

  it('whitelist 模式会保留 GPT-5.4 官方快照的精确映射', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-2026-03-05'], [])

    expect(mapping).toEqual({
      'gpt-5.4-2026-03-05': 'gpt-5.4-2026-03-05'
    })
  })

  it('whitelist keeps GPT-5.4 mini exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-mini'], [])

    expect(mapping).toEqual({
      'gpt-5.4-mini': 'gpt-5.4-mini'
    })
  })

  it('combined 模式会同时保留白名单身份映射和模型映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['gpt-5.4', 'claude-*'],
      [
        { from: 'gpt-latest', to: 'gpt-5.4' },
        { from: 'gpt-5.4', to: 'gpt-5.4-mini' }
      ]
    )

    expect(mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-mini',
      'gpt-latest': 'gpt-5.4'
    })
  })

  it('splitModelMappingObject 会把身份映射还原成白名单，其余保留为映射', () => {
    const parsed = splitModelMappingObject({
      'gpt-5.4': 'gpt-5.4',
      'gpt-latest': 'gpt-5.4',
      ' ': 'gpt-empty',
      broken: 123
    })

    expect(parsed).toEqual({
      allowedModels: ['gpt-5.4'],
      modelMappings: [{ from: 'gpt-latest', to: 'gpt-5.4' }, { from: '', to: 'gpt-empty' }, { from: 'broken', to: '' }]
    })
  })
})


describe('restriction validation', () => {
  it.each([
    [['bad*'], []],
    [[], [{ from: 'bad*source', to: 'model' }]],
    [[], [{ from: 'source', to: '' }]],
    [[], [{ from: 'source', to: 'target*' }]],
    [[], [{ from: 'source', to: 'one' }, { from: 'source', to: 'two' }]],
    [['source'], [{ from: 'source', to: 'other' }]]
  ])('reports the offending row for %j / %j', (allowed, mappings) => {
    expect(validateModelRestrictions('combined', allowed, mappings)[0]).toMatchObject({ row: expect.any(Number), code: expect.any(String) })
  })
  it('accepts case-sensitive identities and aliases', () => {
    expect(validateModelRestrictions('combined', ['Model'], [{ from: 'model-*', to: 'Model' }])).toEqual([])
  })
})


describe('identity-scoped model candidates', () => {
  it.each(['volcengine', 'qianfan', 'bedrock', 'unknown'])('never falls back to Claude for %s', platform => {
    expect(getModelsByPlatform(platform)).toEqual([])
    if (platform !== 'bedrock') expect(getPresetMappingsByPlatform(platform)).toEqual([])
  })
  it.each(['bedrock', 'service_account'])('does not use native Anthropic candidates for hosted type %s', type => {
    expect(getModelsByPlatform('anthropic', { type })).toEqual([])
    expect(getPresetMappingsByPlatform('anthropic', { type })).toEqual([])
  })
  it('keys every identity dimension independently', () => {
    const identity = { host: 'https://native.example', type: 'apikey', profile: 'native', region: 'east', mode: 'payg' }
    const baseline = modelCandidateKey('anthropic', identity)
    for (const field of Object.keys(identity)) expect(modelCandidateKey('anthropic', { ...identity, [field]: 'changed' })).not.toBe(baseline)
    expect(modelCandidateKey('other', identity)).not.toBe(baseline)
  })
})


it.each([
 ['configuration','Configuration'], ['credentials','Credentials'], ['account_type','AccountType'],
 ['not_adapted','NotAdapted'], ['discovery_unconfirmed','DiscoveryUnconfirmed'], ['endpoint_not_found','EndpointNotFound'],
 ['method_not_allowed','MethodNotAllowed'], ['permission','Permission'], ['rate_limited','RateLimited'],
 ['network','Network'], ['upstream','Unavailable'], ['internal','Failed'], ['unsupported','Unsupported'], ['unknown','Failed'],
])('maps discovery reason %s independently of the HTTP status', (reason, suffix) => {
 expect(modelSyncErrorKey({ response: { status: 400, data: { reason: `upstream_model_sync_${reason}` } } })).toBe(`syncUpstreamModels${suffix}`)
})
