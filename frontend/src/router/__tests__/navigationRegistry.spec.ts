import { describe, expect, it } from 'vitest'
import router from '../index'
import {
  CORE_NAVIGATION_ITEMS,
  resolveNavigationBreadcrumb,
  type ShellNavItemSpec
} from '../navigationRegistry'

function collectAllNavSpecs(entries: readonly ShellNavItemSpec[]): ShellNavItemSpec[] {
  return entries.flatMap((entry) =>
    entry.children && entry.children.length > 0
      ? [entry, ...collectAllNavSpecs(entry.children)]
      : [entry]
  )
}

const allNavItems = collectAllNavSpecs(CORE_NAVIGATION_ITEMS)
const paths = new Set(allNavItems.map((item) => item.path))

describe('core console navigation surfaces', () => {
  it.each([
    '/docs',
    '/compatibility',
    '/model-plaza/compare',
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
    '/admin/audit',
    '/admin/members',
    '/admin/plugins',
    '/admin/settings/general'
  ])('registers %s for the command palette and console navigation', (path) => {
    expect(paths.has(path)).toBe(true)
  })
})

describe('navigationRegistry <-> router consistency contract', () => {
  it.each(allNavItems.map((item) => [item.id, item.path] as const))(
    'resolves %s (%s) to a real route rather than NotFound',
    (_id, path) => {
      const resolved = router.resolve(path)
      expect(resolved.matched.length).toBeGreaterThan(0)
      expect(resolved.name).not.toBe('NotFound')
    }
  )

  it('matches slash-containing model IDs to PublicModelDetail while keeping /model-plaza/compare ahead', () => {
    const compareResolved = router.resolve('/model-plaza/compare')
    expect(compareResolved.name).toBe('ModelCompare')

    const rawSlashResolved = router.resolve('/model-plaza/auto/gemini-3.8-flash')
    expect(rawSlashResolved.name).toBe('PublicModelDetail')
    expect(rawSlashResolved.params.id).toBe('auto/gemini-3.8-flash')

    const encodedSlashResolved = router.resolve('/model-plaza/auto%2Fgemini-3.8-flash')
    expect(encodedSlashResolved.name).toBe('PublicModelDetail')
    expect(encodedSlashResolved.params.id).toBe('auto/gemini-3.8-flash')
  })

  it.each([
    ['/admin/security-audit', 'AdminPromptAudit'],
    ['/admin/members', 'AdminMembers'],
    ['/admin/models', 'AdminModelsCatalog'],
    ['/admin/settings/diagnostics', 'AdminSettingsSection'],
    ['/admin/settings/general', 'AdminSettingsSection'],
    ['/admin/settings/security', 'AdminSettingsSection']
  ])('resolves %s to %s', (path, expectedName) => {
    const resolved = router.resolve(path)
    expect(resolved.name).toBe(expectedName)
  })

  it.each([
    ['/admin/pricing', '/admin/channels/pricing'],
    ['/admin/reconciliation', '/admin/billing'],
    ['/admin/project-audit', '/admin/audit'],
    ['/channels/probe', '/monitor'],
    ['/channels/telemetry', '/monitor'],
    ['/quota', '/key-usage'],
    ['/workspaces/2/keys', '/keys']
  ])('registers semantic redirect from %s toward %s without hitting NotFound', (fromPath) => {
    const resolved = router.resolve(fromPath)
    expect(resolved.matched.length).toBeGreaterThan(0)
    expect(resolved.name).not.toBe('NotFound')
    const leafRecord = resolved.matched[resolved.matched.length - 1]
    expect(leafRecord?.redirect).toBeDefined()
  })

  it.each([
    ['/admin/connections/new', '管理 · 连接与路由'],
    ['/admin/connections/42/edit', '管理 · 连接与路由'],
    ['/admin/models', '管理 · 连接与路由'],
    ['/admin/pools', '管理 · 连接与路由'],
    ['/admin/routing', '管理 · 连接与路由'],
    ['/admin/billing', '管理 · 结算与套餐'],
    ['/admin/requests', '管理 · 运行观测'],
    ['/admin/usage', '管理 · 运行观测'],
    ['/admin/audit-logs', '管理 · 系统与治理'],
    ['/admin/members', '管理 · 系统与治理'],
    ['/admin/settings/security', '管理 · 系统与治理']
  ])('resolves breadcrumb section for %s to %s', (path, expectedDomainZh) => {
    expect(resolveNavigationBreadcrumb(path).domainLabelZh).toBe(expectedDomainZh)
  })

  it('resolves /keys, /byok, and /workspaces to 开发者台 · 访问与空间', () => {
    expect(resolveNavigationBreadcrumb('/keys').domainLabelZh).toBe('开发者台 · 访问与空间')
    expect(resolveNavigationBreadcrumb('/byok').domainLabelZh).toBe('开发者台 · 访问与空间')
    expect(resolveNavigationBreadcrumb('/workspaces').domainLabelZh).toBe('开发者台 · 访问与空间')
  })

  it('attaches featureFlag metadata and canonical titleKey on gated user routes [P1-3, P2-1]', () => {
    const availableChannels = router.resolve('/available-channels')
    expect(availableChannels.meta.featureFlag).toBe('availableChannels')
    expect(availableChannels.meta.titleKey).toBe('nav.availableChannels')

    expect(router.resolve('/monitor').meta.featureFlag).toBe('channelMonitor')
    expect(router.resolve('/affiliate').meta.featureFlag).toBe('affiliate')
  })
})
