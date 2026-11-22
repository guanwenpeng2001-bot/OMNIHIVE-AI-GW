import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { identityApi, persistConsoleScope } from '@/features/console/scope'
import PlaygroundCostConfirmModal from '@/features/playground/components/PlaygroundCostConfirmModal.vue'
import PlaygroundView from '../PlaygroundView.vue'

const mocks = vi.hoisted(() => ({
  listScopedModels: vi.fn(),
  listPublicModels: vi.fn(),
  listV1Keys: vi.fn(),
  submitRun: vi.fn(),
  replayRequest: vi.fn(),
  getRunByIdempotencyKey: vi.fn(),
  getRunDetail: vi.fn(),
  cancelRun: vi.fn(),
  authUser: { id: '1', role: 'admin' } as { id: string | number; role?: string } | null
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } })
  }
})

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' }
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: mocks.authUser,
    isAdmin: mocks.authUser?.role === 'admin'
  })
}))

vi.mock('@/api', () => ({
  keysAPI: { list: mocks.listV1Keys }
}))

vi.mock('@/features/console/catalogApi', () => ({
  catalogApi: {
    listScopedModels: mocks.listScopedModels,
    listPublicModels: mocks.listPublicModels
  }
}))

vi.mock('@/features/playground/api', () => ({
  playgroundApi: {
    submitRun: mocks.submitRun,
    replayRequest: mocks.replayRequest,
    getRunByIdempotencyKey: mocks.getRunByIdempotencyKey,
    getRunDetail: mocks.getRunDetail,
    cancelRun: mocks.cancelRun
  }
}))

describe('PlaygroundView 3-pane workbench', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.resetAllMocks()
    vi.spyOn(identityApi, 'listScopes').mockResolvedValue({
      items: [
        {
          workspace_id: '2',
          workspace_name: 'Core Eng',
          workspace_state: 'active',
          projects: [{ id: '9', name: 'Prod Gateway', state: 'active' }]
        }
      ],
      next_cursor: null,
      as_of: '2026-11-19T10:00:00Z'
    })
    vi.spyOn(identityApi, 'listProjectKeys').mockResolvedValue({
      items: [
        {
          id: '15',
          project_id: '9',
          name: 'Service Key',
          prefix: 'oh_live_15',
          state: 'active',
          created_at: '2026-11-19T09:00:00Z',
          version: '1'
        }
      ],
      next_cursor: null,
      as_of: '2026-11-19T10:00:00Z'
    })
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '2',
      project_id: '9',
      actions: ['keys.read', 'playground.run'],
      scope_version: '1',
      expires_at: '2026-11-20T00:00:00Z'
    })
    mocks.listV1Keys.mockResolvedValue({ items: [] })
    mocks.listScopedModels.mockResolvedValue([
      {
        id: 'm_11111111111111111111111111111111',
        slug: 'gpt-5.4-mini',
        displayName: 'GPT-5.4 Mini',
        provider: 'openai',
        protocols: ['openai_chat'],
        modalities: ['text'],
        maxOutputTokens: 8192,
        contextTokens: 128000,
        toolsSupported: true,
        reasoningSupported: true,
        inputRate: { currency: 'USD', amount: '1.00', unit: 'input_token', per: '1000000' },
        outputRate: { currency: 'USD', amount: '4.00', unit: 'output_token', per: '1000000' }
      },
      {
        id: 'm_22222222222222222222222222222222',
        slug: 'claude-4-sonnet',
        displayName: 'Claude 4 Sonnet',
        provider: 'anthropic',
        protocols: ['anthropic_messages'],
        modalities: ['text'],
        maxOutputTokens: 8192,
        contextTokens: 200000,
        toolsSupported: true,
        reasoningSupported: true,
        inputRate: { currency: 'USD', amount: '3.00', unit: 'input_token', per: '1000000' },
        outputRate: { currency: 'USD', amount: '15.00', unit: 'output_token', per: '1000000' }
      }
    ])
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('gates run execution behind cost estimate confirmation before calling POST /playground/runs', async () => {
    mocks.submitRun.mockImplementation(async (_ws, _proj, _payload, opts) => {
      opts.onFrame({
        id: '1',
        event: 'started',
        data: JSON.stringify({ seq: 1, run_id: 'run-a1', request_id: 'req-a1' })
      })
      opts.onFrame({
        id: '2',
        event: 'delta',
        data: JSON.stringify({ seq: 2, run_id: 'run-a1', request_id: 'req-a1', kind: 'text', text: 'Verified output' })
      })
      opts.onFrame({
        id: '3',
        event: 'finished',
        data: JSON.stringify({ seq: 3, run_id: 'run-a1', request_id: 'req-a1', outcome: 'succeeded', settlement_state: 'settled' })
      })
      return { kind: 'stream_completed' }
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()

    await wrapper.get('[data-testid="message-input-0"]').setValue('Explain honeycomb routing')
    await wrapper.get('[data-testid="playground-run-trigger"]').trigger('click')
    await flushPromises()

    // Must NOT have called submitRun before explicit confirmation
    expect(mocks.submitRun).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="playground-cost-confirm-modal"]').text()).toContain('gpt-5.4-mini')

    await wrapper.get('[data-testid="confirm-playground-funding"]').trigger('click')
    await flushPromises()

    expect(mocks.submitRun).toHaveBeenCalledTimes(1)
    expect(mocks.submitRun.mock.calls[0][2]).toMatchObject({
      api_key_id: '15',
      model_id: 'gpt-5.4-mini',
      action: 'text.chat',
      funding_acknowledged: true
    })
    expect(wrapper.get('[data-testid="output-text-A"]').text()).toContain('Verified output')
    wrapper.unmount()
  })

  it('runs Compare Side A and Side B with independent idempotency keys and stops Side A without interrupting Side B', async () => {
    let resolveB!: (v: { kind: 'stream_completed' }) => void
    mocks.submitRun.mockImplementation((_ws, _proj, payload, opts) => {
      if (payload.model_id === 'gpt-5.4-mini') {
        opts.onFrame({
          id: '1',
          event: 'started',
          data: JSON.stringify({ seq: 1, run_id: 'run-left', request_id: 'req-left' })
        })
        return new Promise((_res, rej) => {
          opts.signal?.addEventListener('abort', () => rej(new DOMException('Aborted', 'AbortError')))
        })
      }
      opts.onFrame({
        id: '1',
        event: 'started',
        data: JSON.stringify({ seq: 1, run_id: 'run-right', request_id: 'req-right' })
      })
      opts.onFrame({
        id: '2',
        event: 'delta',
        data: JSON.stringify({ seq: 2, run_id: 'run-right', request_id: 'req-right', kind: 'text', text: 'Right side still streaming!' })
      })
      return new Promise((res) => {
        resolveB = res
      })
    })
    mocks.cancelRun.mockResolvedValue({
      run_id: 'run-left',
      request_id: 'req-left',
      protocol: 'openai_chat',
      state: 'canceled',
      last_seq: 1,
      content_retained: false,
      status_url: '/s',
      detail_url: '/d',
      payer_user_id: '1',
      settlement_state: 'settled'
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()

    // Switch to Compare A/B mode
    const compareBtn = wrapper.findAll('button').find((b) => b.text().includes('Compare A/B'))!
    await compareBtn.trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="message-input-0"]').setValue('Compare latency and structure')
    await wrapper.get('[data-testid="playground-run-trigger"]').trigger('click')
    await flushPromises()

    // Both Side A and Side B estimates displayed
    expect(wrapper.find('[data-testid="cost-estimate-card-0"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="cost-estimate-card-1"]').exists()).toBe(true)

    await wrapper.get('[data-testid="confirm-playground-funding"]').trigger('click')
    await flushPromises()

    expect(mocks.submitRun).toHaveBeenCalledTimes(2)
    const keyA = mocks.submitRun.mock.calls[0][3].idempotencyKey
    const keyB = mocks.submitRun.mock.calls[1][3].idempotencyKey
    expect(keyA).toBeTruthy()
    expect(keyB).toBeTruthy()
    expect(keyA).not.toBe(keyB)

    // Stop Side A only
    await wrapper.get('[data-testid="stop-run-A"]').trigger('click')
    await flushPromises()

    expect(mocks.cancelRun).toHaveBeenCalledTimes(1)
    expect(mocks.cancelRun).toHaveBeenCalledWith('2', '9', 'run-left')
    // Side B remains active and retains its streamed text
    expect(wrapper.get('[data-testid="output-text-B"]').text()).toContain('Right side still streaming!')
    expect(wrapper.find('[data-testid="stop-run-B"]').exists()).toBe(true)

    resolveB({ kind: 'stream_completed' })
    await flushPromises()
    wrapper.unmount()
  })

  it('shows project key empty state without falling back to personal keys and blocks paid confirmation when project keys are empty', async () => {
    vi.spyOn(identityApi, 'listProjectKeys').mockResolvedValue({
      items: [],
      next_cursor: null,
      as_of: '2026-11-19T10:00:00Z'
    })
    mocks.listV1Keys.mockResolvedValue({
      items: [{ id: 9, name: 'Personal Leak Key', key: 'sk-personal-9', status: 'active' }]
    })
    const routerPush = vi.fn()

    const wrapper = mount(PlaygroundView, {
      props: { teleportDisabled: true },
      global: { mocks: { $router: { push: routerPush } } }
    })
    await flushPromises()

    expect(mocks.listV1Keys).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="select-key-a"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="input-key-a"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Personal Leak Key')

    const emptyState = wrapper.get('[data-testid="playground-keys-empty-state"]')
    expect(emptyState.text()).toContain('playgroundKeys.emptyTitle')

    await wrapper.get('[data-testid="message-input-0"]').setValue('Attempt unauthorized key run')
    const runBtn = wrapper.get('[data-testid="playground-run-trigger"]')
    expect(runBtn.attributes('disabled')).toBeDefined()

    await runBtn.trigger('click')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true }))
    await flushPromises()

    expect(wrapper.find('[data-testid="playground-cost-confirm-modal"]').exists()).toBe(false)
    expect(mocks.submitRun).not.toHaveBeenCalled()

    // Compare mode must also block arbitrary key input and running
    const compareBtn = wrapper.findAll('button').find((b) => b.text().includes('Compare A/B'))!
    await compareBtn.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="input-key-b"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="playground-keys-empty-state-b"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="playground-run-trigger"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="playground-create-project-key-btn"]').trigger('click')
    expect(routerPush).toHaveBeenCalledWith('/keys')
    wrapper.unmount()
  })

  it('treats revoked-only project keys as empty and clears stale key selection on scope switch', async () => {
    const listProjectKeysSpy = vi
      .spyOn(identityApi, 'listProjectKeys')
      .mockResolvedValueOnce({
        items: [
          {
            id: '15',
            project_id: '9',
            name: 'Service Key',
            prefix: 'oh_live_15',
            state: 'active',
            created_at: '2026-11-19T09:00:00Z',
            version: '1'
          }
        ],
        next_cursor: null,
        as_of: '2026-11-19T10:00:00Z'
      })
      .mockResolvedValueOnce({
        items: [
          {
            id: '19',
            project_id: '10',
            name: 'Revoked Key',
            prefix: 'oh_live_19',
            state: 'revoked',
            created_at: '2026-11-19T09:00:00Z',
            version: '1'
          }
        ],
        next_cursor: null,
        as_of: '2026-11-19T10:00:00Z'
      })
    vi.spyOn(identityApi, 'listScopes').mockResolvedValue({
      items: [
        {
          workspace_id: '2',
          workspace_name: 'Core Eng',
          workspace_state: 'active',
          projects: [
            { id: '9', name: 'Prod Gateway', state: 'active' },
            { id: '10', name: 'Empty Sandbox', state: 'active' }
          ]
        }
      ],
      next_cursor: null,
      as_of: '2026-11-19T10:00:00Z'
    })
    mocks.listV1Keys.mockResolvedValue({
      items: [{ id: 9, name: 'Personal Leak Key', key: 'sk-personal-9', status: 'active' }]
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()
    expect((wrapper.get('[data-testid="select-key-a"]').element as HTMLSelectElement).value).toBe('15')

    // Switch to project 10 via external scope change event
    persistConsoleScope({ mode: 'workspace', workspaceId: '2', projectId: '10' })
    await flushPromises()

    expect(listProjectKeysSpy).toHaveBeenCalledWith('2', '10', { limit: 100 })
    expect(mocks.listV1Keys).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="playground-keys-empty-state"]').exists()).toBe(true)
    await wrapper.get('[data-testid="message-input-0"]').setValue('Run on sandbox')
    expect(wrapper.get('[data-testid="playground-run-trigger"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="playground-cost-confirm-modal"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('surfaces project key permission error without falling back to personal keys', async () => {
    vi.spyOn(identityApi, 'listProjectKeys').mockRejectedValue({
      status: 403,
      reason: 'ACTION_DENIED',
      message: 'missing keys.read'
    })
    mocks.listV1Keys.mockResolvedValue({
      items: [{ id: 9, name: 'Personal Leak Key', key: 'sk-personal-9', status: 'active' }]
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()

    expect(mocks.listV1Keys).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="console-state-permission"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="playground-keys-empty-state"]').exists()).toBe(true)
    await wrapper.get('[data-testid="message-input-0"]').setValue('Try run')
    expect(wrapper.get('[data-testid="playground-run-trigger"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('blocks execution and shows scope-required empty state when no project scope is available', async () => {
    const listProjectKeysSpy = vi.spyOn(identityApi, 'listProjectKeys')
    vi.spyOn(identityApi, 'listScopes').mockResolvedValue({
      items: [],
      next_cursor: null,
      as_of: '2026-11-19T10:00:00Z'
    })
    mocks.listV1Keys.mockResolvedValue({
      items: [{ id: 9, name: 'Personal Leak Key', key: 'sk-personal-9', status: 'active' }]
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()

    expect(listProjectKeysSpy).not.toHaveBeenCalled()
    expect(mocks.listV1Keys).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="playground-keys-empty-state"]').text()).toContain('playgroundKeys.scopeRequiredTitle')
    await wrapper.get('[data-testid="message-input-0"]').setValue('Try personal run')
    expect(wrapper.get('[data-testid="playground-run-trigger"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('disables funding confirmation inside PlaygroundCostConfirmModal when apiKeyId is empty or scope is required', async () => {
    const wrapper = mount(PlaygroundCostConfirmModal, {
      props: {
        open: true,
        teleportDisabled: true,
        estimates: [
          {
            sideLabel: 'Side A',
            modelId: 'gpt-5.4-mini',
            apiKeyId: '',
            protocol: 'openai_chat',
            estimatedInputTokens: 12,
            maxOutputTokens: 256,
            inputRate: null,
            outputRate: null,
            currency: 'USD',
            estimatedInputCost: null,
            maxOutputCost: null,
            maxTotalCost: null,
            rateKnown: false,
            quoteState: 'requires_scope',
            unpricedReason: 'requires_scope',
            payloadBytes: 120,
            exceedsBodyLimit: false
          }
        ]
      }
    })

    const confirmBtn = wrapper.get('[data-testid="confirm-playground-funding"]')
    expect(confirmBtn.attributes('disabled')).toBeDefined()
    await confirmBtn.trigger('click')
    expect(wrapper.emitted('confirm')).toBeUndefined()
    wrapper.unmount()
  })

  it('filters out non-owned keys by default when keys.act_as is not granted (PG-1)', async () => {
    mocks.authUser = { id: '1', role: 'admin' }
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '2',
      project_id: '9',
      actions: ['keys.read', 'playground.run'],
      scope_version: '1',
      expires_at: '2026-11-20T00:00:00Z'
    })
    vi.spyOn(identityApi, 'listProjectKeys').mockResolvedValue({
      items: [
        {
          id: '15',
          name: 'My Key',
          prefix: 'oh_live_15',
          owner_id: '1',
          state: 'active',
          version: '1',
          credential: null
        },
        {
          id: '22',
          name: 'Other Admin Key',
          prefix: 'oh_live_22',
          owner_id: '99',
          state: 'active',
          version: '1',
          credential: null
        }
      ],
      next_cursor: null,
      as_of: '2026-11-19T10:00:00Z'
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()

    const select = wrapper.get('[data-testid="select-key-a"]')
    const optionValues = select.findAll('option').map((opt) => (opt.element as HTMLOptionElement).value)
    expect(optionValues).toEqual(['15'])
    expect(select.findAll('optgroup')).toHaveLength(0)
    wrapper.unmount()
  })

  it('groups non-owned keys under act_as optgroup and passes payer_user_id when keys.act_as is granted (PG-1)', async () => {
    mocks.authUser = { id: '1', role: 'admin' }
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '2',
      project_id: '9',
      actions: ['keys.read', 'keys.act_as', 'playground.run'],
      scope_version: '1',
      expires_at: '2026-11-20T00:00:00Z'
    })
    vi.spyOn(identityApi, 'listProjectKeys').mockResolvedValue({
      items: [
        {
          id: '15',
          name: 'My Key',
          prefix: 'oh_live_15',
          owner_id: '1',
          state: 'active',
          version: '1',
          credential: null
        },
        {
          id: '22',
          name: 'Teammate Key',
          prefix: 'oh_live_22',
          owner_id: '99',
          state: 'active',
          version: '1',
          credential: null
        }
      ],
      next_cursor: null,
      as_of: '2026-11-19T10:00:00Z'
    })
    mocks.submitRun.mockResolvedValue({
      kind: 'result',
      result: {
        run_id: 'run_act_1',
        request_id: 'req_act_1',
        protocol: 'openai_chat',
        outcome: 'succeeded',
        output: { choices: [{ message: { content: 'ok' } }] },
        settlement_state: 'settled',
        status_url: '/status',
        detail_url: '/detail'
      }
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()

    const select = wrapper.get('[data-testid="select-key-a"]')
    const groups = select.findAll('optgroup')
    expect(groups).toHaveLength(2)
    expect(groups[0].attributes('label')).toBe('playgroundKeys.ownedGroupLabel')
    expect(groups[1].attributes('label')).toBe('playgroundKeys.actAsGroupLabel')
    expect((select.element as HTMLSelectElement).value).toBe('15')

    await select.setValue('22')
    await wrapper.get('[data-testid="message-input-0"]').setValue('Hello')
    await wrapper.get('[data-testid="playground-run-trigger"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="confirm-playground-funding"]').trigger('click')
    await flushPromises()

    expect(mocks.submitRun).toHaveBeenCalledWith(
      '2',
      '9',
      expect.objectContaining({
        api_key_id: '22',
        payer_user_id: '99'
      }),
      expect.any(Object)
    )
    wrapper.unmount()
  })

  it('renders actionable /byok and admin catalog links on 422 NO_ELIGIBLE_ENDPOINT (PG-2)', async () => {
    mocks.authUser = { id: '1', role: 'admin' }
    mocks.submitRun.mockRejectedValue({
      status: 422,
      reason: 'NO_ELIGIBLE_ENDPOINT',
      message: 'No verified endpoint supports this call in the selected scope.'
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()

    await wrapper.get('[data-testid="message-input-0"]').setValue('Test endpoint')
    await wrapper.get('[data-testid="playground-run-trigger"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="confirm-playground-funding"]').trigger('click')
    await flushPromises()

    const banner = wrapper.get('[data-testid="console-state-error"]')
    expect(banner.text()).toContain('NO_ELIGIBLE_ENDPOINT')
    expect(banner.text()).toContain('/byok')
    expect(wrapper.find('[data-testid="playground-goto-byok-A"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="playground-goto-admin-catalog-A"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders explicit read-only option and guidance banner when route ?model= points to a catalog_only model', async () => {
    mocks.authUser = { id: '1', role: 'user' }
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '2',
      project_id: '9',
      actions: ['projects.read', 'models.read', 'playground.run'],
      scope_version: '1',
      expires_at: '2026-11-20T00:00:00Z'
    })

    const wrapper = mount(PlaygroundView, {
      props: { teleportDisabled: true },
      global: {
        mocks: {
          $route: { query: { model: 'catalog-only-model-x' } }
        }
      }
    })
    await flushPromises()

    const selectEl = wrapper.get('[data-testid="select-model-a"]')
    expect((selectEl.element as HTMLSelectElement).value).toBe('catalog-only-model-x')
    expect(wrapper.get('[data-testid="playground-catalog-only-notice-A"]').text()).toContain('catalog-only-model-x')

    await wrapper.get('[data-testid="switch-routed-model-A"]').trigger('click')
    await flushPromises()
    expect((selectEl.element as HTMLSelectElement).value).toBe('gpt-5.4-mini')
    expect(wrapper.find('[data-testid="playground-catalog-only-notice-A"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides /byok quick action on 422 NO_ELIGIBLE_ENDPOINT for members lacking credentials.byok.manage', async () => {
    mocks.authUser = { id: '1', role: 'user' }
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '2',
      project_id: '9',
      actions: ['keys.read', 'playground.run'],
      scope_version: '1',
      expires_at: '2026-11-20T00:00:00Z'
    })
    mocks.submitRun.mockRejectedValue({
      status: 422,
      reason: 'NO_ELIGIBLE_ENDPOINT',
      message: 'No verified endpoint supports this call in the selected scope.'
    })

    const wrapper = mount(PlaygroundView, { props: { teleportDisabled: true } })
    await flushPromises()

    await wrapper.get('[data-testid="message-input-0"]').setValue('Test endpoint')
    await wrapper.get('[data-testid="playground-run-trigger"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="confirm-playground-funding"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="playground-goto-byok-A"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="playground-contact-admin-A"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
