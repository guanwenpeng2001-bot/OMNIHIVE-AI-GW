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

  it('uses workspace-scoped groups endpoint with project_id when workspaceId and projectId are provided', async () => {
    const adapter = vi.fn().mockResolvedValue({
      status: 200,
      data: { code: 0, data: [] },
      headers: {},
      config: {},
      statusText: 'OK',
    })
    apiClient.defaults.adapter = adapter

    await userGroupsAPI.getAvailable({ workspaceId: '4', projectId: '5' })
    await userGroupsAPI.getUserGroupRates({ workspaceId: '4' })

    expect(adapter.mock.calls[0][0].url).toBe('/workspaces/4/groups/available')
    expect(adapter.mock.calls[0][0].params?.project_id).toBe('5')
    expect(adapter.mock.calls[1][0].url).toBe('/workspaces/4/groups/rates')
  })
})
