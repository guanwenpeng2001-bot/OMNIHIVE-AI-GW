import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { userGroupsAPI } from '../groups'

describe('userGroupsAPI workspace and project scoping (E2-5)', () => {
  const originalAdapter = apiClient.defaults.adapter

  afterEach(() => {
    apiClient.defaults.adapter = originalAdapter
  })

  it('uses account-level endpoints when no workspaceId is provided', async () => {
    const adapter = vi.fn().mockResolvedValue({
      status: 200,
      data: { code: 0, data: [] },
      headers: {},
      config: {},
      statusText: 'OK',
    })
    apiClient.defaults.adapter = adapter

    await userGroupsAPI.getAvailable()
    await userGroupsAPI.getUserGroupRates()

    expect(adapter.mock.calls[0][0].url).toBe('/groups/available')
    expect(adapter.mock.calls[0][0].params?.project_id).toBeUndefined()
    expect(adapter.mock.calls[1][0].url).toBe('/groups/rates')
  })

  it('uses v2 project key-groups endpoint with cursor pagination and decimal string IDs', async () => {
    const adapter = vi
      .fn()
      .mockResolvedValueOnce({
        status: 200,
        data: {
          code: 0,
          message: 'success',
          data: {
            items: [{ id: '12', name: 'openai-main', platform: 'openai' }],
            next_cursor: 'cur_page_2',
            as_of: '2026-11-21T02:00:00Z',
          },
        },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      .mockResolvedValueOnce({
        status: 200,
        data: {
          code: 0,
          message: 'success',
          data: {
            items: [{ id: '9007199254740993', name: 'anthropic-pro', platform: 'anthropic' }],
            next_cursor: null,
            as_of: '2026-11-21T02:00:00Z',
          },
        },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      .mockResolvedValueOnce({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
    apiClient.defaults.adapter = adapter

    const groups = await userGroupsAPI.getAvailable({ workspaceId: '4', projectId: '5' })
    await userGroupsAPI.getUserGroupRates({ workspaceId: '4' })

    expect(adapter.mock.calls[0][0].url).toBe('/workspaces/4/projects/5/key-groups')
    expect(adapter.mock.calls[0][0].baseURL).toContain('/api/v2')
    expect(adapter.mock.calls[0][0].params).toEqual({ limit: 100 })
    expect(adapter.mock.calls[1][0].url).toBe('/workspaces/4/projects/5/key-groups')
    expect(adapter.mock.calls[1][0].params).toEqual({ limit: 100, cursor: 'cur_page_2' })
    expect(groups.map((g) => g.id)).toEqual(['12', '9007199254740993'])
    expect(adapter.mock.calls[2][0].url).toBe('/workspaces/4/groups/rates')
  })

  it('restarts pagination from beginning once when cursor returns 410 SNAPSHOT_EXPIRED', async () => {
    const adapter = vi.fn().mockResolvedValue({
      status: 200,
      data: { code: 0, data: { items: [{ id: '12', name: 'openai-main', platform: 'openai' }], next_cursor: null } },
      headers: {},
      config: {},
      statusText: 'OK',
    })
    adapter
      .mockResolvedValueOnce({
        status: 200,
        data: { code: 0, data: { items: [{ id: '11', name: 'old', platform: 'openai' }], next_cursor: 'expired_cur' } },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      .mockResolvedValueOnce({
        status: 410,
        data: { code: 410, reason: 'SNAPSHOT_EXPIRED', message: 'Snapshot expired' },
        headers: {},
        config: {},
        statusText: 'Gone',
      })
    apiClient.defaults.adapter = adapter

    const result = await userGroupsAPI.getProjectKeyGroups('4', '5')
    expect(result).toEqual([{ id: '12', name: 'openai-main', platform: 'openai' }])
    expect(adapter).toHaveBeenCalledTimes(3)
  })
})
