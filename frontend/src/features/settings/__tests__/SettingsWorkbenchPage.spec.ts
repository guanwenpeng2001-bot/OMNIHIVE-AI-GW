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
  totpAPI: { stepUp: vi.fn().mockResolvedValue(undefined) },
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

  it.each([
    ['security', 'audit_log_retention_days', 181],
    ['users', 'default_balance', 12.75],
    ['users', 'default_concurrency', 3],
    ['users', 'default_user_rpm_limit', 0],
    ['users', 'affiliate_rebate_rate', 12.5],
    ['users', 'affiliate_rebate_freeze_hours', 2],
    ['users', 'affiliate_rebate_duration_days', 365],
    ['email', 'smtp_port', 587],
  ] as const)('saves %s/%s as a JSON number and rejects a cleared field', async (section, field, value) => {
    mockGetSettings.mockResolvedValue({ [field]: 1 })
    mockUpdateSettings.mockImplementation(async patch => patch)
    const wrapper = mount(SettingsWorkbenchPage, {
      props: { initialSection: section },
      global: { plugins: [createPinia()] }
    })
    await flushPromises()
    const input = wrapper.get('#settings-' + field)
    const save = wrapper.findAll('button').find(button => button.text() === 'Save Section')!
    await input.setValue('')
    await save.trigger('click')
    expect(mockUpdateSettings).not.toHaveBeenCalled()
    expect(input.attributes('aria-invalid')).toBe('true')
    await input.setValue(String(value))
    await save.trigger('click')
    await flushPromises()
    expect(mockUpdateSettings).toHaveBeenCalledTimes(1)
    expect(mockUpdateSettings).toHaveBeenCalledWith({ [field]: value })
    expect(save.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it.each(['verify', 'cancel'])('handles a server step-up challenge on save (%s)', async action => {
    mockGetSettings.mockResolvedValue({ step_up_enabled: true, passkey_rp_id: 'login.example.org' })
    mockUpdateSettings.mockRejectedValueOnce({ status: 403, reason: 'STEP_UP_REQUIRED' })
      .mockResolvedValueOnce({ step_up_enabled: false })
    const wrapper = mount(SettingsWorkbenchPage, { props: { initialSection: 'security' }, global: { plugins: [createPinia()] } })
    await flushPromises()
    expect(wrapper.get('input[readonly]').element).toHaveProperty('value', 'login.example.org')
    expect(wrapper.text()).toContain('deployment configuration')
    await wrapper.get('#settings-step-up').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'Save Section')!.trigger('click')
    await flushPromises()
    const dialog = wrapper.get('[role="dialog"][aria-modal="true"]')
    if (action === 'verify') await dialog.get('input[autocomplete="one-time-code"]').setValue('123456')
    else document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(mockUpdateSettings.mock.calls).toEqual(action === 'verify'
      ? [[{ step_up_enabled: false }], [{ step_up_enabled: false }]]
      : [[{ step_up_enabled: false }]])
    wrapper.unmount()
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
