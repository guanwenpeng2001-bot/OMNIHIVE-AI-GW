import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('Composite channel platform options', () => {
  it('includes the CN concrete providers for pricing and model mapping', () => {
    const source = readFileSync(resolve('src/views/admin/ChannelsView.vue'), 'utf8')
    expect(source).toContain('CONCRETE_PLATFORM_OPTIONS.map(option => option.value)')
    expect(source).toContain('const compositePlatforms = platformOrder')

  })
})
