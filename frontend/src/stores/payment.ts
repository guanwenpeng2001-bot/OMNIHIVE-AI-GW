/**
 * Payment Store
 * Manages payment configuration, current order state, and subscription plans
 */

import { defineStore } from 'pinia'
import { ref, watch, onScopeDispose } from 'vue'
import { useAuthStore } from './auth'
import { PAYMENT_RECOVERY_STORAGE_KEY, readOwnedPaymentRecoverySnapshot } from '@/components/payment/paymentFlow'
import { AUTH_SESSION_EVENT, AUTH_SESSION_KEY, captureAuthSession, isAuthSessionCurrent } from '@/api/tokenRefresh'
import { paymentAPI } from '@/api/payment'
import type { PaymentConfig, PaymentOrder, SubscriptionPlan, CreateOrderRequest } from '@/types/payment'

interface PendingOrder {
  promise: Promise<PaymentOrder | null>
  controller: AbortController
  consumers: number
}

export const usePaymentStore = defineStore('payment', () => {
  const authStore = useAuthStore()
  let generation = 0
  const pendingOrders = new Map<number, PendingOrder>()
  // ==================== State ====================

  /** Payment configuration from backend */
  const config = ref<PaymentConfig | null>(null)
  /** Currently active order (for payment flow) */
  const currentOrder = ref<PaymentOrder | null>(null)
  /** Available subscription plans */
  const plans = ref<SubscriptionPlan[]>([])

  const configLoading = ref(false)
  const configLoaded = ref(false)
  let configPromise: Promise<PaymentConfig | null> | null = null

  // Reconcile persisted state even when the account changed before this store existed.
  readOwnedPaymentRecoverySnapshot(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY))

  watch([() => authStore.user?.id, () => !!authStore.token], () => {
    clearCurrentOrder()
    localStorage.removeItem(PAYMENT_RECOVERY_STORAGE_KEY)
  }, { flush: 'sync' })

  let session = captureAuthSession()
  const reconcileSession = () => {
    if (isAuthSessionCurrent(session)) return
    session = captureAuthSession()
    clearCurrentOrder()
    localStorage.removeItem(PAYMENT_RECOVERY_STORAGE_KEY)
  }
  const onStorage = (event: StorageEvent) => {
    if (!event.key || [AUTH_SESSION_KEY, 'auth_user', 'auth_token'].includes(event.key)) reconcileSession()
  }
  window.addEventListener(AUTH_SESSION_EVENT, reconcileSession)
  window.addEventListener('storage', onStorage)
  onScopeDispose(() => {
    clearCurrentOrder()
    window.removeEventListener(AUTH_SESSION_EVENT, reconcileSession)
    window.removeEventListener('storage', onStorage)
  })

  // ==================== Actions ====================

  /** Fetch payment configuration */
  async function fetchConfig(force = false): Promise<PaymentConfig | null> {
    reconcileSession()
    if (configPromise) return configPromise
    if (configLoaded.value && !force) return config.value
    configPromise = loadConfig()
    return configPromise
  }

  async function loadConfig(): Promise<PaymentConfig | null> {
    const requestGeneration = generation
    const requestSession = captureAuthSession()

    configLoading.value = true
    try {
      const response = await paymentAPI.getConfig()
      if (requestGeneration !== generation || !isAuthSessionCurrent(requestSession)) return null
      config.value = response.data
      configLoaded.value = true
      return config.value
    } catch (error: unknown) {
      console.error('[payment] Failed to fetch config:', error)
      return null
    } finally {
      if (requestGeneration === generation) {
        configLoading.value = false
        configPromise = null
      }
    }
  }

  /** Fetch available subscription plans */
  async function fetchPlans(): Promise<SubscriptionPlan[]> {
    reconcileSession()
    const requestGeneration = generation
    const requestSession = captureAuthSession()
    try {
      const response = await paymentAPI.getPlans()
      if (requestGeneration !== generation || !isAuthSessionCurrent(requestSession)) return []
      // Backend returns features as newline-separated string; parse to array
      plans.value = (response.data || []).map((p: Omit<SubscriptionPlan, 'features'> & { features: string | string[] }) => ({
        ...p,
        features: typeof p.features === 'string'
          ? p.features.split('\n').map((f: string) => f.trim()).filter(Boolean)
          : (p.features || []),
      }))
      return plans.value
    } catch (error: unknown) {
      console.error('[payment] Failed to fetch plans:', error)
      return []
    }
  }

  /** Create a new order and set it as current */
  async function createOrder(params: CreateOrderRequest) {
    reconcileSession()
    const requestSession = captureAuthSession()
    const requestGeneration = generation
    const response = await paymentAPI.createOrder(params)
    if (requestGeneration !== generation || !isAuthSessionCurrent(requestSession)) throw new Error('Payment session changed')
    return response.data
  }

  /** Poll order status by ID (read-only, no upstream check) */
  function pollOrderStatus(orderId: number, signal?: AbortSignal): Promise<PaymentOrder | null> {
    reconcileSession()
    if (signal?.aborted) return Promise.resolve(null)
    let pending = pendingOrders.get(orderId)
    if (!pending) {
      const requestGeneration = generation
      const requestSession = captureAuthSession()
      const controller = new AbortController()
      const request = paymentAPI.getOrder(orderId, controller.signal).then(response => {
        if (requestGeneration !== generation || !isAuthSessionCurrent(requestSession) || controller.signal.aborted) return null
        const order = response.data
        if (currentOrder.value?.id === orderId) currentOrder.value = order
        return order
      }).catch((error: unknown) => {
        if (requestGeneration === generation && !controller.signal.aborted) {
          console.error('[payment] Failed to poll order status:', error)
        }
        return null
      }).finally(() => {
        if (pendingOrders.get(orderId) === pending) pendingOrders.delete(orderId)
      })
      pending = { promise: request, controller, consumers: 0 }
      pendingOrders.set(orderId, pending)
    }

    // Each consumer owns its cancellation; keep a shared request alive for peers.
    const entry = pending
    entry.consumers += 1
    return new Promise(resolve => {
      let settled = false
      const finish = (order: PaymentOrder | null) => {
        if (settled) return
        settled = true
        signal?.removeEventListener('abort', cancel)
        entry.consumers -= 1
        if (entry.consumers === 0 && pendingOrders.get(orderId) === entry) {
          pendingOrders.delete(orderId)
          entry.controller.abort()
        }
        resolve(order)
      }
      const cancel = () => finish(null)
      signal?.addEventListener('abort', cancel, { once: true })
      entry.promise.then(finish)
    })
  }

  /** Clear current order state */
  function clearCurrentOrder() {
    generation += 1
    configPromise = null
    configLoading.value = false
    configLoaded.value = false
    config.value = null
    plans.value = []
    pendingOrders.forEach(request => request.controller.abort())
    pendingOrders.clear()
    currentOrder.value = null
  }

  return {
    config,
    currentOrder,
    plans,
    configLoading,
    configLoaded,
    fetchConfig,
    fetchPlans,
    createOrder,
    pollOrderStatus,
    clearCurrentOrder
  }
})
