import path from 'node:path'
import { describe, expect, it } from 'vitest'
// @ts-expect-error ESM script without .d.ts declaration
import { runTypeDriftChecks } from '../../../../scripts/check-frontend-type-drift.mjs'

describe('OpenAPI & catalog type drift guard', () => {
  it('passes with zero duplicate catalog DTOs, zero heuristic availability guessing, and single v2 catalog client', () => {
    const repoRoot = path.resolve(process.cwd(), '..')
    const result = runTypeDriftChecks(repoRoot)
    expect(result.violations).toEqual([])
    expect(result.ok).toBe(true)
  })
})
