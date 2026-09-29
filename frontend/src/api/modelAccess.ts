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

/**
 * The public models one of the signed-in user's own Keys can call. Resolved from
 * the panel session, so no secret is needed. Errors propagate so callers can tell
 * "this Key has no visible models" apart from "the server could not answer".
 */
export async function keyVisibleModels(keyID: number, signal?: AbortSignal): Promise<{ models: string[]; fullyEnumerable: boolean }> {
  const path = `/keys/${keyID}`
  const rule = await modelAccess.rule(path, signal)
  const impact = await modelAccess.preview(path, rule)
  return { models: impact.models ?? [], fullyEnumerable: impact.fully_enumerable !== false }
}

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
