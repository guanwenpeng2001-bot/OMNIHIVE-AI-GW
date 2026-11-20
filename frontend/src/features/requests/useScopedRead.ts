import { computed, onMounted, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { isSilentRequestError } from '@/api/client'
import { normalizeApiError, type NormalizedApiError } from '@/utils/apiError'

export function useScopedRead<T>(key: () => string, read: (signal: AbortSignal) => Promise<T>) {
  const data = shallowRef<T | null>(null)
  const error = shallowRef<NormalizedApiError | null>(null)
  const loading = ref(false)
  const lastSynced = ref<string | null>(null)
  const offline = ref(typeof navigator !== 'undefined' && !navigator.onLine)
  let active: AbortController | null = null

  async function refresh() {
    if (!key()) return
    active?.abort()
    const request = new AbortController()
    active = request
    loading.value = true
    error.value = null
    try {
      const result = await read(request.signal)
      if (active !== request || request.signal.aborted) return
      data.value = result
      lastSynced.value = new Date().toISOString()
    } catch (cause) {
      if (active !== request || request.signal.aborted || isSilentRequestError(cause)) return
      const issue = normalizeApiError(cause)
      if (issue.status === 401 || issue.status === 403 || issue.status === 404) data.value = null
      error.value = issue
    } finally {
      if (active === request) loading.value = false
    }
  }

  function updateOnline() {
    offline.value = !navigator.onLine
  }

  function clear() {
    active?.abort()
    active = null
    loading.value = false
    data.value = null
    error.value = null
    lastSynced.value = null
  }

  watch(key, () => {
    clear()
    if (key()) void refresh()
  }, { immediate: true, flush: 'sync' })

  onMounted(() => {
    window.addEventListener('online', updateOnline)
    window.addEventListener('offline', updateOnline)
  })
  onScopeDispose(() => {
    active?.abort()
    window.removeEventListener('online', updateOnline)
    window.removeEventListener('offline', updateOnline)
  })

  const stale = computed(() => data.value !== null && (offline.value || error.value !== null))
  return { data, error, loading, stale, offline, lastSynced, refresh }
}
