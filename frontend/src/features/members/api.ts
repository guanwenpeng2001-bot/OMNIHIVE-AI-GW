import { createIdempotencyKey, controlDelete, controlGet, controlPatch, controlPost, controlPut } from '@/features/shared/controlClient'
import { buildIfMatchHeaders, normalizeGrantInput } from './cas'
import type {
  IdentityAccess,
  IdentityGrantInput,
  IdentityGrantVersion,
  IdentityProject,
  IdentityProjectPage,
  IdentityWorkspacePage
} from './types'

export const membersV2API = {
  async listScopes(params?: { limit?: number; cursor?: string }): Promise<IdentityWorkspacePage> {
    return controlGet<IdentityWorkspacePage>('/me/scopes', params)
  },

  async listProjects(
    workspaceId: string,
    params?: { limit?: number; cursor?: string }
  ): Promise<IdentityProjectPage> {
    return controlGet<IdentityProjectPage>(`/workspaces/${workspaceId}/projects`, params)
  },

  async createProject(workspaceId: string, name: string): Promise<IdentityProject> {
    return controlPost<IdentityProject>(
      `/workspaces/${workspaceId}/projects`,
      { name: name.trim() },
      { 'Idempotency-Key': createIdempotencyKey() }
    )
  },

  async updateProject(
    workspaceId: string,
    projectId: string,
    name: string,
    version: string
  ): Promise<IdentityProject> {
    return controlPatch<IdentityProject>(
      `/workspaces/${workspaceId}/projects/${projectId}`,
      { name: name.trim() },
      buildIfMatchHeaders(version)
    )
  },

  async deleteProject(workspaceId: string, projectId: string, version: string): Promise<IdentityProject> {
    return controlDelete<IdentityProject>(
      `/workspaces/${workspaceId}/projects/${projectId}`,
      buildIfMatchHeaders(version)
    )
  },

  async getProjectAccess(workspaceId: string, projectId: string): Promise<IdentityAccess> {
    return controlGet<IdentityAccess>(`/workspaces/${workspaceId}/projects/${projectId}/access`)
  },

  async setMemberGrants(
    workspaceId: string,
    memberId: string,
    version: string,
    input: IdentityGrantInput
  ): Promise<IdentityGrantVersion> {
    return controlPut<IdentityGrantVersion>(
      `/workspaces/${workspaceId}/members/${memberId}/grants`,
      normalizeGrantInput(input),
      buildIfMatchHeaders(version)
    )
  }
}
