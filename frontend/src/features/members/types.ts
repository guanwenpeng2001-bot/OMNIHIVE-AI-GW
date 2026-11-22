import type { components } from '@/api/generated/openapi'

export type IdentityWorkspace = components['schemas']['IdentityWorkspace']
export type IdentityWorkspacePage = components['schemas']['IdentityWorkspacePage']
export type IdentityProject = components['schemas']['IdentityProject']
export type IdentityProjectPage = components['schemas']['IdentityProjectPage']
export type IdentityAccess = components['schemas']['IdentityAccess']
export type IdentityGrantInput = components['schemas']['IdentityGrantInput']
export type IdentityGrantVersion = components['schemas']['IdentityGrantVersion']

export type IdentityMemberItem = components['schemas']['IdentityMember']
export type IdentityMemberGrants = components['schemas']['IdentityMemberGrants']

export interface WorkspaceRoleProjection {
  canViewMembers: boolean
  canManageGrants: boolean
  canManageProjects: boolean
  canInviteMembers: boolean
  actions: string[]
  scopeVersion: string | null
}

export interface CasConflictState {
  resourceType: 'project' | 'grants'
  resourceId: string
  expectedVersion: string
  serverVersion?: string
  message: string
  requestId?: string
}
