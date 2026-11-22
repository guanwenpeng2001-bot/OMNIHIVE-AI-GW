import { describe, expect, it } from 'vitest'
import { identityKeyAllowsModel, isUsableIdentityKey } from '../keyPolicy'
import type { IdentityKey } from '../scope'
import type { CatalogModelItem } from '../catalogApi'

const key = (patch: Partial<IdentityKey> = {}): IdentityKey => ({ id: '15', name: 'Key', prefix: 'oh_p_', credential: null, owner_id: '1', version: '1', status: 'active', expires_at: null, evaluated_at: '2026-11-22T00:00:00Z', actions: ['text.chat'], model_access: { mode: 'inherit', ids: [] }, excluded_models: [], model_policy_source: 'model_access_rules', platform: 'openai', group_id: '7', ...patch })
const model = (patch: Partial<CatalogModelItem> = {}): CatalogModelItem => ({ id: 'm_11111111111111111111111111111111', slug: 'shared-name', displayName: 'Shared', provider: 'openai', actions: ['text.chat'], protocols: ['openai_chat'], modalities: ['text'], maxOutputTokens: null, contextTokens: null, toolsSupported: false, reasoningSupported: false, inputRate: null, outputRate: null, routeState: 'eligible', accessState: 'allowed', ...patch })

describe('server key lifecycle and effective policy', () => {
  it.each(['revoked', 'expired', 'inactive', 'disabled', 'future_unknown', undefined])('does not execute status %s', status => {
    expect(isUsableIdentityKey(key({ status: status as string }), 'text.chat')).toBe(false)
  })
  it('requires known expiry and action evidence', () => {
    expect(isUsableIdentityKey(key({ expires_at: undefined as unknown as null }), 'text.chat')).toBe(false)
    expect(isUsableIdentityKey(key({ expires_at: '2020-01-01T00:00:00Z' }), 'text.chat')).toBe(false)
    expect(isUsableIdentityKey(key({ actions: [] }), 'text.chat')).toBe(false)
    expect(isUsableIdentityKey(key({ model_access: { mode: 'deny_all', ids: [] } }), 'text.chat')).toBe(false)
    expect(isUsableIdentityKey(key(), 'text.chat')).toBe(true)
  })
  it('separates a shared native name across providers and honors inherit exclusions', () => {
    expect(identityKeyAllowsModel(key(), model({ provider: 'anthropic' }), 'text.chat')).toBe(false)
    expect(identityKeyAllowsModel(key({ excluded_models: ['shared-name'] }), model(), 'text.chat')).toBe(false)
    expect(identityKeyAllowsModel(key({ excluded_models: [model().id] }), model(), 'text.chat')).toBe(false)
    expect(identityKeyAllowsModel(key({ model_access: { mode: 'allow_list', ids: [model().id] } }), model(), 'text.chat')).toBe(true)
    expect(identityKeyAllowsModel(key(), model({ routeState: 'unknown' }), 'text.chat')).toBe(false)
  })

  it('keeps existing composite-group keys usable while applying action and model restrictions', () => {
    const composite = key({ platform: 'composite' })
    expect(identityKeyAllowsModel(composite, model(), 'text.chat')).toBe(true)
    expect(identityKeyAllowsModel(composite, model({ provider: 'anthropic' }), 'text.chat')).toBe(true)
    expect(identityKeyAllowsModel({ ...composite, actions: [] }, model(), 'text.chat')).toBe(false)
    expect(identityKeyAllowsModel({ ...composite, excluded_models: [model().id] }, model(), 'text.chat')).toBe(false)
    expect(identityKeyAllowsModel({ ...composite, model_access: { mode: 'deny_all', ids: [] } }, model(), 'text.chat')).toBe(false)
    expect(identityKeyAllowsModel(composite, model({ routeState: 'unknown' }), 'text.chat')).toBe(false)
  })
})
