import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import SettingsWorkbenchPage from '../SettingsWorkbenchPage.vue'

const { mockGetSettings, mockUpdateSettings, mockApiGet, activeLocale } = vi.hoisted(() => ({
  mockGetSettings: vi.fn(),
  mockUpdateSettings: vi.fn(),
  mockApiGet: vi.fn(),
  activeLocale: { value: 'en' as 'en' | 'zh' }
}))

function lookupTranslation(locale: 'en' | 'zh', key: string, params?: Record<string, unknown>): string {
  const root: Record<string, unknown> = locale === 'zh' ? zh : en
  let cursor: unknown = root
  for (const part of key.split('.')) {
    if (cursor && typeof cursor === 'object' && part in (cursor as Record<string, unknown>)) {
      cursor = (cursor as Record<string, unknown>)[part]
    } else {
      return key
    }
  }
  if (typeof cursor !== 'string') return key
  if (!params) return cursor
  return Object.entries(params).reduce(
    (acc, [name, val]) => acc.replaceAll(`{${name}}`, String(val)),
    cursor
  )
}

vi.mock('vue-i18n', () => ({
  useI18n: () => {
    const localeRef = ref<'en' | 'zh'>(activeLocale.value)
    return {
      locale: localeRef,
      t: (key: string, params?: Record<string, unknown>) =>
        lookupTranslation(localeRef.value, key, params)
    }
  }
}))

vi.mock('@/api', () => ({
  adminAPI: {
    settings: {
      getSettings: mockGetSettings,
      updateSettings: mockUpdateSettings,
      testSmtpConnection: vi.fn()
    }
  }
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: mockApiGet,
    post: vi.fn()
  }
}))

describe('SettingsWorkbenchPage sync state & diagnostics section', () => {
  beforeEach(() => {
    mockGetSettings.mockReset()
    mockUpdateSettings.mockReset()
    mockApiGet.mockReset()
    activeLocale.value = 'en'
  })

  it('preserves an unsaved site name across diagnostics and route-driven tab changes', async () => {
    mockGetSettings.mockResolvedValue({ site_name: 'Original' })
    const wrapper = mount(SettingsWorkbenchPage, {
      props: { initialSection: 'general' },
      global: { plugins: [createPinia()], stubs: { 'router-link': true, ConfigDiagnosticsView: true } }
    })
    await flushPromises()
    const siteName = () => wrapper.findAll('input').find(input => ['Original', 'Draft'].includes(input.element.value))!
    await siteName().setValue('Draft')
    await wrapper.get('[role="tab"][id$="-tab-diagnostics"]').trigger('click')
    expect(wrapper.emitted('select-section')).toEqual([['diagnostics']])
    await wrapper.setProps({ initialSection: 'diagnostics' })
    await wrapper.setProps({ initialSection: 'general' })
    expect(siteName().element.value).toBe('Draft')
    expect(mockGetSettings).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('never renders Synced badge when GET /admin/settings fails', async () => {
    mockGetSettings.mockRejectedValueOnce({
      status: 500,
      message: 'Failed to read system settings',
      metadata: { request_id: 'req-settings-500' }
    })

    const wrapper = mount(SettingsWorkbenchPage, {
      props: { initialSection: 'general' },
      global: {
        plugins: [createPinia()],
        stubs: { 'router-link': true }
      }
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="workbench-sync-badge"]').text()).toBe('Not Synced')
    expect(wrapper.text()).toContain('req-settings-500')
  })

  it('mounts ConfigDiagnosticsView for initialSection="diagnostics" and reflects diagnostics API error state', async () => {
    activeLocale.value = 'zh'
    mockApiGet.mockRejectedValueOnce({
      status: 503,
      message: 'Effective config unavailable'
    })

    const wrapper = mount(SettingsWorkbenchPage, {
      props: { initialSection: 'diagnostics' },
      global: {
        plugins: [createPinia()],
        stubs: { 'router-link': true }
      }
    })
    await flushPromises()

    expect(mockGetSettings).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="config-diagnostics-view"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="workbench-sync-badge"]').text()).toBe('未同步')
  })
})
