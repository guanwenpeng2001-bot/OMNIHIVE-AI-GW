import { describe, expect, it, vi } from 'vitest'
import {
  applyAuthoritativeQuoteToEstimate,
  buildNativePlaygroundInput,
  buildPlaygroundSubmitPayload,
  estimatePlaygroundRunCost,
  resolveCatalogQuoteModelId
} from '../costEstimate'
import { createPlaygroundRunController, type PlaygroundApiAdapter } from '../runMachine'
import { normalizePlaygroundError } from '../errors'
import { PlaygroundSseParser } from '../sseParser'
import type { PlaygroundSideConfig, PlaygroundSummary } from '../types'

function makeSideConfig(overrides: Partial<PlaygroundSideConfig> = {}): PlaygroundSideConfig {
  return {
    label: 'Side A',
    apiKeyId: '12',
    modelId: 'm_0123456789abcdef0123456789abcdef',
    protocol: 'openai_chat',
    temperature: 0.5,
    maxOutputTokens: 1024,
    reasoningEffort: 'medium',
    stream: true,
    includeRawEvents: false,
    retainTextPreview: true,
    ...overrides
  }
}

function createMockAdapter(overrides: Partial<PlaygroundApiAdapter> = {}): PlaygroundApiAdapter {
  return {
    submitRun: vi.fn(),
    replayRequest: vi.fn(),
    getRunByIdempotencyKey: vi.fn(),
    getRunDetail: vi.fn(),
    cancelRun: vi.fn(),
    ...overrides
  }
}

describe('PlaygroundSseParser', () => {
  it('parses frames split across arbitrary chunk boundaries and ignores comments', () => {
    const parser = new PlaygroundSseParser()
    expect(parser.feed(': keepalive\n\nid: 1\nevent: star')).toEqual([])
    const first = parser.feed('ted\ndata: {"seq":1,"run_id":"r1","request_id":"q1"}\n\nid: 2\n')
    expect(first).toEqual([
      {
        id: '1',
        event: 'started',
        data: '{"seq":1,"run_id":"r1","request_id":"q1"}'
      }
    ])
    const second = parser.feed('event: delta\ndata: {"seq":2,"run_id":"r1","request_id":"q1","kind":"text","text":"Hi"}')
    expect(second).toEqual([])
    expect(parser.flush()).toEqual([
      {
        id: '2',
        event: 'delta',
        data: '{"seq":2,"run_id":"r1","request_id":"q1","kind":"text","text":"Hi"}'
      }
    ])
  })
})

describe('Playground SSE state machine, stop, reconnect, and cost confirmation', () => {
  it('accumulates text, reasoning, indexed tool_arguments, observed zero usage, and terminal settlement', async () => {
    let clock = 1000
    const api = createMockAdapter({
      submitRun: vi.fn(async (_ws, _proj, _payload, opts) => {
        opts.onFrame({
          id: '1',
          event: 'started',
          data: JSON.stringify({
            seq: 1,
            run_id: '018f0c82-1111-7000-8000-000000000001',
            request_id: '018f0c82-2222-7000-8000-000000000002',
            protocol: 'openai_chat',
            price_version: 'pv_9'
          })
        })
        clock = 1140
        opts.onFrame({
          id: '2',
          event: 'delta',
          data: JSON.stringify({
            seq: 2,
            run_id: '018f0c82-1111-7000-8000-000000000001',
            request_id: '018f0c82-2222-7000-8000-000000000002',
            kind: 'reasoning',
            text: 'Checking weather tool…'
          })
        })
        // Duplicate / out-of-order seq must be ignored
        opts.onFrame({
          id: '2',
          event: 'delta',
          data: JSON.stringify({
            seq: 2,
            run_id: '018f0c82-1111-7000-8000-000000000001',
            request_id: '018f0c82-2222-7000-8000-000000000002',
            kind: 'text',
            text: 'DUPLICATE'
          })
        })
        opts.onFrame({
          id: '3',
          event: 'delta',
          data: JSON.stringify({
            seq: 3,
            run_id: '018f0c82-1111-7000-8000-000000000001',
            request_id: '018f0c82-2222-7000-8000-000000000002',
            kind: 'tool_arguments',
            index: 0,
            tool_call_id: 'call_weather_1',
            name: 'get_weather',
            fragment: '{"city":'
          })
        })
        opts.onFrame({
          id: '4',
          event: 'delta',
          data: JSON.stringify({
            seq: 4,
            run_id: '018f0c82-1111-7000-8000-000000000001',
            request_id: '018f0c82-2222-7000-8000-000000000002',
            kind: 'tool_arguments',
            index: 0,
            fragment: '"Shanghai"}'
          })
        })
        opts.onFrame({
          id: '5',
          event: 'delta',
          data: JSON.stringify({
            seq: 5,
            run_id: '018f0c82-1111-7000-8000-000000000001',
            request_id: '018f0c82-2222-7000-8000-000000000002',
            kind: 'text',
            text: 'Sunny, 22C.'
          })
        })
        opts.onFrame({
          id: '6',
          event: 'usage',
          data: JSON.stringify({
            seq: 6,
            run_id: '018f0c82-1111-7000-8000-000000000001',
            request_id: '018f0c82-2222-7000-8000-000000000002',
            quantities: [
              { unit: 'input_token', quantity: '48' },
              { unit: 'output_token', quantity: '0' }
            ],
            evidence: 'provider_reported'
          })
        })
        clock = 1520
        opts.onFrame({
          id: '7',
          event: 'finished',
          data: JSON.stringify({
            seq: 7,
            run_id: '018f0c82-1111-7000-8000-000000000001',
            request_id: '018f0c82-2222-7000-8000-000000000002',
            outcome: 'succeeded',
            settlement_state: 'settled',
            status_url: '/api/v2/workspaces/1/projects/1/playground/runs/018f0c82-1111-7000-8000-000000000001',
            detail_url: '/api/v2/workspaces/1/projects/1/requests/018f0c82-2222-7000-8000-000000000002'
          })
        })
        return { kind: 'stream_completed' as const }
      })
    })

    const controller = createPlaygroundRunController({ api, now: () => clock })
    const side = makeSideConfig()
    const input = buildNativePlaygroundInput('openai_chat', {
      systemPrompt: 'Be concise.',
      messages: [{ id: '1', role: 'user', content: 'Weather in Shanghai?' }],
      temperature: 0.5,
      maxOutputTokens: 1024,
      reasoningEffort: 'medium'
    })
    const payload = buildPlaygroundSubmitPayload(side, input)

    await controller.executeApprovedRun('1', '2', payload, 'idem-test-1')

    expect(controller.state.status).toBe('succeeded')
    expect(controller.state.runId).toBe('018f0c82-1111-7000-8000-000000000001')
    expect(controller.state.priceVersion).toBe('pv_9')
    expect(controller.state.reasoningText).toBe('Checking weather tool…')
    expect(controller.state.outputText).toBe('Sunny, 22C.')
    expect(controller.state.toolCalls).toHaveLength(1)
    expect(controller.state.toolCalls[0]).toMatchObject({
      index: 0,
      toolCallId: 'call_weather_1',
      name: 'get_weather',
      rawArguments: '{"city":"Shanghai"}',
      parsedArguments: { city: 'Shanghai' }
    })
    expect(controller.state.inputTokens).toBe('48')
    expect(controller.state.outputTokens).toBe('0')
    expect(controller.state.ttftMs).toBe(140)
    expect(controller.state.elapsedMs).toBe(520)
    expect(controller.state.settlementState).toBe('settled')
  })

  it('stops an in-flight stream via POST /cancel and updates terminal state without touching other runs', async () => {
    let abortSeen = false
    const cancelSummary: PlaygroundSummary = {
      run_id: 'run-stop-1',
      request_id: 'req-stop-1',
      protocol: 'openai_chat',
      state: 'cancel_requested',
      last_seq: 2,
      content_retained: false,
      status_url: '/status',
      detail_url: '/detail',
      payer_user_id: '7',
      settlement_state: 'pending'
    }
    const finalCanceledSummary: PlaygroundSummary = {
      ...cancelSummary,
      state: 'canceled',
      settlement_state: 'settled'
    }

    const api = createMockAdapter({
      submitRun: vi.fn((_ws, _proj, _payload, opts) => {
        opts.onFrame({
          id: '1',
          event: 'started',
          data: JSON.stringify({ seq: 1, run_id: 'run-stop-1', request_id: 'req-stop-1' })
        })
        return new Promise((_resolve, reject) => {
          opts.signal?.addEventListener('abort', () => {
            abortSeen = true
            reject(new DOMException('Aborted', 'AbortError'))
          })
        })
      }),
      cancelRun: vi.fn().mockResolvedValue(cancelSummary),
      getRunDetail: vi.fn().mockResolvedValue(finalCanceledSummary)
    })

    const controller = createPlaygroundRunController({ api })
    const payload = buildPlaygroundSubmitPayload(makeSideConfig(), { messages: [{ role: 'user', content: 'Hi' }] })
    const runPromise = controller.executeApprovedRun('3', '8', payload, 'idem-stop-1')

    expect(controller.state.status).toBe('streaming')
    await controller.stop('3', '8')
    await runPromise

    expect(api.cancelRun).toHaveBeenCalledWith('3', '8', 'run-stop-1')
    expect(abortSeen).toBe(true)
    expect(controller.state.status).toBe('canceled')
    expect(controller.state.settlementState).toBe('settled')
  })

  it('recovers a disconnected stream by original Idempotency-Key without submitting a new paid call', async () => {
    const recoveredSummary: PlaygroundSummary = {
      run_id: 'run-recovered-99',
      request_id: 'req-recovered-99',
      protocol: 'anthropic_messages',
      state: 'succeeded',
      last_seq: 11,
      content_retained: true,
      text_preview: 'Recovered ephemeral preview text',
      status_url: '/api/v2/workspaces/1/projects/4/playground/runs/run-recovered-99',
      detail_url: '/api/v2/workspaces/1/projects/4/requests/req-recovered-99',
      payer_user_id: '5',
      settlement_state: 'settled'
    }

    const api = createMockAdapter({
      submitRun: vi.fn().mockRejectedValue(new TypeError('Failed to fetch')),
      getRunByIdempotencyKey: vi.fn().mockResolvedValue(recoveredSummary)
    })

    const controller = createPlaygroundRunController({ api })
    const payload = buildPlaygroundSubmitPayload(
      makeSideConfig({ protocol: 'anthropic_messages', retainTextPreview: true }),
      { messages: [{ role: 'user', content: 'Hello' }], max_tokens: 512 }
    )

    await controller.executeApprovedRun('1', '4', payload, 'idem-recover-key-77')

    expect(controller.state.status).toBe('disconnected_recoverable')
    expect(controller.state.idempotencyKey).toBe('idem-recover-key-77')
    expect(api.submitRun).toHaveBeenCalledTimes(1)

    await controller.recoverByIdempotencyKey('1', '4')

    expect(api.getRunByIdempotencyKey).toHaveBeenCalledWith('1', '4', 'idem-recover-key-77')
    expect(api.submitRun).toHaveBeenCalledTimes(1)
    expect(controller.state.status).toBe('succeeded')
    expect(controller.state.runId).toBe('run-recovered-99')
    expect(controller.state.outputText).toBe('Recovered ephemeral preview text')
  })

  it('computes upper-bound cost estimates from catalog rates and flags payloads over 256 KiB', () => {
    const side = makeSideConfig({ maxOutputTokens: 2000 })
    const payload = buildPlaygroundSubmitPayload(side, {
      messages: [{ role: 'user', content: 'Compute estimate' }],
      max_completion_tokens: 2000
    })
    const estimate = estimatePlaygroundRunCost(side, payload, {
      id: side.modelId,
      slug: 'gpt-5',
      displayName: 'GPT-5',
      provider: 'openai',
      protocols: ['openai_chat'],
      modalities: ['text'],
      maxOutputTokens: 16384,
      contextTokens: 128000,
      toolsSupported: true,
      reasoningSupported: true,
      inputRate: { currency: 'USD', amount: '2.00', unit: 'input_token', per: '1000000' },
      outputRate: { currency: 'USD', amount: '10.00', unit: 'output_token', per: '1000000' }
    })

    expect(estimate.rateKnown).toBe(true)
    expect(estimate.exceedsBodyLimit).toBe(false)
    expect(Number(estimate.maxOutputCost)).toBeCloseTo(0.02, 6)

    const hugePayload = buildPlaygroundSubmitPayload(side, {
      messages: [{ role: 'user', content: 'x'.repeat(270 * 1024) }]
    })
    const hugeEstimate = estimatePlaygroundRunCost(side, hugePayload, null)
    expect(hugeEstimate.rateKnown).toBe(false)
    expect(hugeEstimate.maxTotalCost).toBeNull()
    expect(hugeEstimate.exceedsBodyLimit).toBe(true)
  })

  it('merges authoritative server quotes including frozen rates, funding source, and unpriced state', () => {
    const side = makeSideConfig({ maxOutputTokens: 1000 })
    const payload = buildPlaygroundSubmitPayload(side, {
      messages: [{ role: 'user', content: 'Quote test' }],
      max_completion_tokens: 1000
    })
    const base = estimatePlaygroundRunCost(side, payload, null)
    expect(resolveCatalogQuoteModelId('gpt-5', {
      id: side.modelId,
      slug: 'gpt-5',
      displayName: 'GPT-5',
      provider: 'openai',
      protocols: ['openai_chat'],
      modalities: ['text'],
      maxOutputTokens: 4096,
      contextTokens: 128000,
      toolsSupported: true,
      reasoningSupported: true,
      inputRate: null,
      outputRate: null
    })).toBe(side.modelId)

    const priced = applyAuthoritativeQuoteToEstimate(
      base,
      {
        state: 'estimated',
        price_version: 'pv_2026_11',
        source: 'published',
        estimated_charge: { currency: 'USD', amount: '0.01500000' },
        components: [
          { kind: 'input', quantity: '20', raw_amount: '0.00005000', numerator: '50', denominator: '1000000' },
          { kind: 'output', quantity: '1000', raw_amount: '0.01495000', numerator: '14950', denominator: '1000000' }
        ],
        rounding_adjustment: null,
        effective_scope: { workspace_id: '1', project_id: '2', scope_version: '4' }
      },
      { keyScoped: true, fundingSource: 'wallet', budgetBlocked: false }
    )
    expect(priced.quoteState).toBe('authoritative')
    expect(priced.rateKnown).toBe(true)
    expect(priced.maxTotalCost).toBe('0.01500000')
    expect(priced.priceVersion).toBe('pv_2026_11')
    expect(priced.fundingSource).toBe('wallet')
    expect(priced.inputRate?.amount).toBe('2.5')
    expect(priced.outputRate?.amount).toBe('14.95')

    const unpriced = applyAuthoritativeQuoteToEstimate(base, {
      state: 'unpriced',
      price_version: null,
      source: null,
      estimated_charge: null,
      components: [],
      rounding_adjustment: null,
      effective_scope: { workspace_id: '1', project_id: '2', scope_version: '4' }
    })
    expect(unpriced.quoteState).toBe('unpriced')
    expect(unpriced.rateKnown).toBe(false)
    expect(unpriced.maxTotalCost).toBeNull()
  })
})

describe('Playground error normalization (PG-1)', () => {
  it('surfaces explicit non-owned key 404 guidance in English and Chinese', () => {
    const err = {
      status: 404,
      reason: 'NOT_FOUND',
      message: 'Resource not found.'
    }

    const enNotice = normalizePlaygroundError(err, {
      isZh: false,
      isNonOwnedKey: true
    })
    expect(enNotice.kind).toBe('error')
    expect(enNotice.title).toContain('404')
    expect(enNotice.message).toContain('keys.act_as')
    expect(enNotice.requiredGrant).toBe('keys.act_as')
    expect(enNotice.retryable).toBe(false)

    const zhNotice = normalizePlaygroundError(err, {
      isZh: true,
      isNonOwnedKey: true
    })
    expect(zhNotice.title).toContain('404')
    expect(zhNotice.message).toContain('keys.act_as')
    expect(zhNotice.message).toContain('付费人')
  })
})
