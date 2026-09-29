// @vitest-environment node
import { beforeAll, describe, expect, it } from 'vitest'
import { loadConfigFromFile, type ProxyOptions } from 'vite'
import { resolve } from 'node:path'

let proxies: Record<string, string | ProxyOptions>

beforeAll(async () => {
  const config = await loadConfigFromFile(
    { command: 'serve', mode: 'development' },
    resolve(__dirname, '../../vite.config.ts'),
  )
  expect(config).not.toBeNull()
  proxies = config!.config.server!.proxy!
})

// Vite treats keys beginning with ^ as regexes and all other keys as prefixes.
function matchingProxy(url: string) {
  const entry = Object.entries(proxies).find(([context]) =>
    context.startsWith('^') ? new RegExp(context).test(url) : url.startsWith(context),
  )
  return entry?.[1]
}

describe('development setup routing', () => {
  it.each([
    '/setup',
    '/setup/',
    '/setup?step=database',
    '/setup/?step=database',
    '/setup-wizard',
    '/setup/status-page',
    '/setup/installing',
    '/setup/status/details',
  ])('leaves %s available to the SPA fallback', (url) => {
    expect(matchingProxy(url)).toBeUndefined()
  })

  it.each(['status', 'test-db', 'test-redis', 'install'])(
    'proxies the %s API, including query strings and trailing slashes', (endpoint) => {
      const apiProxy = proxies['/api'] as ProxyOptions
      for (const suffix of ['', '?check=1', '/', '/?check=1']) {
        const proxy = matchingProxy(`/setup/${endpoint}${suffix}`) as ProxyOptions
        expect(proxy).toMatchObject({ target: apiProxy.target, changeOrigin: true })
        expect(proxy.rewrite).toBeUndefined()
      }
    },
  )

  it('continues proxying the existing API namespaces', () => {
    expect(matchingProxy('/api/v1/settings/public')).toBe(proxies['/api'])
    expect(matchingProxy('/v1/chat/completions')).toBe(proxies['/v1'])
  })
})
