import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import axios from 'axios'
import type { AxiosInstance } from 'axios'

// 需要在导入 client 之前设置 mock
vi.mock('@/i18n', () => ({
  getLocale: () => 'zh-CN',
}))

describe('API Client', () => {
  let apiClient: AxiosInstance

  beforeEach(async () => {
    localStorage.clear()
    sessionStorage.clear()
    window.history.replaceState({}, '', '/')
    // 每次测试重新导入以获取干净的模块状态
    vi.resetModules()
    const mod = await import('@/api/client')
    apiClient = mod.apiClient
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllEnvs()
  })

  // --- 请求拦截器 ---

  describe('请求拦截器', () => {
    it('规范化相对 API base，避免在回调页拼出相对 v1 路径', async () => {
      vi.resetModules()
      vi.stubEnv('VITE_API_BASE_URL', 'api/v1')

      const mod = await import('@/api/client')

      expect(mod.apiClient.defaults.baseURL).toBe('/api/v1')
      expect(mod.buildApiUrl('/auth/oauth/github/callback?code=abc')).toBe(
        '/api/v1/auth/oauth/github/callback?code=abc'
      )
    })

    it('自动附加 Authorization 头', async () => {
      localStorage.setItem('auth_token', 'my-jwt-token')

      // 拦截实际请求
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('Authorization')).toBe('Bearer my-jwt-token')
    })

    it('无 token 时不附加 Authorization 头', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('Authorization')).toBeFalsy()
    })

    it('GET 请求自动附加 timezone 参数', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.params).toHaveProperty('timezone')
    })

    it('v2 GET 请求不自动附加浏览器 timezone，避免严格查询参数接口 422', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/api/v2/public/models', { params: { limit: 20 } })

      const config = adapter.mock.calls[0][0]
      expect(config.params).not.toHaveProperty('timezone')
      expect(config.params.limit).toBe(20)
    })

    it('v2 absolute GET 请求也不自动附加 timezone', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('http://127.0.0.1:18080/api/v2/me/scopes', { baseURL: '', params: { limit: 50 } })

      const config = adapter.mock.calls[0][0]
      expect(config.params).not.toHaveProperty('timezone')
      expect(config.params.limit).toBe(50)
    })

    it('v2 GET 仍保留调用方显式传入的 timezone', async () => {
      const adapter = vi.fn().mockImplementation(config => Promise.resolve({ data: {}, status: 200, statusText: 'OK', headers: {}, config }))
      apiClient.defaults.adapter = adapter

      await apiClient.get('/api/v2/workspaces/1/projects/1/billing', {
        params: { from: '2026-11-01T00:00:00Z', to: '2026-11-02T00:00:00Z', timezone: 'UTC' }
      })

      expect(adapter.mock.calls[0][0].params.timezone).toBe('UTC')
    })

    it.each(['object', 'search', 'tuples'])('preserves explicit timezone for %s params', async shape => {
      const entries = [['timezone', 'America/Los_Angeles'], ['page', '2']]
      const params = shape === 'object' ? Object.fromEntries(entries) : shape === 'search' ? new URLSearchParams(entries) : entries
      const adapter = vi.fn().mockImplementation(config => Promise.resolve({ data: {}, status: 200, statusText: 'OK', headers: {}, config }))
      apiClient.defaults.adapter = adapter
      await apiClient.get('/test', { params })
      const actual = adapter.mock.calls[0][0].params
      const query = actual instanceof URLSearchParams ? actual : new URLSearchParams(actual)
      expect(query.get('timezone')).toBe('America/Los_Angeles')
      expect(query.get('page')).toBe('2')
    })

    it.each(['object', 'search', 'tuples'])('adds missing timezone for %s params without mutating the caller', async shape => {
      const entries = [['page', '2']]
      const params = shape === 'object' ? Object.fromEntries(entries) : shape === 'search' ? new URLSearchParams(entries) : entries
      const adapter = vi.fn().mockImplementation(config => Promise.resolve({ data: {}, status: 200, statusText: 'OK', headers: {}, config }))
      apiClient.defaults.adapter = adapter
      await apiClient.get('/test', { params })
      const actual = adapter.mock.calls[0][0].params
      const query = actual instanceof URLSearchParams ? actual : new URLSearchParams(actual)
      expect(query.get('timezone')).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone)
      expect(new URLSearchParams(params).has('timezone')).toBe(false)
    })

    it('POST 请求不附加 timezone 参数', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.post('/test', { foo: 'bar' })

      const config = adapter.mock.calls[0][0]
      expect(config.params?.timezone).toBeUndefined()
    })

    it('请求默认带 withCredentials 以支持跨域 cookie', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.post('/auth/oauth/bind-token')

      const config = adapter.mock.calls[0][0]
      expect(config.withCredentials).toBe(true)
    })

    it('Admin API 在进入管理页面前也带 Admin UI 标记', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/admin/users')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-Admin-UI-Request')).toBe('1')
    })

    it('管理页面调用共享 API 时带 Admin UI 标记', async () => {
      window.history.replaceState({}, '', '/admin/dashboard')
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/groups/available')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-Admin-UI-Request')).toBe('1')
    })

    it('普通用户页面调用共享 API 时不带 Admin UI 标记', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/groups/available')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-Admin-UI-Request')).toBeFalsy()
    })

    it('用户侧 timing API 自动带 User UI 标记', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/auth/me')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-User-UI-Request')).toBe('1')
      expect(config.headers.get('X-Admin-UI-Request')).toBeFalsy()
    })

    it('支付用户 API 带 User UI 标记，公开支付 API 不带', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/payment/plans')
      expect(adapter.mock.calls[0][0].headers.get('X-User-UI-Request')).toBe('1')

      await apiClient.post('/payment/public/orders/verify', {})
      expect(adapter.mock.calls[1][0].headers.get('X-User-UI-Request')).toBeFalsy()
    })

    it('管理页调用共享 API 时同时带 Admin 与 User UI 标记', async () => {
      window.history.replaceState({}, '', '/admin/dashboard')
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/keys')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('X-Admin-UI-Request')).toBe('1')
      expect(config.headers.get('X-User-UI-Request')).toBe('1')
    })
  })

  // --- 响应拦截器 ---

  describe('响应拦截器', () => {
    it('code=0 时解包 data 字段', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: { name: 'test' }, message: 'ok' },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      const response = await apiClient.get('/test')
      expect(response.data).toEqual({ name: 'test' })
    })

    it('code!=0 时拒绝并返回结构化错误', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 1001, message: '参数错误', data: null },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toEqual(
        expect.objectContaining({
          code: 200,
          message: '参数错误',
        })
      )
    })

    it.each(['legacy', 'current'])('preserves mixed-channel confirmation metadata through the interceptor (%s)', async (format) => {
      const metadata = { group_id: 27, group_name: 'Mixed group', current_platform: 'Anthropic', other_platform: 'Antigravity' }
      apiClient.defaults.adapter = vi.fn().mockRejectedValue({
        response: { status: 409, data: {
          ...(format === 'legacy' ? { error: 'mixed_channel_warning', details: metadata } : { code: 409, reason: 'mixed_channel_warning', metadata }),
          message: 'Confirm mixed group',
        } },
        config: { url: '/admin/accounts', headers: {} },
        code: 'ERR_BAD_REQUEST',
        message: 'Request failed with status code 409',
      })
      await expect(apiClient.post('/admin/accounts', {})).rejects.toEqual({
        code: 409, status: 409, reason: 'mixed_channel_warning', message: 'Confirm mixed group', metadata,
      })
    })

    it.each(['legacy', 'current'])('部署与运营合规未确认时广播事件且保留登录态 (%s)', async (format) => {
      localStorage.setItem('auth_token', 'admin-token')
      const listener = vi.fn()
      window.addEventListener('admin-compliance-required', listener)

      const adapter = vi.fn().mockRejectedValue({
        response: {
          status: 423,
          data: {
            ...(format === 'legacy' ? { code: 'ADMIN_COMPLIANCE_ACK_REQUIRED' } : { code: 423, reason: 'ADMIN_COMPLIANCE_ACK_REQUIRED' }),
            message: 'administrator compliance acknowledgement is required',
            metadata: {
              version: 'v2026.06.10',
              document_path_zh: 'docs/legal/admin-compliance.zh.md',
              document_path_en: 'docs/legal/admin-compliance.en.md',
            },
          },
        },
        config: {
          url: '/admin/users',
          headers: { Authorization: 'Bearer admin-token' },
        },
        code: 'ERR_BAD_REQUEST',
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/admin/users')).rejects.toEqual(
        expect.objectContaining({
          status: 423,
          code: 423,
          reason: 'ADMIN_COMPLIANCE_ACK_REQUIRED',
          metadata: expect.objectContaining({
            version: 'v2026.06.10',
          }),
        })
      )

      expect(listener).toHaveBeenCalledTimes(1)
      expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual(
        expect.objectContaining({
          version: 'v2026.06.10',
        })
      )
      expect(localStorage.getItem('auth_token')).toBe('admin-token')

      window.removeEventListener('admin-compliance-required', listener)
    })
  })

  // --- 401 Token 刷新 ---

  describe('401 Token 刷新', () => {
    it('无 refresh_token 时 401 清除 localStorage', async () => {
      localStorage.setItem('auth_token', 'expired-token')
      // 不设置 refresh_token

      // Mock window.location
      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        value: { ...originalLocation, pathname: '/dashboard', href: '/dashboard' },
        writable: true,
      })

      const adapter = vi.fn().mockRejectedValue({
        response: {
          status: 401,
          data: { code: 'TOKEN_EXPIRED', message: 'Token expired' },
        },
        config: {
          url: '/test',
          headers: { Authorization: 'Bearer expired-token' },
        },
        code: 'ERR_BAD_REQUEST',
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toBeDefined()

      expect(localStorage.getItem('auth_token')).toBeNull()

      // 恢复 location
      Object.defineProperty(window, 'location', {
        value: originalLocation,
        writable: true,
      })
    })

    it('有 refresh_token 时刷新并重试原请求', async () => {
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() - 1))
      localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))

      const adapter = vi.fn()
        .mockRejectedValueOnce({
          response: {
            status: 401,
            data: { code: 'TOKEN_EXPIRED', message: 'Token expired' },
          },
          config: {
            url: '/test',
            headers: { Authorization: 'Bearer expired-token' },
          },
          code: 'ERR_BAD_REQUEST',
        })
        .mockResolvedValueOnce({
          status: 200,
          data: { code: 0, data: { ok: true } },
          headers: {},
          config: {},
          statusText: 'OK',
        })
      apiClient.defaults.adapter = adapter
      vi.spyOn(axios, 'post').mockResolvedValueOnce({
        data: {
          code: 0,
          message: 'ok',
          data: {
            access_token: 'new-token',
            refresh_token: 'new-refresh-token',
            expires_in: 3600,
            token_type: 'Bearer',
          },
        },
      })

      await expect(apiClient.get('/test')).resolves.toMatchObject({ data: { ok: true } })

      expect(adapter).toHaveBeenCalledTimes(2)
      expect(localStorage.getItem('auth_token')).toBe('new-token')
      expect(localStorage.getItem('refresh_token')).toBe('new-refresh-token')
      expect(adapter.mock.calls[1][0].headers.get('Authorization')).toBe('Bearer new-token')
    })

    it.each([429, 500, 503, 0])('刷新暂时失败（%s）时保留会话并返回实际状态', async (status) => {
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
      localStorage.setItem('token_expires_at', '123')
      const refreshError = new axios.AxiosError('Refresh unavailable', 'ERR_NETWORK')
      if (status) {
        refreshError.response = {
          status, data: { message: 'Please try again later' }, statusText: '',
          headers: {}, config: { headers: new axios.AxiosHeaders() },
        }
      }
      const refresh = await import('@/api/tokenRefresh')
      vi.spyOn(refresh, 'refreshAuthTokens').mockRejectedValueOnce(refreshError)
      const adapter = vi.fn().mockRejectedValueOnce({
        response: { status: 401, data: { code: 'TOKEN_EXPIRED' } },
        config: { url: '/test', headers: { Authorization: 'Bearer expired-token' } },
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toMatchObject({
        status, code: status, reason: 'TOKEN_REFRESH_UNAVAILABLE',
        message: status ? 'Please try again later' : 'Refresh unavailable',
      })
      expect(adapter).toHaveBeenCalledTimes(1)
      expect(localStorage.getItem('auth_token')).toBe('expired-token')
      expect(localStorage.getItem('refresh_token')).toBe('refresh-token')
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify({ id: 7 }))
      expect(localStorage.getItem('token_expires_at')).toBe('123')
      expect(sessionStorage.getItem('auth_expired')).toBeNull()
      expect(window.location.pathname).toBe('/')
    })

    it.each([401, 403, null])('刷新被拒绝（%s）时仍清除失效会话', async (status) => {
      window.history.replaceState({}, '', '/login')
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
      localStorage.setItem('token_expires_at', '123')
      const refreshError = status
        ? Object.assign(new axios.AxiosError('Refresh rejected'), { response: { status } })
        : new Error('Invalid refresh response')
      const refresh = await import('@/api/tokenRefresh')
      vi.spyOn(refresh, 'refreshAuthTokens').mockRejectedValueOnce(refreshError)
      apiClient.defaults.adapter = vi.fn().mockRejectedValueOnce({
        response: { status: 401, data: { code: 'TOKEN_EXPIRED' } },
        config: { url: '/test', headers: { Authorization: 'Bearer expired-token' } },
      })

      await expect(apiClient.get('/test')).rejects.toMatchObject({ status: 401, code: 401, reason: 'TOKEN_REFRESH_FAILED' })
      for (const key of ['auth_token', 'refresh_token', 'auth_user', 'token_expires_at']) {
        expect(localStorage.getItem(key)).toBeNull()
      }
      expect(sessionStorage.getItem('auth_expired')).toBe('1')
    })

    it('刷新期间换号时旧请求不会清除新会话', async () => {
      localStorage.setItem('auth_token', 'user-a-access')
      localStorage.setItem('refresh_token', 'user-a-refresh')
      localStorage.setItem('token_expires_at', String(Date.now() - 1))
      localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))

      apiClient.defaults.adapter = vi.fn().mockRejectedValueOnce({
        response: {
          status: 401,
          data: { code: 'TOKEN_EXPIRED', message: 'Token expired' },
        },
        config: {
          url: '/test',
          headers: { Authorization: 'Bearer user-a-access' },
        },
        code: 'ERR_BAD_REQUEST',
      })

      let rejectRefresh!: (reason: Error) => void
      vi.spyOn(axios, 'post').mockImplementationOnce(
        () => new Promise((_resolve, reject) => {
          rejectRefresh = reject
        })
      )

      const staleRequest = apiClient.get('/test')
      await vi.waitFor(() => expect(axios.post).toHaveBeenCalledTimes(1))

      localStorage.setItem('auth_token', 'user-b-access')
      localStorage.setItem('refresh_token', 'user-b-refresh')
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))
      localStorage.setItem('auth_user', JSON.stringify({ id: 8 }))
      rejectRefresh(new Error('stale refresh failed'))

      await expect(staleRequest).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(localStorage.getItem('auth_token')).toBe('user-b-access')
      expect(localStorage.getItem('refresh_token')).toBe('user-b-refresh')
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify({ id: 8 }))
      expect(window.location.pathname).toBe('/')
    })
  })

  describe('request identity snapshots', () => {
    function seed(id: number, token: string, refresh = true) {
      localStorage.setItem('auth_user', JSON.stringify({ id }))
      localStorage.setItem('auth_token', token)
      if (refresh) localStorage.setItem('refresh_token', `refresh-${token}`)
      else localStorage.removeItem('refresh_token')
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))
    }

    it.each([true, false])('never replays a late write under a new user (refresh=%s)', async refresh => {
      seed(1, 'a', refresh)
      const refreshModule = await import('@/api/tokenRefresh')
      const refreshSpy = vi.spyOn(refreshModule, 'refreshAuthTokens')
      const adapter = vi.fn(async config => {
        seed(2, 'b', refresh)
        throw { config, response: { status: 401, data: {} } }
      })
      apiClient.defaults.adapter = adapter
      await expect(apiClient.put('/user', { username: 'for-a' })).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(adapter).toHaveBeenCalledTimes(1)
      expect(refreshSpy).not.toHaveBeenCalled()
      expect(localStorage.getItem('auth_token')).toBe('b')
      expect(sessionStorage.getItem('auth_expired')).toBeNull()
    })

    it('allows a same-session peer rotation before the 401 arrives', async () => {
      seed(1, 'a')
      const adapter = vi.fn(async config => {
        if (config.headers.Authorization === 'Bearer a') {
          seed(1, 'rotated')
          throw { config, response: { status: 401, data: {} } }
        }
        return { config, status: 200, data: { code: 0, data: 'ok' }, headers: {}, statusText: 'OK' }
      })
      apiClient.defaults.adapter = adapter
      await expect(apiClient.get('/user')).resolves.toMatchObject({ data: 'ok' })
      expect(adapter).toHaveBeenCalledTimes(2)
      expect(adapter.mock.calls[1][0].headers.Authorization).toBe('Bearer rotated')
    })

    it('checks identity again after refresh succeeds and before replay', async () => {
      seed(1, 'a')
      const refreshModule = await import('@/api/tokenRefresh')
      vi.spyOn(refreshModule, 'refreshAuthTokens').mockImplementationOnce(async () => {
        seed(2, 'b')
        return { access_token: 'b', refresh_token: 'refresh-b', expires_in: 3600, token_type: 'Bearer' }
      })
      const adapter = vi.fn(async config => { throw { config, response: { status: 401, data: {} } } })
      apiClient.defaults.adapter = adapter
      await expect(apiClient.get('/user')).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(adapter).toHaveBeenCalledTimes(1)
      expect(localStorage.getItem('auth_token')).toBe('b')
    })

    it('cancels in-flight requests on peer logout and preserves caller cancellation', async () => {
      seed(1, 'a')
      apiClient.defaults.adapter = async config => {
        const signal = config.signal!
        localStorage.clear()
        window.dispatchEvent(new StorageEvent('storage', { key: null }))
        expect(signal.aborted).toBe(true)
        throw new axios.CanceledError('canceled', config)
      }
      await expect(apiClient.get('/user')).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      const controller = new AbortController()
      controller.abort()
      await expect(apiClient.get('/user', { signal: controller.signal })).rejects.toMatchObject({ code: 'ERR_CANCELED' })
    })

    it('rejects same-user requests from a previous login generation', async () => {
      seed(1, 'a')
      const { advanceAuthSession } = await import('@/api/tokenRefresh')
      apiClient.defaults.adapter = async config => {
        advanceAuthSession()
        throw { config, response: { status: 401, data: {} } }
      }
      await expect(apiClient.get('/user')).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
      expect(localStorage.getItem('auth_token')).toBe('a')
    })
  })

  // --- 网络错误 ---

  describe('网络错误', () => {
    it('网络错误返回 status 0 的错误', async () => {
      const adapter = vi.fn().mockRejectedValue({
        code: 'ERR_NETWORK',
        message: 'Network Error',
        config: { url: '/test' },
        // 没有 response
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toEqual(
        expect.objectContaining({
          status: 0,
          message: 'Network error. Please check your connection.',
        })
      )
    })
  })

  // --- 请求取消 ---

  describe('请求取消', () => {
    it('取消的请求保持原始取消错误', async () => {
      const source = axios.CancelToken.source()

      const adapter = vi.fn().mockRejectedValue(
        new axios.Cancel('Operation canceled')
      )
      apiClient.defaults.adapter = adapter

      await expect(
        apiClient.get('/test', { cancelToken: source.token })
      ).rejects.toBeDefined()
    })
  })

  // --- 二进制错误正文（导出用 responseType: 'blob'）---

  describe('二进制错误正文', () => {
    const conflict = {
      code: 409,
      reason: 'USAGE_HISTORY_INCOMPLETE',
      message: 'Usage history is incomplete',
      metadata: { raw_retained_from: '2026-03-05T00:00:00Z' },
    }

    it('对象正文的 409 信封保留 reason 与 metadata（对照）', async () => {
      apiClient.defaults.adapter = async config => {
        throw { config, message: 'Request failed with status code 409', response: { status: 409, data: conflict } }
      }

      await expect(apiClient.get('/admin/usage/export', { responseType: 'blob' })).rejects.toMatchObject({
        status: 409,
        reason: 'USAGE_HISTORY_INCOMPLETE',
        metadata: { raw_retained_from: '2026-03-05T00:00:00Z' },
      })
    })

    it('同一个 409 信封作为 Blob 时也保留 reason 与 metadata', async () => {
      apiClient.defaults.adapter = async config => {
        throw {
          config,
          message: 'Request failed with status code 409',
          response: { status: 409, data: new Blob([JSON.stringify(conflict)], { type: 'application/json' }) },
        }
      }

      await expect(apiClient.get('/admin/usage/export', { responseType: 'blob' })).rejects.toMatchObject({
        status: 409,
        reason: 'USAGE_HISTORY_INCOMPLETE',
        metadata: { raw_retained_from: '2026-03-05T00:00:00Z' },
      })
    })

    it('解不开的二进制错误正文不让错误处理本身失败', async () => {
      apiClient.defaults.adapter = async config => {
        throw { config, message: 'Bad gateway', response: { status: 502, data: new Blob(['<html>bad gateway</html>']) } }
      }

      await expect(apiClient.get('/admin/usage/export', { responseType: 'blob' })).rejects.toMatchObject({ status: 502 })
    })
  })

  // --- /api/v2 重入幂等性与恢复维护 503 ---

  describe('/api/v2 路径重入幂等性', () => {
    it('当 baseURL 已指向 /api/v2（如 controlClient 或 401 重入）时，在工作空间路由下保持路径幂等', async () => {
      window.history.replaceState({}, '', '/workspaces/ws-1/keys')
      const seenConfigs: Array<{ baseURL?: string; url?: string }> = []
      apiClient.defaults.adapter = async config => {
        seenConfigs.push({ baseURL: config.baseURL, url: config.url })
        return {
          status: 200,
          data: { code: 0, data: { ok: true } },
          headers: {},
          config,
          statusText: 'OK',
        }
      }

      await apiClient.get('/keys', { baseURL: '/api/v2' })
      await apiClient.get('/api/v2/keys')
      await apiClient.request({ method: 'get', baseURL: seenConfigs[1]?.baseURL, url: seenConfigs[1]?.url })

      expect(seenConfigs[0]).toEqual({
        baseURL: '/api/v2',
        url: '/keys',
      })
      expect(seenConfigs[1]).toEqual({
        baseURL: expect.stringMatching(/\/api\/v2$/),
        url: '/keys',
      })
      expect(seenConfigs[2]).toEqual({
        baseURL: seenConfigs[1]?.baseURL,
        url: '/keys',
      })
    })
  })

  describe('恢复维护模式 503 拦截', () => {
    it('识别 503 RESTORE_MAINTENANCE 与 RESTORE_STATUS_UNAVAILABLE 并注入友好提示与续轮询结构化标记', async () => {
      const mod = await import('@/api/client')
      const events: CustomEvent[] = []
      const onMaintenance = (event: Event) => events.push(event as CustomEvent)
      window.addEventListener('restore-maintenance', onMaintenance)

      try {
        apiClient.defaults.adapter = async config => {
          throw {
            config,
            message: 'Request failed with status code 503',
            response: {
              status: 503,
              headers: { 'retry-after': '2' },
              data: {
                code: 503,
                reason: 'RESTORE_MAINTENANCE',
                message: 'Production restore maintenance. Retry after recovery.',
              },
            },
          }
        }

        let maintenanceErr: unknown
        try {
          await apiClient.get('/admin/operations/op-1')
        } catch (err) {
          maintenanceErr = err
        }

        expect(mod.isRestoreMaintenanceError(maintenanceErr)).toBe(true)
        expect(maintenanceErr).toMatchObject({
          status: 503,
          reason: 'RESTORE_MAINTENANCE',
          message: '系统正在执行生产数据恢复维护，请待恢复完成后重试。',
          restoreMaintenance: true,
          retryable: true,
          shouldContinuePolling: true,
          retryAfterSeconds: 2,
          metadata: expect.objectContaining({
            restore_maintenance: true,
            should_continue_polling: true,
            retry_after_seconds: 2,
            i18n_key: 'errors.restoreMaintenance',
          }),
        })
        expect(events).toHaveLength(1)
        expect(events[0].detail).toMatchObject({
          reason: 'RESTORE_MAINTENANCE',
          retryAfterSeconds: 2,
        })
      } finally {
        window.removeEventListener('restore-maintenance', onMaintenance)
      }
    })
  })
})
