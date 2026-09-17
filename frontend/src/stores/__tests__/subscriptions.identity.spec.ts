import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, getActivePinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { useSubscriptionStore } from '@/stores/subscriptions'
import subscriptionsAPI from '@/api/subscriptions'
import type { User, UserSubscription } from '@/types'

vi.mock('@/api', () => ({}))
vi.mock('@/api/subscriptions', () => ({ default: { getActiveSubscriptions: vi.fn() } }))

const subscription = { id: 1, user_id: 1, group_id: 3 } as UserSubscription
function login(id: number) {
  const auth = useAuthStore()
  auth.token = `token-${id}`
  auth.user = { id, role: 'user' } as User
  return auth
}

describe('subscription identity lifecycle', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.resetAllMocks()
    login(1)
  })
  afterEach(() => {
    disposePinia(getActivePinia()!)
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it('clears cached subscriptions immediately on a direct account switch', async () => {
    const store = useSubscriptionStore()
    vi.mocked(subscriptionsAPI.getActiveSubscriptions).mockResolvedValueOnce([subscription])
    await store.fetchActiveSubscriptions()
    login(2)
    expect(store.activeSubscriptions).toEqual([])
    expect(store.loaded).toBe(false)
    vi.mocked(subscriptionsAPI.getActiveSubscriptions).mockResolvedValueOnce([])
    expect(await store.fetchActiveSubscriptions()).toEqual([])
    expect(subscriptionsAPI.getActiveSubscriptions).toHaveBeenCalledTimes(2)
  })

  it('discards in-flight data after switching accounts, including the returned result', async () => {
    let resolve!: (rows: UserSubscription[]) => void
    vi.mocked(subscriptionsAPI.getActiveSubscriptions).mockReturnValueOnce(new Promise(r => { resolve = r }))
    const store = useSubscriptionStore()
    const oldRequest = store.fetchActiveSubscriptions()
    login(2)
    resolve([subscription])
    expect(await oldRequest).toEqual([])
    expect(store.activeSubscriptions).toEqual([])
    expect(store.loaded).toBe(false)
  })

  it('preserves the cache for a profile update and token rotation of the same principal', async () => {
    const store = useSubscriptionStore()
    vi.mocked(subscriptionsAPI.getActiveSubscriptions).mockResolvedValueOnce([subscription])
    await store.fetchActiveSubscriptions()
    const auth = useAuthStore()
    auth.token = 'rotated'
    auth.user = { ...auth.user!, balance: 42 }
    expect(store.activeSubscriptions).toEqual([subscription])
    expect(await store.fetchActiveSubscriptions()).toEqual([subscription])
    expect(subscriptionsAPI.getActiveSubscriptions).toHaveBeenCalledTimes(1)
  })

  it('stops polling on logout and on store disposal', async () => {
    const store = useSubscriptionStore()
    vi.mocked(subscriptionsAPI.getActiveSubscriptions).mockResolvedValue([])
    store.startPolling()
    useAuthStore().token = null
    await vi.advanceTimersByTimeAsync(300_000)
    expect(subscriptionsAPI.getActiveSubscriptions).not.toHaveBeenCalled()
    login(2)
    store.startPolling()
    store.$dispose()
    await vi.advanceTimersByTimeAsync(300_000)
    expect(subscriptionsAPI.getActiveSubscriptions).not.toHaveBeenCalled()
  })
})
