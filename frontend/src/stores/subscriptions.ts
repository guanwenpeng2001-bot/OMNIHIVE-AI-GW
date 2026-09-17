/**
 * Subscription Store
 * Global state management for user subscriptions with caching and deduplication
 */

import { defineStore } from 'pinia'
import { ref, computed, watch, onScopeDispose } from 'vue'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription } from '@/types'
import { useAuthStore } from './auth'

// Cache TTL: 60 seconds
const CACHE_TTL_MS = 60_000

export const useSubscriptionStore = defineStore('subscriptions', () => {
  const authStore = useAuthStore()
  // Keep generations local to this store, including isolated Pinia instances.
  let requestGeneration = 0
  // State
  const activeSubscriptions = ref<UserSubscription[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref(false)
  const lastFetchedAt = ref<number | null>(null)

  // In-flight request deduplication
  let activePromise: Promise<UserSubscription[]> | null = null

  // Auto-refresh interval
  let pollerInterval: ReturnType<typeof setInterval> | null = null

  // A peer-tab account switch can keep isAuthenticated=true throughout.
  // Invalidate before any new page can consume the previous principal's cache.
  watch(
    [() => authStore.user?.id, () => authStore.user?.role, () => authStore.isAuthenticated, () => authStore.sessionRevision],
    clear,
    { flush: 'sync' }
  )
  onScopeDispose(clear)

  // Computed
  const hasActiveSubscriptions = computed(() => activeSubscriptions.value.length > 0)

  /**
   * Fetch active subscriptions with caching and deduplication
   * @param force - Force refresh even if cache is valid
   */
  async function fetchActiveSubscriptions(force = false): Promise<UserSubscription[]> {
    const now = Date.now()

    // Return cached data if valid
    if (
      !force &&
      loaded.value &&
      lastFetchedAt.value &&
      now - lastFetchedAt.value < CACHE_TTL_MS
    ) {
      return activeSubscriptions.value
    }

    // Return in-flight request if exists (deduplication)
    if (activePromise && !force) {
      return activePromise
    }

    const currentGeneration = ++requestGeneration

    // Start new request
    loading.value = true
    error.value = false
    const requestPromise = subscriptionsAPI
      .getActiveSubscriptions()
      .then((data) => {
        if (currentGeneration !== requestGeneration) return []
        activeSubscriptions.value = data
        loaded.value = true
        lastFetchedAt.value = Date.now()
        return data
      })
      .catch((cause) => {
        if (currentGeneration === requestGeneration) error.value = true
        console.error('Failed to fetch active subscriptions:', cause)
        throw cause
      })
      .finally(() => {
        if (activePromise === requestPromise) {
          loading.value = false
          activePromise = null
        }
      })

    activePromise = requestPromise

    return activePromise
  }

  /**
   * Start auto-refresh polling 
   */
  function startPolling() {
    if (pollerInterval) return

    pollerInterval = setInterval(() => {
      fetchActiveSubscriptions(true).catch((cause) => {
        console.error('Subscription polling failed:', cause)
      })
    }, 5 * 60 * 1000)
  }

  /**
   * Stop auto-refresh polling
   */
  function stopPolling() {
    if (pollerInterval) {
      clearInterval(pollerInterval)
      pollerInterval = null
    }
  }

  /**
   * Clear all subscription data and stop polling
   */
  function clear() {
    requestGeneration++
    activePromise = null
    activeSubscriptions.value = []
    loaded.value = false
    loading.value = false
    error.value = false
    lastFetchedAt.value = null
    stopPolling()
  }

  /**
   * Invalidate cache (force next fetch to reload)
   */
  function invalidateCache() {
    lastFetchedAt.value = null
  }

  return {
    // State
    activeSubscriptions,
    loaded,
    error,
    loading,
    hasActiveSubscriptions,

    // Actions
    fetchActiveSubscriptions,
    startPolling,
    stopPolling,
    clear,
    invalidateCache
  }
})
