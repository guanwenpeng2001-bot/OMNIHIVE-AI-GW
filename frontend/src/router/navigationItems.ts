import type { Router } from 'vue-router'

interface NavigationItem {
  path: string
  expandOnly?: boolean
  children?: NavigationItem[]
}

/** Preserve ordering and distinct parameters, ignoring translated labels. */
export function uniqueNavigationItems<Item extends NavigationItem>(items: Item[], router: Router): Item[] {
  const seen = new Set<string>()
  const visit = (entries: Item[]): Item[] => entries.flatMap(item => {
    if (!item.expandOnly) {
      const route = router.resolve(item.path)
      const record = route.matched[route.matched.length - 1]
      const identity = JSON.stringify([
        record?.aliasOf?.path ?? record?.path ?? route.path,
        Object.entries(route.params).sort(), Object.entries(route.query).sort(), route.hash
      ])
      if (seen.has(identity)) return []
      seen.add(identity)
    }
    return [{ ...item, ...(item.children ? { children: visit(item.children as Item[]) } : {}) }]
  })
  return visit(items)
}
