import { describe, expect, it, vi, beforeEach } from 'vitest'
import { buildIfMatchHeaders, normalizeGrantInput, projectMemberPermissions, resolveCasMutationError } from '../cas'
import { membersV2API } from '../api'
import * as v2Client from '@/features/shared/v2Client'

describe('members CAS & role projections', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('builds If-Match header from non-empty version and rejects missing version', () => {
    expect(buildIfMatchHeaders('v-42')).toEqual({ 'If-Match': 'v-42' })
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
    const putSpy = vi.spyOn(v2Client, 'v2Put').mockResolvedValue({
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
      { 'If-Match': '1' }
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
    expect(readOnly.canViewMembers).toBe(true)
    expect(readOnly.canManageGrants).toBe(false)
    expect(readOnly.canManageProjects).toBe(false)

    const admin = projectMemberPermissions(null, 'admin')
    expect(admin.canManageGrants).toBe(true)
    expect(admin.canManageProjects).toBe(true)
  })
})
