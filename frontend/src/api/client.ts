/**
 * Axios HTTP Client Configuration
 * Base client with interceptors for authentication, token refresh, and error handling
 */

import axios, { AxiosInstance, AxiosError, InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import type { ApiResponse } from '@/types'
import { normalizeApiError, decodeErrorEnvelopeBody } from '@/utils/apiError'
import { getLocale } from '@/i18n'
import {
  ADMIN_UI_REQUEST_HEADER,
  USER_UI_REQUEST_HEADER,
  shouldMarkAdminUIRequest,
  shouldMarkUserUIRequest,
} from './adminUIRequest'
import {
  refreshAuthTokens, captureAuthSession, isAuthSessionCurrent, authSessionChangedError,
  advanceAuthSession, authStorage, opsEnabledStorage, clearPersistedAuthCredentials,
  AUTH_SESSION_EVENT, type AuthSessionSnapshot
} from './tokenRefresh'
import { getAPIBaseURL, buildGatewayUrl } from './url'
import { workspaceRequestURL } from './workspaceScope'
export { buildApiUrl, buildGatewayUrl } from './url'

export function isSilentRequestError(error: unknown): boolean {
  if (!error) return false
  if (axios.isCancel(error)) return true
  const candidate = error as { code?: string; name?: string }
  return (
    candidate.code === 'ERR_CANCELED' ||
    candidate.name === 'CanceledError' ||
    candidate.name === 'AbortError' ||
    candidate.code === 'AUTH_SESSION_CHANGED'
  )
}

function resolveVersionedRequestConfig(url: string | undefined, pathname: string): {
  baseURL?: string
  url?: string
} {
  if (!url) return { url }
  if (url === '/api/v2' || url.startsWith('/api/v2/')) {
    return {
      baseURL: buildGatewayUrl('/api/v2'),
      url: url.slice('/api/v2'.length) || '/'
    }
  }
  if (url === '/v2' || url.startsWith('/v2/')) {
    return {
      baseURL: buildGatewayUrl('/api/v2'),
      url: url.slice('/v2'.length) || '/'
    }
  }
  return {
    url: workspaceRequestURL(url, pathname)
  }
}

// ==================== Axios Instance Configuration ====================

export const apiClient: AxiosInstance = axios.create({
  baseURL: getAPIBaseURL(),
  withCredentials: true,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

// ==================== Request Interceptor ====================

type SessionRequest = InternalAxiosRequestConfig & {
  _retry?: boolean
  _authSession?: AuthSessionSnapshot
  _releaseSession?: () => void
}

// Get user's timezone
const getUserTimezone = (): string => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

const shouldInjectDefaultTimezone = (url: string | undefined, baseURL: string | undefined): boolean => {
  const path = String(url || '')
  const base = String(baseURL || '').replace(/\/+$/, '')
  // v2 contracts are explicit: every date window carries its own timezone.
  // Injecting a browser default into strict query binding yields 422 on
  // public catalog/status, scopes, tasks, Playground and request reads.
  return !(
    path.startsWith('/api/v2/') ||
    /\/api\/v2(?:\/|$)/.test(path) ||
    base.endsWith('/api/v2')
  )
}

apiClient.interceptors.request.use(
  (config: SessionRequest) => {
    const resolvedUrl = resolveVersionedRequestConfig(config.url, window.location.pathname)
    if (resolvedUrl.baseURL) {
      config.baseURL = resolvedUrl.baseURL
    }
    config.url = resolvedUrl.url
    if (config._authSession && !isAuthSessionCurrent(config._authSession)) {
      return Promise.reject(authSessionChangedError())
    }
    config._authSession ??= captureAuthSession()
    if (config._authSession.accessToken) {
      const controller = new AbortController()
      const callerSignal = config.signal
      const abort = () => controller.abort()
      const checkSession = () => {
        if (!isAuthSessionCurrent(config._authSession!)) controller.abort()
      }
      callerSignal?.addEventListener?.('abort', abort)
      if (callerSignal?.aborted) controller.abort()
      window.addEventListener('storage', checkSession)
      window.addEventListener(AUTH_SESSION_EVENT, checkSession)
      config.signal = controller.signal
      config._releaseSession = () => {
        callerSignal?.removeEventListener?.('abort', abort)
        window.removeEventListener('storage', checkSession)
        window.removeEventListener(AUTH_SESSION_EVENT, checkSession)
        config.signal = callerSignal
      }
    }
    const token = authStorage.token.get()
    if (token && config.headers) {
      config.headers.Authorization = `Bearer ${token}`
    }

    // Attach locale for backend translations
    if (config.headers) {
      config.headers['Accept-Language'] = getLocale()
    }

    // Attach timezone for all GET requests (backend may use it for default date ranges)
    if (config.method === 'get' && shouldInjectDefaultTimezone(config.url, config.baseURL)) {
      // Normalize tuple parameters so Axios serializes them as query pairs.
      if (Array.isArray(config.params)) config.params = new URLSearchParams(config.params)
      if (config.params instanceof URLSearchParams) {
        const params = new URLSearchParams(config.params)
        if (!params.has('timezone')) params.set('timezone', getUserTimezone())
        config.params = params
      } else {
        config.params = { ...config.params }
        if (!Object.prototype.hasOwnProperty.call(config.params, 'timezone')) {
          config.params.timezone = getUserTimezone()
        }
      }
    }

    if (config.headers) {
      const requestURL = String(config.url || '')
      if (shouldMarkAdminUIRequest(requestURL)) {
        config.headers[ADMIN_UI_REQUEST_HEADER] = '1'
      }
      if (shouldMarkUserUIRequest(requestURL)) {
        config.headers[USER_UI_REQUEST_HEADER] = '1'
      }
    }

    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// ==================== Response Interceptor ====================

apiClient.interceptors.response.use(
  (response: AxiosResponse) => {
    const request = response.config as SessionRequest
    request?._releaseSession?.()
    if (request?._authSession && !isAuthSessionCurrent(request._authSession)) {
      return Promise.reject(authSessionChangedError())
    }
    // Unwrap standard API response format { code, message, data }
    const apiResponse = response.data as ApiResponse<unknown>
    if (apiResponse && typeof apiResponse === 'object' && 'code' in apiResponse) {
      if (apiResponse.code === 0) {
        // Success - return the data portion
        response.data = apiResponse.data
      } else {
        // API error
        return Promise.reject(normalizeApiError({ response }))
      }
    }
    return response
  },
  async (error: AxiosError<ApiResponse<unknown>>) => {
    const originalRequest = error.config as SessionRequest | undefined
    originalRequest?._releaseSession?.()
    if (error.code === 'AUTH_SESSION_CHANGED') return Promise.reject(error)
    if (originalRequest?._authSession && !isAuthSessionCurrent(originalRequest._authSession)) {
      return Promise.reject(authSessionChangedError())
    }
    // Request cancellation: keep the original axios cancellation error so callers can ignore it.
    // Otherwise we'd misclassify it as a generic "network error".
    if (error.code === 'ERR_CANCELED' || axios.isCancel(error)) {
      return Promise.reject(error)
    }

    // Handle common errors
    if (error.response) {
      const { status } = error.response
      const url = String(error.config?.url || '')

      // 二进制响应的错误正文（responseType: 'blob'）里还是 JSON 信封：必须在规范化之前
      // 读回来，否则 409 的 reason/metadata 在这里就丢了，页面只能报一句通用失败。
      const decodedEnvelope = await decodeErrorEnvelopeBody(error.response.data)
      if (decodedEnvelope !== undefined) error.response.data = decodedEnvelope as ApiResponse<unknown>

      // Validate `data` shape to avoid HTML error pages breaking our error handling.
      const apiError = normalizeApiError(error)

      // Ops monitoring disabled: treat as feature-flagged 404, and proactively redirect away
      // from ops pages to avoid broken UI states.
      if (status === 404 && (apiError.reason === 'OPS_DISABLED' || apiError.message === 'Ops monitoring is disabled')) {
        opsEnabledStorage.set('false')
        try {
          window.dispatchEvent(new CustomEvent('ops-monitoring-disabled'))
        } catch {
          // ignore event failures
        }

        if (window.location.pathname.startsWith('/admin/ops')) {
          window.location.href = '/admin/settings'
        }

        return Promise.reject(normalizeApiError({ ...apiError, reason: 'OPS_DISABLED' }))
      }

      if (status === 423 && apiError.reason === 'ADMIN_COMPLIANCE_ACK_REQUIRED') {
        try {
          const currentToken = authStorage.token.get()
          // A rejected request from a previous login must not lock the current admin.
          if (currentToken && (originalRequest?._authSession
            ? isAuthSessionCurrent(originalRequest._authSession)
            : error.config?.headers?.Authorization === `Bearer ${currentToken}`)) {
            window.dispatchEvent(new CustomEvent('admin-compliance-required', {
              detail: apiError.metadata
            }))
          }
        } catch {
          // ignore event failures
        }

        return Promise.reject(apiError)
      }

      // 401: Try to refresh the token if we have a refresh token
      // This handles TOKEN_EXPIRED, INVALID_TOKEN, TOKEN_REVOKED, etc.
      if (status === 401 && originalRequest && !originalRequest._retry) {
        const sentToken = originalRequest.headers?.Authorization
        if (!originalRequest._authSession && sentToken &&
          sentToken !== `Bearer ${authStorage.token.get()}`) {
          return Promise.reject(authSessionChangedError())
        }
        const refreshToken = authStorage.refreshToken.get()
        const isAuthEndpoint =
          url.includes('/auth/login') || url.includes('/auth/register') || url.includes('/auth/refresh')

        // If we have a refresh token and this is not an auth endpoint, try to refresh
        if (refreshToken && !isAuthEndpoint) {
          const refreshSessionUser = authStorage.user.get()
          originalRequest._retry = true

          try {
            const headers = originalRequest.headers as Record<string, unknown> | undefined
            const authHeader = headers?.Authorization ?? headers?.authorization
            const failedAccessToken =
              typeof authHeader === 'string' && authHeader.startsWith('Bearer ')
                ? authHeader.slice('Bearer '.length)
                : null
            // Older/custom adapters may omit interceptor metadata; never adopt a different token
            // without the original principal snapshot.
            if (!originalRequest._authSession && failedAccessToken !== authStorage.token.get()) {
              throw authSessionChangedError()
            }
            const session = originalRequest._authSession ?? captureAuthSession()
            originalRequest._authSession = session
            const tokens = await refreshAuthTokens({ failedAccessToken, session })
            if (!isAuthSessionCurrent(session)) throw authSessionChangedError()

            // Retry the original request with the refreshed token
            if (originalRequest.headers) {
              originalRequest.headers.Authorization = `Bearer ${tokens.access_token}`
            }
            return apiClient(originalRequest)
          } catch (refreshError) {
            // A stale request must never destroy a session that was logged out or replaced while
            // its refresh was in flight (for example, when another tab signs in as another user).
            const sessionChanged =
              (refreshError as { code?: string }).code === 'AUTH_SESSION_CHANGED' ||
              (originalRequest._authSession && !isAuthSessionCurrent(originalRequest._authSession)) ||
              authStorage.refreshToken.get() !== refreshToken ||
              authStorage.user.get() !== refreshSessionUser
            if (sessionChanged) {
              return Promise.reject(authSessionChangedError())
            }

            if (axios.isAxiosError(refreshError)) {
              const refreshStatus = refreshError.response?.status ?? 0
              if (refreshStatus === 0 || refreshStatus === 429 || refreshStatus >= 500) {
                return Promise.reject(normalizeApiError({
                  status: refreshStatus,
                  reason: 'TOKEN_REFRESH_UNAVAILABLE',
                  message: normalizeApiError(refreshError).message
                }))
              }
            }

            // Clear tokens and redirect to login
            clearPersistedAuthCredentials()
            advanceAuthSession()
            authStorage.expiredNotice.set('1')

            if (!window.location.pathname.includes('/login')) {
              window.location.href = '/login'
            }

            return Promise.reject(normalizeApiError({
              status: 401,
              code: 'TOKEN_REFRESH_FAILED',
              message: 'Session expired. Please log in again.'
            }))
          }
        }

        // No refresh token or is auth endpoint - clear auth and redirect
        const hasToken = !!authStorage.token.get()
        const headers = error.config?.headers as Record<string, unknown> | undefined
        const authHeader = headers?.Authorization ?? headers?.authorization
        const sentAuth =
          typeof authHeader === 'string'
            ? authHeader.trim() !== ''
            : Array.isArray(authHeader)
              ? authHeader.length > 0
              : !!authHeader

        clearPersistedAuthCredentials()
        advanceAuthSession()
        if ((hasToken || sentAuth) && !isAuthEndpoint) {
          authStorage.expiredNotice.set('1')
        }
        // Only redirect if not already on login page
        if (!window.location.pathname.includes('/login')) {
          window.location.href = '/login'
        }
      }

      return Promise.reject(apiError)
    }

    // Network error
    return Promise.reject(normalizeApiError({
      status: 0,
      message: 'Network error. Please check your connection.'
    }))
  }
)

export default apiClient
