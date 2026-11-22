import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import ConnectionWizardView from '@/views/admin/connections/ConnectionWizardView.vue'
import ReAuthAccountModal from '@/components/admin/account/ReAuthAccountModal.vue'
import WorkBuddyAuthFields from '@/components/account/WorkBuddyAuthFields.vue'
import type { ConnectionSchema } from '@/features/connections/types'
import type { Account } from '@/types'
import enAccounts from '@/i18n/locales/en/admin/accounts'

const {
  mockRoute,
  mockRouterPush,
  mockConnectionsApi,
  mockGetProxies,
  mockAccountUpdate,
  startOAuthMock,
  pollOAuthMock
} = vi.hoisted(() => ({
  mockRoute: { params: {} as Record<string, string>, meta: {} as Record<string, unknown> },
  mockRouterPush: vi.fn(),
  mockConnectionsApi: {
    listProviderSchemas: vi.fn(),
    listPools: vi.fn(),
    getConnection: vi.fn(),
    createConnection: vi.fn(),
    patchConnection: vi.fn(),
    changeCredential: vi.fn(),
    attachAuthorization: vi.fn(),
    verifyConnection: vi.fn(),
    previewConnection: vi.fn(),
    activateConnection: vi.fn(),
    createPool: vi.fn(),
    listEvents: vi.fn()
  },
  mockGetProxies: vi.fn(),
  mockAccountUpdate: vi.fn(),
  startOAuthMock: vi.fn(),
  pollOAuthMock: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => mockRoute,
  useRouter: () => ({ push: mockRouterPush })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () =>
    reactive({
      siteName: 'OmniHive',
      showSuccess: vi.fn(),
      showError: vi.fn()
    })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    proxies: { getAll: mockGetProxies },
    accounts: {
      update: mockAccountUpdate,
      applyOAuthCredentials: vi.fn()
    }
  }
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

vi.mock('@/features/connections/api', () => ({
  connectionsApi: mockConnectionsApi,
  pollConnectionOperation: vi.fn()
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

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'en',
    messages: {
      en: {
        common: { cancel: () => 'Cancel', confirm: () => 'Confirm' },
        admin: { accounts: wrapMessages(enAccounts.accounts) }
      }
    },
    missingWarn: false,
    fallbackWarn: false
  })
}

const workbuddySchemas: ConnectionSchema[] = [
  {
    provider: 'workbuddy',
    schema_version: '1',
    auth_types: ['oauth', 'api_key'],
    fields: [{ path: 'endpoint', required: false }],
    supported_actions: ['connections.verify', 'connections.activate']
  }
]

describe('WorkBuddy ConnectionWizardView & ReAuthAccountModal flows', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockRoute.params = {}
    mockRoute.meta = {}
    mockConnectionsApi.listProviderSchemas.mockReset().mockResolvedValue({ items: workbuddySchemas })
    mockConnectionsApi.listPools.mockReset().mockResolvedValue({ items: [] })
    mockConnectionsApi.listEvents.mockReset().mockResolvedValue({ items: [] })
    mockGetProxies.mockReset().mockResolvedValue([])
    mockAccountUpdate.mockReset()
    startOAuthMock.mockReset()
    pollOAuthMock.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('handles WorkBuddy site selection and OAuth login in ConnectionWizardView', async () => {
    startOAuthMock.mockResolvedValue({
      session_id: 'wb-wiz-1',
      auth_url: 'https://www.workbuddy.ai/oauth/login?s=wiz',
      state: 'wiz-st',
      site: 'global',
      expires_in: 120
    })
    pollOAuthMock.mockResolvedValue({
      status: 'completed',
      site: 'global',
      account_name: 'Wizard Global User',
      credentials: {
        site: 'global',
        access_token: 'wiz-at',
        refresh_token: 'wiz-rt',
        expires_at: '2026-12-31T00:00:00Z',
        refresh_expires_at: '2027-01-31T00:00:00Z',
        uid: 'wiz-uid',
        domain: 'www.workbuddy.ai',
        enterprise_id: 'wiz-ent'
      }
    })
    mockConnectionsApi.createConnection.mockResolvedValue({
      id: 'conn_wb_oauth_1',
      workspace_id: '10',
      version: '1',
      state: 'draft',
      provider: 'workbuddy',
      schema_version: '1',
      auth_type: 'oauth',
      name: 'Wizard Global User',
      endpoint: 'https://www.workbuddy.ai',
      network_policy_id: '',
      credential: { version: '1', type: 'oauth', state: 'unset', last_four: null, updated_at: '' },
      last_verification: null,
      model_ids: [],
      pool_ids: [],
      limits: { concurrency: 4 },
      quota: { state: 'unknown', remaining: null, last_success_at: null },
      allowed_actions: ['connections.edit', 'connections.verify'],
      redactions: []
    })
    mockConnectionsApi.changeCredential.mockResolvedValue({
      version: '2',
      credential: {
        version: '2',
        type: 'oauth',
        state: 'unverified',
        last_four: 'z-at',
        updated_at: '2026-11-22T00:00:00Z'
      }
    })

    const wrapper = mount(ConnectionWizardView, {
      global: {
        plugins: [makeI18n()],
        stubs: {
          'router-link': true,
          AppLayout: defineComponent({ template: '<div><slot /></div>' })
        }
      }
    })
    await flushPromises()

    // Step 1: default CN site -> switch to Global -> endpoint updates
    const globalSiteBtn = wrapper.get('[data-testid="wizard-workbuddy-site-global"]')
    await globalSiteBtn.trigger('click')
    const endpointInput = wrapper.findAll('input').find((el) => (el.element as HTMLInputElement).value === 'https://www.workbuddy.ai')
    expect(endpointInput).toBeDefined()

    // Advance to Step 2 (credentials)
    const nextBtn = wrapper.findAll('button').find((btn) => btn.text().includes('Continue'))!
    await nextBtn.trigger('click')
    await flushPromises()

    // Step 2: start WorkBuddy OAuth login
    await wrapper.get('[data-testid="workbuddy-oauth-start"]').trigger('click')
    await flushPromises()
    expect(startOAuthMock).toHaveBeenCalledWith({ site: 'global' }, expect.any(AbortSignal))

    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()
    expect((wrapper.get('[data-testid="workbuddy-access-token"]').element as HTMLInputElement).value).toBe('wiz-at')

    // Persist Step 2 via Save Draft -> changeCredential must send full WorkBuddy OAuth metadata
    const saveDraftBtn = wrapper.findAll('button').find((btn) => btn.text().trim() === 'Save Draft')!
    await saveDraftBtn.trigger('click')
    await flushPromises()
    expect(mockConnectionsApi.changeCredential).toHaveBeenCalledWith('conn_wb_oauth_1', '1', {
      operation: 'set',
      type: 'oauth',
      secret: 'wiz-at',
      refresh_token: 'wiz-rt',
      metadata: {
        site: 'global',
        uid: 'wiz-uid',
        domain: 'www.workbuddy.ai',
        enterprise_id: 'wiz-ent',
        expires_at: '2026-12-31T00:00:00Z',
        refresh_expires_at: '2027-01-31T00:00:00Z'
      }
    })
    wrapper.unmount()
  })

  it('cancels active WorkBuddy OAuth polling when switching mode from oauth to apikey (FE-08)', async () => {
    startOAuthMock.mockResolvedValue({
      session_id: 'wb-cancel-1',
      auth_url: 'https://copilot.tencent.com/oauth/cancel',
      state: 'cancel-st',
      site: 'cn',
      expires_in: 120
    })
    pollOAuthMock.mockResolvedValue({ status: 'pending', site: 'cn' })

    const wrapper = mount(WorkBuddyAuthFields, {
      props: { mode: 'oauth', site: 'cn' },
      global: { plugins: [makeI18n()] }
    })
    await wrapper.get('[data-testid="workbuddy-oauth-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('busy')?.some((args) => args[0] === true)).toBe(true)

    await wrapper.setProps({ mode: 'apikey' })
    await flushPromises()
    const busyCalls = wrapper.emitted('busy') ?? []
    expect(busyCalls[busyCalls.length - 1]).toEqual([false])
    const callsBeforeWait = pollOAuthMock.mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    expect(pollOAuthMock.mock.calls.length).toBe(callsBeforeWait)
    wrapper.unmount()
  })

  it('re-authorizes a WorkBuddy OAuth account in ReAuthAccountModal', async () => {
    startOAuthMock.mockResolvedValue({
      session_id: 'wb-reauth-1',
      auth_url: 'https://copilot.tencent.com/oauth/reauth',
      state: 'reauth-st',
      site: 'cn',
      expires_in: 120
    })
    pollOAuthMock.mockResolvedValue({
      status: 'completed',
      site: 'cn',
      account_name: 'ReAuth CN',
      credentials: {
        site: 'cn',
        access_token: 'new-cn-at',
        refresh_token: 'new-cn-rt',
        expires_at: '2026-12-31T00:00:00Z',
        uid: 'uid-cn',
        domain: 'www.workbuddy.cn'
      }
    })
    const updatedAccount = { id: 88, platform: 'workbuddy', type: 'oauth' } as Account
    mockAccountUpdate.mockResolvedValue(updatedAccount)

    const wrapper = mount(ReAuthAccountModal, {
      props: {
        show: true,
        account: {
          id: 88,
          name: 'WB CN Account',
          platform: 'workbuddy',
          type: 'oauth',
          credentials: { site: 'cn', domain: 'www.workbuddy.cn' }
        } as unknown as Account
      },
      global: {
        plugins: [makeI18n()],
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })
    await flushPromises()

    await wrapper.get('[data-testid="workbuddy-oauth-start"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()

    await wrapper.get('[data-testid="workbuddy-reauth-submit"]').trigger('click')
    await flushPromises()

    expect(mockAccountUpdate).toHaveBeenCalledWith(88, {
      credentials: expect.objectContaining({
        site: 'cn',
        access_token: 'new-cn-at',
        refresh_token: 'new-cn-rt',
        uid: 'uid-cn',
        domain: 'www.workbuddy.cn'
      })
    })
    expect(wrapper.emitted('reauthorized')).toEqual([[updatedAccount]])
    wrapper.unmount()
  })
})
