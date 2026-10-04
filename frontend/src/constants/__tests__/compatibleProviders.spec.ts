import { describe, expect, it } from 'vitest'
import { buildCompatibleCredentials, compatibleMode, compatibleProvider, compatibleDiscoveryDisabled, type CompatibleProvider } from '@/constants/compatibleProviders'

const provider: CompatibleProvider = {
  id: 'example-cloud', label: 'Example Cloud', label_zh: '示例云', base_source: 'preset', default_mode: 'coding',
  evidence_url: 'https://docs.example.com', evidence_observed: '2026-10-03',
  modes: [{ mode: 'coding', default_protocol: 'anthropic', endpoints: [
    { protocol: 'anthropic', base_url: 'https://example.com/messages', auth: 'x_api_key' },
    { protocol: 'chat_completions', base_url: 'https://example.com/v1', auth: 'bearer' }
  ], bridges: ['responses_to_chat'], discovery: 'none', capability_profile: 'example-cloud' }]
}

describe('compatible account credentials', () => {
  it('uses the declared coding-only default and rejects unsupported modes and protocols', () => {
    expect(compatibleMode(provider)?.mode).toBe('coding')
    expect(compatibleMode(provider, 'payg')).toBeUndefined()
    expect(() => buildCompatibleCredentials(provider, { account_mode: 'payg' })).toThrow()
    expect(() => buildCompatibleCredentials(provider, { api_protocol: 'responses' })).toThrow()
    expect(buildCompatibleCredentials(provider, {})).toMatchObject({ account_mode: 'coding', api_protocol: 'adaptive' })
  })
  it('preserves custom bases, key references, models and proxy metadata on edit', () => {
    const saved = { account_mode: 'coding', api_protocol: 'anthropic', base_url: 'https://proxy.example.com', api_base_urls: { anthropic: 'https://proxy.example.com/messages' }, api_key_ref: 'vault:key', model_mapping: { public: 'deployment' }, proxy_id: 8 }
    expect(buildCompatibleCredentials(provider, saved)).toEqual(saved)
  })
  it('requires an account resource base without adding a deployment credential', () => {
    const resource: CompatibleProvider = { ...provider, base_source: 'account', modes: [{ ...provider.modes[0], endpoints: [{ protocol: 'anthropic', path: '/openai/v1', auth: 'api_key_header' }] }] }
    expect(() => buildCompatibleCredentials(resource, {})).toThrow()
    expect(buildCompatibleCredentials(resource, { base_url: 'https://resource.example.com', model_mapping: { public: 'deployment-name' } })).toEqual({ base_url: 'https://resource.example.com', model_mapping: { public: 'deployment-name' }, account_mode: 'coding', api_protocol: 'adaptive' })
  })
})


it.each(['qiniu', 'dmxapi', 'aihubmix'])('enables only the documented authenticated list for %s payg', id => {
 const provider = compatibleProvider(id)!
 const mode = compatibleMode(provider, 'payg')!
 expect(mode.discovery).toBe('openai_models')
 expect(mode.discovery_protocol).toBe('chat_completions')
 expect(compatibleDiscoveryDisabled(provider, { account_mode: 'payg' })).toBe(false)
})
it.each([['qiniu', 'subscription'], ['dmxapi_com', 'payg'], ['dmxapi_ssvip', 'payg'], ['atlascloud', 'payg'], ['atlascloud', 'coding'], ['a6api', 'payg']])('keeps discovery closed for %s %s', (id, mode) => {
 expect(compatibleDiscoveryDisabled(compatibleProvider(id)!, { account_mode: mode })).toBe(true)
})
