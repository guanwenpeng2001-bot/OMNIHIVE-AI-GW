import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  buildIfMatchHeaders,
  computeConflictingProjectIds,
  computeProjectGrantsDiff,
  hydrateProjectGrantsFromResponse,
  isProtectedSharedDefaultProject,
  normalizeGrantInput,
  normalizeProjectGrants,
  projectMemberPermissions,
  resolveCasMutationError,
  resolvePresetActions
} from '../cas'
import { membersV2API } from '../api'
import * as controlClient from '@/features/shared/controlClient'

describe('members CAS & role projections', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('builds If-Match header from non-empty version and rejects missing version', () => {
    expect(buildIfMatchHeaders('42')).toEqual({ 'If-Match': '"42"' })
    expect(buildIfMatchHeaders('"42"')).toEqual({ 'If-Match': '"42"' })
    expect(() => buildIfMatchHeaders('')).toThrow(/If-Match/)
    expect(() => buildIfMatchHeaders(null)).toThrow(/If-Match/)
  })

  it('normalizes and deduplicates grant actions and project_ids', () => {
    expect(
      normalizeGrantInput({
        actions: [' projects.read ', 'members.grants', 'projects.read', ''],
        project_ids: ['proj_b', 'proj_a', 'proj_b']
      })
    ).toEqual({
      actions: ['members.grants', 'projects.read'],
      project_ids: ['proj_a', 'proj_b']
    })
  })

  it('sends If-Match header when updating member grants via v2 API', async () => {
    const putSpy = vi.spyOn(controlClient, 'controlPut').mockResolvedValue({
      member_id: 'mem_1',
      version: '2',
      effective_at: '2026-11-19T10:00:00Z'
    })

    const res = await membersV2API.setMemberGrants('ws_1', 'mem_1', '1', {
      actions: ['text.chat', 'usage.read'],
      project_ids: ['proj_2', 'proj_1']
    })

    expect(putSpy).toHaveBeenCalledWith(
      '/workspaces/ws_1/members/mem_1/grants',
      {
        actions: ['text.chat', 'usage.read'],
        project_ids: ['proj_1', 'proj_2']
      },
      { 'If-Match': '"1"' }
    )
    expect(res.version).toBe('2')
  })

  it('classifies 409 conflict and 403 forbidden responses', () => {
    const conflict = resolveCasMutationError(
      { response: { status: 409, data: { code: 409, reason: 'CONFLICT', message: 'Stale version' } } },
      'grants',
      'mem_1',
      '1'
    )
    expect(conflict.kind).toBe('conflict')
    if (conflict.kind === 'conflict') {
      expect(conflict.conflict.expectedVersion).toBe('1')
      expect(conflict.conflict.resourceId).toBe('mem_1')
    }

    const forbidden = resolveCasMutationError(
      { response: { status: 403, data: { code: 403, reason: 'FORBIDDEN', message: 'Insufficient role' } } },
      'project',
      'proj_1',
      '3'
    )
    expect(forbidden.kind).toBe('forbidden')
  })

  it('projects capabilities from IdentityAccess actions and respects non-admin restrictions', () => {
    const readOnly = projectMemberPermissions(
      {
        actor_id: 'u_1',
        workspace_id: 'ws_1',
        project_id: 'proj_1',
        actions: ['projects.read', 'usage.read'],
        scope_version: 'sv_10',
        expires_at: '2026-11-20T00:00:00Z'
      },
      'member'
    )
    expect(readOnly.canViewMembers).toBe(false)
    expect(readOnly.canManageGrants).toBe(false)
    expect(readOnly.canManageProjects).toBe(false)

    const memberViewer = projectMemberPermissions(
      {
        actor_id: 'u_2',
        workspace_id: 'ws_1',
        project_id: 'proj_1',
        actions: ['members.read'],
        scope_version: 'sv_11',
        expires_at: '2026-11-20T00:00:00Z'
      },
      'member'
    )
    expect(memberViewer.canViewMembers).toBe(true)
    expect(memberViewer.canManageGrants).toBe(false)

    const admin = projectMemberPermissions(null, 'admin')
    expect(admin.canManageGrants).toBe(true)
    expect(admin.canManageProjects).toBe(true)

    // When a real access payload is returned without admin capabilities, do not fall back to workspace role
    const nonAdminWithLoadedAccess = projectMemberPermissions(
      {
        actor_id: 'u_3',
        workspace_id: 'ws_1',
        project_id: null,
        actions: ['projects.read'],
        effective_admin: false,
        membership_role: 'member',
        role_source: 'direct',
        scope_version: 'sv_12',
        expires_at: '2026-11-20T00:00:00Z'
      },
      'admin'
    )
    expect(nonAdminWithLoadedAccess.canManageGrants).toBe(false)
    expect(nonAdminWithLoadedAccess.effectiveAdmin).toBe(false)
  })

  it('protects shared Default project only when both workspace and project have is_default=true', () => {
    expect(isProtectedSharedDefaultProject({ is_default: true }, { is_default: true })).toBe(true)
    expect(isProtectedSharedDefaultProject({ is_default: false }, { is_default: true })).toBe(false)
    expect(isProtectedSharedDefaultProject({ is_default: true }, { is_default: false })).toBe(false)
    expect(isProtectedSharedDefaultProject(null, { is_default: true })).toBe(false)
  })

  it('normalizes asymmetric project_grants and sends exact representation on PUT', async () => {
    const normalized = normalizeGrantInput({
      project_grants: [
        { project_id: '22', actions: ['keys.read', 'keys.read', ' '] },
        { project_id: '11', actions: ['text.chat', 'playground.run'] },
        { project_id: '33', actions: [] }
      ]
    })
    expect(normalized).toEqual({
      project_grants: [
        { project_id: '11', actions: ['playground.run', 'text.chat'] },
        { project_id: '22', actions: ['keys.read'] }
      ]
    })

    const putSpy = vi.spyOn(controlClient, 'controlPut').mockResolvedValue({
      member_id: '7',
      version: '4',
      effective_at: '2026-11-23T00:00:00Z'
    })

    await membersV2API.setMemberGrants('ws_1', '7', '3', {
      project_grants: [
        { project_id: '22', actions: ['keys.read'] },
        { project_id: '11', actions: ['text.chat'] }
      ]
    })

    expect(putSpy).toHaveBeenCalledWith(
      '/workspaces/ws_1/members/7/grants',
      {
        project_grants: [
          { project_id: '11', actions: ['text.chat'] },
          { project_id: '22', actions: ['keys.read'] }
        ]
      },
      { 'If-Match': '"3"' }
    )
  })

  it('computes exact per-project grant diffs, 412 conflict rows, and least-privilege presets', () => {
    const baseline = normalizeProjectGrants([
      { project_id: '11', actions: ['text.chat'] },
      { project_id: '22', actions: ['keys.read', 'usage.read'] }
    ])
    const draft = normalizeProjectGrants([
      { project_id: '11', actions: ['text.chat', 'playground.run'] },
      { project_id: '22', actions: ['keys.read'] }
    ])
    const diff = computeProjectGrantsDiff(baseline, draft)
    expect(diff.changedCount).toBe(2)
    expect(diff.rows).toEqual([
      { projectId: '11', added: ['playground.run'], removed: [], unchanged: false },
      { projectId: '22', added: [], removed: ['usage.read'], unchanged: false }
    ])

    const serverCurrent = normalizeProjectGrants([
      { project_id: '11', actions: ['text.chat'] },
      { project_id: '22', actions: ['keys.read', 'keys.create'] }
    ])
    const conflicts = computeConflictingProjectIds(baseline, serverCurrent, draft)
    expect(conflicts.has('11')).toBe(true)
    expect(conflicts.has('22')).toBe(true)

    const hydrated = hydrateProjectGrantsFromResponse({
      member_id: '7',
      version: '2',
      project_grants: [{ project_id: '99', actions: ['requests.read'] }]
    })
    expect(hydrated).toEqual([{ project_id: '99', actions: ['requests.read'] }])

    const callerActions = resolvePresetActions('caller', [
      'projects.read',
      'playground.run',
      'text.chat',
      'keys.create'
    ])
    expect(callerActions).toEqual(['playground.run', 'projects.read', 'text.chat'])
  })
})
