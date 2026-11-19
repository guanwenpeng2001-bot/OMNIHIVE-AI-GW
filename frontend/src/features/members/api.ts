import { createIdempotencyKey, v2Delete, v2Get, v2Patch, v2Post, v2Put } from '@/features/shared/v2Client'
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
    return v2Get<IdentityWorkspacePage>('/me/scopes', params)
  },

  async listProjects(
    workspaceId: string,
    params?: { limit?: number; cursor?: string }
  ): Promise<IdentityProjectPage> {
    return v2Get<IdentityProjectPage>(`/workspaces/${workspaceId}/projects`, params)
  },

  async createProject(workspaceId: string, name: string): Promise<IdentityProject> {
    return v2Post<IdentityProject>(
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
    return v2Patch<IdentityProject>(
      `/workspaces/${workspaceId}/projects/${projectId}`,
      { name: name.trim() },
      buildIfMatchHeaders(version)
    )
  },

  async deleteProject(workspaceId: string, projectId: string, version: string): Promise<IdentityProject> {
    return v2Delete<IdentityProject>(
      `/workspaces/${workspaceId}/projects/${projectId}`,
      buildIfMatchHeaders(version)
    )
  },

  async getProjectAccess(workspaceId: string, projectId: string): Promise<IdentityAccess> {
    return v2Get<IdentityAccess>(`/workspaces/${workspaceId}/projects/${projectId}/access`)
  },

  async setMemberGrants(
    workspaceId: string,
    memberId: string,
    version: string,
    input: IdentityGrantInput
  ): Promise<IdentityGrantVersion> {
    return v2Put<IdentityGrantVersion>(
      `/workspaces/${workspaceId}/members/${memberId}/grants`,
      normalizeGrantInput(input),
      buildIfMatchHeaders(version)
    )
  }
}
