import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

type NavigationGuard = (
  to: Record<string, any>,
  from: Record<string, any>,
  next: ReturnType<typeof vi.fn>
) => Promise<void>

const routerHarness = vi.hoisted(() => ({
  guard: null as NavigationGuard | null,
}))

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  token: 'session-a',
  sessionRevision: 'session-a',
  user: { id: 1, role: 'admin' },
  isAuthenticated: true,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  publicSettingsLoaded: false,
  cachedPublicSettings: null as null | {
    payment_enabled?: boolean
    risk_control_enabled?: boolean
    subscription_enabled?: boolean
    custom_menu_items?: []
  },
  fetchPublicSettings: vi.fn(),
}))

vi.mock('vue-router', () => ({
  createWebHistory: vi.fn(() => ({})),
  createRouter: vi.fn(() => ({
    beforeEach: vi.fn((guard: NavigationGuard) => {
      routerHarness.guard = guard
    }),
    afterEach: vi.fn(),
    onError: vi.fn(),
  })),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

const complianceStore = vi.hoisted(() => ({
  status: { required: false } as { required: boolean } | null,
  fetchStatus: vi.fn(),
  getIdentityRevision: vi.fn(() => 0),
  requireAcknowledgement: vi.fn(),
}))
vi.mock('@/stores/adminCompliance', () => ({ useAdminComplianceStore: () => complianceStore }))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function runGuard(meta: Record<string, unknown>, path: string) {
  if (!routerHarness.guard) {
    throw new Error('router guard was not registered')
  }

  const next = vi.fn()
  const navigation = routerHarness.guard(
    {
      path,
      fullPath: path,
      name: 'FeatureRoute',
      params: {},
      query: {},
      meta: { requiresAuth: true, ...meta },
    },
    {},
    next
  )
  return { navigation, next }
}

describe('feature route guard', () => {
  beforeAll(async () => {
    await import('@/router')
  })

  beforeEach(() => {
    authStore.isAuthenticated = true
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    appStore.publicSettingsLoaded = false
    appStore.cachedPublicSettings = null
    appStore.fetchPublicSettings.mockReset()
  })

  it('waits for the first public-settings request before deciding payment access', async () => {
    const deferred = createDeferred<{ payment_enabled: boolean }>()
    appStore.fetchPublicSettings.mockImplementation(async () => {
      const settings = await deferred.promise
      appStore.cachedPublicSettings = settings
      appStore.publicSettingsLoaded = true
      return settings
    })

    const { navigation, next } = runGuard({ requiresPayment: true }, '/purchase')

    await vi.waitFor(() => expect(appStore.fetchPublicSettings).toHaveBeenCalledTimes(1))
    expect(next).not.toHaveBeenCalled()

    deferred.resolve({ payment_enabled: true })
    await navigation
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    ['payment', { requiresPayment: true }, '/purchase'],
    ['risk control', { requiresRiskControl: true }, '/admin/risk-control'],
    ['subscription', { requiresSubscription: true }, '/subscriptions'],
  ])('does not treat a failed %s settings load as explicitly disabled', async (_name, meta, path) => {
    authStore.isAdmin = meta.requiresRiskControl === true
    appStore.fetchPublicSettings.mockResolvedValue(null)

    const { navigation, next } = runGuard(meta, path)
    await navigation

    expect(appStore.publicSettingsLoaded).toBe(false)
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    ['payment', { requiresPayment: true }, { payment_enabled: false }, '/dashboard'],
    [
      'risk control',
      { requiresRiskControl: true },
      { risk_control_enabled: false },
      '/admin/settings',
    ],
    ['subscription', { requiresSubscription: true }, { subscription_enabled: false }, '/dashboard'],
  ])('redirects when loaded settings explicitly disable %s', async (_name, meta, settings, target) => {
    authStore.isAdmin = meta.requiresRiskControl === true
    appStore.cachedPublicSettings = settings
    appStore.publicSettingsLoaded = true

    const { navigation, next } = runGuard(meta, '/feature')
    await navigation

    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith(target)
  })
})

describe('subscription route guard (opt-out flag)', () => {
  beforeEach(() => {
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    appStore.publicSettingsLoaded = true
    appStore.fetchPublicSettings.mockReset()
  })

  it.each([
    ['missing key', {}],
    ['explicit true', { subscription_enabled: true }],
  ])('lets /subscriptions through when the flag is %s', async (_name, settings) => {
    appStore.cachedPublicSettings = settings

    const { navigation, next } = runGuard({ requiresSubscription: true }, '/subscriptions')
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it('sends admins to the admin dashboard when subscriptions are disabled', async () => {
    authStore.isAdmin = true
    appStore.cachedPublicSettings = { subscription_enabled: false }

    const { navigation, next } = runGuard({ requiresSubscription: true }, '/subscriptions')
    await navigation

    expect(next).toHaveBeenCalledWith('/admin/dashboard')
  })
})


describe('mandatory compliance route gate', () => {
  beforeEach(() => {
    authStore.isAuthenticated = true
    authStore.isAdmin = true
    authStore.isSimpleMode = false
    authStore.sessionRevision = 'session-a'
    complianceStore.status = { required: false }
    complianceStore.fetchStatus.mockReset().mockResolvedValue({ required: false })
    complianceStore.getIdentityRevision.mockReturnValue(0)
    complianceStore.requireAcknowledgement.mockReset().mockImplementation(() => {
      complianceStore.status = { required: true }
    })
  })

  it('waits for acknowledgement status and gates unaccepted admins', async () => {
    const pending = createDeferred<void>()
    complianceStore.fetchStatus.mockReturnValue(pending.promise)
    const { navigation, next } = runGuard({ requiresAdmin: true }, '/admin/settings')
    expect(next).not.toHaveBeenCalled()
    complianceStore.status = { required: true }
    pending.resolve()
    await navigation
    expect(next).toHaveBeenCalledWith({ path: '/admin/compliance', query: { redirect: '/admin/settings' } })
  })

  it.each([null, { required: true }])('blocks unknown or required status %j', async status => {
    complianceStore.status = status
    const { navigation, next } = runGuard({ requiresAdmin: true }, '/admin/dashboard')
    await navigation
    expect(next).toHaveBeenCalledWith({ path: '/admin/compliance', query: { redirect: '/admin/dashboard' } })
  })

  it('fails closed on network errors and leaves the gate reachable', async () => {
    complianceStore.fetchStatus.mockRejectedValue(new Error('offline'))
    const restricted = runGuard({ requiresAdmin: true }, '/admin/settings')
    await restricted.navigation
    expect(restricted.next).toHaveBeenCalledWith({ path: '/admin/compliance', query: { redirect: '/admin/settings' } })
    const gate = runGuard({ requiresAdmin: true }, '/admin/compliance')
    await gate.navigation
    expect(gate.next).toHaveBeenCalledWith()
  })

  it.each(['logout', 'switch', 'switch-back'])('cancels an old navigation after %s', async change => {
    const pending = createDeferred<void>()
    complianceStore.fetchStatus.mockReturnValue(pending.promise)
    const { navigation, next } = runGuard({ requiresAdmin: true }, '/admin/settings')
    if (change === 'logout') authStore.isAuthenticated = false
    if (change === 'switch') authStore.sessionRevision = 'session-b'
    if (change === 'switch-back') complianceStore.getIdentityRevision.mockReturnValue(2)
    pending.resolve()
    await navigation
    expect(next).toHaveBeenCalledWith(false)
  })

  it('allows an explicitly acknowledged current admin', async () => {
    const { navigation, next } = runGuard({ requiresAdmin: true }, '/admin/settings')
    await navigation
    expect(next).toHaveBeenCalledWith()
  })
})

it('redirects an acknowledged admin away from the empty gate', async () => {
  authStore.isAdmin = true
  authStore.isAuthenticated = true
  complianceStore.status = { required: false }
  complianceStore.fetchStatus.mockResolvedValue({ required: false })
  const { navigation, next } = runGuard({ requiresAdmin: true }, '/admin/compliance')
  await navigation
  expect(next).toHaveBeenCalledWith('/admin/dashboard')
})

it('rechecks admin identity and acknowledgement after waiting for feature settings', async () => {
  authStore.isAdmin = true
  authStore.isAuthenticated = true
  complianceStore.status = { required: false }
  complianceStore.fetchStatus.mockResolvedValue({ required: false })
  appStore.publicSettingsLoaded = false
  const pending = createDeferred<void>()
  appStore.fetchPublicSettings.mockReset().mockReturnValue(pending.promise)
  const { navigation, next } = runGuard({ requiresAdmin: true, requiresPayment: true }, '/admin/orders')
  await vi.waitFor(() => expect(appStore.fetchPublicSettings).toHaveBeenCalled())
  complianceStore.status = { required: true }
  pending.resolve()
  await navigation
  expect(next).toHaveBeenCalledWith(false)
})
