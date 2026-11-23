// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import {
  buildKiloCredentials,
  cnQuotaCellVisible,
  DEFAULT_KILO_BASE_URL,
  defaultCNAdaptiveBaseUrls,
  defaultCNBaseUrl,
  defaultOpenCodeProtocolRules,
  isHeaderOverrideCapable,
  resolveKiloAccountMode,
  resolveOpenCodeAccountMode
} from '../credentialsBuilder'
import {
  CONCRETE_PLATFORM_OPTIONS,
  GROUP_PLATFORM_OPTIONS,
  getPlatformLabel,
  platformFamily,
  platformSupportsModelCatalog,
  platformSupportsOAuth
} from '@/constants/platforms'
import * as platformColors from '@/utils/platformColors'
import { getProviderDisplayMeta, isHeaderOverrideSupported } from '@/features/connections/rules'
import {
  buildCredentialInputPayload,
  canProceedFromStep,
  createInitialWizardState,
  hasPendingCredentialMutation,
  isWizardKeylessPublicLane
} from '@/features/connections/wizardState'
import AccountUsageCell from '../AccountUsageCell.vue'
import EditAccountModal from '../EditAccountModal.vue'
import enAdminAccounts from '@/i18n/locales/en/admin/accounts'
import zhAdminAccounts from '@/i18n/locales/zh/admin/accounts'
import enDashboard from '@/i18n/locales/en/dashboard'
import zhDashboard from '@/i18n/locales/zh/dashboard'

const { showErrorMock, updateAccountMock } = vi.hoisted(() => ({
  showErrorMock: vi.fn(),
  updateAccountMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isSimpleMode: true
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      update: updateAccountMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false })
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      const parts = key.split('.')
      let cur: unknown = { admin: { accounts: enAdminAccounts.accounts }, monitorCommon: enDashboard.monitorCommon }
      for (const part of parts) {
        if (cur && typeof cur === 'object' && part in (cur as Record<string, unknown>)) {
          cur = (cur as Record<string, unknown>)[part]
        } else {
          return key
        }
      }
      return typeof cur === 'string' ? cur : key
    }
  })
}))

describe('Free Models (OpenCode Zen Free + Kilo) frontend support', () => {
  it('supports OpenCode zen_free mode URLs, protocol rules, and quota suppression', () => {
    expect(resolveOpenCodeAccountMode('zen_free')).toBe('zen_free')
    expect(resolveOpenCodeAccountMode('zen')).toBe('zen')
    expect(resolveOpenCodeAccountMode('go')).toBe('go')
    expect(resolveOpenCodeAccountMode(undefined)).toBe('go')

    expect(defaultCNBaseUrl('opencode_go', 'zen_free', 'adaptive')).toBe('https://opencode.ai/zen/v1')
    expect(defaultCNBaseUrl('opencode_go', 'zen_free', 'chat_completions')).toBe('https://opencode.ai/zen/v1')
    expect(defaultCNBaseUrl('opencode_go', 'zen_free', 'responses')).toBe('https://opencode.ai/zen/v1')
    expect(defaultCNBaseUrl('opencode_go', 'zen_free', 'anthropic')).toBe('https://opencode.ai/zen')
    expect(defaultCNAdaptiveBaseUrls('opencode_go', 'zen_free')).toEqual({
      chat_completions: 'https://opencode.ai/zen/v1',
      anthropic: 'https://opencode.ai/zen',
      responses: 'https://opencode.ai/zen/v1'
    })

    const rules = defaultOpenCodeProtocolRules('zen_free')
    expect(rules.some(rule => rule.pattern === 'union-alpha' && rule.protocol === 'anthropic')).toBe(true)
    expect(rules.some(rule => rule.pattern === 'muse-spark-*' && rule.protocol === 'responses')).toBe(true)

    expect(cnQuotaCellVisible('opencode_go', 'zen_free')).toBe(false)
    expect(cnQuotaCellVisible('opencode_go', 'zen')).toBe(false)
    expect(cnQuotaCellVisible('opencode_go', 'go')).toBe(true)
  })

  it('builds Kilo credentials cleanly for keyless free mode and apikey mode', () => {
    expect(resolveKiloAccountMode(undefined)).toBe('free')
    expect(resolveKiloAccountMode('free')).toBe('free')
    expect(resolveKiloAccountMode('apikey')).toBe('apikey')

    const freeCreds = buildKiloCredentials({
      mode: 'free',
      apiKey: 'should-be-stripped',
      existing: { api_key: 'old-key', model_mapping: { a: 'b' } }
    })
    expect(freeCreds.account_mode).toBe('free')
    expect(freeCreds.api_key).toBeUndefined()
    expect(freeCreds.base_url).toBe(DEFAULT_KILO_BASE_URL)
    expect(freeCreds.model_mapping).toEqual({ a: 'b' })

    const keyCreds = buildKiloCredentials({
      mode: 'apikey',
      apiKey: '  kilo-secret-123  ',
      baseUrl: 'https://custom.kilo.example/gateway'
    })
    expect(keyCreds).toEqual({
      account_mode: 'apikey',
      api_key: 'kilo-secret-123',
      base_url: 'https://custom.kilo.example/gateway'
    })

    expect(defaultCNBaseUrl('kilo', 'payg', 'chat_completions')).toBe(DEFAULT_KILO_BASE_URL)
    expect(isHeaderOverrideCapable('kilo', 'apikey')).toBe(true)
    expect(isHeaderOverrideSupported('kilo', 'api_key')).toBe(true)
  })

  it('registers Kilo platform options, color tokens, and locale-neutral labels', () => {
    expect(CONCRETE_PLATFORM_OPTIONS.some(option => option.value === 'kilo')).toBe(true)
    expect(GROUP_PLATFORM_OPTIONS.some(option => option.value === 'kilo')).toBe(true)
    expect(platformSupportsOAuth('kilo')).toBe(false)
    expect(platformSupportsModelCatalog('kilo')).toBe(true)
    expect(platformFamily('kilo')).toBe('openaiCompatible')
    expect(getPlatformLabel('kilo')).toBe('Kilo')
    expect(platformColors.platformBadgeClass('kilo')).toContain('slate-')
    expect(platformColors.platformAccentColor('kilo')).toBe('#475569')
    expect(getProviderDisplayMeta('kilo', false).defaultEndpoint).toBe(DEFAULT_KILO_BASE_URL)

    expect(Object.keys(enAdminAccounts.accounts.platforms).sort()).toEqual(
      Object.keys(zhAdminAccounts.accounts.platforms).sort()
    )
    expect(enAdminAccounts.accounts.platforms.kilo).toBe('Kilo')
    expect(zhAdminAccounts.accounts.platforms.kilo).toBe('Kilo')
    expect(enDashboard.monitorCommon.providers.kilo).toBe('Kilo')
    expect(zhDashboard.monitorCommon.providers.kilo).toBe('Kilo')
  })

  it('allows blank secrets on keyless public lanes in connection wizard state', () => {
    const kiloState = createInitialWizardState('create')
    kiloState.provider = 'kilo'
    kiloState.authType = 'api_key'
    kiloState.kiloMode = 'free'
    expect(isWizardKeylessPublicLane(kiloState)).toBe(true)
    expect(canProceedFromStep(kiloState, 'credentials')).toEqual({ valid: true })
    expect(hasPendingCredentialMutation(kiloState)).toBe(true)
    expect(buildCredentialInputPayload(kiloState)).toEqual({
      operation: 'set',
      type: 'api_key',
      secret: ''
    })

    kiloState.kiloMode = 'apikey'
    expect(isWizardKeylessPublicLane(kiloState)).toBe(false)
    expect(canProceedFromStep(kiloState, 'credentials').valid).toBe(false)

    const openCodeState = createInitialWizardState('create')
    openCodeState.provider = 'opencode_go'
    openCodeState.authType = 'api_key'
    openCodeState.openCodeMode = 'zen_free'
    expect(isWizardKeylessPublicLane(openCodeState)).toBe(true)
    expect(canProceedFromStep(openCodeState, 'credentials')).toEqual({ valid: true })
    expect(buildCredentialInputPayload(openCodeState)).toEqual({
      operation: 'set',
      type: 'api_key',
      secret: 'public'
    })
  })

  it('renders dedicated free-pool usage badges for OpenCode zen_free and Kilo accounts', () => {
    const zenFreeWrapper = mount(AccountUsageCell, {
      props: {
        account: {
          id: 101,
          name: 'OpenCode Free',
          platform: 'opencode_go',
          type: 'apikey',
          status: 'active',
          schedulable: true,
          credentials: { account_mode: 'zen_free' }
        } as any
      }
    })
    expect(zenFreeWrapper.find('[data-testid="opencode-zen-free-usage-badge"]').text()).toBe('Zen Free Pool')

    const kiloFreeWrapper = mount(AccountUsageCell, {
      props: {
        account: {
          id: 102,
          name: 'Kilo Free',
          platform: 'kilo',
          type: 'apikey',
          status: 'active',
          schedulable: true,
          credentials: { account_mode: 'free' }
        } as any
      }
    })
    expect(kiloFreeWrapper.find('[data-testid="kilo-usage-badge"]').text()).toBe('Kilo Free Pool')
  })

  it('blocks EditAccountModal submit when switching OpenCode zen_free to paid go/zen without a real API key', async () => {
    showErrorMock.mockReset()
    updateAccountMock.mockReset()
    updateAccountMock.mockResolvedValue({ id: 201 })

    const BaseDialogStub = defineComponent({
      props: { show: { type: Boolean, default: false } },
      template: '<div v-if="show"><slot /><slot name="footer" /></div>'
    })

    const wrapper = mount(EditAccountModal, {
      props: {
        show: true,
        account: {
          id: 201,
          name: 'OpenCode Free',
          platform: 'opencode_go',
          type: 'apikey',
          status: 'active',
          schedulable: true,
          concurrency: 1,
          priority: 1,
          rate_multiplier: 1,
          group_ids: [],
          expires_at: null,
          auto_pause_on_expired: false,
          credentials: { account_mode: 'zen_free', base_url: 'https://opencode.ai/zen/v1' },
          credentials_status: { has_api_key: true }
        } as any,
        proxies: [],
        groups: []
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: true,
          Icon: true,
          ProxySelector: true,
          GroupSelector: true,
          ModelWhitelistSelector: true
        }
      }
    })
    await flushPromises()

    // Switch mode from zen_free to go without entering a new API key
    const goBtn = wrapper.findAll('[role="radio"]').find((el) => el.text().includes('Go'))!
    await goBtn.trigger('click')
    await flushPromises()

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith(enAdminAccounts.accounts.apiKeyIsRequired)
    expect(updateAccountMock).not.toHaveBeenCalled()

    // Now provide a real API key -> submit succeeds
    const apiKeyInput = wrapper.get('[data-account-field="editApiKey"]')
    await apiKeyInput.setValue('sk-opencode-real-key')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledWith(
      201,
      expect.objectContaining({
        credentials: expect.objectContaining({
          account_mode: 'go',
          api_key: 'sk-opencode-real-key'
        })
      })
    )
    wrapper.unmount()
  })
})
