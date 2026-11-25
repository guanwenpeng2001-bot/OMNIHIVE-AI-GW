import type { PlatformModelPreset } from '@/api/admin/platformModelPresets'
import { modelSyncErrorKey, validateModelRestrictions } from '../useModelWhitelist'
import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import {
  accountModelAllowlist,
  buildAccountWorkflowModelPayload,
  buildExplicitModelMappingObject,
  buildModelMappingObject,
  buildUpdateModelAllowlistRequest,
  getModelsByPlatform,
  modelCandidateKey,
  getPresetMappingsByPlatform,
  isLegacySystemFreezeAllowlist,
  mergeMappingTargetsIntoModels,
  normalizeAccountModelSelection,
  splitModelMappingObject
} from '../useModelWhitelist'

describe('useModelWhitelist', () => {
  it('projects GPT-6.2 only from matching server presets and preserves exact restrictions', () => {
    const models = ['gpt-6.2-sol', 'gpt-6.2-astra'].map(id => ({ id, display_name: id, lifecycle: 'active' }))
    const presets = [{ platform: 'openai', provider_profile: 'native', models }] as PlatformModelPreset[]
    expect(getModelsByPlatform('openai', {}, presets)).toEqual(models.map(model => model.id))
    expect(getModelsByPlatform('openai', {}, [])).toEqual([])
    expect(getModelsByPlatform('grok', {}, presets)).toEqual([])
    expect(getPresetMappingsByPlatform('openai', {}, presets).map(mapping => mapping.to)).toEqual(models.map(model => model.id))
    const saved = { 'gpt-6.2-sol': 'gpt-6.2-sol', 'gpt-6.2-astra': 'gpt-6.2-astra', 'omnihive-alias': 'gpt-6.2-sol' }
    const split = splitModelMappingObject(saved)
    expect(buildModelMappingObject('combined', split.allowedModels, split.modelMappings)).toEqual(saved)
  })
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

describe('v0.3.9 unified account model selection helpers', () => {
  it('normalizes an account with scope models and empty model_mapping as restricted (never deny_all)', () => {
    const freeAccount = {
      platform: 'openai',
      credentials: { model_mapping: {} },
      extra: {
        upstream_model_allowlist: {
          enabled: true,
          models: ['openrouter/free', 'opencode/zen-free'],
          reason: '免费模型范围'
        }
      }
    }
    expect(normalizeAccountModelSelection({ account: freeAccount })).toEqual({
      mode: 'restricted',
      models: ['openrouter/free', 'opencode/zen-free'],
      mappings: [],
      reason: '免费模型范围'
    })
  })

  it('normalizes legacy system_upgrade_baseline_freeze as inherit instead of deny_all', () => {
    const frozenAccount = {
      platform: 'openai',
      credentials: { model_mapping: {} },
      extra: {
        upstream_model_allowlist: {
          enabled: true,
          models: [],
          reason: 'system_upgrade_baseline_freeze',
          confirmed_by: 0,
          confirmed_at: ''
        }
      }
    }
    expect(isLegacySystemFreezeAllowlist(accountModelAllowlist(frozenAccount), frozenAccount.extra)).toBe(true)
    expect(normalizeAccountModelSelection({ account: frozenAccount })).toEqual({
      mode: 'inherit',
      models: [],
      mappings: [],
      reason: ''
    })
  })

  it('normalizes explicit admin deny_all with reason and distinguishes it from inherit', () => {
    const deniedAccount = {
      platform: 'anthropic',
      credentials: { model_mapping: {} },
      extra: {
        upstream_model_allowlist: {
          enabled: true,
          models: [],
          reason: '上游额度耗尽暂停',
          confirmed_by: 7,
          confirmed_at: '2026-11-25T16:00:00Z'
        }
      }
    }
    expect(normalizeAccountModelSelection({ account: deniedAccount })).toEqual({
      mode: 'deny_all',
      models: [],
      mappings: [],
      reason: '上游额度耗尽暂停'
    })
    expect(normalizeAccountModelSelection({ account: { credentials: {}, extra: {} } })).toEqual({
      mode: 'inherit',
      models: [],
      mappings: [],
      reason: ''
    })
  })

  it('merges mapping targets into models and builds explicit mapping objects without identity entries', () => {
    const mappings = [
      { from: 'public-alias', to: 'vendor/model-b' },
      { from: 'claude-*', to: 'vendor/model-c' }
    ]
    expect(mergeMappingTargetsIntoModels(['vendor/model-a'], mappings)).toEqual([
      'vendor/model-a',
      'vendor/model-b',
      'vendor/model-c'
    ])
    expect(buildExplicitModelMappingObject(mappings)).toEqual({
      'public-alias': 'vendor/model-b',
      'claude-*': 'vendor/model-c'
    })
  })

  it('builds allowlist requests for inherit, restricted and deny_all modes', () => {
    expect(
      buildUpdateModelAllowlistRequest({
        mode: 'inherit',
        models: ['ignored'],
        mappings: [],
        expectedRulesRevision: '12'
      })
    ).toEqual({
      mode: 'inherit',
      enabled: false,
      models: [],
      reason: '',
      expected_rules_revision: '12',
      model_mapping: {}
    })

    expect(
      buildUpdateModelAllowlistRequest({
        mode: 'restricted',
        models: ['openrouter/free'],
        mappings: [],
        reason: '免费模型',
        expectedRulesRevision: '13'
      })
    ).toEqual({
      mode: 'restricted',
      enabled: true,
      models: ['openrouter/free'],
      reason: '免费模型',
      expected_rules_revision: '13',
      model_mapping: { 'openrouter/free': 'openrouter/free' }
    })

    expect(
      buildUpdateModelAllowlistRequest({
        mode: 'deny_all',
        models: ['ignored'],
        mappings: [{ from: 'a', to: 'b' }],
        reason: ' 维护停用 ',
        expectedRulesRevision: '14'
      })
    ).toEqual({
      mode: 'deny_all',
      enabled: true,
      models: [],
      reason: '维护停用',
      confirm_deny_all: true,
      expected_rules_revision: '14',
      model_mapping: {}
    })
  })

  it('builds workflow model payloads without sending empty model_mapping on inherit by default', () => {
    expect(
      buildAccountWorkflowModelPayload({
        mode: 'inherit',
        models: [],
        mappings: []
      })
    ).toEqual({
      mode: 'inherit',
      models: []
    })
    expect(
      buildAccountWorkflowModelPayload({
        mode: 'inherit',
        models: [],
        mappings: [],
        clearMappingsOnInherit: true
      })
    ).toEqual({
      mode: 'inherit',
      models: [],
      model_mapping: {}
    })
    expect(
      buildAccountWorkflowModelPayload({
        mode: 'restricted',
        models: ['model-a'],
        mappings: [{ from: 'alias-b', to: 'model-b' }]
      })
    ).toEqual({
      mode: 'restricted',
      models: ['model-a', 'model-b'],
      model_mapping: {
        'model-a': 'model-a',
        'alias-b': 'model-b'
      }
    })
  })

  it('validates emptyRestrictedSelection, denyAllReasonRequired and new error keys', () => {
    expect(
      validateModelRestrictions('restricted', [], [], { requireNonEmptyRestricted: true })
    ).toEqual([{ code: 'emptyRestrictedSelection', row: 1, model: '', from: '', to: '' }])
    expect(
      validateModelRestrictions('deny_all', [], [], { reason: '   ' })
    ).toEqual([{ code: 'denyAllReasonRequired', row: 1, model: '', from: '', to: '' }])
    expect(
      validateModelRestrictions('deny_all', [], [], { reason: '维护暂停' })
    ).toEqual([])
    expect(
      modelSyncErrorKey({ response: { status: 409, data: { reason: 'MODEL_SELECTION_DENIED' } } })
    ).toBe('modelSync.deniedError')
    expect(
      modelSyncErrorKey({ response: { status: 409, data: { reason: 'MODEL_SELECTION_CONFLICT' } } })
    ).toBe('modelSync.conflict')
    expect(
      modelSyncErrorKey({ response: { status: 400, data: { reason: 'MODEL_SELECTION_REASON_REQUIRED' } } })
    ).toBe('modelValidation.denyAllReasonRequired')
  })
})
