import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import {
  CONCRETE_PLATFORM_OPTIONS,
  NATIVE_PLATFORM_OPTIONS,
  getPlatformLabel,
  platformSupportsModelCatalog,
  platformSupportsOAuth,
  platformSupportsPricingSync
} from '@/constants/platforms'
import {
  platformAccentColor,
  platformBadgeClass,
  platformSelectionClass,
  platformSolidBadgeClass
} from '@/utils/platformColors'
import { PROVIDERS, PROVIDER_WORKBUDDY } from '@/constants/channelMonitor'
import { getKeyGroupProvider } from '@/utils/keyGroupProviders'
import {
  FEATURED_PROVIDER_IDS,
  getProviderDisplayMeta,
  inferWorkBuddySiteFromEndpoint,
  isHeaderOverrideSupported
} from '@/features/connections/rules'
import {
  canProceedFromStep,
  createInitialWizardState,
  hydrateWizardFromResource
} from '@/features/connections/wizardState'
import type { ConnectionResource } from '@/features/connections/types'
import type { Account } from '@/types'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import AccountUsageCell from '@/components/account/AccountUsageCell.vue'
import WorkBuddyAuthFields from '@/components/account/WorkBuddyAuthFields.vue'
import { buildWorkBuddyCredentials } from '@/components/account/credentialsBuilder'
import enAccounts from '@/i18n/locales/en/admin/accounts'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'

const { startOAuthMock, pollOAuthMock } = vi.hoisted(() => ({
  startOAuthMock: vi.fn(),
  pollOAuthMock: vi.fn()
}))

vi.mock('@/api/admin/workbuddy', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/admin/workbuddy')>()
  return {
    ...actual,
    startOAuth: startOAuthMock,
    pollOAuth: pollOAuthMock,
    workbuddyAPI: {
      startOAuth: startOAuthMock,
      pollOAuth: pollOAuthMock
    }
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryQuota: vi.fn() },
    accounts: { getUsage: vi.fn() }
  }
}))

function wrapMessages(obj: unknown): unknown {
  if (typeof obj === 'string') return () => obj
  if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
    return Object.fromEntries(
      Object.entries(obj as Record<string, unknown>).map(([k, v]) => [k, wrapMessages(v)])
    )
  }
  return obj
}

function makeI18n(locale: 'en' | 'zh') {
  return createI18n({
    legacy: false,
    locale,
    messages: {
      en: {
        common: { cancel: () => 'Cancel' },
        admin: { accounts: wrapMessages(enAccounts.accounts) }
      },
      zh: {
        common: { cancel: () => '取消' },
        admin: { accounts: wrapMessages(zhAccounts.accounts) }
      }
    },
    missingWarn: false,
    fallbackWarn: false
  })
}

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 101,
    name: 'wb-acc',
    platform: 'workbuddy',
    type: 'oauth',
    proxy_id: null,
    concurrency: 2,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-11-20T00:00:00Z',
    updated_at: '2026-11-20T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

describe('WorkBuddy platform registration, colors, badges, usage, auth & wizard', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    startOAuthMock.mockReset()
    pollOAuthMock.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('registers workbuddy across platform catalogs, colors, monitors, groups, and icons', () => {
    expect(NATIVE_PLATFORM_OPTIONS.some((item) => item.value === 'workbuddy' && item.supportsOAuth)).toBe(true)
    expect(CONCRETE_PLATFORM_OPTIONS.some((item) => item.value === 'workbuddy')).toBe(true)
    expect(platformSupportsOAuth('workbuddy')).toBe(true)
    expect(platformSupportsModelCatalog('workbuddy')).toBe(true)
    expect(platformSupportsPricingSync('workbuddy')).toBe(false)
    expect(getPlatformLabel('workbuddy')).toBe('WorkBuddy')

    expect(platformAccentColor('workbuddy')).toBe('#00C48C')
    expect(platformBadgeClass('workbuddy')).toContain('emerald')
    expect(platformSolidBadgeClass('workbuddy')).toContain('emerald')
    expect(platformSelectionClass('workbuddy')).toContain('emerald')
    expect(PROVIDERS).toContain(PROVIDER_WORKBUDDY)
    expect(getKeyGroupProvider('workbuddy')).toBe('other')

    const iconWrapper = mount(PlatformIcon, { props: { platform: 'workbuddy' } })
    const fallbackWrapper = mount(PlatformIcon)
    expect(iconWrapper.html()).not.toBe(fallbackWrapper.html())
    expect(iconWrapper.findAll('path').length).toBeGreaterThan(0)
  })

  it('renders localized CN and Global site badges in PlatformTypeBadge', async () => {
    const zhWrapper = mount(PlatformTypeBadge, {
      props: { platform: 'workbuddy', type: 'oauth', site: 'cn' },
      global: { plugins: [makeI18n('zh')] }
    })
    expect(zhWrapper.get('[data-testid="platform-label"]').text()).toBe('WorkBuddy')
    expect(zhWrapper.get('[data-testid="workbuddy-site-badge"]').text()).toBe('国内站')

    await zhWrapper.setProps({ site: 'global' })
    expect(zhWrapper.get('[data-testid="workbuddy-site-badge"]').text()).toBe('国际站')

    const enWrapper = mount(PlatformTypeBadge, {
      props: { platform: 'workbuddy', type: 'apikey', site: 'cn' },
      global: { plugins: [makeI18n('en')] }
    })
    expect(enWrapper.get('[data-testid="workbuddy-site-badge"]').text()).toBe('CN')
    await enWrapper.setProps({ site: 'global' })
    expect(enWrapper.get('[data-testid="workbuddy-site-badge"]').text()).toBe('Global')
  })

  it('renders workbuddy_usage_snapshot base and extra credits in AccountUsageCell', async () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: makeAccount({
          platform: 'workbuddy',
          type: 'oauth',
          extra: {
            workbuddy_usage_snapshot: {
              site: 'cn',
              plan: 'Pro',
              base_used: 380,
              base_total: 500,
              base_remaining: 120,
              extra_used: 60,
              extra_total: 100,
              extra_remaining: 40,
              total_remaining: 160
            }
          }
        })
      },
      global: { plugins: [makeI18n('en')] }
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="workbuddy-usage"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="workbuddy-base-credits"]').text()).toContain('120 / 500')
    expect(wrapper.get('[data-testid="workbuddy-extra-credits"]').text()).toContain('40 / 100')

    await wrapper.setProps({
      account: makeAccount({
        platform: 'workbuddy',
        type: 'apikey',
        extra: {
          workbuddy_usage_snapshot: {
            site: 'global',
            base_used: 10,
            base_total: 100,
            base_remaining: 90,
            extra_used: 0,
            extra_total: 0,
            extra_remaining: 0,
            total_remaining: 90
          }
        }
      })
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="workbuddy-base-credits"]').text()).toContain('90 / 100')
    expect(wrapper.find('[data-testid="workbuddy-extra-credits"]').exists()).toBe(false)
  })

  it('supports CN/Global site selection, browser OAuth polling, manual token import, and API key mode in WorkBuddyAuthFields', async () => {
    startOAuthMock.mockResolvedValue({
      session_id: 'sess-wb-global',
      auth_url: 'https://www.workbuddy.ai/oauth/login?code=123',
      state: 'st-1',
      site: 'global',
      expires_in: 120
    })
    pollOAuthMock.mockResolvedValue({
      status: 'completed',
      site: 'global',
      account_name: 'workbuddy-global-user',
      credentials: {
        site: 'global',
        access_token: 'wb-at-global',
        refresh_token: 'wb-rt-global',
        expires_at: '2026-12-31T00:00:00Z',
        uid: 'u-global',
        domain: 'www.workbuddy.ai'
      }
    })

    const wrapper = mount(WorkBuddyAuthFields, {
      props: { mode: 'oauth', site: 'cn' },
      global: { plugins: [makeI18n('en')] }
    })

    await wrapper.get('[data-testid="workbuddy-site-global"]').trigger('click')
    expect(wrapper.emitted('update:site')?.[0]).toEqual(['global'])

    await wrapper.setProps({ site: 'global' })
    await wrapper.get('[data-testid="workbuddy-oauth-start"]').trigger('click')
    await flushPromises()

    expect(startOAuthMock).toHaveBeenCalledWith({ site: 'global' }, expect.any(AbortSignal))
    expect(wrapper.get('[data-testid="workbuddy-oauth-url"]').attributes('href')).toBe(
      'https://www.workbuddy.ai/oauth/login?code=123'
    )

    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()

    expect(pollOAuthMock).toHaveBeenCalledWith({ session_id: 'sess-wb-global' }, expect.any(AbortSignal))
    expect(wrapper.emitted('suggested-name')?.[0]).toEqual(['workbuddy-global-user'])
    expect((wrapper.get('[data-testid="workbuddy-access-token"]').element as HTMLInputElement).value).toBe('wb-at-global')
    expect((wrapper.get('[data-testid="workbuddy-uid"]').element as HTMLInputElement).value).toBe('u-global')
    expect((wrapper.get('[data-testid="workbuddy-domain"]').element as HTMLInputElement).value).toBe('www.workbuddy.ai')

    // Manual token import + buildWorkBuddyCredentials
    await wrapper.get('[data-testid="workbuddy-access-token"]').setValue('manual-at')
    await wrapper.get('[data-testid="workbuddy-refresh-token"]').setValue('manual-rt')
    const builtOAuth = buildWorkBuddyCredentials({
      mode: 'oauth',
      site: 'global',
      oauthCredentials: {
        access_token: 'manual-at',
        refresh_token: 'manual-rt',
        uid: 'u-global',
        domain: 'www.workbuddy.ai',
        expires_at: '2026-12-31T00:00:00Z'
      }
    })
    expect(builtOAuth).toEqual({
      site: 'global',
      access_token: 'manual-at',
      refresh_token: 'manual-rt',
      uid: 'u-global',
      domain: 'www.workbuddy.ai',
      expires_at: '2026-12-31T00:00:00Z'
    })

    // API Key mode (optional base_url)
    await wrapper.setProps({ mode: 'apikey', site: 'cn', apiKey: '', baseUrl: '' })
    await wrapper.get('[data-testid="workbuddy-api-key"]').setValue('wb-key-123')
    expect(
      buildWorkBuddyCredentials({ mode: 'apikey', site: 'cn', apiKey: 'wb-key-123', baseUrl: '' })
    ).toEqual({ site: 'cn', api_key: 'wb-key-123' })
  })

  it('supports workbuddy rules and wizard state for both cn and global sites', () => {
    expect(FEATURED_PROVIDER_IDS).toContain('workbuddy')
    expect(getProviderDisplayMeta('workbuddy', false)).toMatchObject({
      id: 'workbuddy',
      label: 'WorkBuddy',
      defaultEndpoint: 'https://copilot.tencent.com'
    })
    expect(getProviderDisplayMeta('workbuddy', true).protocolSummary).toContain('国内站')
    expect(isHeaderOverrideSupported('workbuddy', 'api_key')).toBe(true)
    expect(inferWorkBuddySiteFromEndpoint('https://www.workbuddy.ai')).toBe('global')
    expect(inferWorkBuddySiteFromEndpoint('https://copilot.tencent.com')).toBe('cn')

    const state = createInitialWizardState('create')
    state.provider = 'workbuddy'
    state.authType = 'oauth'
    state.name = 'WorkBuddy CN'
    state.endpoint = 'https://copilot.tencent.com'
    expect(canProceedFromStep(state, 'provider').valid).toBe(true)

    state.credentialSecret = 'wb-at'
    expect(canProceedFromStep(state, 'credentials').valid).toBe(true)

    const resource: ConnectionResource = {
      id: 'conn_wb',
      workspace_id: 'ws_1',
      version: '1',
      state: 'draft',
      provider: 'workbuddy',
      schema_version: '1',
      auth_type: 'api_key',
      name: 'WorkBuddy Global',
      endpoint: 'https://www.workbuddy.ai',
      network_policy_id: '',
      credential: { version: '1', type: 'api_key', state: 'verified', last_four: '1234', updated_at: '' },
      last_verification: null,
      model_ids: [],
      pool_ids: [],
      limits: { concurrency: 4 },
      quota: { state: 'unknown', remaining: null, last_success_at: null },
      allowed_actions: [],
      redactions: []
    }
    const hydrated = hydrateWizardFromResource(createInitialWizardState('edit'), resource)
    expect(hydrated.workbuddySite).toBe('global')
    expect(hydrated.workbuddyOAuth.domain).toBe('www.workbuddy.ai')
  })
})
