import { describe, expect, it } from 'vitest'
import { firstAvailableScope } from '../useProjectScope'

describe('project scope bootstrap', () => {
  it('selects the first workspace that has a project', () => {
    expect(firstAvailableScope([
      { workspace_id: '1', projects: [] },
      { workspace_id: '2', projects: [{ id: '9' }] },
      { workspace_id: '3', projects: [{ id: '10' }] }
    ])).toEqual({ workspaceId: '2', projectId: '9' })
  })

  it('does not invent a scope when no project is available', () => {
    expect(firstAvailableScope([{ workspace_id: '1', projects: [] }])).toBeNull()
  })
})
