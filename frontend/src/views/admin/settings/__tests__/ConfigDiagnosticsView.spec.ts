import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import type { EffectiveConfigItem } from '@/api/admin/config'
import ConfigDiagnosticsView from '../ConfigDiagnosticsView.vue'

const { mockApiGet, mockApiPost, activeLocale } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPost: vi.fn(),
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

vi.mock('@/api/client', () => ({
  apiClient: {
    get: mockApiGet,
    post: mockApiPost
  }
}))

function sampleDiagnosticItems(): EffectiveConfigItem[] {
  return [
    {
      key: 'default_balance',
      group: 'onboarding',
      value_type: 'float',
      value: 0,
      source: 'database',
      authority: 'database_only',
      fallback_chain: ['database', 'default'],
      restart_required: false,
      resettable: false,
      sensitive: false,
      state: 'effective',
      configured: null,
      install_seed_source: 'unknown'
    },
    {
      key: 'default_balance_install_seed_source',
      group: 'onboarding',
      value_type: 'string',
      value: null,
      source: 'database',
      authority: 'database_only',
      fallback_chain: ['database', 'default'],
      restart_required: false,
      resettable: false,
      sensitive: false,
      state: 'effective',
      configured: null,
      install_seed_source: 'environment'
    },
    {
      key: 'linuxdo_connect_client_secret',
      group: 'linuxdo_connect',
      value_type: 'string',
      value: 'NEVER_SHOW_THIS_SECRET_VALUE',
      baseline: 'NEVER_SHOW_THIS_SECRET_BASELINE',
      source: 'database',
      authority: 'database_override',
      fallback_chain: ['database', 'environment', 'yaml', 'default'],
      restart_required: false,
      resettable: true,
      sensitive: true,
      state: 'effective',
      configured: true
    },
    {
      key: 'linuxdo_connect_enabled',
      group: 'linuxdo_connect',
      value_type: 'bool',
      value: true,
      baseline: false,
      source: 'database',
      authority: 'database_override',
      fallback_chain: ['database', 'environment', 'yaml', 'default'],
      restart_required: false,
      resettable: true,
      sensitive: false,
      state: 'effective',
      configured: null
    },
    {
      key: 'passkey_enabled',
      group: 'security',
      value_type: 'bool',
      value: false,
      source: 'database',
      authority: 'database_only',
      fallback_chain: ['database', 'default'],
      restart_required: false,
      resettable: false,
      sensitive: false,
      state: 'blocked_by_hard_gate',
      configured: null,
      blocked_by: 'webauthn.enabled'
    },
    {
      key: 'turnstile.effective_enabled',
      group: 'security',
      value_type: 'bool',
      value: false,
      source: 'derived',
      authority: 'deployment',
      fallback_chain: ['derived'],
      restart_required: false,
      resettable: false,
      sensitive: false,
      state: 'blocked_by_hard_gate',
      configured: null,
      blocked_by: 'turnstile.required',
      error_code: 'TURNSTILE_REQUIRED_NOT_CONFIGURED'
    },
    {
      key: 'ops_advanced_settings',
      group: 'ops',
      value_type: 'object',
      value: {},
      source: 'database',
      authority: 'database_override',
      fallback_chain: ['database', 'environment', 'yaml', 'default'],
      restart_required: false,
      resettable: true,
      sensitive: false,
      state: 'invalid',
      configured: null,
      error_code: 'CONFIG_INVALID'
    }
  ]
}

describe('ConfigDiagnosticsView', () => {
  beforeEach(() => {
    mockApiGet.mockReset()
    mockApiPost.mockReset()
    activeLocale.value = 'en'
  })

  it('renders effective values, sources, hard/soft gate explanations, seed provenance, and never displays secrets', async () => {
    mockApiGet.mockResolvedValue({
      data: {
        config_file: '/app/data/config.yaml',
        generated_at: '2026-11-23T01:30:00Z',
        items: sampleDiagnosticItems(),
        next_cursor: ''
      }
    })

    const wrapper = mount(ConfigDiagnosticsView, {
      global: {
        plugins: [createPinia()],
        stubs: { 'router-link': true }
      }
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="diagnostics-sync-badge"]').text()).toBe('Synced')
    expect(wrapper.get('[data-testid="diagnostics-issues-badge"]').text()).toBe('1 Issues')
    expect(wrapper.get('[data-testid="diagnostics-config-file"]').text()).toBe('/app/data/config.yaml')
    expect(wrapper.find('[data-testid="diagnostics-unhealthy-banner"]').exists()).toBe(true)

    // Secret redaction check in table
    const secretValueCell = wrapper.get('[data-testid="diag-value-linuxdo_connect_client_secret"]')
    expect(secretValueCell.text()).toContain('Configured (secret redacted)')
    expect(wrapper.html()).not.toContain('NEVER_SHOW_THIS_SECRET_VALUE')
    expect(wrapper.html()).not.toContain('NEVER_SHOW_THIS_SECRET_BASELINE')

    // Seed status check: runtime source is Database while seed badge shows Environment Seed
    expect(wrapper.get('[data-testid="diag-source-default_balance"]').text()).toContain('Database')
    expect(wrapper.get('[data-testid="seed-badge-default_balance"]').text()).toContain(
      'Initial Seed: Environment Seed'
    )
    expect(wrapper.get('[data-testid="diag-row-default_balance"]').text()).toContain(
      'subsequent new users only'
    )

    // Hard vs soft gate explanation check
    const passkeyState = wrapper.get('[data-testid="diag-state-passkey_enabled"]')
    expect(passkeyState.text()).toContain('Blocked by Hard Gate')
    expect(passkeyState.text()).toContain('webauthn.enabled')

    const turnstileState = wrapper.get('[data-testid="diag-state-turnstile.effective_enabled"]')
    expect(turnstileState.text()).toContain('turnstile.required')
    expect(turnstileState.text()).toContain('sign-in verification fails closed')
  })

  it('requires explicit confirmation before resetting overrides and redacts secret baselines in the modal', async () => {
    mockApiGet.mockResolvedValue({
      data: {
        config_file: '/app/data/config.yaml',
        generated_at: '2026-11-23T01:30:00Z',
        items: sampleDiagnosticItems(),
        next_cursor: ''
      }
    })
    mockApiPost.mockResolvedValue({
      data: {
        settings: { site_name: 'OmniHive' },
        generated_at: '2026-11-23T01:35:00Z',
        items: [
          {
            key: 'linuxdo_connect_enabled',
            group: 'linuxdo_connect',
            value_type: 'bool',
            value: false,
            baseline: false,
            source: 'yaml',
            authority: 'database_override',
            fallback_chain: ['database', 'environment', 'yaml', 'default'],
            restart_required: false,
            resettable: true,
            sensitive: false,
            state: 'effective',
            configured: null
          },
          {
            key: 'linuxdo_connect_client_secret',
            group: 'linuxdo_connect',
            value_type: 'string',
            value: 'LEAK_AFTER_RESET',
            baseline: 'LEAK_BASELINE_AFTER_RESET',
            source: 'yaml',
            authority: 'database_override',
            fallback_chain: ['database', 'environment', 'yaml', 'default'],
            restart_required: false,
            resettable: true,
            sensitive: true,
            state: 'effective',
            configured: false
          }
        ]
      }
    })

    const wrapper = mount(ConfigDiagnosticsView, {
      global: {
        plugins: [createPinia()],
        stubs: { 'router-link': true }
      }
    })
    await flushPromises()

    // Non-resettable rows must not expose a reset button
    expect(wrapper.find('[data-testid="reset-btn-default_balance"]').exists()).toBe(false)

    // Clicking reset opens confirmation modal without sending POST
    await wrapper.get('[data-testid="reset-btn-linuxdo_connect_enabled"]').trigger('click')
    expect(mockApiPost).not.toHaveBeenCalled()

    const modal = wrapper.get('[data-testid="reset-confirm-modal"]')
    expect(modal.text()).toContain('Atomic group reset: LinuxDo Connect')
    expect(
      wrapper.get('[data-testid="reset-preview-linuxdo_connect_enabled"]').text()
    ).toContain('false')
    expect(
      wrapper.get('[data-testid="reset-preview-linuxdo_connect_client_secret"]').text()
    ).toContain('Redacted — secret baseline is resolved server-side without displaying plaintext')
    expect(wrapper.html()).not.toContain('NEVER_SHOW_THIS_SECRET_BASELINE')

    // Confirming executes reset and updates rendered values & sources
    await wrapper.get('[data-testid="confirm-reset-btn"]').trigger('click')
    await flushPromises()

    expect(mockApiPost).toHaveBeenCalledWith('/admin/config/reset', {
      keys: ['linuxdo_connect_enabled']
    })
    expect(wrapper.find('[data-testid="reset-confirm-modal"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="diag-source-linuxdo_connect_enabled"]').text()).toContain(
      'YAML File'
    )
    expect(wrapper.get('[data-testid="diag-value-linuxdo_connect_enabled"]').text().trim()).toBe('false')
    expect(
      wrapper.get('[data-testid="diag-value-linuxdo_connect_client_secret"]').text()
    ).toContain('Not configured (secret redacted)')
    expect(wrapper.html()).not.toContain('LEAK_AFTER_RESET')
  })

  it('handles initial API failure and refresh failure without pretending synced', async () => {
    mockApiGet.mockRejectedValueOnce({
      status: 500,
      reason: 'INTERNAL_ERROR',
      message: 'Backend config registry unavailable',
      metadata: { request_id: 'req-diag-err-1' }
    })

    const wrapper = mount(ConfigDiagnosticsView, {
      global: {
        plugins: [createPinia()],
        stubs: { 'router-link': true }
      }
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="diagnostics-sync-badge"]').text()).toBe('Not Synced')
    expect(wrapper.text()).not.toContain('Synced\n')
    expect(wrapper.find('[data-testid="diagnostics-load-error"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="diagnostics-load-error"]').text()).toContain('req-diag-err-1')
    expect(wrapper.find('[data-testid="config-diagnostics-table"]').exists()).toBe(false)

    // Retry succeeds
    mockApiGet.mockResolvedValueOnce({
      data: {
        config_file: '',
        generated_at: '2026-11-23T03:00:00Z',
        items: [sampleDiagnosticItems()[0]],
        next_cursor: 'model_plaza_enabled'
      }
    })
    await wrapper.get('[data-testid="diagnostics-refresh-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="diagnostics-sync-badge"]').text()).toBe('Synced')

    // Subsequent loadMore failure marks snapshot as Sync Failed (stale) rather than Synced
    mockApiGet.mockRejectedValueOnce({
      status: 502,
      message: 'Gateway bad upstream'
    })
    await wrapper.get('[data-testid="diagnostics-load-more"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="diagnostics-sync-badge"]').text()).toBe('Sync Failed')
    expect(wrapper.find('[data-testid="diagnostics-stale-error"]').exists()).toBe(true)
  })
})
