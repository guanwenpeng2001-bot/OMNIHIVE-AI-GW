import { beforeEach, describe, expect, it, vi } from 'vitest'
import { catalogApi, normalizeCatalogModel } from '../catalogApi'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get }, buildGatewayUrl: (path: string) => path }))

describe('project catalog authority', () => {
  beforeEach(() => get.mockReset())

  it('requires an explicit project and never uses the public directory as a fallback', async () => {
    await expect(catalogApi.listScopedModels('', '')).rejects.toMatchObject({ reason: 'PROJECT_SCOPE_REQUIRED' })
    expect(get).not.toHaveBeenCalled()
    get.mockRejectedValueOnce({ status: 503 })
    await expect(catalogApi.listScopedModels('8', '13')).rejects.toMatchObject({ status: 503 })
    expect(get).toHaveBeenCalledTimes(1)
    expect(get.mock.calls[0][0]).toBe('/api/v2/workspaces/8/projects/13/models')
  })

  it('retains provider-distinct models through all pages of the same scope', async () => {
    get.mockResolvedValueOnce({ data: { items: [{ id: 'a', model: 'same', provider: 'provider-a' }], next_cursor: 'page-2' } })
      .mockResolvedValueOnce({ data: { items: [{ id: 'b', model: 'same', provider: 'provider-b' }], next_cursor: null } })
    const models = await catalogApi.listScopedModels('8', '13')
    expect(models.map(model => [model.id, model.provider])).toEqual([['a', 'provider-a'], ['b', 'provider-b']])
    expect(get.mock.calls[1][1].params).toMatchObject({ cursor: 'page-2', accessible: true })
  })

  it('fails explicitly if a cursor cycle would make the list incomplete', async () => {
    get.mockResolvedValue({ data: { items: [], next_cursor: 'repeated' } })
    await expect(catalogApi.listScopedModels('8', '13')).rejects.toThrow('CATALOG_PAGINATION_INCOMPLETE')
  })

  it('omits the access predicate for operations inventory instead of selecting only denied models', async () => {
    get.mockResolvedValueOnce({ data: { items: [], next_cursor: null } })
    await catalogApi.listScopedModels('8', '13', { accessible: null })
    expect(get.mock.calls[0][1].params.accessible).toBeUndefined()
  })

  it('never promotes unknown capabilities or invalid limits to supported facts', () => {
    const unknown = normalizeCatalogModel({ id: 'a', model: 'same', facts: { tools: 'unknown', reasoning: 'unknown', max_output_tokens: '-10' } })
    expect(unknown).toMatchObject({ toolsSupported: false, reasoningSupported: false, maxOutputTokens: null })
    expect(normalizeCatalogModel({ id: 'b', model: 'same', facts: { tools: true, reasoning: 'supported' } })).toMatchObject({ toolsSupported: true, reasoningSupported: true })
  })
})
