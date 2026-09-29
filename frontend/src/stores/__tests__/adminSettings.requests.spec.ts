import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, getActivePinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { useAdminSettingsStore } from '../adminSettings'
const mocks = vi.hoisted(() => ({ settings: vi.fn(), payment: vi.fn(), auth: null as any }))
vi.mock('@/api', () => ({ adminAPI: { settings: { getSettings: mocks.settings }, payment: { getConfig: mocks.payment } } }))
vi.mock('../auth', () => ({ useAuthStore: () => mocks.auth }))
function deferred() {
  let resolve!: (value: any) => void
  let reject!: (error: Error) => void
  const promise = new Promise<any>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  setActivePinia(createPinia())
  mocks.auth = reactive({ user: { id: 1, role: 'admin' }, isAuthenticated: true, isAdmin: true, sessionRevision: 1 })
  mocks.payment.mockResolvedValue({ data: { enabled: true } })
})
afterEach(() => disposePinia(getActivePinia()!))
describe('admin settings request lifecycle', () => {
  it('makes both concurrent consumers wait for exactly one pair of requests', async () => {
    const response = deferred()
    mocks.settings.mockReturnValue(response.promise)
    const store = useAdminSettingsStore()
    let settled = false
    const first = store.fetch()
    const second = store.fetch(true).then(() => { settled = true })
    await Promise.resolve()
    expect(settled).toBe(false)
    expect(mocks.settings).toHaveBeenCalledTimes(1)
    expect(mocks.payment).toHaveBeenCalledTimes(1)
    response.resolve({ ops_monitoring_enabled: false })
    await Promise.all([first, second])
    expect(store.loaded).toBe(true)
    expect(store.opsMonitoringEnabled).toBe(false)
  })
  it('retries after failure', async () => {
    mocks.settings.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({})
    const store = useAdminSettingsStore()
    await store.fetch()
    expect(store.loaded).toBe(false)
    await store.fetch()
    expect(mocks.settings).toHaveBeenCalledTimes(2)
    expect(store.loaded).toBe(true)
  })
  it.each(['user', 'session', 'role', 'save'])('invalidates pending results on %s changes', async change => {
    const old = deferred()
    const current = deferred()
    mocks.settings.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const store = useAdminSettingsStore()
    const first = store.fetch()
    if (change === 'user') mocks.auth.user.id++
    if (change === 'session') mocks.auth.sessionRevision++
    if (change === 'role') { mocks.auth.user.role = 'user'; mocks.auth.isAdmin = false }
    if (change === 'save') store.setOpsMonitoringEnabledLocal(false)
    const second = store.fetch()
    old.resolve({ custom_menu_items: [{ id: 'private' }], ops_monitoring_enabled: true })
    await first
    expect(store.customMenuItems).toEqual([])
    expect(store.loading).toBe(change !== 'role')
    if (change === 'save') expect(store.opsMonitoringEnabled).toBe(false)
    current.resolve({})
    await second
  })
  it('ignores late failures and removes event listeners on disposal', async () => {
    const old = deferred()
    mocks.settings.mockReturnValue(old.promise)
    const store = useAdminSettingsStore()
    const first = store.fetch()
    store.$dispose()
    window.dispatchEvent(new Event('ops-monitoring-disabled'))
    old.reject(new Error('private old error'))
    await first
    expect(store.loaded).toBe(false)
    expect(store.opsMonitoringEnabled).toBe(true)
  })
})
