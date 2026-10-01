import { beforeEach, describe, expect, it, vi } from 'vitest'
const api = vi.hoisted(() => ({ accounts: { generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), refreshOpenAIToken: vi.fn() }, gemini: { generateAuthUrl: vi.fn(), exchangeCode: vi.fn() }, grok: { generateAuthUrl: vi.fn(), exchangeCode: vi.fn() }, antigravity: { generateAuthUrl: vi.fn(), exchangeCode: vi.fn() } }))
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import { useAccountOAuth } from '../useAccountOAuth'
import { useOpenAIOAuth } from '../useOpenAIOAuth'
import { useGeminiOAuth } from '../useGeminiOAuth'
import { useAntigravityOAuth } from '../useAntigravityOAuth'
import { useGrokOAuth } from '../useGrokOAuth'

describe('logical group authorization', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    Object.values(api).forEach(provider => provider.generateAuthUrl.mockResolvedValue({ auth_url: 'https://auth.example/?state=s', session_id: 'pending-1', state: 's' }))
  })
  it.each([['Anthropic', () => useAccountOAuth(), api.accounts], ['OpenAI', () => useOpenAIOAuth(), api.accounts], ['Gemini', () => useGeminiOAuth(), api.gemini], ['Antigravity', () => useAntigravityOAuth(), api.antigravity], ['Grok', () => useGrokOAuth(), api.grok]] as const)('%s sends the logical group without a node ID', async (name, make, provider) => {
    const flow = make()
    if (name === 'Anthropic') await (flow as ReturnType<typeof useAccountOAuth>).generateAuthUrl('oauth', { proxy_group_id: 71 })
    else await (flow as ReturnType<typeof useOpenAIOAuth>).generateAuthUrl({ proxy_group_id: 71 })
    const args = provider.generateAuthUrl.mock.calls[0]
    const payload = args.at(-1)
    expect(payload).toEqual({ proxy_group_id: 71 })
    expect(payload).not.toHaveProperty('proxy_id')
  })
  it('keeps no-proxy authorization empty and preserves legacy numeric node calls', async () => {
    const flow = useOpenAIOAuth()
    await flow.generateAuthUrl(null)
    expect(api.accounts.generateAuthUrl).toHaveBeenLastCalledWith('/admin/openai/generate-auth-url', {})
    await flow.generateAuthUrl(17)
    expect(api.accounts.generateAuthUrl).toHaveBeenLastCalledWith('/admin/openai/generate-auth-url', { proxy_id: 17 })
  })
  it('keeps the logical group in the code exchange instead of inventing a resolved node', async () => {
    api.grok.exchangeCode.mockResolvedValue({ access_token: 'test' })
    await useGrokOAuth().exchangeAuthCode({ code: ' c ', state: 's', sessionId: 'pending-1', proxyId: { proxy_group_id: 71 } })
    expect(api.grok.exchangeCode).toHaveBeenCalledWith({ code: 'c', state: 's', session_id: 'pending-1', proxy_group_id: 71 })
  })
})
