import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ConnectionWizardView from '../ConnectionWizardView.vue'
import type { ConnectionResource, ConnectionSchema } from '@/features/connections/types'

const {
  mockRoute,
  mockRouterPush,
  mockConnectionsApi,
  mockPollConnectionOperation,
  mockGetProxies
} = vi.hoisted(() => ({
  mockRoute: {
    params: {} as Record<string, string>,
    meta: {} as Record<string, unknown>
  },
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
  mockPollConnectionOperation: vi.fn(),
  mockGetProxies: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => mockRoute,
  useRouter: () => ({
    push: mockRouterPush
  })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'en-US' }
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () =>
    reactive({
      siteName: 'OmniHive'
    })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    proxies: {
      getAll: mockGetProxies
    }
  }
}))

vi.mock('@/features/connections/api', () => ({
  connectionsApi: mockConnectionsApi,
  pollConnectionOperation: mockPollConnectionOperation
}))

const providerSchemas: ConnectionSchema[] = [
  {
    provider: 'openai',
    schema_version: '1',
    auth_types: ['api_key'],
    fields: [{ path: 'endpoint', required: true }]
  } as unknown as ConnectionSchema,
  {
    provider: 'custom_compat',
    schema_version: '1',
    auth_types: ['api_key'],
    fields: [{ path: 'endpoint', required: true }]
  } as unknown as ConnectionSchema
]

function buildActiveResource(overrides: Partial<ConnectionResource> = {}): ConnectionResource {
  const now = Date.now()
  return {
    id: 'conn_active_1',
    workspace_id: '10',
    version: '5',
    state: 'active',
    provider: 'openai',
    schema_version: '1',
    auth_type: 'api_key',
    name: 'OpenAI Production',
    endpoint: 'https://api.openai.com',
    network_policy_id: '',
    credential: {
      version: '2',
      type: 'api_key',
      state: 'verified',
      last_four: '9999',
      updated_at: new Date(now - 60_000).toISOString()
    },
    last_verification: {
      verification_id: 'ver_valid_1',
      models: ['gpt-4o', 'gpt-4.1'],
      verified_at: new Date(now - 60_000).toISOString(),
      expires_at: new Date(now + 3_600_000).toISOString(),
      checks: [
        { name: 'network', state: 'passed', reason: null },
        { name: 'authorization', state: 'passed', reason: null },
        { name: 'discover_models', state: 'passed', reason: null }
      ]
    },
    model_ids: ['gpt-4o'],
    pool_ids: ['pool_1'],
    limits: { concurrency: 4 },
    quota: { state: 'fresh', remaining: '100', last_success_at: new Date(now).toISOString() },
    allowed_actions: ['connections.edit', 'connections.verify', 'connections.activate'],
    redactions: ['credential'],
    ...overrides
  }
}

function mountWizard() {
  return mount(ConnectionWizardView, {
    global: {
      stubs: {
        'router-link': true,
        AppLayout: { template: '<div data-testid="app-layout"><slot /></div>' }
      }
    }
  })
}

describe('ConnectionWizardView state machine & truth invariants (FE-F09-01)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockRoute.params = {}
    mockRoute.meta = {}
    mockConnectionsApi.listProviderSchemas.mockResolvedValue({ items: providerSchemas })
    mockConnectionsApi.listPools.mockResolvedValue({
      items: [
        { id: 'pool_1', name: 'Primary Pool', max_concurrency: 10 },
        { id: 'pool_2', name: 'Overflow Pool', max_concurrency: 20 }
      ]
    })
    mockConnectionsApi.listEvents.mockResolvedValue({ items: [] })
    mockGetProxies.mockResolvedValue([
      { id: 7, name: 'Tokyo Egress', protocol: 'http', host: '10.0.0.7', port: 8080, status: 'active' }
    ])
  })

  it('editing an active connection and running Preview never sends PATCH or demotes status to draft', async () => {
    mockRoute.params = { id: 'conn_active_1' }
    const activeResource = buildActiveResource()
    mockConnectionsApi.getConnection.mockResolvedValue(activeResource)
    mockConnectionsApi.previewConnection.mockResolvedValue({
      preview_id: 'prev_live_1',
      adds: ['pool_2'],
      removes: [],
      blocked: [],
      expires_at: new Date(Date.now() + 300_000).toISOString(),
      items: [{ id: 'conn_active_1', base_version: '5', state: 'applicable' }]
    })

    const wrapper = mountWizard()
    await flushPromises()

    // Advance Step 1 -> Step 2 -> Step 3 -> Step 4 preserving existing fresh verification
    for (let step = 0; step < 3; step += 1) {
      const continueBtn = wrapper.findAll('button').find((btn) => btn.text().includes('Continue'))
      expect(continueBtn?.attributes('disabled')).toBeUndefined()
      await continueBtn!.trigger('click')
      await flushPromises()
    }

    expect(wrapper.text()).toContain('4. Pool Assignment & Preview')

    // Update exposed models in Step 4 and click Generate Preview
    const modelInput = wrapper
      .findAll('input')
      .find((input) => input.attributes('placeholder') === 'gpt-4o, gpt-4.1-mini')!
    await modelInput.setValue('gpt-4o, gpt-4.1')

    const generatePreviewBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('Generate Preview'))!
    await generatePreviewBtn.trigger('click')
    await flushPromises()

    // Crucial invariant: zero PATCH calls during Preview, candidate models passed to previewConnection
    expect(mockConnectionsApi.patchConnection).not.toHaveBeenCalled()
    expect(mockConnectionsApi.previewConnection).toHaveBeenCalledWith('conn_active_1', '5', {
      model_ids: ['gpt-4o', 'gpt-4.1'],
      pool_ids: ['pool_1'],
      limits: { concurrency: 4 }
    })

    // Advance to Step 5 Activate and confirm connection remains active
    const continueToActivateBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('Continue'))!
    expect(continueToActivateBtn.attributes('disabled')).toBeUndefined()
    await continueToActivateBtn.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Connection Active')
    const activateBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('Activate Connection'))!
    expect(activateBtn.attributes('disabled')).toBeUndefined()
  })

  it('renders diagnostic Retry / Save Draft & Exit actions on Step 3 EVIDENCE_UNAVAILABLE and blocks Step 4 deadlock', async () => {
    mockRoute.params = { id: 'conn_compat_1' }
    const draftCompat = buildActiveResource({
      id: 'conn_compat_1',
      state: 'draft',
      provider: 'custom_compat',
      endpoint: 'https://compat.example.com',
      last_verification: null,
      model_ids: []
    })
    mockConnectionsApi.getConnection.mockResolvedValue(draftCompat)
    mockConnectionsApi.verifyConnection.mockResolvedValue({
      operation_id: 'op_verify_1',
      state: 'running',
      status_url: '/api/v2/workspaces/10/operations/op_verify_1'
    })
    mockPollConnectionOperation.mockResolvedValue({
      operation_id: 'op_verify_1',
      state: 'failed',
      status_url: '/api/v2/workspaces/10/operations/op_verify_1',
      result: {
        verification_id: 'ver_unavailable_1',
        models: [],
        verified_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 3_600_000).toISOString(),
        checks: [
          { name: 'network', state: 'unknown', reason: 'EVIDENCE_UNAVAILABLE' },
          { name: 'authorization', state: 'unknown', reason: 'EVIDENCE_UNAVAILABLE' }
        ]
      }
    })

    const wrapper = mountWizard()
    await flushPromises()

    // Navigate Step 1 -> Step 2 -> Step 3 (Verify)
    for (let step = 0; step < 2; step += 1) {
      const continueBtn = wrapper.findAll('button').find((btn) => btn.text().includes('Continue'))!
      await continueBtn.trigger('click')
      await flushPromises()
    }
    expect(wrapper.text()).toContain('3. Connection & Auth Verification')

    const runProbeBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('Run Verification Probe'))!
    await runProbeBtn.trigger('click')
    await flushPromises()

    const banner = wrapper.get('[data-testid="verify-evidence-unavailable-banner"]')
    expect(banner.text()).toContain('EVIDENCE_UNAVAILABLE')
    expect(wrapper.find('[data-testid="verify-retry-probe-btn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="verify-save-draft-exit-btn"]').exists()).toBe(true)

    // Continue button must remain disabled so user cannot be trapped in Step 4 VERIFICATION_REQUIRED deadlock
    const continueBtn = wrapper.findAll('button').find((btn) => btn.text().includes('Continue'))!
    expect(continueBtn.attributes('disabled')).toBeDefined()
    await continueBtn.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('3. Connection & Auth Verification')
    expect(wrapper.text()).not.toContain('4. Pool Assignment & Preview')

    // Clicking Save as Draft & Exit exits cleanly to /admin/accounts
    await wrapper.get('[data-testid="verify-save-draft-exit-btn"]').trigger('click')
    await flushPromises()
    expect(mockRouterPush).toHaveBeenCalledWith('/admin/accounts')
  })

  it('sequences credential clear BEFORE patchConnection when updating endpoint/egress to prevent 409 conflict', async () => {
    mockRoute.params = { id: 'conn_clear_1' }
    const existing = buildActiveResource({
      id: 'conn_clear_1',
      version: '3',
      state: 'draft',
      endpoint: 'https://old.example.com'
    })
    mockConnectionsApi.getConnection.mockResolvedValue(existing)

    const callOrder: string[] = []
    mockConnectionsApi.changeCredential.mockImplementation(async (_id: string, version: string, payload: { operation: string }) => {
      callOrder.push(`credential:${payload.operation}@v${version}`)
      return {
        version: '4',
        credential: {
          version: '3',
          type: 'api_key',
          state: 'unset',
          last_four: null,
          updated_at: new Date().toISOString()
        }
      }
    })
    mockConnectionsApi.patchConnection.mockImplementation(async (_id: string, version: string) => {
      callOrder.push(`patch@v${version}`)
      return { version: '5', redactions: [] }
    })

    const wrapper = mountWizard()
    await flushPromises()

    // Change endpoint in Step 1
    const endpointInput = wrapper
      .findAll('input')
      .find((input) => (input.element as HTMLInputElement).value === 'https://old.example.com')!
    await endpointInput.setValue('https://new.example.com')

    // Go to Step 2 and select Clear Secret
    const continueBtn = wrapper.findAll('button').find((btn) => btn.text().includes('Continue'))!
    await continueBtn.trigger('click')
    await flushPromises()

    const clearSegmentBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('Clear Secret'))!
    await clearSegmentBtn.trigger('click')
    await flushPromises()

    // Save Draft -> must clear credential FIRST (at v3), then patch endpoint (at v4)
    const saveDraftBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().trim() === 'Save Draft')!
    await saveDraftBtn.trigger('click')
    await flushPromises()

    expect(callOrder).toEqual(['credential:clear@v3', 'patch@v4'])
    expect(mockConnectionsApi.patchConnection).toHaveBeenCalledWith('conn_clear_1', '4', {
      endpoint: 'https://new.example.com'
    })
  })

  it('renders URL_NOT_ALLOWED, UPSTREAM_RATE_LIMITED, and safe plain-text check.detail diagnostics on Step 3', async () => {
    mockRoute.params = { id: 'conn_diag_1' }
    const draftCustom = buildActiveResource({
      id: 'conn_diag_1',
      state: 'draft',
      provider: 'custom_compat',
      endpoint: 'https://custom-llm.internal.example',
      last_verification: null,
      model_ids: []
    })
    mockConnectionsApi.getConnection.mockResolvedValue(draftCustom)
    mockConnectionsApi.verifyConnection.mockResolvedValue({
      operation_id: 'op_diag_1',
      state: 'running',
      status_url: '/api/v2/workspaces/10/operations/op_diag_1'
    })
    mockPollConnectionOperation.mockResolvedValueOnce({
      operation_id: 'op_diag_1',
      state: 'failed',
      status_url: '/api/v2/workspaces/10/operations/op_diag_1',
      result: {
        verification_id: 'ver_url_1',
        models: [],
        verified_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 3_600_000).toISOString(),
        checks: [
          {
            name: 'network',
            state: 'failed',
            reason: 'URL_NOT_ALLOWED',
            detail: 'Endpoint host custom-llm.internal.example rejected <script>alert(1)</script>'
          }
        ]
      }
    })

    const wrapper = mountWizard()
    await flushPromises()

    for (let step = 0; step < 2; step += 1) {
      const continueBtn = wrapper.findAll('button').find((btn) => btn.text().includes('Continue'))!
      await continueBtn.trigger('click')
      await flushPromises()
    }

    const runProbeBtn = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('Run Verification Probe'))!
    await runProbeBtn.trigger('click')
    await flushPromises()

    const failedBanner = wrapper.get('[data-testid="verify-failed-diagnostic-banner"]')
    expect(failedBanner.text()).toContain('URL_NOT_ALLOWED')
    expect(failedBanner.text()).toContain('SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS')
    expect(failedBanner.text()).toContain('.env')

    const detailEl = wrapper.get('[data-testid="verification-check-detail"]')
    expect(detailEl.text()).toContain('custom-llm.internal.example')
    expect(detailEl.text()).toContain('<script>alert(1)</script>')
    expect(detailEl.find('script').exists()).toBe(false)

    // Re-run probe returning UPSTREAM_RATE_LIMITED
    mockPollConnectionOperation.mockResolvedValueOnce({
      operation_id: 'op_diag_2',
      state: 'failed',
      status_url: '/api/v2/workspaces/10/operations/op_diag_2',
      result: {
        verification_id: 'ver_429',
        models: [],
        verified_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 3_600_000).toISOString(),
        checks: [
          {
            name: 'network',
            state: 'failed',
            reason: 'UPSTREAM_RATE_LIMITED',
            detail: 'upstream returned 429 Too Many Requests'
          }
        ]
      }
    })

    await wrapper.get('[data-testid="verify-retry-probe-btn"]').trigger('click')
    await flushPromises()

    const updatedBanner = wrapper.get('[data-testid="verify-failed-diagnostic-banner"]')
    expect(updatedBanner.text()).toContain('UPSTREAM_RATE_LIMITED')
    expect(updatedBanner.text()).toContain('429')
    expect(wrapper.get('[data-testid="verification-check-detail"]').text()).toContain(
      'upstream returned 429 Too Many Requests'
    )
  })
})
