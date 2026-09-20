import { effectScope } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAccountOAuth } from '../useAccountOAuth'
import { useOpenAIOAuth } from '../useOpenAIOAuth'
import { useGeminiOAuth } from '../useGeminiOAuth'
import { useGrokOAuth } from '../useGrokOAuth'
import { useAntigravityOAuth } from '../useAntigravityOAuth'

const { generate, showError } = vi.hoisted(() => ({ generate: vi.fn(), showError: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { generateAuthUrl: generate },
    gemini: { generateAuthUrl: generate },
    grok: { generateAuthUrl: generate },
    antigravity: { generateAuthUrl: generate },
  },
}))

const providers = [
  { name: 'Claude', create: () => { const oauth = useAccountOAuth(); return { ...oauth, generate: () => oauth.generateAuthUrl('oauth') } } },
  { name: 'OpenAI', create: () => { const oauth = useOpenAIOAuth(); return { ...oauth, generate: () => oauth.generateAuthUrl() } } },
  { name: 'Gemini', create: () => { const oauth = useGeminiOAuth(); return { ...oauth, generate: () => oauth.generateAuthUrl(null) } } },
  { name: 'Grok', create: () => { const oauth = useGrokOAuth(); return { ...oauth, generate: () => oauth.generateAuthUrl(null) } } },
  { name: 'Antigravity', create: () => { const oauth = useAntigravityOAuth(); return { ...oauth, generate: () => oauth.generateAuthUrl(null) } } },
]
const response = { auth_url: 'https://example.com/oauth?state=sample', session_id: 'session', state: 'sample' }

describe.each(providers)('$name authorization lifecycle', ({ create }) => {
  beforeEach(() => { generate.mockReset(); showError.mockReset() })

  it('does not restore authorization state after reset or disposal', async () => {
    for (const dispose of [false, true]) {
      let resolveRequest!: (value: typeof response) => void
      generate.mockImplementationOnce(() => new Promise(resolve => { resolveRequest = resolve }))
      const scope = effectScope()
      const oauth = scope.run(create)!
      const pending = oauth.generate()
      if (dispose) scope.stop()
      else oauth.resetState()
      resolveRequest(response)
      expect(await pending).toBe(false)
      expect(oauth.authUrl.value).toBe('')
      expect(oauth.sessionId.value).toBe('')
      expect(oauth.loading.value).toBe(false)
      expect(showError).not.toHaveBeenCalled()
      scope.stop()
    }
  })

  it('does not let an old failure or finally clear a newer attempt', async () => {
    let rejectOld!: (reason: unknown) => void
    let resolveCurrent!: (value: typeof response) => void
    generate.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectOld = reject }))
      .mockImplementationOnce(() => new Promise(resolve => { resolveCurrent = resolve }))
    const scope = effectScope()
    const oauth = scope.run(create)!
    const oldRequest = oauth.generate()
    const currentRequest = oauth.generate()
    rejectOld(new Error('old failure'))
    expect(await oldRequest).toBe(false)
    expect(oauth.loading.value).toBe(true)
    expect(oauth.error.value).toBe('')
    expect(showError).not.toHaveBeenCalled()
    resolveCurrent(response)
    expect(await currentRequest).toBe(true)
    expect(oauth.authUrl.value).toBe(response.auth_url)
    expect(oauth.loading.value).toBe(false)
    scope.stop()
  })

  it.each(['ERR_CANCELED', 'AUTH_SESSION_CHANGED'])('silently settles %s', async code => {
    generate.mockRejectedValueOnce({ code })
    const scope = effectScope()
    const oauth = scope.run(create)!
    expect(await oauth.generate()).toBe(false)
    expect(oauth.error.value).toBe('')
    expect(oauth.loading.value).toBe(false)
    expect(showError).not.toHaveBeenCalled()
    scope.stop()
  })

  it('reports a current request failure once', async () => {
    generate.mockRejectedValueOnce(new Error('current failure'))
    const scope = effectScope()
    const oauth = scope.run(create)!
    expect(await oauth.generate()).toBe(false)
    expect(showError).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledWith(oauth.error.value)
    scope.stop()
  })
})
