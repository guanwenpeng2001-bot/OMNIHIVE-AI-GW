import { apiClient } from './client'

export interface ModelAccessGroup {
  id: number
  name: string
  models: string[]
  revision: number
  references: number
}
export interface ModelAccessRule {
  access_group_id: number | null
  models: string[] | null
  exclude: string[]
  revision: number
}
export interface ModelAccessImpact { models: string[]; fully_enumerable: boolean }

export const modelAccess = {
  async groups(admin = false) {
    return (await apiClient.get<ModelAccessGroup[]>(`${admin ? '/admin' : ''}/model-access-groups`)).data
  },
  async saveGroup(group: ModelAccessGroup) {
    const path = '/admin/model-access-groups'
    return (group.id
      ? await apiClient.put<ModelAccessGroup>(`${path}/${group.id}`, group)
      : await apiClient.post<ModelAccessGroup>(path, group)).data
  },
  async deleteGroup(group: ModelAccessGroup) {
    await apiClient.delete(`/admin/model-access-groups/${group.id}`, { params: { revision: group.revision } })
  },
  async rule(path: string, signal?: AbortSignal) {
    return (await apiClient.get<ModelAccessRule>(`${path}/model-access`, { signal })).data
  },
  async saveRule(path: string, rule: ModelAccessRule) {
    return (await apiClient.put<ModelAccessRule>(`${path}/model-access`, rule)).data
  },
  async preview(path: string, rule: ModelAccessRule) {
    return (await apiClient.post<ModelAccessImpact>(`${path}/model-access/preview`, rule)).data
  }
}
