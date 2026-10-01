import { apiClient } from '../client'
import type { Proxy } from '@/types/admin'
import type { PaginatedResponse } from '@/types/common'

export interface ProxyGroup {
  id: number
  name: string
  description?: string
  region?: string
  target_services: string[]
  status: 'active' | 'inactive'
  fallback_mode: 'none' | 'direct' | 'proxy'
  fallback_proxy_id?: number | null
  binding_ttl_seconds: number
  member_count: number
  schedulable_member_count: number
  bound_account_count: number
  created_at: string
  updated_at: string
}
export interface ProxyGroupMember {
  proxy: Proxy
  cooldown_until?: string | null
  cooldown_reason?: string
}
export interface ProxyGroupDetail extends ProxyGroup { members: ProxyGroupMember[] }
export type ProxyGroupInput = Pick<ProxyGroup, 'name' | 'description' | 'region' | 'target_services' | 'status' | 'fallback_mode' | 'fallback_proxy_id' | 'binding_ttl_seconds'>
export interface ProxyGroupBinding {
  account_id: number
  account_name: string
  proxy_id?: number | null
  proxy_name?: string
  expires_at?: string | null
  eligible: boolean
}
const base = '/admin/proxy-groups'
export const proxyGroupsAPI = {
  async list(page = 1, pageSize = 20, filters: { search?: string; status?: string; target_service?: string } = {}) {
    return (await apiClient.get<PaginatedResponse<ProxyGroup>>(base, { params: { page, page_size: pageSize, ...filters } })).data
  },
  async get(id: number) { return (await apiClient.get<ProxyGroupDetail>(`${base}/${id}`)).data },
  async create(input: ProxyGroupInput) { return (await apiClient.post<ProxyGroup>(base, input)).data },
  async update(id: number, input: Partial<ProxyGroupInput>) { return (await apiClient.put<ProxyGroup>(`${base}/${id}`, input)).data },
  async remove(id: number) { await apiClient.delete(`${base}/${id}`) },
  async setMembers(id: number, proxyIds: number[]) {
    await apiClient.put(`${base}/${id}/members`, { proxy_ids: proxyIds })
    return this.get(id)
  },
  async bindings(id: number) { return (await apiClient.get<{ items: ProxyGroupBinding[]; total: number }>(`${base}/${id}/bindings`)).data },
  async resetBindings(id: number) { return (await apiClient.delete<{ cleared: number }>(`${base}/${id}/bindings`)).data }
}
