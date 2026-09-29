import { describe, expect, it } from 'vitest'
import {
  CC_SWITCH_USAGE_SCRIPT,
  resolveCcSwitchImportConfig,
  buildCcSwitchImportDeeplink,
  preferredCcSwitchModel
} from '@/utils/ccswitchImport'
import type { GroupPlatform } from '@/types'

function paramsFromDeeplink(deeplink: string): URLSearchParams {
  const query = deeplink.split('?')[1] || ''
  return new URLSearchParams(query)
}

const baseInput = {
  baseUrl: 'https://api.example.com',
  providerName: 'Sub2API',
  apiKey: 'sk-test',
  usageScript: 'return true'
}

describe('ccswitchImport utils', () => {
  it('picks a Codex-family model from the key visible set', () => {
    expect(preferredCcSwitchModel(['text-embedding-3-large', 'gpt-5.5', 'gpt-5-mini'], 'codex')).toBe('gpt-5.5')
  })

  it('picks a Grok model from the key visible set', () => {
    expect(preferredCcSwitchModel(['grok-imagine-video', 'grok-4.5'], 'grok')).toBe('grok-4.5')
  })

  // Negative case: no visible model means no model parameter, never a constant.
  it.each([
    { name: 'empty list', models: [] as string[] },
    { name: 'null', models: null },
    { name: 'only media/utility models', models: ['text-embedding-3-large', 'tts-1', 'grok-imagine-image'] },
    { name: 'only other vendors', models: ['gemini-3-pro', 'claude-sonnet-4-6'] }
  ])('omits the model for $name', ({ models }) => {
    expect(preferredCcSwitchModel(models, 'codex')).toBeUndefined()
    expect(preferredCcSwitchModel(models, 'grok')).toBeUndefined()

    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform: 'openai',
        clientType: 'claude',
        visibleModels: models
      })
    )
    expect(params.has('model')).toBe(false)
  })

  it('adds the key-visible Codex model parameter for OpenAI imports', () => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform: 'openai',
        clientType: 'claude',
        visibleModels: ['gpt-5.5']
      })
    )

    expect(params.get('resource')).toBe('provider')
    expect(params.get('app')).toBe('codex')
    expect(params.get('endpoint')).toBe(baseInput.baseUrl)
    expect(params.get('model')).toBe('gpt-5.5')
    expect(params.get('apiKey')).toBe(baseInput.apiKey)
    expect(atob(params.get('usageScript') || '')).toBe(baseInput.usageScript)
  })

  it.each([
    'https://api.example.com',
    'https://api.example.com/',
    'https://api.example.com/v1',
    'https://api.example.com/v1/'
  ])('imports Grok Build with one /v1 suffix for base URL %s', (baseUrl) => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        baseUrl,
        platform: 'grok',
        clientType: 'claude',
        visibleModels: ['grok-4.5']
      })
    )

    expect(params.get('app')).toBe('grokbuild')
    expect(params.get('endpoint')).toBe('https://api.example.com/v1')
    expect(params.get('model')).toBe('grok-4.5')
  })

  it.each([
    { platform: 'anthropic' as GroupPlatform, clientType: 'claude' as const, app: 'claude' },
    { platform: 'gemini' as GroupPlatform, clientType: 'gemini' as const, app: 'gemini' }
  ])('does not add a model parameter for $platform imports', ({ platform, clientType, app }) => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform,
        clientType,
        visibleModels: ['gpt-5.5', 'grok-4.5']
      })
    )

    expect(params.get('app')).toBe(app)
    expect(params.get('endpoint')).toBe(baseInput.baseUrl)
    expect(params.has('model')).toBe(false)
  })

  it('keeps Antigravity imports on the selected client endpoint without a model parameter', () => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform: 'antigravity',
        clientType: 'gemini',
        visibleModels: ['gemini-3-pro']
      })
    )

    expect(params.get('app')).toBe('gemini')
    expect(params.get('endpoint')).toBe(`${baseInput.baseUrl}/antigravity`)
    expect(params.has('model')).toBe(false)
  })
})

describe('CC Switch usage script', () => {
  // Mirrors CC Switch: substitute the template vars as text, evaluate, read request.url.
  function usageUrlFor(baseUrl: string): string {
    const script = CC_SWITCH_USAGE_SCRIPT.split('{{baseUrl}}').join(baseUrl).split('{{apiKey}}').join('sk-test')
    const config = new Function(`return ${script}`)() as { request: { url: string } }
    return config.request.url
  }

  it.each([
    'https://api.example.com',
    'https://api.example.com/',
    'https://api.example.com/v1',
    'https://api.example.com/v1/'
  ])('queries exactly one /v1/usage for base URL %s', (baseUrl) => {
    expect(usageUrlFor(baseUrl)).toBe('https://api.example.com/v1/usage')
  })

  it('works against the endpoint every platform import stores', () => {
    for (const platform of ['anthropic', 'openai', 'grok', 'gemini'] as GroupPlatform[]) {
      const endpoint = paramsFromDeeplink(
        buildCcSwitchImportDeeplink({
          baseUrl: 'https://api.example.com',
          platform,
          clientType: platform === 'gemini' ? 'gemini' : 'claude',
          providerName: 'Sub2API',
          apiKey: 'sk-test',
          usageScript: CC_SWITCH_USAGE_SCRIPT
        })
      ).get('endpoint') as string
      expect(usageUrlFor(endpoint)).toBe('https://api.example.com/v1/usage')
    }
  })
})

it.each(['https://api.example.com/', 'https://api.example.com///'])('keeps Codex at the provider root for %s', (baseUrl) => {
  expect(resolveCcSwitchImportConfig('openai', 'claude', baseUrl, ['gpt-5']).endpoint).toBe('https://api.example.com')
})
