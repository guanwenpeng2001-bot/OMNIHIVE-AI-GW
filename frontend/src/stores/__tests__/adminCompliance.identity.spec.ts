import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, getActivePinia, setActivePinia } from 'pinia'
import { useAuthStore } from '../auth'
import { useAdminComplianceStore } from '../adminCompliance'
import { advanceAuthSession } from '@/api/tokenRefresh'
import type { AdminComplianceStatus } from '@/api/admin/compliance'
import type { User } from '@/types'

const api = vi.hoisted(() => ({ getStatus: vi.fn(), accept: vi.fn() }))
vi.mock('@/api', () => ({}))
vi.mock('@/api/admin/compliance', () => ({ default: api }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const accepted = { required: false } as AdminComplianceStatus
function deferred() {
  let resolve!: (status: AdminComplianceStatus) => void
  const promise = new Promise<AdminComplianceStatus>(done => { resolve = done })
  return { promise, resolve }
}
function login(id: number) {
  const auth = useAuthStore()
  auth.user = { id, role: 'admin' } as User
  auth.token = `token-${id}`
}
beforeEach(() => {
  localStorage.clear()
  vi.resetAllMocks()
  setActivePinia(createPinia())
  login(1)
})
afterEach(() => disposePinia(getActivePinia()!))

describe('compliance identity lifecycle', () => {
  it('deduplicates status checks and retains acknowledgement on token rotation', async () => {
    const pending = deferred()
    api.getStatus.mockReturnValue(pending.promise)
    const store = useAdminComplianceStore()
    const first = store.fetchStatus()
    const second = store.fetchStatus()
    expect(api.getStatus).toHaveBeenCalledTimes(1)
    useAuthStore().token = 'renewed'
    pending.resolve(accepted)
    await Promise.all([first, second])
    expect(store.initialized).toBe(true)
    await store.fetchStatus()
    expect(api.getStatus).toHaveBeenCalledTimes(1)
  })

  it('ignores A status while B status remains loading', async () => {
    const old = deferred()
    const current = deferred()
    api.getStatus.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const store = useAdminComplianceStore()
    const first = store.fetchStatus()
    login(2)
    const second = store.fetchStatus()
    old.resolve(accepted)
    await first
    expect(store.status).toBeNull()
    expect(store.loading).toBe(true)
    current.resolve({ required: true } as AdminComplianceStatus)
    await second
    expect(store.required).toBe(true)
    expect(store.loading).toBe(false)
  })

  it('invalidates same-user acceptance on a new login generation', async () => {
    const pending = deferred()
    api.accept.mockReturnValue(pending.promise)
    const store = useAdminComplianceStore()
    const request = store.accept('phrase')
    localStorage.setItem('auth_user', JSON.stringify(useAuthStore().user))
    localStorage.setItem('auth_token', 'new-login')
    advanceAuthSession()
    expect(store.submitting).toBe(false)
    pending.resolve(accepted)
    await request
    expect(store.status).toBeNull()
    expect(store.initialized).toBe(false)
  })

  it('does not let an older status overwrite an acknowledgement or a new 423', async () => {
    const pending = deferred()
    api.getStatus.mockReturnValue(pending.promise)
    api.accept.mockResolvedValue(accepted)
    const store = useAdminComplianceStore()
    const request = store.fetchStatus()
    await store.accept('phrase')
    store.requireAcknowledgement()
    pending.resolve(accepted)
    await request
    expect(store.required).toBe(true)
  })
})
