import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  compareProjectModels,
  comparePublicModels,
  getProjectModel,
  getPublicModel,
  isCatalogAbortError,
  isSnapshotExpiredError,
  listProjectModels,
  listPublicModels,
  quoteProjectModel
} from '@/api/catalog'
import { catalogApi } from '@/features/console/catalogApi'

const { getMock, postMock } = vi.hoisted(() => ({
  getMock: vi.fn(),
  postMock: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: getMock,
    post: postMock
  }
}))

describe('unified v2 catalog client', () => {
  beforeEach(() => {
    getMock.mockReset()
    postMock.mockReset()
  })

  it('queries public and scoped v2 endpoints without implicit timezone and passes api_key_id', async () => {
    getMock.mockResolvedValueOnce({ data: { data: { items: [], next_cursor: null } } })
    await listPublicModels({
      q: 'claude',
      availability_state: 'live',
      billing: 'conflict',
      source: ['channel_published'],
      limit: 50
    })
    expect(getMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models',
      expect.objectContaining({
        params: expect.objectContaining({
          q: 'claude',
          availability_state: 'live',
          billing: 'conflict',
          source: ['channel_published'],
          limit: 50,
          timezone: undefined
        })
      })
    )

    getMock.mockResolvedValueOnce({
      data: { items: [], next_cursor: null, scope_version: '17' }
    })
    const scoped = await listProjectModels('2', '9', {
      api_key_id: '42',
      accessible: true
    })
    expect(scoped.scope_version).toBe('17')
    expect(getMock).toHaveBeenLastCalledWith(
      '/api/v2/workspaces/2/projects/9/models',
      expect.objectContaining({
        params: expect.objectContaining({
          api_key_id: '42',
          accessible: true,
          timezone: undefined
        })
      })
    )

    getMock.mockResolvedValueOnce({ data: { model: { id: 'm_1', model: 'gpt-4o' } } })
    await getProjectModel('2', '9', 'gpt-4o', { api_key_id: '42', catalog_version: 'cv_1' })
    expect(getMock).toHaveBeenLastCalledWith(
      '/api/v2/workspaces/2/projects/9/models/gpt-4o',
      expect.objectContaining({
        params: expect.objectContaining({
          api_key_id: '42',
          catalog_version: 'cv_1',
          timezone: undefined
        })
      })
    )

    postMock.mockResolvedValueOnce({ data: { rows: [] } })
    await comparePublicModels(['m_1', 'm_2'], {
      scenario: { input_tokens: '100', output_tokens: '50', requests: '1' }
    })
    expect(postMock).toHaveBeenLastCalledWith(
      '/api/v2/public/models/compare',
      expect.objectContaining({ model_ids: ['m_1', 'm_2'] })
    )

    postMock.mockResolvedValueOnce({ data: { rows: [] } })
    await compareProjectModels('2', '9', { model_ids: ['m_1', 'm_2'], api_key_id: '42' })
    expect(postMock).toHaveBeenLastCalledWith(
      '/api/v2/workspaces/2/projects/9/models/compare',
      expect.objectContaining({ model_ids: ['m_1', 'm_2'], api_key_id: '42' })
    )

    postMock.mockResolvedValueOnce({ data: { state: 'estimated', billing_state: 'priced' } })
    const quote = await quoteProjectModel('2', '9', {
      model_id: 'm_1',
      protocol: 'openai_chat',
      action: 'text.chat',
      scenario: { input_tokens: '1000', output_tokens: '200' }
    })
    expect(quote.state).toBe('estimated')
    expect(postMock).toHaveBeenLastCalledWith(
      '/api/v2/workspaces/2/projects/9/quotes',
      expect.objectContaining({ model_id: 'm_1' })
    )
  })

  it('fails closed in console catalogApi.listScopedModels without falling back to public models', async () => {
    expect(await catalogApi.listScopedModels('', '')).toEqual([])
    expect(getMock).not.toHaveBeenCalled()

    getMock.mockRejectedValueOnce({ status: 403, reason: 'INSUFFICIENT_GRANT' })
    await expect(catalogApi.listScopedModels('2', '9')).rejects.toMatchObject({ status: 403 })
    expect(getMock).toHaveBeenCalledTimes(1)
  })

  it('detects snapshot expiry and abort errors accurately, and resolves bare slug detail once', async () => {
    expect(isSnapshotExpiredError({ response: { status: 410, data: { reason: 'SNAPSHOT_EXPIRED' } } })).toBe(true)
    expect(isSnapshotExpiredError({ status: 409 })).toBe(true)
    expect(isSnapshotExpiredError({ status: 500 })).toBe(false)
    expect(isCatalogAbortError({ name: 'AbortError' })).toBe(true)
    expect(isCatalogAbortError({ code: 'ERR_CANCELED' })).toBe(true)

    const stableId = 'm_0123456789abcdef0123456789abcdef'
    getMock
      .mockRejectedValueOnce({ status: 404 })
      .mockResolvedValueOnce({ data: { items: [{ id: stableId, provider: 'openai', model: 'gpt-4o' }] } })
      .mockResolvedValueOnce({ data: { model: { id: stableId, provider: 'openai', model: 'gpt-4o' } } })

    const detail = await getPublicModel('openai/gpt-4o')
    expect(detail.model.id).toBe(stableId)
  })
})
