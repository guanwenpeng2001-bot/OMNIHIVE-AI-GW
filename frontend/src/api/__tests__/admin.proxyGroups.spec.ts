import { beforeEach, describe, expect, it, vi } from 'vitest'
const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
import { proxyGroupsAPI } from '../admin/proxyGroups'
import { workspaceRequestURL } from '../workspaceScope'

describe('proxy group REST contract', () => {
  beforeEach(() => vi.resetAllMocks())
  it('sends server-side filters and preserves pagination', async () => {
    const result = { items: [], total: 0, page: 3, page_size: 20 }
    client.get.mockResolvedValue({ data: result })
    expect(await proxyGroupsAPI.list(3, 20, { search: 'US', status: 'inactive', target_service: 'grok' })).toEqual(result)
    expect(client.get).toHaveBeenCalledWith('/admin/proxy-groups', { params: { page: 3, page_size: 20, search: 'US', status: 'inactive', target_service: 'grok' } })
  })
  it('allows empty member replacement and refetches the canonical detail', async () => {
    client.put.mockResolvedValue({ data: [] })
    client.get.mockResolvedValue({ data: { id: 9, members: [] } })
    expect(await proxyGroupsAPI.setMembers(9, [])).toEqual({ id: 9, members: [] })
    expect(client.put).toHaveBeenCalledWith('/admin/proxy-groups/9/members', { proxy_ids: [] })
    expect(client.get).toHaveBeenCalledWith('/admin/proxy-groups/9')
  })
  it('does not convert a failed replacement into a successful refetch', async () => {
    client.put.mockRejectedValue(new Error('forbidden'))
    await expect(proxyGroupsAPI.setMembers(9, [18])).rejects.toThrow('forbidden')
    expect(client.get).not.toHaveBeenCalled()
  })
  it('preserves omitted update fields and explicit fallback clear', async () => {
    client.put.mockResolvedValue({ data: { id: 9 } })
    await proxyGroupsAPI.update(9, { name: 'Renamed' })
    expect(client.put).toHaveBeenLastCalledWith('/admin/proxy-groups/9', { name: 'Renamed' })
    await proxyGroupsAPI.update(9, { fallback_mode: 'none', fallback_proxy_id: 0 })
    expect(client.put).toHaveBeenLastCalledWith('/admin/proxy-groups/9', { fallback_mode: 'none', fallback_proxy_id: 0 })
  })
  it('resets bindings using DELETE and returns the cleared count', async () => {
    client.delete.mockResolvedValue({ data: { cleared: 2 } })
    expect(await proxyGroupsAPI.resetBindings(9)).toEqual({ cleared: 2 })
    expect(client.delete).toHaveBeenCalledWith('/admin/proxy-groups/9/bindings')
  })
  it('delegates group requests to the current workspace', () => {
    expect(workspaceRequestURL('/admin/proxy-groups/9/members', '/workspaces/4/admin/proxy-groups')).toBe('/workspaces/4/proxy-groups/9/members')
    expect(workspaceRequestURL('/admin/proxy-groups', '/admin/proxy-groups')).toBe('/admin/proxy-groups')
  })
})
