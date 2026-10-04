import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api/client'
import { queryManagementUsage } from '@/api/admin/managementUsage'

vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
const originalAdapter = apiClient.defaults.adapter
describe('management usage through real API client', () => {
  beforeEach(() => { localStorage.clear(); sessionStorage.clear(); window.history.replaceState({}, '', '/') })
  afterEach(() => { apiClient.defaults.adapter = originalAdapter })
  it.each([undefined, { year: 2026, month: 10 }])('serializes only supported query fields: %s', async month => {
    let actualURL = ''
    apiClient.defaults.adapter = async config => {
      actualURL = apiClient.getUri(config)
      return { status: 200, statusText: 'OK', headers: {}, config, data: { code: 0, data: { success: true } } }
    }
    expect(await queryManagementUsage(12, month)).toEqual({ success: true })
    const parsed = new URL(actualURL, 'https://application.example')
    expect(parsed.pathname).toMatch(/\/admin\/cn-providers\/accounts\/12\/usage$/)
    expect([...parsed.searchParams.entries()].sort()).toEqual(month ? [['month', '10'], ['year', '2026']] : [])
    expect(actualURL).not.toContain('timezone')
  })
})
