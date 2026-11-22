import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  formatContextTokens,
  formatMetricsSource,
  formatMetricsWindow,
  maturityLabel,
  publicTokenPrice,
  routeStateLabel,
  statusReasonLabel,
  statusStateLabel
} from '../format'
import {
  getPublicIncident,
  getPublicModel,
  getPublicStatus,
  listPublicIncidents,
  listPublicModels
} from '../api'

const { getMock } = vi.hoisted(() => ({
  getMock: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: getMock,
    post: vi.fn()
  }
}))

describe('public catalog formatters & API wiring', () => {
  beforeEach(() => {
    getMock.mockReset()
  })

  it('formats per-1M token rates accurately in English and Chinese', () => {
    expect(
      publicTokenPrice({ currency: 'USD', amount: '2.5', per: '1000000', unit: 'input_token' }, 2)
    ).toBe('$2.50')
    expect(
      publicTokenPrice({ currency: 'USD', amount: '0', per: '1000000', unit: 'token' }, 2, true)
    ).toBe('免费')
    expect(
      publicTokenPrice({ currency: 'USD', amount: '0', per: '1000000', unit: 'token' }, 2, false)
    ).toBe('Free')
    expect(
      publicTokenPrice({ currency: 'USD', amount: '0.075', per: '1000000', unit: 'input_token' }, 2)
    ).toBe('$0.075')
    expect(formatContextTokens('128000')).toBe('128K')
    expect(formatContextTokens('1000000')).toBe('1M')
  })

  it('formats honest unobserved and undeclared governance labels', () => {
    expect(routeStateLabel('eligible', true)).toBe('可路由')
    expect(maturityLabel('unknown', true)).toBe('未声明')
    expect(statusStateLabel('unknown', true)).toBe('未观测 / 样本不足')
    expect(statusReasonLabel('INSUFFICIENT_SAMPLES', true)).toBe('样本数未达阈值')
    expect(formatMetricsWindow({ window: '24h' }, false)).toBe('24h')
    expect(
      formatMetricsWindow(
        {
          window_start: '2026-11-19T12:00:00Z',
          window_end: '2026-11-20T12:00:00Z'
        },
        true
      )
    ).toContain('24h (')
    expect(
      formatMetricsWindow({ window_start: '0001-01-01T00:00:00Z' }, true)
    ).toBe('未观测')
    expect(formatMetricsSource('gateway_observations', false)).toBe('Gateway live traffic')
    expect(formatMetricsSource('unavailable', true)).toBe('未观测')
  })

  it('wires v2 public models, detail, status, and incident endpoints', async () => {
    getMock.mockResolvedValueOnce({ data: { items: [], next_cursor: null } })
    await listPublicModels({ provider: ['openai'], protocol: ['openai_chat'], sort: 'price_input' })
    expect(getMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({
          provider: ['openai'],
          protocol: ['openai_chat'],
          sort: 'price_input'
        })
      })
    )

    getMock.mockResolvedValueOnce({ data: { model: { id: 'm1', model: 'gpt-5.6-sol' } } })
    await getPublicModel('m1')
    expect(getMock).toHaveBeenLastCalledWith('/api/v2/public/models/m1', expect.any(Object))

    getMock.mockResolvedValueOnce({ data: { overall: 'operational', components: [] } })
    await getPublicStatus('7d')
    expect(getMock).toHaveBeenLastCalledWith(
      '/api/v2/public/status',
      expect.objectContaining({ params: expect.objectContaining({ window: '7d' }) })
    )

    getMock.mockResolvedValueOnce({ data: { items: [] } })
    await listPublicIncidents({ limit: 10 })
    expect(getMock).toHaveBeenLastCalledWith('/api/v2/public/status/incidents', expect.any(Object))

    getMock.mockResolvedValueOnce({ data: { id: 'inc-1', title: 'Notice', updates: [] } })
    await getPublicIncident('inc-1')
    expect(getMock).toHaveBeenLastCalledWith(
      '/api/v2/public/status/incidents/inc-1',
      expect.any(Object)
    )
  })

  it('resolves canonical provider/model slugs via encoded detail fetch and m_<32hex> catalog fallback on 404', async () => {
    const stableId = 'm_0123456789abcdef0123456789abcdef'

    // 1) Direct 404 on slug -> fallback listPublicModels({ q: 'auto/gemini-3.8-flash', limit: 100 }) -> fetch by stable m_ ID
    getMock
      .mockRejectedValueOnce({ status: 404, message: 'Resource not found.' })
      .mockResolvedValueOnce({
        data: {
          items: [
            {
              id: stableId,
              provider: 'openai',
              model: 'auto/gemini-3.8-flash',
              display_name: 'Gemini 3.8 Flash'
            }
          ]
        }
      })
      .mockResolvedValueOnce({
        data: {
          model: {
            id: stableId,
            provider: 'openai',
            model: 'auto/gemini-3.8-flash',
            display_name: 'Gemini 3.8 Flash'
          }
        }
      })

    const detail = await getPublicModel('auto/gemini-3.8-flash')
    expect(detail.model.id).toBe(stableId)
    expect(getMock).toHaveBeenNthCalledWith(
      1,
      '/api/v2/public/models/auto%2Fgemini-3.8-flash',
      expect.any(Object)
    )
    expect(getMock).toHaveBeenNthCalledWith(
      2,
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ q: 'auto/gemini-3.8-flash', limit: 100 })
      })
    )
    expect(getMock).toHaveBeenNthCalledWith(
      3,
      `/api/v2/public/models/${stableId}`,
      expect.any(Object)
    )
  })

  it('avoids double-encoding pre-encoded slugs and resolves synthesized provider/model pairs', async () => {
    const stableId = 'm_fedcba9876543210fedcba9876543210'

    // Synthesized `${provider}/${model}` when primary `q: 'openai/gpt-5.6-sol'` returns empty
    getMock
      .mockRejectedValueOnce({ response: { status: 404 } })
      .mockResolvedValueOnce({ data: { items: [] } })
      .mockResolvedValueOnce({
        data: {
          items: [
            {
              id: stableId,
              provider: 'openai',
              model: 'gpt-5.6-sol'
            }
          ]
        }
      })
      .mockResolvedValueOnce({
        data: {
          model: {
            id: stableId,
            provider: 'openai',
            model: 'gpt-5.6-sol'
          }
        }
      })

    const resolved = await getPublicModel('openai%2Fgpt-5.6-sol')
    expect(resolved.model.id).toBe(stableId)
    expect(getMock).toHaveBeenNthCalledWith(
      1,
      '/api/v2/public/models/openai%2Fgpt-5.6-sol',
      expect.any(Object)
    )
    expect(getMock).toHaveBeenNthCalledWith(
      3,
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({ q: 'gpt-5.6-sol', limit: 100 })
      })
    )
  })
})
