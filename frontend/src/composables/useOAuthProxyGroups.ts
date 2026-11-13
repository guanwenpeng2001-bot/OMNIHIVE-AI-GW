import { ref } from 'vue'
import { proxyGroupsAPI, type ProxyGroup } from '@/api/admin/proxyGroups'

export function unwrapOAuthProxyGroups(data: unknown): ProxyGroup[] {
  if (Array.isArray(data)) return data
  if (data && typeof data === 'object' && 'items' in data && Array.isArray(data.items)) {
    return data.items
  }
  return []
}

export function useOAuthProxyGroups() {
  const proxyGroups = ref<ProxyGroup[]>([])
  const loadingProxyGroups = ref(false)
  let requestSequence = 0

  async function loadProxyGroups(): Promise<void> {
    const sequence = ++requestSequence
    loadingProxyGroups.value = true
    try {
      const groups: ProxyGroup[] = []
      let page = 1
      let received = 0
      let total = 0
      do {
        const data: unknown = await proxyGroupsAPI.list(page++, 100, { status: 'active' })
        if (sequence !== requestSequence) return
        const items = unwrapOAuthProxyGroups(data)
        received += items.length
        groups.push(...items.filter(group => group?.status === 'active'))
        total = data && typeof data === 'object' && 'total' in data &&
          typeof data.total === 'number' && Number.isFinite(data.total) ? data.total : 0
        if (!items.length) break
      } while (received < total)
      proxyGroups.value = groups
    } finally {
      if (sequence === requestSequence) loadingProxyGroups.value = false
    }
  }

  return { proxyGroups, loadingProxyGroups, loadProxyGroups }
}
