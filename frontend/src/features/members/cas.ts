import { classifyAsyncError } from '@/features/shared/asyncState'
import { extractApiErrorMetadata } from '@/utils/apiError'
import type {
  CasConflictState,
  ExactGrantInput,
  GrantPresetId,
  IdentityAccess,
  IdentityGrantInput,
  IdentityMemberGrants,
  LegacyGrantInput,
  ProjectGrantPair,
  ProjectGrantRowDiff,
  WorkspaceRoleProjection
} from './types'

export function buildIfMatchHeaders(version: string | number | null | undefined): Record<string, string> {
  const normalized = String(version ?? '')
    .trim()
    .replace(/^"+|"+$/g, '')
    .trim()
  if (!normalized) {
    throw new Error('Missing resource version for If-Match CAS header')
  }
  return { 'If-Match': `"${normalized}"` }
}

export function isProtectedSharedDefaultProject(
  workspace: { is_default?: boolean } | null | undefined,
  project: { is_default?: boolean } | null | undefined
): boolean {
  return Boolean(workspace?.is_default && project?.is_default)
}

export function normalizeProjectGrants(pairs: ReadonlyArray<ProjectGrantPair> | null | undefined): ProjectGrantPair[] {
  if (!Array.isArray(pairs) || pairs.length === 0) return []
  const byProject = new Map<string, Set<string>>()
  for (const row of pairs) {
    const projectId = String(row?.project_id ?? '').trim()
    if (!projectId) continue
    const bucket = byProject.get(projectId) ?? new Set<string>()
    const rawActions = Array.isArray(row?.actions) ? row.actions : []
    for (const act of rawActions) {
      const trimmed = String(act ?? '').trim()
      if (trimmed) bucket.add(trimmed)
    }
    if (bucket.size > 0) {
      byProject.set(projectId, bucket)
    }
  }
  return Array.from(byProject.entries())
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([project_id, actionSet]) => ({
      project_id,
      actions: Array.from(actionSet).sort()
    }))
}

export function normalizeExactGrantInput(input: ExactGrantInput): ExactGrantInput {
  return {
    project_grants: normalizeProjectGrants(input.project_grants)
  }
}

export function normalizeGrantInput(input: IdentityGrantInput): IdentityGrantInput {
  if ('project_grants' in input && Array.isArray(input.project_grants)) {
    return normalizeExactGrantInput(input)
  }
  const legacy = input as LegacyGrantInput
  const actions = Array.from(
    new Set(legacy.actions.map((item: string) => item.trim()).filter(Boolean))
  ).sort()
  const project_ids = Array.from(
    new Set(legacy.project_ids.map((item: string) => item.trim()).filter(Boolean))
  ).sort()
  return { actions, project_ids }
}

export function hydrateProjectGrantsFromResponse(
  grants: IdentityMemberGrants | null | undefined
): ProjectGrantPair[] {
  if (!grants) return []
  if (Array.isArray(grants.project_grants)) {
    return normalizeProjectGrants(grants.project_grants)
  }
  if (Array.isArray(grants.project_ids) && Array.isArray(grants.actions)) {
    return normalizeProjectGrants(
      grants.project_ids.map((project_id) => ({
        project_id,
        actions: grants.actions ?? []
      }))
    )
  }
  return []
}

export function computeProjectGrantsDiff(
  before: ReadonlyArray<ProjectGrantPair>,
  after: ReadonlyArray<ProjectGrantPair>
): {
  rows: ProjectGrantRowDiff[]
  changedCount: number
  unchangedCount: number
} {
  const normBefore = normalizeProjectGrants(before)
  const normAfter = normalizeProjectGrants(after)
  const beforeMap = new Map(normBefore.map((row) => [row.project_id, new Set(row.actions)]))
  const afterMap = new Map(normAfter.map((row) => [row.project_id, new Set(row.actions)]))
  const allProjectIds = Array.from(new Set([...beforeMap.keys(), ...afterMap.keys()])).sort()

  const rows: ProjectGrantRowDiff[] = []
  let changedCount = 0
  let unchangedCount = 0

  for (const projectId of allProjectIds) {
    const prevSet = beforeMap.get(projectId) ?? new Set<string>()
    const nextSet = afterMap.get(projectId) ?? new Set<string>()
    const added = Array.from(nextSet).filter((act) => !prevSet.has(act)).sort()
    const removed = Array.from(prevSet).filter((act) => !nextSet.has(act)).sort()
    const unchanged = added.length === 0 && removed.length === 0
    if (unchanged) {
      unchangedCount += 1
    } else {
      changedCount += 1
    }
    rows.push({
      projectId,
      added,
      removed,
      unchanged
    })
  }

  return { rows, changedCount, unchangedCount }
}

export function computeConflictingProjectIds(
  baseline: ReadonlyArray<ProjectGrantPair>,
  serverCurrent: ReadonlyArray<ProjectGrantPair>,
  draft: ReadonlyArray<ProjectGrantPair>
): Set<string> {
  const serverVsBaseline = computeProjectGrantsDiff(baseline, serverCurrent)
  const serverVsDraft = computeProjectGrantsDiff(serverCurrent, draft)
  const conflicts = new Set<string>()
  for (const row of serverVsBaseline.rows) {
    if (!row.unchanged) conflicts.add(row.projectId)
  }
  for (const row of serverVsDraft.rows) {
    if (!row.unchanged) conflicts.add(row.projectId)
  }
  return conflicts
}

export const GRANT_PRESET_ACTIONS: Record<GrantPresetId, readonly string[]> = {
  observer: [
    'projects.read',
    'models.read',
    'keys.read',
    'requests.read',
    'usage.read',
    'billing.read',
    'tasks.read',
    'tasks.artifacts.read',
    'playground.read'
  ],
  caller: [
    'projects.read',
    'models.read',
    'playground.read',
    'playground.run',
    'playground.cancel',
    'text.chat',
    'text.responses',
    'text.embeddings',
    'image.generate',
    'requests.read',
    'usage.read',
    'tasks.read',
    'tasks.create',
    'tasks.cancel',
    'tasks.retry',
    'tasks.artifacts.read'
  ],
  issuer: [
    'projects.read',
    'models.read',
    'keys.read',
    'keys.create',
    'keys.revoke',
    'playground.read',
    'playground.run',
    'text.chat',
    'text.responses',
    'text.embeddings',
    'image.generate',
    'requests.read',
    'usage.read',
    'billing.read'
  ],
  maintainer: ['*']
}

export function resolvePresetActions(
  preset: GrantPresetId,
  grantableActions: ReadonlySet<string> | ReadonlyArray<string>
): string[] {
  const allowed =
    grantableActions instanceof Set ? grantableActions : new Set(grantableActions)
  if (preset === 'maintainer') {
    return Array.from(allowed).sort()
  }
  return GRANT_PRESET_ACTIONS[preset].filter((act) => allowed.has(act)).sort()
}

export function projectMemberPermissions(
  access: IdentityAccess | null | undefined,
  fallbackRole?: 'admin' | 'member' | string
): WorkspaceRoleProjection {
  const actions = access?.actions ?? []
  const isEffectiveAdmin =
    Boolean(access?.effective_admin) || (!access && fallbackRole === 'admin')
  const hasAction = (prefix: string) =>
    actions.some((action) => action === '*' || action === prefix || action.startsWith(`${prefix}.`))

  return {
    canViewMembers:
      isEffectiveAdmin ||
      hasAction('members.grants') ||
      hasAction('members.read') ||
      hasAction('workspace.read'),
    canManageGrants:
      isEffectiveAdmin ||
      hasAction('members.grants') ||
      hasAction('members.write') ||
      hasAction('workspace.admin'),
    canManageProjects:
      isEffectiveAdmin ||
      hasAction('projects.create') ||
      hasAction('projects.edit') ||
      hasAction('projects.delete') ||
      hasAction('projects.write') ||
      hasAction('workspace.admin'),
    canInviteMembers:
      isEffectiveAdmin ||
      hasAction('members.grants.write') ||
      hasAction('members.invite') ||
      hasAction('workspace.admin'),
    actions,
    scopeVersion: access?.scope_version ?? null,
    effectiveAdmin: Boolean(access?.effective_admin ?? isEffectiveAdmin),
    membershipRole: access?.membership_role ?? fallbackRole ?? null,
    roleSource: access?.role_source ?? null
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
    const meta = extractApiErrorMetadata(error)
    const serverVersion =
      typeof meta.current_version === 'string' && meta.current_version.trim()
        ? meta.current_version.trim()
        : undefined
    return {
      kind: 'conflict',
      conflict: {
        resourceType,
        resourceId,
        expectedVersion,
        serverVersion,
        reason: classified.reason,
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
