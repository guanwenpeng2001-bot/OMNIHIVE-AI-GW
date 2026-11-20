import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { identityApi } from '@/features/console/scope'
import PlaygroundView from '../PlaygroundView.vue'

const mocks = vi.hoisted(() => ({
  listScopedModels: vi.fn(),
  listPublicModels: vi.fn(),
  listV1Keys: vi.fn(),
  submitRun: vi.fn(),
  replayRequest: vi.fn(),
  getRunByIdempotencyKey: vi.fn(),
  getRunDetail: vi.fn(),
  cancelRun: vi.fn()
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
})
