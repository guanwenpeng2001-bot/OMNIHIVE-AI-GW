import { classifyAsyncError } from '@/features/shared/asyncState'
import type { CasConflictState, IdentityAccess, IdentityGrantInput, WorkspaceRoleProjection } from './types'

export function buildIfMatchHeaders(version: string | number | null | undefined): Record<string, string> {
  const normalized = String(version ?? '').trim()
  if (!normalized) {
    throw new Error('Missing resource version for If-Match CAS header')
  }
  return { 'If-Match': normalized }
}

export function normalizeGrantInput(input: IdentityGrantInput): IdentityGrantInput {
  const actions = Array.from(new Set(input.actions.map((item) => item.trim()).filter(Boolean))).sort()
  const project_ids = Array.from(new Set(input.project_ids.map((item) => item.trim()).filter(Boolean))).sort()
  return { actions, project_ids }
}

export function projectMemberPermissions(
  access: IdentityAccess | null | undefined,
  fallbackRole?: 'admin' | 'member' | string
): WorkspaceRoleProjection {
  const actions = access?.actions ?? []
  const isAdminFallback = fallbackRole === 'admin'
  const hasAction = (prefix: string) =>
    actions.some((action) => action === '*' || action === prefix || action.startsWith(`${prefix}.`))

  return {
    canViewMembers: isAdminFallback || hasAction('members.read') || hasAction('workspace.read') || actions.length > 0,
    canManageGrants: isAdminFallback || hasAction('members.grants') || hasAction('members.write') || hasAction('workspace.admin'),
    canManageProjects: isAdminFallback || hasAction('projects.write') || hasAction('workspace.admin'),
    canInviteMembers: isAdminFallback || hasAction('members.invite') || hasAction('workspace.admin'),
    actions,
    scopeVersion: access?.scope_version ?? null
  }
}

export function resolveCasMutationError(
  error: unknown,
  resourceType: CasConflictState['resourceType'],
  resourceId: string,
  expectedVersion: string
): { kind: 'conflict'; conflict: CasConflictState } | { kind: 'forbidden' | 'unknown' | 'error'; message: string; requestId?: string } {
  const classified = classifyAsyncError(error)
  if (classified.state === 'conflict') {
    return {
      kind: 'conflict',
      conflict: {
        resourceType,
        resourceId,
        expectedVersion,
        message: classified.message,
        requestId: classified.requestId
      }
    }
  }
  return {
    kind: classified.state,
    message: classified.message,
    requestId: classified.requestId
  }
}
