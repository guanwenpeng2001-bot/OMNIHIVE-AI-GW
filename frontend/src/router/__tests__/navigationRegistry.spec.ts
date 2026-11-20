import { describe, expect, it } from 'vitest'
import { CORE_NAVIGATION_ITEMS } from '../navigationRegistry'

const paths = new Set(CORE_NAVIGATION_ITEMS.map((item) => item.path))

describe('core console navigation surfaces', () => {
  it.each([
    '/byok',
    '/playground',
    '/requests',
    '/tasks',
    '/billing',
    '/admin/connections/new',
    '/admin/pools',
    '/admin/routing',
    '/admin/requests',
    '/admin/billing',
    '/admin/audit'
  ])('registers %s for the command palette and console navigation', (path) => {
    expect(paths.has(path)).toBe(true)
  })
})
