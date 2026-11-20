import { describe, expect, it } from 'vitest'
import { computeBulkDiffSummary, toggleBulkSelection } from '../wizardState'
import type { ConnectionItem } from '../types'

describe('connections/bulkDiff', () => {
  it('summarizes applicable, blocked, conflict, and applied items', () => {
    const items: ConnectionItem[] = [
      { id: 'conn_1', base_version: '2', state: 'applicable' },
      { id: 'conn_2', base_version: '4', state: 'conflict', reason: 'Version mismatch' },
      { id: 'conn_3', base_version: '1', state: 'blocked', reason: 'Missing permission' },
      { id: 'conn_4', base_version: '5', state: 'applicable' },
      { id: 'conn_5', base_version: '6', state: 'applied' }
    ]

    expect(computeBulkDiffSummary(items)).toEqual({
      applicableCount: 2,
      blockedCount: 1,
      conflictCount: 1,
      appliedCount: 1,
      selectableIds: ['conn_1', 'conn_4']
    })
  })

  it('toggles bulk selection immutably', () => {
    const initial = ['conn_1', 'conn_4']
    expect(toggleBulkSelection(initial, 'conn_1')).toEqual(['conn_4'])
    expect(toggleBulkSelection(initial, 'conn_7')).toEqual(['conn_1', 'conn_4', 'conn_7'])
    expect(initial).toEqual(['conn_1', 'conn_4'])
  })
})
