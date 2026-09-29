import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import type { PaymentOrder } from '@/types/payment'

const api = vi.hoisted(() => ({ getOrder: vi.fn(), createOrder: vi.fn() }))
const auth = reactive({ user: { id: 1 } as { id: number } | null, token: 'token-a' as string | null })
vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))

import { advanceAuthSession } from '@/api/tokenRefresh'
import { usePaymentStore } from '../payment'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

describe('payment store request lifecycle', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    auth.user = { id: 1 }
    auth.token = 'token-a'
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('coalesces concurrent polls for the same order and permits retry after settlement', async () => {
    const pending = deferred<{ data: PaymentOrder }>()
    api.getOrder.mockReturnValue(pending.promise)
    const store = usePaymentStore()
    const first = store.pollOrderStatus(42)
    const second = store.pollOrderStatus(42)
    expect(api.getOrder).toHaveBeenCalledTimes(1)
    pending.resolve({ data: { id: 42, status: 'PAID' } as PaymentOrder })
    expect(await first).toEqual(await second)
    await store.pollOrderStatus(42)
    expect(api.getOrder).toHaveBeenCalledTimes(2)
  })

  it.each([null, { id: 2 }])('clears credentials and ignores old responses on identity change to %s', async (user) => {
    const pending = deferred<{ data: PaymentOrder }>()
    api.getOrder.mockReturnValue(pending.promise)
    const store = usePaymentStore()
    store.currentOrder = { id: 42 } as PaymentOrder
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, 'private credentials')
    const request = store.pollOrderStatus(42)
    auth.user = user
    expect(store.currentOrder).toBeNull()
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
    pending.resolve({ data: { id: 42, status: 'PAID' } as PaymentOrder })
    expect(await request).toBeNull()
    expect(store.currentOrder).toBeNull()
  })

  it('does not return old create-order credentials to the next account', async () => {
    const pending = deferred<{ data: { client_secret: string } }>()
    api.createOrder.mockReturnValue(pending.promise)
    const store = usePaymentStore()
    const request = store.createOrder({ amount: 10, payment_type: 'stripe', order_type: 'balance' })
    auth.user = { id: 2 }
    pending.resolve({ data: { client_secret: 'old-secret' } })
    await expect(request).rejects.toThrow('Payment session changed')
  })

  it('old completion cannot evict the new session pending request', async () => {
    const old = deferred<{ data: PaymentOrder }>()
    const fresh = deferred<{ data: PaymentOrder }>()
    api.getOrder.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const store = usePaymentStore()
    const staleRequest = store.pollOrderStatus(42)
    auth.user = { id: 2 }
    const newRequest = store.pollOrderStatus(42)
    old.resolve({ data: { id: 42, status: 'PAID' } as PaymentOrder })
    expect(await staleRequest).toBeNull()
    const coalesced = store.pollOrderStatus(42)
    expect(api.getOrder).toHaveBeenCalledTimes(2)
    fresh.resolve({ data: { id: 42, status: 'PENDING' } as PaymentOrder })
    expect(await newRequest).toEqual(await coalesced)
  })

  it('cancels an individual waiter without aborting other consumers', async () => {
    const pending = deferred<{ data: PaymentOrder }>()
    api.getOrder.mockReturnValue(pending.promise)
    const store = usePaymentStore()
    const controller = new AbortController()
    const cancelled = store.pollOrderStatus(42, controller.signal)
    const active = store.pollOrderStatus(42)
    const networkSignal = api.getOrder.mock.calls[0][1] as AbortSignal
    controller.abort()
    expect(await cancelled).toBeNull()
    expect(networkSignal.aborted).toBe(false)
    pending.resolve({ data: { id: 42, status: 'PAID' } as PaymentOrder })
    expect(await active).toMatchObject({ status: 'PAID' })
    expect(api.getOrder).toHaveBeenCalledTimes(1)
  })

  it('aborts the network when the last consumer leaves and ignores a late response', async () => {
    const pending = deferred<{ data: PaymentOrder }>()
    api.getOrder.mockReturnValue(pending.promise)
    const store = usePaymentStore()
    store.currentOrder = { id: 42, status: 'PENDING' } as PaymentOrder
    const controller = new AbortController()
    const request = store.pollOrderStatus(42, controller.signal)
    const networkSignal = api.getOrder.mock.calls[0][1] as AbortSignal
    controller.abort()
    expect(await request).toBeNull()
    expect(networkSignal.aborted).toBe(true)
    pending.resolve({ data: { id: 42, status: 'PAID' } as PaymentOrder })
    await pending.promise
    await Promise.resolve()
    expect(store.currentOrder.status).toBe('PENDING')
  })

  it('retains recovery on same-user refresh and token rotation, clearing as soon as auth is lost', () => {
    usePaymentStore()
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, 'credentials')
    auth.user = { id: 1 }
    auth.token = 'rotated-token-a'
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBe('credentials')
    auth.token = null
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })
  it('uses the shared session revision to stop a poll after same-user reauthentication', async () => {
    localStorage.setItem('auth_token', 'token-a')
    localStorage.setItem('auth_user', JSON.stringify({ id: 1 }))
    const pending = deferred<{ data: PaymentOrder }>()
    api.getOrder.mockReturnValue(pending.promise)
    const store = usePaymentStore()
    const request = store.pollOrderStatus(42)
    const signal = api.getOrder.mock.calls[0][1] as AbortSignal
    advanceAuthSession()
    expect(signal.aborted).toBe(true)
    pending.resolve({ data: { id: 42, status: 'PAID' } as PaymentOrder })
    expect(await request).toBeNull()
  })

  it('allows another poll after a failed shared request', async () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    api.getOrder.mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ data: { id: 42, status: 'PENDING' } })
    const store = usePaymentStore()
    expect(await store.pollOrderStatus(42)).toBeNull()
    expect(await store.pollOrderStatus(42)).toMatchObject({ id: 42, status: 'PENDING' })
    expect(api.getOrder).toHaveBeenCalledTimes(2)
    log.mockRestore()
  })

})
