/**
 * Authentication Store
 * Manages user authentication state, login/logout, token refresh, and token persistence
 */

import { defineStore } from 'pinia'
import { ref, computed, readonly, onScopeDispose } from 'vue'
import {
  captureAuthSession, isAuthSessionCurrent, authSessionChangedError,
  advanceAuthSession, AUTH_SESSION_KEY, AUTH_SESSION_EVENT
} from '@/api/tokenRefresh'
import { authAPI, isTotp2FARequired, passkeyAPI, type LoginResponse } from '@/api'
import type {
  User,
  LoginRequest,
  RegisterRequest,
  AuthResponse,
  ActionCaptchaRequestProof
} from '@/types'

const AUTH_TOKEN_KEY = 'auth_token'
const AUTH_USER_KEY = 'auth_user'
const REFRESH_TOKEN_KEY = 'refresh_token'
const TOKEN_EXPIRES_AT_KEY = 'token_expires_at' // 存储过期时间戳而非有效期
const PENDING_AUTH_SESSION_KEY = 'pending_auth_session'
const AUTO_REFRESH_INTERVAL = 60 * 1000 // 60 seconds for user data refresh
const TOKEN_REFRESH_BUFFER = 120 * 1000 // 120 seconds before expiry to refresh token

type PendingAuthTokenField = 'pending_auth_token' | 'pending_oauth_token'

interface PendingAuthSessionSummary {
  token: string
  token_field: PendingAuthTokenField
  provider: string
  redirect?: string
  adoption_required?: boolean
  suggested_display_name?: string
  suggested_avatar_url?: string
}

function normalizePendingAuthTokenField(value: unknown): PendingAuthTokenField {
  return value === 'pending_oauth_token' ? 'pending_oauth_token' : 'pending_auth_token'
}

function getPersistedPendingAuthSession(): PendingAuthSessionSummary | null {
  const raw = localStorage.getItem(PENDING_AUTH_SESSION_KEY)
  if (!raw) {
    return null
  }

  try {
    const parsed = JSON.parse(raw) as Partial<PendingAuthSessionSummary> | null
    const provider = typeof parsed?.provider === 'string' ? parsed.provider.trim() : ''
    if (!provider) {
      localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
      return null
    }
    return {
      token: typeof parsed?.token === 'string' ? parsed.token : '',
      token_field: normalizePendingAuthTokenField(parsed?.token_field),
      provider,
      redirect: typeof parsed?.redirect === 'string' ? parsed.redirect : undefined,
      adoption_required: typeof parsed?.adoption_required === 'boolean' ? parsed.adoption_required : undefined,
      suggested_display_name: typeof parsed?.suggested_display_name === 'string' ? parsed.suggested_display_name : undefined,
      suggested_avatar_url: typeof parsed?.suggested_avatar_url === 'string' ? parsed.suggested_avatar_url : undefined
    }
  } catch {
    localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
    return null
  }
}

function persistPendingAuthSession(session: PendingAuthSessionSummary): void {
  localStorage.setItem(PENDING_AUTH_SESSION_KEY, JSON.stringify(session))
}

function clearPendingAuthSessionStorage(): void {
  localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
}

export const useAuthStore = defineStore('auth', () => {
  // ==================== State ====================

  const user = ref<User | null>(null)
  const token = ref<string | null>(null)
  const refreshTokenValue = ref<string | null>(null)
  const tokenExpiresAt = ref<number | null>(null) // 过期时间戳（毫秒）
  const runMode = ref<'standard' | 'simple'>('standard')
  const pendingAuthSession = ref<PendingAuthSessionSummary | null>(null)
  let refreshIntervalId: ReturnType<typeof setInterval> | null = null
  let tokenRefreshTimeoutId: ReturnType<typeof setTimeout> | null = null
  let identityRevision = 0
  const sessionRevision = ref(localStorage.getItem(AUTH_SESSION_KEY))

  // Storage events run in peer tabs; the custom event covers local interceptor expiry.
  // Read the complete persisted state rather than an individual event's stale newValue.
  function syncAuthFromStorage(event: Event): void {
    if (event instanceof StorageEvent &&
      ((event.storageArea && event.storageArea !== localStorage) ||
       (event.key !== null && ![AUTH_TOKEN_KEY, AUTH_USER_KEY, REFRESH_TOKEN_KEY,
         TOKEN_EXPIRES_AT_KEY, AUTH_SESSION_KEY, PENDING_AUTH_SESSION_KEY].includes(event.key)))) return
    identityRevision++
    sessionRevision.value = localStorage.getItem(AUTH_SESSION_KEY)
    stopAutoRefresh()
    stopTokenRefresh()
    const savedToken = localStorage.getItem(AUTH_TOKEN_KEY)
    let savedUser: (User & { run_mode?: 'standard' | 'simple' }) | null = null
    try {
      savedUser = JSON.parse(localStorage.getItem(AUTH_USER_KEY) || 'null')
    } catch { /* Invalid peer state is unauthenticated; never erase the peer's storage. */ }
    const sameUser = user.value?.id === savedUser?.id
    token.value = savedToken && savedUser ? savedToken : null
    user.value = token.value ? savedUser : null
    refreshTokenValue.value = token.value ? localStorage.getItem(REFRESH_TOKEN_KEY) : null
    const expiresAt = Number(localStorage.getItem(TOKEN_EXPIRES_AT_KEY))
    tokenExpiresAt.value = token.value && expiresAt > 0 ? expiresAt : null
    runMode.value = savedUser?.run_mode || (sameUser && user.value ? runMode.value : 'standard')
    pendingAuthSession.value = getPersistedPendingAuthSession()
    if (token.value) {
      startAutoRefresh()
      if (refreshTokenValue.value && tokenExpiresAt.value !== null) {
        scheduleTokenRefreshAt(tokenExpiresAt.value)
      }
    }
  }

  window.addEventListener('storage', syncAuthFromStorage)
  window.addEventListener(AUTH_SESSION_EVENT, syncAuthFromStorage)
  onScopeDispose(() => {
    identityRevision++
    stopAutoRefresh()
    stopTokenRefresh()
    window.removeEventListener('storage', syncAuthFromStorage)
    window.removeEventListener(AUTH_SESSION_EVENT, syncAuthFromStorage)
  })

  // ==================== Computed ====================

  const isAuthenticated = computed(() => {
    return !!token.value && !!user.value
  })

  const isAdmin = computed(() => {
    return user.value?.role === 'admin'
  })

  const isSimpleMode = computed(() => runMode.value === 'simple')
  const hasPendingAuthSession = computed(() => pendingAuthSession.value !== null)

  // ==================== Actions ====================

  /**
   * Initialize auth state from localStorage
   * Call this on app startup to restore session
   * Also starts auto-refresh and immediately fetches latest user data
   */
  function checkAuth(): void {
    identityRevision++
    const savedToken = localStorage.getItem(AUTH_TOKEN_KEY)
    const savedUser = localStorage.getItem(AUTH_USER_KEY)
    const savedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)
    pendingAuthSession.value = getPersistedPendingAuthSession()

    if (savedToken && savedUser) {
      try {
        token.value = savedToken
        user.value = JSON.parse(savedUser)
        refreshTokenValue.value = savedRefreshToken
        tokenExpiresAt.value = savedExpiresAt ? parseInt(savedExpiresAt, 10) : null

        // Immediately refresh user data from backend (async, don't block)
        refreshUser().catch((error) => {
          console.error('Failed to refresh user on init:', error)
        })

        // Start auto-refresh interval for user data
        startAutoRefresh()

        // Start proactive token refresh if we have refresh token and expiry info
        // Note: use !== null to handle case when tokenExpiresAt.value is 0 (expired)
        if (savedRefreshToken && tokenExpiresAt.value !== null) {
          scheduleTokenRefreshAt(tokenExpiresAt.value)
        }
      } catch (error) {
        console.error('Failed to parse saved user data:', error)
        clearAuth({ preservePendingAuthSession: true })
      }
    }
  }

  /**
   * Start auto-refresh interval for user data
   * Refreshes user data every 60 seconds
   */
  function startAutoRefresh(): void {
    // Clear existing interval if any
    stopAutoRefresh()

    refreshIntervalId = setInterval(() => {
      if (token.value) {
        refreshUser().catch((error) => {
          console.error('Auto-refresh user failed:', error)
        })
      }
    }, AUTO_REFRESH_INTERVAL)
  }

  /**
   * Stop auto-refresh interval
   */
  function stopAutoRefresh(): void {
    if (refreshIntervalId) {
      clearInterval(refreshIntervalId)
      refreshIntervalId = null
    }
  }

  /**
   * Schedule proactive token refresh before expiry (based on expiry timestamp)
   * @param expiresAtMs - Token expiry timestamp in milliseconds
   */
  function scheduleTokenRefreshAt(expiresAtMs: number): void {
    // Clear any existing timeout
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }

    // Calculate remaining time until refresh (buffer time before expiry)
    const now = Date.now()
    const refreshInMs = Math.max(0, expiresAtMs - now - TOKEN_REFRESH_BUFFER)

    if (refreshInMs <= 0) {
      // Token is about to expire or already expired, refresh immediately
      performTokenRefresh()
      return
    }

    tokenRefreshTimeoutId = setTimeout(() => {
      performTokenRefresh()
    }, refreshInMs)
  }

  /**
   * Schedule proactive token refresh before expiry (based on expires_in seconds)
   * @param expiresInSeconds - Token expiry time in seconds from now
   */
  function scheduleTokenRefresh(expiresInSeconds: number): void {
    const expiresAtMs = Date.now() + expiresInSeconds * 1000
    tokenExpiresAt.value = expiresAtMs
    localStorage.setItem(TOKEN_EXPIRES_AT_KEY, String(expiresAtMs))
    scheduleTokenRefreshAt(expiresAtMs)
  }

  /**
   * Perform the actual token refresh
   */
  async function performTokenRefresh(): Promise<void> {
    if (!refreshTokenValue.value) {
      return
    }

    const revision = identityRevision
    const session = captureAuthSession()
    try {
      const response = await authAPI.refreshToken()
      if (revision !== identityRevision || !isAuthSessionCurrent(session)) return

      // Update state
      token.value = response.access_token
      refreshTokenValue.value = response.refresh_token

      // Schedule next refresh (this also updates tokenExpiresAt and localStorage)
      scheduleTokenRefresh(response.expires_in)
    } catch (error) {
      console.error('Token refresh failed:', error)
      // Don't clear auth here - the interceptor will handle 401 errors
    }
  }

  /**
   * Stop token refresh timeout
   */
  function stopTokenRefresh(): void {
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }
  }

  /**
   * User login
   * @param credentials - Login credentials (email and password)
   * @returns Promise resolving to the login response (may require 2FA)
   * @throws Error if login fails
   */
  async function login(credentials: LoginRequest): Promise<LoginResponse> {
    const revision = identityRevision
    try {
      const response = await authAPI.login(credentials)
      if (revision !== identityRevision) throw authSessionChangedError()

      // If 2FA is required, return the response without setting auth state
      if (isTotp2FARequired(response)) {
        return response
      }

      // Set auth state from the response
      setAuthFromResponse(response)

      return response
    } catch (error) {
      // Clear any partial state on error
      if (revision === identityRevision && (error as { code?: string }).code !== 'AUTH_SESSION_CHANGED') {
        clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      }
      throw error
    }
  }

  /**
   * Complete login with 2FA code
   * @param tempToken - Temporary token from initial login
   * @param totpCode - 6-digit TOTP code
   * @returns Promise resolving to the authenticated user
   * @throws Error if 2FA verification fails
   */
  async function login2FA(tempToken: string, totpCode: string): Promise<User> {
    const revision = identityRevision
    try {
      const response = await authAPI.login2FA({ temp_token: tempToken, totp_code: totpCode })
      if (revision !== identityRevision) throw authSessionChangedError()
      setAuthFromResponse(response)
      return user.value!
    } catch (error) {
      if (revision === identityRevision && (error as { code?: string }).code !== 'AUTH_SESSION_CHANGED') {
        clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      }
      throw error
    }
  }

  async function loginWithPasskey(proof?: ActionCaptchaRequestProof): Promise<User> {
    const revision = identityRevision
    try {
      const response = await passkeyAPI.login(proof)
      if (revision !== identityRevision) throw authSessionChangedError()
      setAuthFromResponse(response)
      return user.value!
    } catch (error) {
      if (revision === identityRevision && (error as { code?: string }).code !== 'AUTH_SESSION_CHANGED') {
        clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      }
      throw error
    }
  }

  /**
   * Set auth state from an AuthResponse
   * Internal helper function
   */
  function setAuthFromResponse(response: AuthResponse): void {
    identityRevision++
    stopTokenRefresh()
    refreshTokenValue.value = null
    tokenExpiresAt.value = null
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    localStorage.removeItem(TOKEN_EXPIRES_AT_KEY)
    // Store token and user
    token.value = response.access_token

    // Store refresh token if present
    if (response.refresh_token) {
      refreshTokenValue.value = response.refresh_token
      localStorage.setItem(REFRESH_TOKEN_KEY, response.refresh_token)
    }

    // Extract run_mode if present
    runMode.value = response.user.run_mode || 'standard'
    const { run_mode: _run_mode, ...userData } = response.user
    user.value = userData

    // Persist to localStorage
    localStorage.setItem(AUTH_TOKEN_KEY, response.access_token)
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify(userData))
    clearPendingAuthSession()
    advanceAuthSession()

    // Start auto-refresh interval for user data
    startAutoRefresh()

    // Start proactive token refresh if we have refresh token and expiry info
    // scheduleTokenRefresh will also store the expiry timestamp
    if (response.refresh_token && response.expires_in) {
      scheduleTokenRefresh(response.expires_in)
    }
  }

  /**
   * User registration
   * @param userData - Registration data (username, email, password)
   * @returns Promise resolving to the newly registered and authenticated user
   * @throws Error if registration fails
   */
  async function register(userData: RegisterRequest): Promise<User> {
    const revision = identityRevision
    try {
      const response = await authAPI.register(userData)
      if (revision !== identityRevision) throw authSessionChangedError()

      // Use the common helper to set auth state
      setAuthFromResponse(response)

      return user.value!
    } catch (error) {
      // Clear any partial state on error
      if (revision === identityRevision && (error as { code?: string }).code !== 'AUTH_SESSION_CHANGED') {
        clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      }
      throw error
    }
  }

  /**
   * 直接设置 token（用于 OAuth/SSO 回调），并加载当前用户信息。
   * 会自动读取 localStorage 中已设置的 refresh_token 和 token_expires_in
   * @param newToken - 后端签发的 JWT access token
   */
  async function setToken(newToken: string): Promise<User> {
    identityRevision++
    // Clear any previous state first (avoid mixing sessions)
    // Note: Don't clear localStorage here as OAuth callback may have set refresh_token
    stopAutoRefresh()
    stopTokenRefresh()
    token.value = null
    user.value = null
    runMode.value = 'standard'
    localStorage.removeItem(AUTH_USER_KEY)

    token.value = newToken
    localStorage.setItem(AUTH_TOKEN_KEY, newToken)
    // Announce invalidation before hydration; restore the token after the storage listener
    // correctly treats the not-yet-hydrated persisted identity as unauthenticated.
    advanceAuthSession()
    token.value = newToken

    // Read refresh token and expires_at from localStorage if set by OAuth callback
    const savedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)

    refreshTokenValue.value = savedRefreshToken
    tokenExpiresAt.value = savedExpiresAt ? parseInt(savedExpiresAt, 10) : null

    try {
      const userData = await refreshUser()
      startAutoRefresh()

      // Start proactive token refresh if we have refresh token and expiry info
      // Note: use !== null to handle case when tokenExpiresAt.value is 0 (expired)
      if (savedRefreshToken && tokenExpiresAt.value !== null) {
        scheduleTokenRefreshAt(tokenExpiresAt.value)
      }

      clearPendingAuthSession()
      return userData
    } catch (error) {
      if ((error as { code?: string }).code !== 'AUTH_SESSION_CHANGED') {
        clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      }
      throw error
    }
  }

  function setPendingAuthSession(session: PendingAuthSessionSummary | null): void {
    pendingAuthSession.value = session

    if (session) {
      persistPendingAuthSession(session)
      return
    }

    clearPendingAuthSessionStorage()
  }

  function clearPendingAuthSession(): void {
    setPendingAuthSession(null)
  }

  /**
   * User logout
   * Clears all authentication state and persisted data
   */
  async function logout(): Promise<void> {
    const revision = identityRevision
    try {
      // Call API logout (revokes refresh token on server)
      await authAPI.logout()
    } catch (err) {
      // 服务端吊销失败（网络/5xx/超时）不应阻止本地登出，否则用户点了退出仍处于登录态。
      console.warn('Logout API call failed, clearing local session anyway', err)
    } finally {
      // A delayed logout must not clear a replacement login.
      if (revision === identityRevision) clearAuth()
    }
  }

  /**
   * Refresh current user data
   * Fetches latest user info from the server
   * @returns Promise resolving to the updated user
   * @throws Error if not authenticated or request fails
   */
  async function refreshUser(): Promise<User> {
    if (!token.value) {
      throw new Error('Not authenticated')
    }

    const session = captureAuthSession()
    const revision = identityRevision
    const requestToken = token.value
    const requestUserID = user.value?.id
    const isCurrent = () => revision === identityRevision && token.value === requestToken &&
      user.value?.id === requestUserID && isAuthSessionCurrent(session)
    try {
      const response = await authAPI.getCurrentUser()
      if (!isCurrent() || (requestUserID !== undefined && response.data.id !== requestUserID)) {
        throw authSessionChangedError()
      }
      if (response.data.run_mode) {
        runMode.value = response.data.run_mode
      }
      const { run_mode: _run_mode, ...userData } = response.data
      user.value = userData

      // Update localStorage
      localStorage.setItem(AUTH_USER_KEY, JSON.stringify(userData))

      return userData
    } catch (error) {
      if (!isCurrent()) throw authSessionChangedError()
      // If refresh fails with 401, clear auth state
      if ((error as { status?: number }).status === 401 &&
        (error as { code?: string }).code !== 'AUTH_SESSION_CHANGED') {
        clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      }
      throw error
    }
  }

  /**
   * Clear all authentication state
   * Internal helper function
   */
  function clearAuth(options?: { preservePendingAuthSession?: boolean }): void {
    identityRevision++
    // Stop auto-refresh
    stopAutoRefresh()
    // Stop token refresh
    stopTokenRefresh()

    token.value = null
    refreshTokenValue.value = null
    tokenExpiresAt.value = null
    user.value = null
    runMode.value = 'standard'
    localStorage.removeItem(AUTH_TOKEN_KEY)
    localStorage.removeItem(AUTH_USER_KEY)
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    localStorage.removeItem(TOKEN_EXPIRES_AT_KEY)
    advanceAuthSession()

    if (options?.preservePendingAuthSession) {
      pendingAuthSession.value = getPersistedPendingAuthSession()
      return
    }

    pendingAuthSession.value = null
    clearPendingAuthSessionStorage()
  }

  // ==================== Return Store API ====================

  return {
    // State
    user,
    token,
    sessionRevision: readonly(sessionRevision),
    runMode: readonly(runMode),
    pendingAuthSession: readonly(pendingAuthSession),

    // Computed
    isAuthenticated,
    isAdmin,
    isSimpleMode,
    hasPendingAuthSession,

    // Actions
    login,
    loginWithPasskey,
    login2FA,
    register,
    setToken,
    logout,
    checkAuth,
    refreshUser,
    setPendingAuthSession,
    clearPendingAuthSession
  }
})
