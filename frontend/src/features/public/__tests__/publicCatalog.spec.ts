import { describe, expect, it, vi } from 'vitest'
import {
  formatContextTokens,
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
    expect(formatContextTokens('128000')).toBe('128K')
    expect(formatContextTokens('1000000')).toBe('1M')
  })

  it('formats honest unobserved and undeclared governance labels', () => {
    expect(routeStateLabel('eligible', true)).toBe('可路由')
    expect(maturityLabel('unknown', true)).toBe('未声明')
    expect(statusStateLabel('unknown', true)).toBe('未观测 / 样本不足')
    expect(statusReasonLabel('INSUFFICIENT_SAMPLES', true)).toBe('样本数未达阈值')
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
})
