import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setActivePinia, createPinia, getActivePinia, disposePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'

// Mock authAPI
const mockLogin = vi.fn()
const mockLogin2FA = vi.fn()
const mockPasskeyLogin = vi.fn()
const mockLogout = vi.fn()
const mockGetCurrentUser = vi.fn()
const mockRegister = vi.fn()
const mockRefreshToken = vi.fn()

vi.mock('@/api', () => ({
  passkeyAPI: { login: (...args: any[]) => mockPasskeyLogin(...args) },
  authAPI: {
    login: (...args: any[]) => mockLogin(...args),
    login2FA: (...args: any[]) => mockLogin2FA(...args),
    logout: (...args: any[]) => mockLogout(...args),
    getCurrentUser: (...args: any[]) => mockGetCurrentUser(...args),
    register: (...args: any[]) => mockRegister(...args),
    refreshToken: (...args: any[]) => mockRefreshToken(...args),
  },
  isTotp2FARequired: (response: any) => response?.requires_2fa === true,
}))

const fakeUser = {
  id: 1,
  username: 'testuser',
  email: 'test@example.com',
  role: 'user' as const,
  balance: 100,
  concurrency: 5,
  status: 'active' as const,
  allowed_groups: null,
  created_at: '2024-01-01',
  updated_at: '2024-01-01',
}

const fakeAdminUser = {
  ...fakeUser,
  id: 2,
  username: 'admin',
  email: 'admin@example.com',
  role: 'admin' as const,
}

const fakeAuthResponse = {
  access_token: 'test-token-123',
  refresh_token: 'refresh-token-456',
  expires_in: 3600,
  token_type: 'Bearer',
  user: { ...fakeUser },
}

describe('useAuthStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.useFakeTimers()
    vi.resetAllMocks()
  })

  afterEach(() => {
    disposePinia(getActivePinia()!)
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  // --- login ---

  describe('login', () => {
    it('成功登录后设置 token 和 user', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.token).toBe('test-token-123')
      expect(store.user).toEqual(fakeUser)
      expect(store.isAuthenticated).toBe(true)
      expect(localStorage.getItem('auth_token')).toBe('test-token-123')
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify(fakeUser))
    })

    it('登录失败时清除状态并抛出错误', async () => {
      mockLogin.mockRejectedValue(new Error('Invalid credentials'))
      const store = useAuthStore()

      await expect(store.login({ email: 'test@example.com', password: 'wrong' })).rejects.toThrow(
        'Invalid credentials'
      )

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })

    it('需要 2FA 时返回响应但不设置认证状态', async () => {
      const twoFAResponse = { requires_2fa: true, temp_token: 'temp-123' }
      mockLogin.mockResolvedValue(twoFAResponse)
      const store = useAuthStore()

      const result = await store.login({ email: 'test@example.com', password: '123456' })

      expect(result).toEqual(twoFAResponse)
      expect(store.token).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })
  })

  // --- login2FA ---

  describe('login2FA', () => {
    it('2FA 验证成功后设置认证状态', async () => {
      mockLogin2FA.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      const user = await store.login2FA('temp-123', '654321')

      expect(store.token).toBe('test-token-123')
      expect(store.user).toEqual(fakeUser)
      expect(user).toEqual(fakeUser)
      expect(mockLogin2FA).toHaveBeenCalledWith({
        temp_token: 'temp-123',
        totp_code: '654321',
      })
    })

    it('2FA 验证失败时清除状态并抛出错误', async () => {
      mockLogin2FA.mockRejectedValue(new Error('Invalid TOTP'))
      const store = useAuthStore()

      await expect(store.login2FA('temp-123', '000000')).rejects.toThrow('Invalid TOTP')
      expect(store.token).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })
  })

  // --- logout ---

  describe('logout', () => {
    it('注销后清除所有状态和 localStorage', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      mockLogout.mockResolvedValue(undefined)
      const store = useAuthStore()

      // 先登录
      await store.login({ email: 'test@example.com', password: '123456' })
      expect(store.isAuthenticated).toBe(true)

      // 注销
      await store.logout()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
      expect(localStorage.getItem('auth_token')).toBeNull()
      expect(localStorage.getItem('auth_user')).toBeNull()
      expect(localStorage.getItem('refresh_token')).toBeNull()
      expect(localStorage.getItem('token_expires_at')).toBeNull()
    })
  })

  // --- checkAuth ---

  describe('checkAuth', () => {
    it('从 localStorage 恢复持久化状态', () => {
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', JSON.stringify(fakeUser))

      // Mock refreshUser (getCurrentUser) 防止后台刷新报错
      mockGetCurrentUser.mockResolvedValue({ data: fakeUser })

      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBe('saved-token')
      expect(store.user).toEqual(fakeUser)
      expect(store.isAuthenticated).toBe(true)
    })

    it('localStorage 无数据时保持未认证状态', () => {
      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })

    it('localStorage 中用户数据损坏时清除状态', () => {
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', 'invalid-json{{{')

      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(localStorage.getItem('auth_token')).toBeNull()
    })

    it('恢复 refresh token 和过期时间', () => {
      const futureTs = String(Date.now() + 3600_000)
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', JSON.stringify(fakeUser))
      localStorage.setItem('refresh_token', 'saved-refresh')
      localStorage.setItem('token_expires_at', futureTs)

      mockGetCurrentUser.mockResolvedValue({ data: fakeUser })

      const store = useAuthStore()
      store.checkAuth()

      expect(store.isAuthenticated).toBe(true)
    })

    it('恢复持久化 pending auth session', () => {
      localStorage.setItem(
        'pending_auth_session',
        JSON.stringify({
          token: 'pending-token',
          token_field: 'pending_auth_token',
          provider: 'wechat',
          redirect: '/profile',
        })
      )

      const store = useAuthStore()
      store.checkAuth()

      expect(store.hasPendingAuthSession).toBe(true)
      expect(store.pendingAuthSession).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })
    })
  })

  describe('pending auth session', () => {
    it('persists and clears pending auth session state', () => {
      const store = useAuthStore()

      store.setPendingAuthSession({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })

      expect(store.hasPendingAuthSession).toBe(true)
      expect(JSON.parse(localStorage.getItem('pending_auth_session') || 'null')).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })

      store.clearPendingAuthSession()

      expect(store.hasPendingAuthSession).toBe(false)
      expect(localStorage.getItem('pending_auth_session')).toBeNull()
    })

    it('restores a persisted pending oauth session without requiring a token value', () => {
      const firstStore = useAuthStore()

      firstStore.setPendingAuthSession({
        token: '',
        token_field: 'pending_oauth_token',
        provider: 'oidc',
        redirect: '/welcome',
        adoption_required: true,
        suggested_display_name: 'OIDC Nick'
      })

      firstStore.$dispose()
      setActivePinia(createPinia())
      const restoredStore = useAuthStore()
      restoredStore.checkAuth()

      expect(restoredStore.isAuthenticated).toBe(false)
      expect(restoredStore.hasPendingAuthSession).toBe(true)
      expect(restoredStore.pendingAuthSession).toEqual({
        token: '',
        token_field: 'pending_oauth_token',
        provider: 'oidc',
        redirect: '/welcome',
        adoption_required: true,
        suggested_display_name: 'OIDC Nick',
        suggested_avatar_url: undefined
      })
    })

    it('preserves pending auth session when registration fails', async () => {
      const store = useAuthStore()
      store.setPendingAuthSession({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'oidc',
        redirect: '/register',
      })
      mockRegister.mockRejectedValue(new Error('Register failed'))

      await expect(
        store.register({ email: 'user@example.com', password: 'secret-123' })
      ).rejects.toThrow('Register failed')

      expect(store.hasPendingAuthSession).toBe(true)
      expect(store.pendingAuthSession).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'oidc',
        redirect: '/register',
      })
    })
  })

  // --- isAdmin ---

  describe('isAdmin', () => {
    it('管理员用户返回 true', async () => {
      const adminResponse = { ...fakeAuthResponse, user: { ...fakeAdminUser } }
      mockLogin.mockResolvedValue(adminResponse)
      const store = useAuthStore()

      await store.login({ email: 'admin@example.com', password: '123456' })

      expect(store.isAdmin).toBe(true)
    })

    it('普通用户返回 false', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.isAdmin).toBe(false)
    })

    it('未登录时返回 false', () => {
      const store = useAuthStore()
      expect(store.isAdmin).toBe(false)
    })
  })

  // --- refreshUser ---

  describe('refreshUser', () => {
    it('刷新用户数据并更新 localStorage', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      const updatedUser = { ...fakeUser, username: 'updated-name' }
      mockGetCurrentUser.mockResolvedValue({ data: updatedUser })

      const result = await store.refreshUser()

      expect(result).toEqual(updatedUser)
      expect(store.user).toEqual(updatedUser)
      expect(JSON.parse(localStorage.getItem('auth_user')!)).toEqual(updatedUser)
    })

    it('未认证时抛出错误', async () => {
      const store = useAuthStore()
      await expect(store.refreshUser()).rejects.toThrow('Not authenticated')
    })
  })

  describe('identity races', () => {
    async function login(user = fakeUser, token = 'session-a') {
      mockLogin.mockResolvedValue({ ...fakeAuthResponse, access_token: token, user })
      const store = useAuthStore()
      await store.login({ email: user.email, password: 'secret' })
      return store
    }

    it('does not clear a replacement session when logout returns late', async () => {
      const store = await login()
      let finish!: () => void
      mockLogout.mockReturnValueOnce(new Promise<void>(resolve => { finish = resolve }))
      const pending = store.logout()
      await login(fakeAdminUser, 'session-b')
      finish()
      await pending
      expect(store.token).toBe('session-b')
      expect(store.user?.id).toBe(fakeAdminUser.id)
    })

    it('rejects an old login result after a newer login has completed', async () => {
      const store = useAuthStore()
      let finish!: (response: typeof fakeAuthResponse) => void
      mockLogin.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
      const pending = store.login({ email: fakeUser.email, password: 'secret' })
      await login(fakeAdminUser, 'session-b')
      finish(fakeAuthResponse)
      await expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(store.token).toBe('session-b')
      expect(store.user?.id).toBe(fakeAdminUser.id)
    })

    it.each(['success', '401', 'session-changed'])('ignores late %s after another login', async outcome => {
      const store = await login()
      let resolve!: (value: unknown) => void
      let reject!: (error: unknown) => void
      mockGetCurrentUser.mockImplementationOnce(() => new Promise((res, rej) => { resolve = res; reject = rej }))
      const pending = store.refreshUser()
      await login(fakeAdminUser, 'session-b')
      if (outcome === 'success') resolve({ data: { ...fakeUser, run_mode: 'simple' } })
      else reject({ status: 401, code: outcome === '401' ? 'TOKEN_EXPIRED' : 'AUTH_SESSION_CHANGED' })
      await expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(store.token).toBe('session-b')
      expect(store.user?.id).toBe(fakeAdminUser.id)
      expect(store.runMode).toBe('standard')
      expect(JSON.parse(localStorage.getItem('auth_user')!).id).toBe(fakeAdminUser.id)
    })

    it('invalidates a late response even when the same user and token log in again', async () => {
      const store = await login()
      let resolve!: (value: unknown) => void
      mockGetCurrentUser.mockImplementationOnce(() => new Promise(res => { resolve = res }))
      const pending = store.refreshUser()
      await login()
      resolve({ data: { ...fakeUser, username: 'stale' } })
      await expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(store.user?.username).toBe(fakeUser.username)
    })

    it('preserves session-changed errors but clears an ordinary current-session 401', async () => {
      const store = await login()
      mockGetCurrentUser.mockRejectedValueOnce({ status: 401, code: 'AUTH_SESSION_CHANGED' })
      await expect(store.refreshUser()).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(store.isAuthenticated).toBe(true)
      mockGetCurrentUser.mockRejectedValueOnce({ status: 401, code: 'TOKEN_EXPIRED' })
      await expect(store.refreshUser()).rejects.toMatchObject({ code: 'TOKEN_EXPIRED' })
      expect(store.isAuthenticated).toBe(false)
    })

    it('synchronizes peer account changes, rotation and logout immediately', async () => {
      const store = await login()
      localStorage.setItem('auth_token', 'peer-b')
      localStorage.setItem('auth_user', JSON.stringify(fakeAdminUser))
      window.dispatchEvent(new StorageEvent('storage', { key: 'auth_user', storageArea: localStorage }))
      expect(store.token).toBe('peer-b')
      expect(store.isAdmin).toBe(true)
      localStorage.setItem('auth_token', 'rotated-b')
      window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token', storageArea: localStorage }))
      expect(store.token).toBe('rotated-b')
      expect(store.user?.id).toBe(fakeAdminUser.id)
      localStorage.clear()
      window.dispatchEvent(new StorageEvent('storage', { key: null, storageArea: localStorage }))
      expect(store.isAuthenticated).toBe(false)
      expect(store.user).toBeNull()
      mockGetCurrentUser.mockClear()
      await vi.advanceTimersByTimeAsync(60_000)
      expect(mockGetCurrentUser).not.toHaveBeenCalled()
    })

    it('does not clear a new login after an old OAuth hydration fails', async () => {
      const store = useAuthStore()
      let reject!: (error: unknown) => void
      mockGetCurrentUser.mockImplementationOnce(() => new Promise((_res, rej) => { reject = rej }))
      const pending = store.setToken('oauth-a')
      await login(fakeAdminUser, 'session-b')
      reject({ status: 401 })
      await expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(store.token).toBe('session-b')
    })

    it('does not restore old proactive refresh tokens after switching accounts', async () => {
      let resolve!: (value: unknown) => void
      mockRefreshToken.mockImplementationOnce(() => new Promise(res => { resolve = res }))
      mockLogin.mockResolvedValue({ ...fakeAuthResponse, expires_in: 121 })
      const store = useAuthStore()
      await store.login({ email: fakeUser.email, password: 'secret' })
      await vi.advanceTimersByTimeAsync(1000)
      expect(mockRefreshToken).toHaveBeenCalledTimes(1)
      await login(fakeAdminUser, 'session-b')
      resolve({ access_token: 'stale-token', refresh_token: 'stale-refresh', expires_in: 1 })
      await Promise.resolve()
      expect(store.token).toBe('session-b')
      expect(localStorage.getItem('auth_token')).toBe('session-b')
      expect(Number(localStorage.getItem('token_expires_at'))).toBeGreaterThan(Date.now() + 120_000)
    })

    it('does not revive a logged-out session when user data arrives late', async () => {
      const store = await login()
      let resolve!: (value: unknown) => void
      mockGetCurrentUser.mockImplementationOnce(() => new Promise(res => { resolve = res }))
      const pending = store.refreshUser()
      mockLogout.mockResolvedValue(undefined)
      await store.logout()
      resolve({ data: fakeUser })
      await expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(store.user).toBeNull()
      expect(localStorage.getItem('auth_user')).toBeNull()
    })

    it.each(['login', '2fa', 'passkey', 'register'])('does not clear current identity for a canceled old %s', async method => {
      const store = await login(fakeAdminUser, 'session-b')
      const changed = { status: 401, code: 'AUTH_SESSION_CHANGED' }
      mockLogin.mockRejectedValueOnce(changed)
      mockLogin2FA.mockRejectedValueOnce(changed)
      mockPasskeyLogin.mockRejectedValueOnce(changed)
      mockRegister.mockRejectedValueOnce(changed)
      const pending = method === 'login' ? store.login({ email: fakeUser.email, password: 'secret' })
        : method === '2fa' ? store.login2FA('temp', '123456')
        : method === 'passkey' ? store.loginWithPasskey()
        : store.register({ email: fakeUser.email, password: 'secret' })
      await expect(pending).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(store.token).toBe('session-b')
      expect(store.user?.id).toBe(fakeAdminUser.id)
    })

    it('removes storage listeners when the store is disposed', async () => {
      const store = await login()
      store.$dispose()
      localStorage.clear()
      window.dispatchEvent(new StorageEvent('storage', { key: null }))
      expect(store.token).toBe('session-a')
      mockGetCurrentUser.mockClear()
      await vi.advanceTimersByTimeAsync(60_000)
      expect(mockGetCurrentUser).not.toHaveBeenCalled()
    })
  })

  // --- isSimpleMode ---

  describe('isSimpleMode', () => {
    it('run_mode 为 simple 时返回 true', async () => {
      const simpleResponse = {
        ...fakeAuthResponse,
        user: { ...fakeUser, run_mode: 'simple' as const },
      }
      mockLogin.mockResolvedValue(simpleResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.isSimpleMode).toBe(true)
    })

    it('默认为 standard 模式', () => {
      const store = useAuthStore()
      expect(store.isSimpleMode).toBe(false)
    })
  })
})
