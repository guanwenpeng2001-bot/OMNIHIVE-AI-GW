import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

function readSource(path: string): string {
  return readFileSync(resolve(path), 'utf8')
}

/** 目录与显示名分开断言：目录必须来自共享常量，名字必须走共享解析器
 *  getPlatformLabel（而不是直接渲染 catalog 的静态 label）。 */
function expectSharedPlatformImport(source: string, catalog: string) {
  expect(source).toMatch(new RegExp(`import \\{[^}]*${catalog}[^}]*\\} from ['"]@/constants/platforms['"]`, 's'))
  expect(source).toMatch(/import \{[^}]*getPlatformLabel[^}]*\} from ['"]@\/constants\/platforms['"]/s)
}

describe('admin platform filters', () => {
  it('uses the group platform catalog on the subscriptions page', () => {
    const source = readSource('src/views/admin/SubscriptionsView.vue')
    expectSharedPlatformImport(source, 'PRODUCT_GROUP_PLATFORM_OPTIONS')
    expect(source).toMatch(/const platformFilterOptions[\s\S]*?\.\.\.PRODUCT_GROUP_PLATFORM_OPTIONS/)
  })

  it('uses the shared catalogs on the groups page', () => {
    const source = readSource('src/views/admin/GroupsView.vue')
    expect(source).toContain('...PRODUCT_GROUP_PLATFORM_OPTIONS')
    expect(source).toContain('...PRODUCT_PLATFORM_OPTIONS')
    expectSharedPlatformImport(source, 'PRODUCT_PLATFORM_OPTIONS')
  })

  it('uses the concrete platform catalog wherever concrete platforms are selected', () => {
    for (const path of [
      'src/components/admin/account/AccountTableFilters.vue',
      'src/components/admin/ErrorPassthroughRulesModal.vue',
      'src/views/admin/ops/components/OpsDashboardHeader.vue'
    ]) {
      const source = readSource(path)
      expectSharedPlatformImport(source, 'PRODUCT_PLATFORM_OPTIONS')
      expect(source).toMatch(/platformOptions\s*=.*PRODUCT_PLATFORM_OPTIONS|pOpts.*\.\.\.PRODUCT_PLATFORM_OPTIONS/s)
    }
  })
})
