import { effectScope, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { projectScope, queryParams, requestLocation } from '../api'
import { requestFilters, timeWindow } from '../filters'
import { useCursor } from '../useCursor'

describe('scoped request filters', () => {
  it('converts an exclusive calendar window across a daylight-saving boundary', () => {
    expect(timeWindow({ from: '2026-11-01', to: '2026-11-02', timezone: 'America/New_York' })).toEqual({
      from: '2026-11-01T04:00:00.000Z', to: '2026-11-02T05:00:00.000Z', timezone: 'America/New_York',
    })
    expect(timeWindow({ from: '2026-02-31', to: '2026-03-01', timezone: 'UTC' })).toBeNull()
    expect(timeWindow({ from: '2026-11-01', to: '2026-11-01', timezone: 'UTC' })).toBeNull()
  })

  it('preserves decimal identities and rejects ambiguous query scopes', () => {
    const scope = projectScope('9007199254740993', '24')
    expect(requestLocation(scope!, 'request/42')).toEqual({
      path: '/requests/request%2F42', query: { workspace_id: '9007199254740993', project_id: '24' },
    })
    expect(projectScope(['7', '8'], '24')).toBeNull()
    const filters = requestFilters({ key_id: '9007199254740993', model_id: 'anthropic/fixture', outcome: 'interrupted', timezone: 'UTC' })
    expect(queryParams({ key_id: filters.key_id, model_id: filters.model_id, outcome: filters.outcome }).toString()).toBe(
      'key_id=9007199254740993&model_id=anthropic%2Ffixture&outcome=interrupted',
    )
  })

  it('discards seek history when the project or filters change', () => {
    const component = effectScope()
    const key = ref('project/24/failed')
    const pager = component.run(() => useCursor(() => key.value))!
    pager.next('opaque-a')
    pager.next('opaque-b')
    pager.previous()
    expect(pager.cursor.value).toBe('opaque-a')
    expect(pager.hasPrevious.value).toBe(true)
    key.value = 'project/25/failed'
    expect(pager.cursor.value).toBeNull()
    expect(pager.hasPrevious.value).toBe(false)
    component.stop()
  })
})
