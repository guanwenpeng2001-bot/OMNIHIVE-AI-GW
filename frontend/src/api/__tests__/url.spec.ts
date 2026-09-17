import { afterEach, describe, expect, it, vi } from 'vitest'

afterEach(() => {
  vi.unstubAllEnvs()
  vi.resetModules()
})

describe('gateway URL deployment paths', () => {
  it.each([
    ['/api/v1', ''],
    ['api/v1', ''],
    ['/proxy/sub2api/api/v1/', '/proxy/sub2api'],
    ['https://gateway.example:8443/api/v1', 'https://gateway.example:8443'],
    ['https://gateway.example:8443/proxy/api/v1/', 'https://gateway.example:8443/proxy'],
    ['//gateway.example/proxy/api/v1', `${window.location.protocol}//gateway.example/proxy`],
    ['/custom-management', ''],
  ])('resolves %s without losing the gateway prefix', async (base, prefix) => {
    vi.stubEnv('VITE_API_BASE_URL', base)
    vi.resetModules()
    const { buildGatewayUrl } = await import('../url')
    const origin = prefix.startsWith('http') ? '' : window.location.origin
    expect(buildGatewayUrl('/v1')).toBe(`${origin}${prefix}/v1`)
    expect(buildGatewayUrl('v1/messages')).toBe(`${origin}${prefix}/v1/messages`)
    expect(buildGatewayUrl('/api/v1/admin/ops/ws/qps')).toBe(`${origin}${prefix}/api/v1/admin/ops/ws/qps`)
  })
})
