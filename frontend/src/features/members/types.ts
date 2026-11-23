import type { components } from '@/api/generated/openapi'

export type IdentityWorkspace = components['schemas']['IdentityWorkspace'] & {
  is_default?: boolean
}
export type IdentityWorkspacePage = Omit<components['schemas']['IdentityWorkspacePage'], 'items'> & {
  items: IdentityWorkspace[]
}
export type IdentityProject = components['schemas']['IdentityProject'] & {
  is_default?: boolean
  state?: 'active' | 'archived' | string
}
export type IdentityProjectPage = Omit<components['schemas']['IdentityProjectPage'], 'items'> & {
  items: IdentityProject[]
}
export type IdentityAccess = components['schemas']['IdentityAccess'] & {
  effective_admin?: boolean
  membership_role?: 'admin' | 'member' | string | null
  role_source?: 'inherited' | 'delegated' | 'direct' | string | null
  grantable_actions?: string[]
  issuable_key_actions?: string[]
}

export interface ProjectGrantPair {
  project_id: string
  actions: string[]
}

export interface ExactGrantInput {
  project_grants: ProjectGrantPair[]
}

export type LegacyGrantInput = components['schemas']['IdentityGrantInput']
export type IdentityGrantInput = LegacyGrantInput | ExactGrantInput
export type IdentityGrantVersion = components['schemas']['IdentityGrantVersion']

export type GrantPresetId = 'observer' | 'caller' | 'issuer' | 'maintainer'

export interface ProjectGrantRowDiff {
  projectId: string
  added: string[]
  removed: string[]
  unchanged: boolean
}

export interface IdentityMemberItem {
  id: string
  user_id: string
  email: string
  username: string
  role: string
  role_source?: 'inherited' | 'delegated' | 'direct' | string | null
  status: string
  version: string
}

export interface IdentityMemberGrants extends IdentityGrantVersion {
  project_grants?: ProjectGrantPair[]
  project_ids?: string[]
  actions?: string[]
}

export interface WorkspaceRoleProjection {
  canViewMembers: boolean
  canManageGrants: boolean
  canManageProjects: boolean
  canInviteMembers: boolean
  actions: string[]
  scopeVersion: string | null
  effectiveAdmin?: boolean
  membershipRole?: string | null
  roleSource?: string | null
}

export interface CasConflictState {
  resourceType: 'project' | 'grants' | 'access_request'
  resourceId: string
  expectedVersion: string
  serverVersion?: string
  reason?: string
  message: string
  requestId?: string
}
