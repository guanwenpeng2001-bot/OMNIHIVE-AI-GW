import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios, { type AxiosInstance, type InternalAxiosRequestConfig } from 'axios'
import type { AtlasUsageResult } from '../admin/atlasUsage'

vi.mock('@/i18n', () => ({ getLocale: () => 'zh-CN' }))

describe('Atlas usage through the shared API client', () => {
  let apiClient: AxiosInstance
  let queryAtlasUsage: typeof import('../admin/atlasUsage').queryAtlasUsage
  const usageWindow = { start_date: '2026-10-01', end_date: '2026-10-02' }
  const usageResult: AtlasUsageResult = {
    provider: 'atlascloud', kind: 'consumed_usage', success: true,
    fetched_at: 1790812800, persisted: false,
    usage: {
      ...usageWindow, scope: 'self', data: [], has_more: false,
      next_page: null, pages_fetched: 1, partial: false,
    },
  }

  function response(config: InternalAxiosRequestConfig) {
    return { config, status: 200, statusText: 'OK', headers: {}, data: { code: 0, data: usageResult } }
  }

  beforeEach(async () => {
    vi.resetModules()
    localStorage.clear()
    sessionStorage.clear()
    window.history.replaceState({}, '', '/')
    localStorage.setItem('auth_token', 'test-admin-token')
    localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
    apiClient = (await import('../client')).apiClient
    queryAtlasUsage = (await import('../admin/atlasUsage')).queryAtlasUsage
  })

  afterEach(() => {
    vi.restoreAllMocks()
    localStorage.clear()
    sessionStorage.clear()
  })

  it('serializes only UTC dates while retaining authentication and response unwrapping', async () => {
    let sentURL: URL | undefined
    const adapter = vi.fn(async (config: InternalAxiosRequestConfig) => {
      // Serialize the configuration AFTER the real request interceptors have run.
      sentURL = new URL(axios.getUri(config), 'http://localhost')
      return response(config)
    })
    apiClient.defaults.adapter = adapter

    const params = Object.freeze({ ...usageWindow })
    await expect(queryAtlasUsage(12, params)).resolves.toEqual(usageResult)
    expect(sentURL?.pathname).toBe('/api/v1/admin/cn-providers/accounts/12/usage')
    expect(Array.from(sentURL!.searchParams.entries())).toEqual(Object.entries(usageWindow))
    expect(params).toEqual(usageWindow)
    expect(adapter).toHaveBeenCalledTimes(1)
    const config = adapter.mock.calls[0][0]
    expect(config.headers.get('Authorization')).toBe('Bearer test-admin-token')
    expect(config.headers.get('Accept-Language')).toBe('zh-CN')
  })

  it('still injects the browser timezone for other GET requests', async () => {
    apiClient.defaults.adapter = async config => {
      const url = new URL(axios.getUri(config), 'http://localhost')
      expect(url.searchParams.get('timezone')).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone)
      expect(url.searchParams.get('page')).toBe('2')
      return response(config)
    }
    await apiClient.get('/admin/accounts', { params: { page: 2 } })
  })

  it('preserves the Qiniu usage query including its explicit timezone', async () => {
    const { queryUsage } = await import('../admin/cnProviders')
    const params = {
      granularity: 'day' as const, start: '2026-10-01T00:00:00+08:00',
      end: '2026-10-02T00:00:00+08:00', timezone: 'Asia/Shanghai',
    }
    apiClient.defaults.adapter = async config => {
      const url = new URL(axios.getUri(config), 'http://localhost')
      expect(Array.from(url.searchParams.entries())).toEqual(Object.entries(params))
      return response(config)
    }
    await queryUsage(13, params)
  })

  it('preserves cancellation through the shared client signal', async () => {
    const controller = new AbortController()
    apiClient.defaults.signal = controller.signal
    apiClient.defaults.adapter = async config => {
      controller.abort()
      expect(config.signal?.aborted).toBe(true)
      throw new axios.CanceledError('canceled', config)
    }
    await expect(queryAtlasUsage(12, usageWindow)).rejects.toMatchObject({ code: 'ERR_CANCELED' })
  })

  it('rejects an old response after the authenticated session changes', async () => {
    apiClient.defaults.adapter = async config => {
      localStorage.setItem('auth_token', 'replacement-admin-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 8 }))
      return response(config)
    }
    await expect(queryAtlasUsage(12, usageWindow)).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(localStorage.getItem('auth_token')).toBe('replacement-admin-token')
  })
})
